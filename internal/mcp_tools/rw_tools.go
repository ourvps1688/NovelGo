package mcp_tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/rag"
	"github.com/novelgo/novelgo/internal/review"
	"github.com/novelgo/novelgo/internal/skill"
	"github.com/novelgo/novelgo/internal/text"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/novelgo/novelgo/internal/writing"
	"github.com/novelgo/novelgo/internal/writingconfig"
)

// ── edit ──────────────────────────────────────────────────

// EditArgs 是 edit 工具的参数。
type EditArgs struct {
	Path       string `json:"path" jsonschema:"required,description=要编辑的文件路径。推荐按阅读序号（第N章）定位以避免找错章：chapters/no_{N}.md（正文第N章）、outlines/no_{N}.md（大纲第N章）；也可按稳定 ID 定位：chapters/id_{章节id}.md（主格式）或 chapters/{卷id}/id_{章节id}.md（等价容错写法）、outlines/id_{章节id}.md 或 outlines/{卷id}/id_{章节id}.md。注意：chapter_id 是数据库主键（稳定），不等于阅读序号；换卷/重排后两者对应关系会变，因此精修「第N章」务必用 no_{N} 而非自行用序号推算 id。新建章节：chapters/{卷id}/new.md 或 chapters/new.md（不带卷时默认分进最后一卷，整本书未建卷则创建未分卷章节）；新建大纲：outlines/{卷id}/new.md 或 outlines/new.md（规则同章节，只能 full_replace）；卷纲：volumes/id_{卷id}.md（卷 ID 请先用 list_volumes 获取，不要猜测序号）；故事状态 novelgo.md；小说级技能 skills/<name>.md；用户级技能 ~/.novelgo/skills/<name>.md" validate:"required"`
	ChangeType string `json:"change_type" jsonschema:"required,enum=full_replace,enum=search_replace,enum=line_range_replace,description=编辑方式。full_replace：全文替换；search_replace：查找并替换指定文本；line_range_replace：替换指定行范围" validate:"required,oneof=full_replace search_replace line_range_replace"`
	SearchText string `json:"search_text" jsonschema:"description=要查找的原文片段（search_replace 时必填）。请从文件中精确复制" validate:"omitempty"`
	NewContent string `json:"new_content" jsonschema:"description=新内容。full_replace 时为完整全文（必填，传空会报错；若要清空整个文件请改用 line_range_replace(1, total_lines, \"\")，total_lines 从 read 返回获取）；search_replace 时为替换后的文本（传空则删除匹配到的文本）；line_range_replace 时为替换该行范围的新内容（传空则删除该范围行）" validate:"omitempty"`
	ReplaceAll bool   `json:"replace_all" jsonschema:"description=是否替换所有匹配项。默认 false（仅替换第一个匹配）" validate:"omitempty"`
	StartLine  int    `json:"start_line" jsonschema:"description=起始行号 1-based 含此行（line_range_replace 时必填），必须 <= end_line" validate:"omitempty,min=1"`
	EndLine    int    `json:"end_line" jsonschema:"description=结束行号 1-based 含此行（line_range_replace 时必填）" validate:"omitempty,min=1"`
	Reason     string `json:"reason" jsonschema:"required,description=必填。本次修改的原因/意图，供作者审批参考" validate:"required"`
	Title      string `json:"title" jsonschema:"description=章节标题。new.md 新建时可选；对已有章节/大纲传入时覆盖原标题（chapters/ 与 outlines/ 路径均生效）" validate:"omitempty"`
}

// EditTool 编辑文件（章节或故事状态），支持全文替换、查找替换、行范围替换。
// 修改在内存中完成后生成 git diff 提交审批，通过后写入文件。
type EditTool struct{}

func (t *EditTool) Name() string           { return "edit" }
func (t *EditTool) Description() string    { return editDescription }
func (t *EditTool) Category() ToolCategory { return CategoryWritingAssistant }

func (t *EditTool) JSONSchema() json.RawMessage { return SchemaOf(EditArgs{}) }
func (t *EditTool) ExposeToLLM() bool           { return true }
func (t *EditTool) NewArgs() any                { return &EditArgs{} }

func (t *EditTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*EditArgs)

	// 内置 skill 只读
	if strings.HasPrefix(a.Path, "/builtin/skills/") {
		return &ToolResult{Success: false, Error: "内置 skill 为只读，不可编辑"}, nil
	}

	if volumeID, ok := git.ParseVolumePath(a.Path); ok {
		return t.editVolumeOutline(ctx, a, tc, volumeID)
	}

	// 章节/大纲路径走章节流程；其余（novelgo.md、技能文件）走通用文件流程
	if ref, ok := git.ParseChapterLikePath(a.Path); ok {
		return t.editChapterLike(ctx, a, tc, ref)
	}
	if !validPath(a.Path) {
		return &ToolResult{Success: false, Error: invalidPathHint}, nil
	}
	return t.editPlainFile(ctx, a, tc)
}

// invalidPathHint 是 edit/read 共用的非法路径提示。
const invalidPathHint = "无效文件路径。按稳定 ID 定位：章节正文 chapters/id_{id}.md、大纲 outlines/id_{id}.md；按实时阅读序号（第N章）定位：chapters/no_{N}.md、outlines/no_{N}.md（推荐，避免用序号推算错误）；卷纲 volumes/id_{卷ID}.md；新建 chapters/{卷ID}/new.md 或 chapters/new.md、outlines/{卷ID}/new.md 或 outlines/new.md；novelgo.md、skills/<name>.md、~/.novelgo/skills/<name>.md"

// readFileForEdit 读取待编辑文件的当前内容。
// 文件不存在时 full_replace 视为从空文件创建，其余模式返回 os.ErrNotExist。
func readFileForEdit(novelID int64, path, changeType string) (string, error) {
	content, err := git.ReadFile(novelID, path)
	if err == nil {
		return content, nil
	}
	if errors.Is(err, os.ErrNotExist) && changeType == "full_replace" {
		return "", nil
	}
	return "", err
}

// fileReadError 将读取文件的已知业务错误转换为 ToolResult。
// handled=false 表示非业务错误，交由上层作为系统错误返回。
func fileReadError(path string, err error) (*ToolResult, bool) {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &ToolResult{Success: false, Error: "文件不存在: " + path}, true
	case errors.Is(err, git.ErrPathEscape):
		return &ToolResult{Success: false, Error: "路径非法: " + path}, true
	default:
		return nil, false
	}
}

// approvalPayload 构造审批事件负载。title 非空时附带展示（标题将被修改）。
func approvalPayload(path, current, proposed string, a *EditArgs) map[string]any {
	payload := map[string]any{
		"original":    current,
		"modified":    proposed,
		"path":        path,
		"change_type": a.ChangeType,
		"reason":      a.Reason,
	}
	if a.Title != "" {
		payload["title"] = a.Title
	}
	return payload
}

// requestApproval 阻塞等待用户审批。
// res 非 nil 表示审批被拒绝或操作被中断，调用方直接返回该结果；
// err 表示审批通道自身的系统错误。
func requestApproval(ctx context.Context, tc ToolContext, payload map[string]any) (feedback string, res *ToolResult, err error) {
	if tc.Approver == nil {
		return "", nil, nil
	}
	if tc.EmitApproval != nil {
		tc.EmitApproval(tc.ToolID, "file_edit", payload)
	}
	ap, err := tc.Approver.RequestApproval(ctx, tc.ToolID, payload)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", &ToolResult{Success: false, Error: "操作被中断"}, nil
		}
		return "", nil, fmt.Errorf("approval: %w", err)
	}
	if !ap.Approved {
		info := "你的修改被用户拒绝"
		if ap.Feedback != "" {
			info += "。用户反馈：" + ap.Feedback
		}
		return "", &ToolResult{
			Success: false,
			Error:   "审批未通过",
			Data: map[string]any{
				"path":        payload["path"],
				"change_type": payload["change_type"],
				"approved":    false,
			},
			Inject: []InjectMessage{{Role: "user", Content: info}},
		}, nil
	}
	return ap.Feedback, nil, nil
}

// writeApproved 写入前的并发冲突检查 + 落盘 + file:changed 事件。
// res 非 nil 表示业务失败（并发冲突/路径非法），err 表示系统错误。
func writeApproved(ctx context.Context, tc ToolContext, path, current, proposed string) (res *ToolResult, err error) {
	if err := git.WriteFileIfUnchanged(tc.NovelID, path, current, proposed); err != nil {
		if errors.Is(err, git.ErrFileChanged) {
			return &ToolResult{Success: false, Error: "文件已被修改，请重新读取最新内容后重试"}, nil
		}
		if errors.Is(err, git.ErrPathEscape) {
			return &ToolResult{Success: false, Error: "路径非法: " + path}, nil
		}
		return nil, fmt.Errorf("write file: %w", err)
	}
	emitFileChanged(ctx, tc.NovelID, path)
	return nil, nil
}

// emitFileChanged 推送 file:changed 事件。wails runtime 在上下文缺失时
// 会 log.Fatalf 终止进程，因此无 wails 运行时上下文（如测试环境）时静默跳过。
func emitFileChanged(ctx context.Context, novelID int64, path string) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	wails.EventsEmit(ctx, "file:changed", map[string]any{
		"novel_id": novelID,
		"path":     path,
	})
}

func emitFileMetadataChanged(ctx context.Context, novelID int64, path string) {
	if ctx == nil || ctx.Value("events") == nil {
		return
	}
	wails.EventsEmit(ctx, "file:changed", map[string]any{
		"novel_id":      novelID,
		"path":          path,
		"metadata_only": true,
	})
}

// physicalRWPath 章节正文的物理扁平路径（isOutline=false 时为章节，true 时为大纲）。
func physicalRWPath(isOutline bool, id int64) string {
	if isOutline {
		return git.OutlinePath(id)
	}
	return git.ChapterPath(id)
}

// resolveChapterRef 将 no_{N} 阅读序号寻址解析为按稳定 ID 寻址的 ref。
// id_ 与 new 形式原样返回；no_{N} 经 chapter.Store 反查为对应的 chapter_id 后，
// 清除 ByReadingNumber/ReadingNumber 标志，使后续物理路径与记录查询逻辑完全统一
// （第N章与 id 的对应关系会随换卷/重排变化，必须实时反查，不能缓存）。
// 返回的业务错误（第N章越界）以 Error 形式回传给 LLM，便于其纠正目标章节。
func resolveChapterRef(ctx context.Context, tc ToolContext, ref git.ChapterPathRef) (git.ChapterPathRef, error) {
	if !ref.ByReadingNumber || ref.IsNew {
		return ref, nil
	}
	id, err := chapter.NewStore(tc.DB, tc.LoggerOrDefault()).GetChapterIDByReadingNumber(ctx, nil, tc.NovelID, ref.ReadingNumber)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return git.ChapterPathRef{}, fmt.Errorf("未找到第%d章（阅读序号越界或本书章节数不足）", ref.ReadingNumber)
		}
		return git.ChapterPathRef{}, fmt.Errorf("resolve chapter by reading number: %w", err)
	}
	return git.ChapterPathRef{ID: id, VolumeID: ref.VolumeID, IsOutline: ref.IsOutline}, nil
}

// ── 大纲写前门禁辅助 ─────────────────────────────────────

// outlineMinContent 是大纲判空的阈值（有效非空白字符数），复用 oh-story 的
// "非空白字符 < 30" 口径：低于此值视为该章尚无实质细纲。
const outlineMinContent = 30

// novelUsesOutlines 判断本书是否已启用大纲工作流：outlines 目录下存在任意
// 大纲文件即视为启用。用于门禁的「是否启用」前置判断，避免对纯即兴写作的
// 本书强制大纲，符合「宁可漏拦」原则。
func novelUsesOutlines(novelID int64) bool {
	dir := filepath.Join(config.NovelDirPath(novelID), "outlines")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
	}
	return false
}

// outlineState 返回本章大纲的状态。missing=true 表示大纲文件不存在；
// empty=true 表示大纲文件存在但有效内容不足 outlineMinContent 字（视为空纲）。
// 二者皆否表示大纲已就绪。
func outlineState(novelID int64, chapterID int64) (missing, empty bool) {
	content, err := git.ReadFile(novelID, git.OutlinePath(chapterID))
	if err != nil {
		return true, false
	}
	if countNonBlank(content) < outlineMinContent {
		return false, true
	}
	return false, false
}

// countNonBlank 统计字符串中非空非空白的 rune 数量（中文按字计，不计入
// 空格/制表/换行/全角空格等）。
func countNonBlank(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

// editPlainFile 编辑通用文件（novelgo.md、技能文件），无章节记录联动。
func (t *EditTool) editPlainFile(ctx context.Context, a *EditArgs, tc ToolContext) (*ToolResult, error) {
	current, err := readFileForEdit(tc.NovelID, a.Path, a.ChangeType)
	if err != nil {
		if res, handled := fileReadError(a.Path, err); handled {
			return res, nil
		}
		return nil, fmt.Errorf("read file %s: %w", a.Path, err)
	}

	proposed, err := applyChange(a, current)
	if err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("编辑操作失败: %s", err.Error())}, nil
	}
	if proposed == current {
		return &ToolResult{Success: true, Data: map[string]any{"path": a.Path, "message": "内容未变化，跳过"}}, nil
	}

	// skill 格式校验（审批前，格式不对直接返回 LLM 修正）
	if isSkillPath(a.Path) {
		if _, err := skill.ParseBytes([]byte(proposed), ""); err != nil {
			return &ToolResult{Success: false, Error: fmt.Sprintf("skill 格式错误: %s", err.Error())}, nil
		}
	}

	feedback, res, err := requestApproval(ctx, tc, approvalPayload(a.Path, current, proposed, a))
	if err != nil {
		return nil, err
	}
	if res != nil {
		return res, nil
	}

	if res, err := writeApproved(ctx, tc, a.Path, current, proposed); res != nil || err != nil {
		return res, err
	}

	var injects []InjectMessage
	if feedback != "" {
		injects = append(injects, InjectMessage{Role: "user", Content: "用户通过了审批并反馈：" + feedback})
	}
	data := map[string]any{
		"path":        a.Path,
		"change_type": a.ChangeType,
		"approved":    true,
	}
	if a.ChangeType == "line_range_replace" {
		afterEnd := a.StartLine + strings.Count(a.NewContent, "\n")
		data["before"] = linePreview(current, a.StartLine, a.EndLine)
		data["after"] = linePreview(proposed, a.StartLine, afterEnd)
	}
	return &ToolResult{Success: true, Data: data, Inject: injects}, nil
}

// editVolumeOutline 编辑已有卷的卷纲文件。文件可首次由 full_replace 创建，
// 但卷记录必须存在且属于当前小说，避免落盘孤儿卷纲。
func (t *EditTool) editVolumeOutline(ctx context.Context, a *EditArgs, tc ToolContext, volumeID int64) (*ToolResult, error) {
	if _, err := volume.NewStore(tc.DB, tc.LoggerOrDefault()).GetByID(ctx, nil, tc.NovelID, volumeID); err != nil {
		if errors.Is(err, volume.ErrNotFound) {
			return &ToolResult{Success: false, Error: err.Error()}, nil
		}
		return nil, fmt.Errorf("query volume: %w", err)
	}
	return t.editPlainFile(ctx, a, tc)
}

// editChapterLike 编辑章节正文或大纲（含 new.md 新建通道）。
//
// 路径与存储的关系：
//   - 物理文件永远按 id 扁平落盘（chapters/id_{id}.md / outlines/id_{id}.md），
//     两级路径 chapters/{vid}/id_{id}.md 仅为容错别名，读写时归一化为扁平路径
//   - 新建走 new.md 通道：先建章节记录拿 id，再写物理文件；写文件失败补偿删记录
func (t *EditTool) editChapterLike(ctx context.Context, a *EditArgs, tc ToolContext, ref git.ChapterPathRef) (*ToolResult, error) {
	// no_{N} 阅读序号寻址 → 解析为稳定 chapter_id（第N章与 id 的映射会随重排变化）
	if resolved, rerr := resolveChapterRef(ctx, tc, ref); rerr != nil {
		return &ToolResult{Success: false, Error: rerr.Error()}, nil
	} else {
		ref = resolved
	}

	physical := physicalRWPath(ref.IsOutline, ref.ID)
	chStore := chapter.NewStore(tc.DB, tc.LoggerOrDefault())

	// 大纲写前门禁（P1-C，蒸馏自 oh-story guard-outline-before-prose）：
	// 仅对「正文」且「本书已启用大纲工作流」生效，不影响人工 UI（走独立 API），也不误伤
	// 纯即兴写作的本书。两条确定性规则：
	//  1) 新建正文通道（chapters/new.md）：本书已有任意大纲时，要求先 outlines/new.md 建纲；
	//  2) 已有章节写正文：若本章大纲文件存在但有效内容 < 30 字，拒绝，提示先补细纲。
	//     大纲文件完全不存在则视为即兴章节，放行（宁可漏拦，避免误伤 body-first 章节/混合本书）。
	if !ref.IsOutline && novelUsesOutlines(tc.NovelID) {
		if ref.IsNew {
			return &ToolResult{Success: false, Error: "本书已启用大纲工作流（已存在大纲）。请先通过 outlines/new.md 创建本章大纲，再写正文；不要改用 chapters/new.md 直接建正文（否则会产生无纲章节）。"}, nil
		}
		if missing, empty := outlineState(tc.NovelID, ref.ID); !missing && empty {
			return &ToolResult{Success: false, Error: fmt.Sprintf("本章（id=%d）大纲为空，请先用 outlines/id_%d.md 补充细纲（至少 30 字有效内容）再写正文。", ref.ID, ref.ID)}, nil
		}
	}

	var (
		ch       *chapter.Chapter
		volumeID *int64
	)
	if ref.IsNew {
		// 新建通道：只能 full_replace；卷可选——带卷校验归属，
		// 不带卷时默认最后一卷；无卷则归入未分卷组。
		if a.ChangeType != "full_replace" {
			return &ToolResult{Success: false, Error: "new.md 仅支持 full_replace（新建文件无原文可查改）"}, nil
		}
		if ref.VolumeID != 0 {
			v, err := volume.NewStore(tc.DB, tc.LoggerOrDefault()).GetByID(ctx, nil, tc.NovelID, ref.VolumeID)
			if err != nil {
				if errors.Is(err, volume.ErrNotFound) {
					return &ToolResult{Success: false, Error: err.Error()}, nil
				}
				return nil, fmt.Errorf("query volume: %w", err)
			}
			volumeID = &v.ID
		}
	} else {
		// 已有章节：记录必须存在，新建走 new.md 通道
		rec, err := getChapterRecord(ctx, tc.DB, tc.NovelID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("query chapter record: %w", err)
		}
		if rec == nil {
			return &ToolResult{Success: false, Error: fmt.Sprintf("章节记录不存在: id=%d，新建请用 chapters/{卷ID}/new.md 或 chapters/new.md", ref.ID)}, nil
		}
		ch = rec
	}

	// 读取当前内容（new 通道视为空文件）
	current := ""
	if !ref.IsNew {
		content, err := git.ReadFile(tc.NovelID, physical)
		if err != nil {
			// 大纲先行等工作流：记录存在但文件尚未落盘，full_replace 视为空文件创建
			if !errors.Is(err, os.ErrNotExist) || a.ChangeType != "full_replace" {
				if res, handled := fileReadError(physical, err); handled {
					return res, nil
				}
				return nil, fmt.Errorf("read file %s: %w", physical, err)
			}
		} else {
			current = content
		}
	}

	proposed, err := applyChange(a, current)
	if err != nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("编辑操作失败: %s", err.Error())}, nil
	}
	if proposed == current {
		// 内容未变：title 有变化时仍执行标题更新，否则直接跳过
		if !ref.IsNew && a.Title != "" && ch.Title != a.Title {
			if err := chStore.UpdateTitle(ctx, nil, tc.NovelID, ch.ID, a.Title); err != nil {
				return nil, fmt.Errorf("update chapter title: %w", err)
			}
			ch.Title = a.Title
			emitFileMetadataChanged(ctx, tc.NovelID, physical)
			return &ToolResult{Success: true, Data: map[string]any{
				"path":    physical,
				"title":   a.Title,
				"message": "内容未变化，仅更新标题",
			}}, nil
		}
		return &ToolResult{Success: true, Data: map[string]any{"path": physical, "message": "内容未变化，跳过"}}, nil
	}

	// 正文（非大纲）去 AI 味硬校验：命中确定性 AI 味硬伤（破折号、模板化身体/声音
	// 描写、命运/开端套话、「他不知道的是」式上帝视角预告等）直接拒绝，提示词约束无效，
	// 此处强制改写。规则集见 internal/text/deslop.go（源自 oh-story 一级禁用词；
	// 多年以后/从这一刻开始/深吸一口气/判断套词等真人常用语已降级为 advisory，不硬拒）。
	// 拒绝消息带命中句上下文与「只改命中句、不要重写整章」引导，避免整章重写
	// 反复命中导致连续失败 10 次中断对话。
	var pendingScore *review.ScoreResult
	if !ref.IsOutline {
		if issues := text.BlockingIssuesForGenre(proposed, novelGenre(ctx, tc)); len(issues) > 0 {
			return &ToolResult{Success: false, Error: text.BlockingErrorMessage(issues)}, nil
		}
		// 评分门禁（仅成章写：new.md 新建或正文 full_replace）：低于阈值且有重试余量时
		// 退回重写；通过或超重试上限则放行并落库分数。失败以 ErrKindQuality 标记，
		// 不计入全局连续失败中断配额。
		if scored, failRes := t.maybeScoreChapter(ctx, tc, ref, a, ch, proposed); failRes != nil {
			return failRes, nil
		} else {
			pendingScore = scored
		}
	}

	// 大纲审批 × 章纲任务卡联动核验（只报告不拦截）：审批载荷带本章任务卡四件套，
	// 作者批准大纲时对照「本章为什么写」的契约核验；本书启用任务卡但本章缺卡时
	// 提示先立卡。整本零卡（纯即兴写作）零打扰。
	var taskCardExtra map[string]any
	taskCardNote := ""
	if ref.IsOutline {
		taskCardExtra, taskCardNote = outlineTaskCardLink(ctx, tc, ref.ID, ref.IsNew)
	}

	// 审批（new 通道展示虚拟路径，已有章节展示物理路径）
	payloadPath := physical
	if ref.IsNew {
		payloadPath = a.Path
	}
	payload := approvalPayload(payloadPath, current, proposed, a)
	for k, v := range taskCardExtra {
		payload[k] = v
	}
	feedback, res, err := requestApproval(ctx, tc, payload)
	if err != nil {
		return nil, err
	}
	if res != nil {
		return res, nil
	}

	// 新建在外层事务中取得 id 并写入首个文件；文件失败时回滚记录与排序调整。
	// 已有章节的标题更新仍延后到正文落盘之后，见下。
	if ref.IsNew {
		// 未指定卷：默认最后一卷；整本书无卷则创建未分卷章节。
		if volumeID == nil {
			last, err := volume.NewStore(tc.DB, tc.LoggerOrDefault()).LastByNovel(ctx, nil, tc.NovelID)
			if err != nil {
				return nil, fmt.Errorf("resolve last volume: %w", err)
			}
			if last != nil {
				volumeID = &last.ID
			}
		}
		var created *chapter.Chapter
		if err := tc.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var err error
			created, err = chStore.Create(ctx, tx, tc.NovelID, volumeID, a.Title)
			if err != nil {
				return fmt.Errorf("create chapter record: %w", err)
			}
			// 物理路径按刚分配的章节 id 重新计算（ref.ID 对新建为 0）。
			physical = physicalRWPath(ref.IsOutline, created.ID)
			if err := git.WriteFile(tc.NovelID, physical, proposed); err != nil {
				return fmt.Errorf("write file: %w", err)
			}
			return nil
		}); err != nil {
			if errors.Is(err, git.ErrPathEscape) {
				return &ToolResult{Success: false, Error: "路径非法: " + physical}, nil
			}
			return nil, err
		}
		ch = created
	} else if err := git.WriteFileIfUnchanged(tc.NovelID, physical, current, proposed); err != nil {
		if errors.Is(err, git.ErrFileChanged) {
			return &ToolResult{Success: false, Error: "文件已被修改，请重新读取最新内容后重试"}, nil
		}
		if errors.Is(err, git.ErrPathEscape) {
			return &ToolResult{Success: false, Error: "路径非法: " + physical}, nil
		}
		return nil, fmt.Errorf("write file: %w", err)
	}

	// 标题更新放在正文写入成功之后：写文件失败时不会留下「标题已改、正文未写」的不一致状态，
	// 用户与 AI 看到的标题不会先于正文生效。也必须早于下面的维护链路——
	// RAG 刷新是异步的，切块时读的是 DB 里的标题。
	if !ref.IsNew && a.Title != "" && ch.Title != a.Title {
		if err := chStore.UpdateTitle(ctx, nil, tc.NovelID, ch.ID, a.Title); err != nil {
			if rollbackErr := git.WriteFileIfUnchanged(tc.NovelID, physical, proposed, current); rollbackErr != nil {
				if errors.Is(rollbackErr, git.ErrFileChanged) {
					return nil, fmt.Errorf("update chapter title（标题未更新，正文已被后续修改，未回退 %s）: %w", physical, err)
				}
				return nil, fmt.Errorf("update chapter title（正文已写入 %s，标题未更新）: %v；回退正文失败: %w", physical, err, rollbackErr)
			}
			return nil, fmt.Errorf("update chapter title（正文已回退 %s）: %w", physical, err)
		}
		ch.Title = a.Title
	}

	// 正文（非大纲）落盘后的维护链路：向量/搜索缓存/字数/写作日志
	if !ref.IsOutline {
		maintainChapterAfterEdit(ctx, tc, ch, current, proposed)
		scheduleStateSync(ctx, tc, ch)
		// 章节 CI（R2）：L0 确定性检查同步落档案 + L1 增量检查去抖派发
		ScheduleChapterCI(ctx, tc, ch, proposed)
		// 落库评分结果（评分门禁判分后在此写入章节记录；nil 表示未评分或评分被跳过）
		if pendingScore != nil {
			persistChapterScore(ctx, tc, ch, pendingScore)
		}
	}
	emitFileChanged(ctx, tc.NovelID, physical)

	data := map[string]any{
		"path":        physical,
		"change_type": a.ChangeType,
		"approved":    true,
	}
	if ref.IsNew {
		activity.NewStore(tc.DB, tc.LoggerOrDefault()).AddActivity(ctx, activity.ActivityDelta{ChaptersCreated: 1})
		data["chapter_id"] = ch.ID
		// 显式给出同源章节的另一条物理路径，避免 LLM 误用 new.md 再次建章：
		// 大纲通道(outlines/new.md)创建的章节，写正文必须用返回的 body_path（chapters/id_X.md）；
		// 正文通道(chapters/new.md)创建的章节，写大纲用 outline_path（outlines/id_X.md）。
		if ref.IsOutline {
			data["body_path"] = git.ChapterPath(ch.ID)
		} else {
			data["outline_path"] = git.OutlinePath(ch.ID)
		}
		if ch.VolumeID != nil {
			data["volume_id"] = *ch.VolumeID
		} else {
			data["volume_id"] = nil
		}
	}

	var injects []InjectMessage
	if feedback != "" {
		injects = append(injects, InjectMessage{Role: "user", Content: "用户通过了审批并反馈：" + feedback})
	}
	// 大纲 × 任务卡联动核验提示（对卡核验 / 先立卡；纯即兴写作为空零打扰）
	if ref.IsOutline && taskCardNote != "" {
		injects = append(injects, InjectMessage{Role: "user", Content: taskCardNote})
	}
	// 写后状态维护已由后端自动派发 statesync 子 Agent 处理（见 scheduleStateSync），
	// 不再要求主 Agent 内联执行维护写工具——否则主 Agent 容易陷入「反复 get_* 确认」的
	// 只读循环，被 isStuckLoop 守卫强制中断（见之前 dev 日志 turn 39/41 卡死）。
	// 这里只注入一条说明，提示主 Agent 无需手动维护，直接基于本次修改向用户汇报即可。
	if !ref.IsOutline && a.ChangeType == "full_replace" && len([]rune(proposed)) > 500 {
		reminder := fmt.Sprintf("你刚刚完成了《%s》的全量替换。", ch.Title)
		if number, err := chStore.GetReadingNumberByID(ctx, nil, tc.NovelID, ch.ID); err != nil {
			tc.LoggerOrDefault().Warn("计算实时章节号失败", "chapter_id", ch.ID, "err", err)
		} else {
			reminder = fmt.Sprintf("你刚刚完成了《%s》（第%d章）的全量替换。", ch.Title, number)
		}
		injects = append(injects, InjectMessage{
			Role:    "user",
			Content: reminder + "系统已在后台自动派发「状态同步子 Agent」负责维护（更新角色/关系/地点/弧线/时间线/读者认知台账）。你无需手动调用任何维护写工具，直接基于本次修改向用户做简要汇报即可。",
		})
	}
	if a.ChangeType == "line_range_replace" {
		afterEnd := a.StartLine + strings.Count(a.NewContent, "\n")
		data["before"] = linePreview(current, a.StartLine, a.EndLine)
		data["after"] = linePreview(proposed, a.StartLine, afterEnd)
	}
	return &ToolResult{Success: true, Data: data, Inject: injects}, nil
}

// linePreview 返回指定行范围的前后上下文预览，带行号。区间 1-based 闭区间。
func linePreview(content string, start, end int) string {
	lines := strings.Split(content, "\n")
	ctxStart := start - 1
	if ctxStart < 0 {
		ctxStart = 0
	}
	ctxEnd := end
	if ctxEnd > len(lines) {
		ctxEnd = len(lines)
	}

	// 前后各多取一行上下文
	preStart := ctxStart - 1
	if preStart < 0 {
		preStart = 0
	}
	postEnd := ctxEnd + 1
	if postEnd > len(lines) {
		postEnd = len(lines)
	}

	var b strings.Builder
	for i := preStart; i < postEnd; i++ {
		if i == ctxStart {
			b.WriteString("─── 改动区间 ───\n")
		}
		fmt.Fprintf(&b, "%d|%s\n", i+1, lines[i])
		if i == ctxEnd-1 {
			b.WriteString("─── 改动结束 ───\n")
		}
	}
	return b.String()
}

// ── 编辑操作 ──────────────────────────────────────────────

func applyChange(a *EditArgs, current string) (string, error) {
	switch a.ChangeType {
	case "full_replace":
		if a.NewContent == "" {
			return "", fmt.Errorf("full_replace 模式需要提供 new_content；若要清空整个文件请用 line_range_replace(1, 总行数, \"\")")
		}
		return a.NewContent, nil

	case "search_replace":
		if a.SearchText == "" {
			return "", fmt.Errorf("search_replace 模式需要提供 search_text")
		}
		result, found, hint := searchReplace(current, a.SearchText, a.NewContent, a.ReplaceAll)
		if !found {
			if hint != "" {
				return "", fmt.Errorf("%s", hint)
			}
			return "", fmt.Errorf("未找到匹配文本，请用精确文本重试")
		}
		return result, nil

	case "line_range_replace":
		if a.StartLine <= 0 || a.EndLine <= 0 {
			return "", fmt.Errorf("line_range_replace 模式需要提供 start_line 和 end_line")
		}
		if a.StartLine > a.EndLine {
			return "", fmt.Errorf("start_line 不能大于 end_line")
		}
		return lineRangeReplace(current, a.StartLine, a.EndLine, a.NewContent)

	default:
		return "", fmt.Errorf("未知的 change_type: %s", a.ChangeType)
	}
}

// searchReplace 在 content 中查找 searchText 并替换为 newContent。
// replaceAll=false 时仅替换第一个匹配。返回修改后的内容、是否找到匹配、以及失败时的模糊匹配提示。
func searchReplace(content, searchText, newContent string, replaceAll bool) (result string, found bool, hint string) {
	searchText = strings.TrimRight(searchText, "\n")

	// 层 1：精确匹配
	if idx := strings.Index(content, searchText); idx >= 0 {
		n := 1
		if replaceAll {
			n = -1
		}
		return strings.Replace(content, searchText, newContent, n), true, ""
	}

	// 层 2：TrimSpace 后精确匹配
	trimmedSearch := strings.TrimSpace(searchText)
	if trimmedSearch != searchText {
		if idx := strings.Index(content, trimmedSearch); idx >= 0 {
			n := 1
			if replaceAll {
				n = -1
			}
			return strings.Replace(content, trimmedSearch, newContent, n), true, ""
		}
	}

	// 层 3：标点归一化后匹配（引号变体统一再比）
	// 只要两边有一方被归一化改变就重新比对
	normSearch := normalizePunctuation(searchText)
	normContent := normalizePunctuation(content)
	if normSearch != searchText || normContent != content {
		// 归一化后字符宽度可能不同（弯引号 3 字节 → ASCII 1 字节），必须按 rune 对齐
		normCRunes := []rune(normContent)
		normSRunes := []rune(normSearch)
		if pos := runeIndex(normCRunes, normSRunes); pos >= 0 {
			origRunes := []rune(content)
			original := string(origRunes[pos : pos+len(normSRunes)])
			n := 1
			if replaceAll {
				n = -1
			}
			return strings.Replace(content, original, newContent, n), true, ""
		}
	}

	// 层 4：模糊匹配反馈（不替换，只告诉 LLM 正确文本长什么样）
	hint = fuzzyHint(searchText, content)
	return "", false, hint
}

// normalizePunctuation 将中文标点变体统一映射为 ASCII 等效字符。
// 仅在查找匹配时使用，不修改文件内容。
var quoteReplace = strings.NewReplacer(
	"“", `"`, "”", `"`, // " " 弯引号
	"「", `"`, "」", `"`, // 「 」 直角引号
	"『", `"`, "』", `"`, // 『 』 双直角引号
	"＂", `"`, // ＂ 全角引号
	"‘", `'`, "’", `'`, // ' ' 弯单引号
	"＇", `'`, // ＇ 全角单引号
)

func normalizePunctuation(s string) string {
	return quoteReplace.Replace(s)
}

// runeIndex 在 rune 切片 a 中查找子切片 b，返回首位置，未找到返回 -1。
func runeIndex(a, b []rune) int {
	for i := 0; i <= len(a)-len(b); i++ {
		match := true
		for j, r := range b {
			if a[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// fuzzyHint 在 content 中找到与 searchText 最相似的段落，返回格式化的提示信息。
// 失败时调用，帮助 LLM 根据实际内容修正 search_text 后重试。
func fuzzyHint(searchText, content string) string {
	searchLines := strings.Split(strings.TrimSpace(searchText), "\n")
	contentLines := strings.Split(content, "\n")
	if len(searchLines) == 0 || len(contentLines) == 0 {
		return ""
	}

	w := len(searchLines)
	bestScore := 0.0
	bestStart := 0
	bestW := w

	// 滑动窗口逐段比较
	for i := 0; i <= len(contentLines)-w; i++ {
		candidate := strings.Join(contentLines[i:i+w], "\n")
		score := partialRatio(searchText, candidate)
		if score > bestScore {
			bestScore = score
			bestStart = i
		}
	}
	// 也尝试窗口 ±2 行
	for _, delta := range []int{2, -2, 1, -1} {
		ws := w + delta
		if ws <= 0 || ws > len(contentLines) {
			continue
		}
		for i := 0; i <= len(contentLines)-ws; i++ {
			candidate := strings.Join(contentLines[i:i+ws], "\n")
			score := partialRatio(searchText, candidate)
			if score > bestScore {
				bestScore = score
				bestStart = i
				bestW = ws
			}
		}
	}

	if bestScore < 0.4 {
		return "未找到任何相似内容，请用精确文本或 line_range_replace 重试。"
	}

	contextStart := bestStart
	if contextStart > 2 {
		contextStart -= 2
	}
	contextEnd := bestStart + bestW
	if contextEnd+2 < len(contentLines) {
		contextEnd += 2
	} else {
		contextEnd = len(contentLines)
	}
	// 取匹配行 + 前后各 2 行上下文
	contextLines := contentLines[contextStart:contextEnd]
	if len(contextLines) > 8 {
		contextLines = contextLines[:8]
	}
	nearby := strings.Join(contextLines, "\n")

	return fmt.Sprintf(
		"未找到精确匹配。以下为模糊匹配到的最相似片段（相似度 %.0f%%，第 %d-%d 行附近），仅供参考——请自行判断是否就是你想要修改的位置：\n%s\n如果确认就是此处，可直接用 line_range_replace(start_line=%d, end_line=%d) 修改，或根据实际内容修正 search_text 后重新调用 search_replace。",
		bestScore*100, bestStart+1, bestStart+bestW, nearby, bestStart+1, bestStart+bestW,
	)
}

// partialRatio 计算两段文本的字符级相似度（0.0~1.0）。
// 使用滑动窗口在较长的文本中找与较短文本最匹配的片段。
func partialRatio(a, b string) float64 {
	short, long := a, b
	if len(a) > len(b) {
		short, long = b, a
	}
	if len(short) == 0 {
		if len(long) == 0 {
			return 1.0
		}
		return 0
	}
	shortRunes := []rune(short)
	longRunes := []rune(long)
	if len(longRunes) < len(shortRunes) {
		longRunes, shortRunes = shortRunes, longRunes
	}

	best := 0.0
	for i := 0; i <= len(longRunes)-len(shortRunes); i++ {
		matches := 0
		for j, sr := range shortRunes {
			if sr == longRunes[i+j] {
				matches++
			}
		}
		score := float64(matches) / float64(len(shortRunes))
		if score > best {
			best = score
		}
	}
	return best
}

// lineRangeReplace 替换 [startLine, endLine] 区间（1-based，含两端）。
func lineRangeReplace(content string, startLine, endLine int, newContent string) (string, error) {
	lines := strings.Split(content, "\n")
	if startLine < 1 || endLine > len(lines) || startLine > endLine {
		return "", fmt.Errorf("行号超出范围: start=%d end=%d 总行数=%d", startLine, endLine, len(lines))
	}

	var result []string
	result = append(result, lines[:startLine-1]...)
	if newContent != "" {
		result = append(result, strings.Split(newContent, "\n")...)
	}
	result = append(result, lines[endLine:]...)
	return strings.Join(result, "\n"), nil
}

// ── 路径解析 ──────────────────────────────────────────────

var plainPathRe = regexp.MustCompile(`^(novelgo\.md|skills/[^/]+\.md|~/.novelgo/skills/[^/]+\.md)$`)

// validPath 校验 edit/read 支持的全部路径形态。
func validPath(p string) bool {
	if _, ok := git.ParseChapterLikePath(p); ok {
		return true
	}
	if _, ok := git.ParseVolumePath(p); ok {
		return true
	}
	return plainPathRe.MatchString(p)
}

func isSkillPath(p string) bool {
	return strings.HasPrefix(p, "skills/") || strings.HasPrefix(p, "~/.novelgo/skills/")
}

// ── 章节记录 ──────────────────────────────────────────────

// novelGenre 取当前小说的题材，用于去 AI 味门禁的题材感知豁免（fate-cliche / 指节泛白
// 在网文题材下为真人常用，放行；未选题材则保守硬拒）。取不到（DB 空或查询失败）时返回
// 空串，门禁退化为无题材上下文的保底行为。表名用 gorm 默认的 novels，避免引入 novel 包依赖。
func novelGenre(ctx context.Context, tc ToolContext) string {
	if tc.DB == nil {
		return ""
	}
	var g string
	if err := tc.DB.WithContext(ctx).Table("novels").Where("id = ?", tc.NovelID).Limit(1).Pluck("genre", &g).Error; err != nil {
		return ""
	}
	return g
}

// getChapterRecord 按章节 id 取当前小说的章节记录，不存在返回 (nil, nil)。
func getChapterRecord(ctx context.Context, db *gorm.DB, novelID, id int64) (*chapter.Chapter, error) {
	var ch chapter.Chapter
	err := db.WithContext(ctx).Where("id = ? AND novel_id = ?", id, novelID).First(&ch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// maintainChapterAfterEdit 章节正文落盘后的维护链路：向量刷新、搜索缓存、字数统计。
// 向量与搜索按 chapter_id 键控；写作日志将在其所属领域迁移时切换。
// ── 状态同步（写章后自动回填台账）─────────────────────────
// 章节正文落盘后，后端自动（而非依赖 LLM 自觉）派发 state-sync 子 Agent，
// 把本章涉及的角色/地点/弧线/时间线/读者认知增量同步到结构化台账。
// 用 debounce 合并同一章节连续多次编辑，避免每次 edit 都起一个子 Agent。

var (
	stateSyncMu     sync.Mutex
	stateSyncTimers = map[string]*time.Timer{}
)

const stateSyncDebounce = 6 * time.Second

func scheduleStateSync(ctx context.Context, tc ToolContext, ch *chapter.Chapter) {
	if tc.RunSubAgent == nil || ch == nil {
		return
	}
	key := fmt.Sprintf("%d:%d", tc.NovelID, ch.ID)
	stateSyncMu.Lock()
	if t, ok := stateSyncTimers[key]; ok {
		t.Stop()
	}
	stateSyncTimers[key] = time.AfterFunc(stateSyncDebounce, func() {
		stateSyncMu.Lock()
		delete(stateSyncTimers, key)
		stateSyncMu.Unlock()
		runStateSync(context.WithoutCancel(ctx), tc, tc.NovelID, ch.ID)
	})
	stateSyncMu.Unlock()
}

// RunStateSyncForChapter 对指定章节执行后台状态同步（statesync 子 Agent）。
// runSub 为实际派发子 Agent 的函数（调用方决定用 ToolContext.RunSubAgent 还是 agent.RunSubAgent）；
// logger 用于失败日志；toolID 标识调用来源（用于 statesync 进度事件）。返回派发错误（非阻断调用方可忽略）。
func RunStateSyncForChapter(ctx context.Context, runSub func(ctx context.Context, req SubAgentRequest) (string, error), logger *slog.Logger, toolID string, novelID, chapterID int64) error {
	wails.EventsEmit(ctx, "statesync:progress", map[string]any{
		"novel_id":   novelID,
		"chapter_id": chapterID,
		"status":     "started",
	})
	instruction := fmt.Sprintf(
		"刚刚完成了章节（chapter_id=%d）的修改并已落盘。请读取该章正文（read 路径 chapters/id_%d.md），"+
			"将其涉及的角色、地点、弧线节点、时间线（伏笔/章节计划）、读者认知变化，对照现有台账做**增量**同步："+
			"新增首次出场者、更新已变化者、推进/回收伏笔与弧线节点、记录读者已知/误知变化。"+
			"只调用台账类工具（角色/关系/地点/弧线/时间线/读者视角 的 create_*/update_*），"+
			"严禁编辑章节文件或 novelgo.md，严禁再派发子 Agent。完成后输出一份简短同步摘要（改了哪些台账）。",
		chapterID, chapterID,
	)
	if _, err := runSub(ctx, SubAgentRequest{
		AgentType:   "statesync",
		NovelID:     novelID,
		Instruction: instruction,
		ToolID:      toolID,
	}); err != nil {
		if logger != nil {
			logger.Warn("state-sync 子 Agent 派发失败（非阻断）", "err", err, "novelID", novelID, "chapterID", chapterID)
		}
		wails.EventsEmit(ctx, "statesync:progress", map[string]any{
			"novel_id":   novelID,
			"chapter_id": chapterID,
			"status":     "done",
			"error":      err.Error(),
		})
		return err
	}
	wails.EventsEmit(ctx, "statesync:progress", map[string]any{
		"novel_id":   novelID,
		"chapter_id": chapterID,
		"status":     "done",
	})
	return nil
}

// runStateSync 写后由后端自动派发（见 scheduleStateSync），委托 RunStateSyncForChapter 完成实际工作。
func runStateSync(ctx context.Context, tc ToolContext, novelID, chapterID int64) {
	if tc.RunSubAgent == nil {
		return
	}
	_ = RunStateSyncForChapter(ctx, tc.RunSubAgent, tc.LoggerOrDefault(), tc.ToolID, novelID, chapterID)
}

func maintainChapterAfterEdit(ctx context.Context, tc ToolContext, ch *chapter.Chapter, previous, content string) {
	rag.SubmitRefresh(tc.NovelID, ch.ID, content)
	if tc.SearchService != nil {
		tc.SearchService.UpdateCachedChapter(tc.NovelID, ch.ID, content)
	}

	stats := text.ComputeStats(content)

	changes := text.ComputeWordChanges(previous, content)
	writing.NewStore(tc.DB, tc.LoggerOrDefault()).LogChanges(ctx, tc.NovelID, ch.ID, changes.Added, changes.Deleted)

	tc.DB.WithContext(ctx).Model(&chapter.Chapter{}).
		Where("id = ?", ch.ID).
		Update("word_count", stats.WordCount)
}

// ── 评分门禁 ──────────────────────────────────────────

// maybeScoreChapter 评分门禁：仅对"成章写"（new.md 新建或正文 full_replace）生效。
// 返回 (scored, failResult)：
//   - failResult 非 nil：评分低于阈值且有重试余量，调用方应直接返回它（Success:false、
//     ErrKindQuality），让 Agent 在既有对话循环里自动重写，不进入审批/落盘。
//   - 其余情况 proceed：scored 非 nil 时由调用方在章节记录可知后落库（通过或超重试放行）。
//
// 设计要点：复用 deslop 的「reject→重写」回路，但有独立的有界重试计数
// （review.QualityAttempt，按章节/标题 key），不会吃掉全局连续失败中断配额
// （ErrKindQuality 在 agent/interrupt.go 被排除）。
func (t *EditTool) maybeScoreChapter(ctx context.Context, tc ToolContext, ref git.ChapterPathRef, a *EditArgs, ch *chapter.Chapter, proposed string) (*review.ScoreResult, *ToolResult) {
	if tc.LLM == nil || tc.ProviderName == "" || tc.ModelID == "" {
		return nil, nil // 未注入 LLM，跳过评分
	}
	cfg, err := writingconfig.NewStore(tc.DB, tc.LoggerOrDefault()).Get(ctx, tc.NovelID)
	if err != nil {
		tc.LoggerOrDefault().Warn("读取写作配置失败，跳过评分", "err", err)
		return nil, nil
	}
	if !cfg.AutoScoreEnabled {
		return nil, nil
	}
	// 仅成章写触发：新建正文通道，或已有正文 full_replace。增量编辑（search_replace/
	// line_range_replace）与一切大纲编辑不评分。
	if !ref.IsNew && (ch == nil || a.ChangeType != "full_replace") {
		return nil, nil
	}

	// 重试计数 key：已有章节稳定用 id；新建章节无 id，用标题（重写通常保留标题）。
	key := "new:" + strings.TrimSpace(a.Title)
	if ch != nil {
		key = fmt.Sprintf("chapter:%d", ch.ID)
	}

	// 读取本章大纲（仅已有章节可取），供判官对照字数预期与目标
	outline := ""
	if ch != nil {
		if ol, rerr := git.ReadFile(tc.NovelID, git.OutlinePath(ch.ID)); rerr == nil {
			outline = ol
		}
	}
	stats := text.ComputeStats(proposed)
	score, serr := review.ScoreChapter(ctx, tc.LLM, tc.ProviderName, tc.ModelID, proposed, outline, stats.WordCount)
	if serr != nil {
		tc.LoggerOrDefault().Warn("章节评分失败，放行不阻断", "err", serr)
		return nil, nil
	}

	attempts := review.NewAttemptStore(tc.DB, tc.LoggerOrDefault())
	if score.Overall >= cfg.ScoreThreshold {
		// 通过：清理可能存在的脏重试计数
		if cerr := attempts.Clear(ctx, tc.NovelID, key); cerr != nil {
			tc.LoggerOrDefault().Warn("清理评分重试计数失败", "err", cerr)
		}
		return &score, nil
	}

	// 不达标：递增重试计数，决定是否退回重写或放行
	retries, ierr := attempts.Increment(ctx, tc.NovelID, key, score.Overall, score.Critique)
	if ierr != nil {
		tc.LoggerOrDefault().Warn("评分重试计数失败", "err", ierr)
	}
	if retries <= cfg.MaxRetries {
		msg := fmt.Sprintf(
			"本章评分 %d 分（阈值 %d），未达标，系统已退回重写。请参考以下意见修改后重提——只改有问题的部分，不要整体推翻或重写无关段落：\n%s",
			score.Overall, cfg.ScoreThreshold, score.Critique)
		return &score, &ToolResult{Success: false, Error: msg, ErrKind: ErrKindQuality}
	}
	// 超出重试上限：放行并保留低分，让用户在审批时看到评分与警告
	if cerr := attempts.Clear(ctx, tc.NovelID, key); cerr != nil {
		tc.LoggerOrDefault().Warn("清理评分重试计数失败", "err", cerr)
	}
	tc.LoggerOrDefault().Warn("章节评分多次不达标，放行", "score", score.Overall, "retries", retries)
	return &score, nil
}

// persistChapterScore 把评分结果写入章节记录（QualityScore / QualityReport / QualityScoredAt）。
func persistChapterScore(ctx context.Context, tc ToolContext, ch *chapter.Chapter, scored *review.ScoreResult) {
	if ch == nil || scored == nil {
		return
	}
	now := time.Now()
	if err := tc.DB.WithContext(ctx).Model(&chapter.Chapter{}).
		Where("id = ?", ch.ID).
		Updates(map[string]any{
			"quality_score":     scored.Overall,
			"quality_report":    scored.Critique,
			"quality_scored_at": now,
		}).Error; err != nil {
		tc.LoggerOrDefault().Warn("落库章节评分失败", "chapter_id", ch.ID, "err", err)
	}
}

// ── 工具描述 ──────────────────────────────────────────────

const editDescription = `编辑小说文件（章节正文、大纲、卷纲、故事状态 novelgo.md 或技能文件）。支持三种编辑模式：full_replace（全文替换）、search_replace（查找替换）、line_range_replace（行范围替换）。

定位章节——务必用阅读序号或稳定 ID，不要自行推算 chapter_id：
- 精修/修改「第N章」请直接用 no_{N} 路径：正文 chapters/no_{N}.md、大纲 outlines/no_{N}.md。系统会按实时阅读顺序确定性解析到第N章对应的稳定 chapter_id，换卷/重排后依然准确。
- 也可用稳定 ID 路径 chapters/id_{id}.md / outlines/id_{id}.md，但 id 是数据库主键、不等于阅读序号，严禁把 get_chapter_list 里的「第N章」直接当成 id 去猜。
- 错误示例：用户说「精修第1章」却用 chapters/id_1.md——id_1 往往不是阅读顺序里的第1章。正确做法是 chapters/no_1.md。

大纲写前门禁（系统硬规则）：若本书已存在任意大纲，则正文必须走「先 outlines/new.md 建纲 → 再写 chapters/id_{chapter_id}.md 正文」流程；严禁用 chapters/new.md 直接建正文（会被拒绝）。已有章节若大纲文件存在但实质为空（不足 30 字有效内容），写正文也会被拒绝，需先补细纲。无大纲的本书按即兴写作放行。

新建章节或大纲：path 使用 new.md 形式（格式见 path 参数说明），且只能 full_replace；成功后返回值携带分配的 chapter_id、volume_id 与物理路径，后续编辑一律使用返回的物理路径。
重要——大纲与正文属于同一章节（共用 chapter_id）：通过 outlines/new.md 创建大纲后，该章节的正文必须写入返回值 body_path 指定的 chapters/id_{chapter_id}.md，严禁再用 chapters/new.md 新建（否则会产生两个重复章节）；反之通过 chapters/new.md 创建正文后，其大纲写入返回值 outline_path 指定的 outlines/id_{chapter_id}.md。只有当目标章节确实不存在时才使用 new.md。
卷纲：path 使用 volumes/id_{卷ID}.md；卷必须存在，full_replace 可首次创建对应卷纲文件。

各模式必填参数：
- full_replace：new_content
- search_replace：search_text + new_content
- line_range_replace：start_line + end_line + new_content
所有模式都必填：path、change_type、reason
（new_content 传空的具体行为见 new_content 参数说明）

模式选择策略：
- search_replace 连续两次因"未找到匹配"失败，直接换 line_range_replace，不要在同一种模式上反复重试
- line_range_replace 使用前务必重新 read 确认行号（行号可能因前序操作偏移）；执行后会返回 before/after 上下文，若 before 与预期不符说明行号偏移，需重新 read 修正后再试

所有修改会先生成 git diff 提交用户审批，审批通过后才写入文件。被拒绝时返回用户反馈，可根据反馈修正后重试。`

// ── read ──────────────────────────────────────────────────

// ReadArgs 是 read 工具的参数。
type ReadArgs struct {
	Path         string `json:"path" jsonschema:"required,description=要读取的文件路径。推荐按阅读序号（第N章）定位以避免找错章：chapters/no_{N}.md（正文第N章）、outlines/no_{N}.md（大纲第N章）；也可按稳定 ID 定位：chapters/id_{章节id}.md（主格式）或 chapters/{卷id}/id_{章节id}.md（等价容错写法）、outlines/id_{章节id}.md 或 outlines/{卷id}/id_{章节id}.md。注意：chapter_id 是数据库主键（稳定），不等于阅读序号；换卷/重排后两者对应关系会变，因此「读取第N章」务必用 no_{N} 而非自行用序号推算 id。卷纲：volumes/id_{卷id}.md（卷 ID 请先用 list_volumes 获取，不要猜测序号）；故事状态 novelgo.md；小说级技能 skills/<name>.md；用户级技能 ~/.novelgo/skills/<name>.md；内置技能 /builtin/skills/<name>.md（只读）" validate:"required"`
	IncludeLines *bool  `json:"include_lines" jsonschema:"default=true,description=是否包含行号前缀（如 123|）。默认 true，用于精确引用和行范围编辑。传 false 获取纯文本"`
	StartLine    int    `json:"start_line" jsonschema:"default=1,description=起始行号 1-based 含此行，必须 <= end_line" validate:"omitempty,min=1"`
	EndLine      int    `json:"end_line" jsonschema:"default=2000,description=结束行号 1-based 含此行，超出自动截到文末。不传或传 0 均按默认 2000 处理" validate:"omitempty,min=0"`
}

// ReadTool 读取文件内容（章节正文或故事状态 novelgo.md）。
// 默认含行号前缀（123|），LLM 传 include_lines=false 获取纯文本。
// start_line/end_line 支持行范围读取，用于翻页和精确引用。
type ReadTool struct{}

func (t *ReadTool) Name() string           { return "read" }
func (t *ReadTool) Description() string    { return readDescription }
func (t *ReadTool) Category() ToolCategory { return CategoryNovelManagement }

func (t *ReadTool) JSONSchema() json.RawMessage { return SchemaOf(ReadArgs{}) }
func (t *ReadTool) ExposeToLLM() bool           { return true }
func (t *ReadTool) NewArgs() any                { return &ReadArgs{} }

func (t *ReadTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*ReadArgs)

	// builtin skill 走 store 内存
	if strings.HasPrefix(a.Path, "/builtin/skills/") {
		return t.readBuiltinSkill(a, tc)
	}

	if volumeID, ok := git.ParseVolumePath(a.Path); ok {
		return t.readVolumeOutline(ctx, a, tc, volumeID)
	}

	// 章节/大纲按 id 扁平路径读取（两级路径归一化）
	if ref, ok := git.ParseChapterLikePath(a.Path); ok {
		return t.readChapterLike(ctx, a, tc, ref)
	}
	if !validPath(a.Path) {
		return &ToolResult{Success: false, Error: invalidPathHint}, nil
	}

	content, err := git.ReadFile(tc.NovelID, a.Path)
	if err != nil {
		if res, handled := fileReadError(a.Path, err); handled {
			return res, nil
		}
		return nil, fmt.Errorf("read file %s: %w", a.Path, err)
	}
	return renderReadResult(a, a.Path, a.Path, content)
}

// readChapterLike 读取章节正文或大纲。
// 物理文件永远按 id 扁平路径读，两级路径仅作容错别名归一化。
func (t *ReadTool) readChapterLike(ctx context.Context, a *ReadArgs, tc ToolContext, ref git.ChapterPathRef) (*ToolResult, error) {
	if ref.IsNew {
		return &ToolResult{Success: false, Error: "new.md 仅用于 edit 新建章节/大纲，不可读取"}, nil
	}

	// no_{N} 阅读序号寻址 → 解析为稳定 chapter_id（第N章与 id 的映射会随重排变化）
	if resolved, rerr := resolveChapterRef(ctx, tc, ref); rerr != nil {
		return &ToolResult{Success: false, Error: rerr.Error()}, nil
	} else {
		ref = resolved
	}

	physical := git.ChapterPath(ref.ID)
	suffix := ""
	if ref.IsOutline {
		physical = git.OutlinePath(ref.ID)
		suffix = "（大纲）"
	}

	content, err := git.ReadFile(tc.NovelID, physical)
	if err != nil {
		if res, handled := fileReadError(physical, err); handled {
			return res, nil
		}
		return nil, fmt.Errorf("read file %s: %w", physical, err)
	}

	display := physical
	ch, err := getChapterRecord(ctx, tc.DB, tc.NovelID, ref.ID)
	if err != nil {
		return nil, fmt.Errorf("query chapter record: %w", err)
	}
	if ch != nil {
		display = ch.Title + suffix
	}

	return renderReadResult(a, physical, display, content)
}

// readVolumeOutline 读取已有卷的卷纲文件，并以卷名作为展示标题。
func (t *ReadTool) readVolumeOutline(ctx context.Context, a *ReadArgs, tc ToolContext, volumeID int64) (*ToolResult, error) {
	v, err := volume.NewStore(tc.DB, tc.LoggerOrDefault()).GetByID(ctx, nil, tc.NovelID, volumeID)
	if err != nil {
		if errors.Is(err, volume.ErrNotFound) {
			return &ToolResult{Success: false, Error: err.Error()}, nil
		}
		return nil, fmt.Errorf("query volume: %w", err)
	}

	path := git.VolumePath(volumeID)
	content, err := git.ReadFile(tc.NovelID, path)
	if err != nil {
		if res, handled := fileReadError(path, err); handled {
			return res, nil
		}
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}
	return renderReadResult(a, path, v.Name+"（卷纲）", content)
}

// renderReadResult 按行窗口切片并渲染读取结果（行号前缀、截断标记）。
func renderReadResult(a *ReadArgs, path, display, content string) (*ToolResult, error) {
	start := a.StartLine
	if start == 0 {
		start = 1
	}
	end := a.EndLine
	if end == 0 {
		end = 2000
	}

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	var note string
	switch {
	case start > end:
		// 倒序窗口归一（#233 续，现场实证：start_line=200 配 end_line=80）：
		// 倒序几乎都源于陈旧行号混搭（旧版偏移配上新版总行数），而非调用方
		// 真要倒着读。硬报错只把坏窗口抛回；改为两端各自收敛到 [1,total]，
		// 仍倒序则交换，显式 note 标注原始值与归一结果，调用方据实际行号
		// 前缀一次调用内即可重锚。编辑类工具（line_range_replace）的倒序
		// 仍硬错——写操作需精确意图，此处只放宽只读的 read。
		origStart, origEnd := start, end
		if start > totalLines {
			start = totalLines
		}
		if end > totalLines {
			end = totalLines
		}
		if start > end {
			start, end = end, start
		}
		note = fmt.Sprintf("start_line %d > end_line %d（行号可能已过期或倒序），已归一为 %d-%d 行",
			origStart, origEnd, start, end)
	case start > totalLines:
		// 行号越界收敛（#233）：LLM 可能携带陈旧行号（文件被改短后仍传旧偏移，如 250 行
		// 的章节传 start_line=1880）。硬报错只把问题抛回让调用方猜正确行号；改为把请求
		// 窗口平移到文件尾部（保持窗口大小）并显式 note 标注收敛——调用方从 total_lines
		// 与实际行号前缀即可感知文件已变短并自行重锚，一次调用即可恢复。
		// end 超界沿既有静默截断（默认 end=2000 大于多数章节，属常态不逐次提示）。
		win := end - start + 1
		if win > totalLines {
			win = totalLines
		}
		origStart := start
		start = totalLines - win + 1
		end = totalLines
		note = fmt.Sprintf("start_line %d 超出文件总行数 %d（行号可能已过期），已收敛到文件尾部 %d-%d 行",
			origStart, totalLines, start, end)
	case end > totalLines:
		end = totalLines
	}

	selected := lines[start-1 : end]

	includeLines := a.IncludeLines == nil || *a.IncludeLines

	var output string
	if includeLines {
		var sb strings.Builder
		for i, line := range selected {
			fmt.Fprintf(&sb, "%d|%s\n", start+i, line)
		}
		output = strings.TrimRight(sb.String(), "\n")
	} else {
		output = strings.Join(selected, "\n")
	}

	data := map[string]any{
		"path":        path,
		"display":     display,
		"content":     output,
		"total_lines": totalLines,
		"start_line":  start,
		"end_line":    end,
	}
	if end < totalLines {
		data["truncated"] = true
	}
	if note != "" {
		data["note"] = note
	}

	return &ToolResult{Success: true, Data: data}, nil
}

// readBuiltinSkill 从 store 内存读取内置 skill，全量返回。
func (t *ReadTool) readBuiltinSkill(a *ReadArgs, tc ToolContext) (*ToolResult, error) {
	if tc.SkillStore == nil {
		return &ToolResult{Success: false, Error: "skill store 未初始化"}, nil
	}

	name := strings.TrimSuffix(strings.TrimPrefix(a.Path, "/builtin/skills/"), ".md")
	if name == "" {
		return &ToolResult{Success: false, Error: "无效的 skill 路径: " + a.Path}, nil
	}

	sk, ok := tc.SkillStore.Get(tc.NovelID, name)
	if !ok {
		return &ToolResult{Success: false, Error: fmt.Sprintf("内置 skill %q 不存在", name)}, nil
	}

	return &ToolResult{Success: true, Data: map[string]any{
		"path":    a.Path,
		"display": fmt.Sprintf("技能: %s", sk.Name),
		"content": sk.RawContent,
	}}, nil
}

// ── 工具描述 ──────────────────────────────────────────────

const readDescription = `读取小说文件、卷纲或技能文件。

定位章节——务必用阅读序号或稳定 ID，不要自行推算 chapter_id：
- 读取「第N章」请直接用 no_{N} 路径：正文 chapters/no_{N}.md、大纲 outlines/no_{N}.md。系统会按实时阅读顺序确定性解析到第N章对应的稳定 chapter_id，换卷/重排后依然准确。
- 也可用稳定 ID 路径 chapters/id_{id}.md / outlines/id_{id}.md，但 id 是数据库主键、不等于阅读序号，严禁把 get_chapter_list 里的「第N章」直接当成 id 去猜。

特性：
- 默认添加行号前缀，方便后续 edit 工具进行 line_range_replace 和 search_replace
- 返回 total_lines 表示全文行数，用于判断是否被截断；可用 line_range_replace(1, total_lines, "") 清空整个文件
- start_line 和 end_line 支持行范围读取，用于翻页或精确引用`

// ── 注册 ──────────────────────────────────────────────────

// RegisterRWTools 注册读写工具。
func RegisterRWTools(r *Registry) {
	r.Register(&ReadTool{})
	r.Register(&EditTool{})
}
