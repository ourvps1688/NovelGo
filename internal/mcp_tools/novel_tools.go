package mcp_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/storage"
	"github.com/novelgo/novelgo/internal/volume"
)

// ── get_chapter_list ─────────────────────────────────

// GetChapterListArgs 是 get_chapter_list 的参数。
type GetChapterListArgs struct {
	PageArgs // 嵌入分页参数
}

// GetChapterListTool 获取章节目录，按阅读顺序降序。
type GetChapterListTool struct{}

func (t *GetChapterListTool) Name() string { return "get_chapter_list" }
func (t *GetChapterListTool) Description() string {
	return "获取小说的章节目录，支持分页。按阅读顺序降序排列（最新的在前），按卷分组展示每章的稳定 ID、实时阅读序号、标题和字数。"
}
func (t *GetChapterListTool) Category() ToolCategory { return CategoryNovelManagement }

func (t *GetChapterListTool) JSONSchema() json.RawMessage {
	return SchemaOf(GetChapterListArgs{})
}

func (t *GetChapterListTool) ExposeToLLM() bool { return true }
func (t *GetChapterListTool) NewArgs() any      { return &GetChapterListArgs{} }

func (t *GetChapterListTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*GetChapterListArgs)
	a.NormalizePage()

	chStore := chapter.NewStore(tc.DB, tc.LoggerOrDefault())
	result, err := chStore.ListByNovel(ctx, nil, tc.NovelID, chapter.ListByNovelOptions{
		PageParams: storage.PageParams{Page: a.Page, Size: a.Size},
		Order:      "desc",
	})
	if err != nil {
		return nil, fmt.Errorf("list chapters: %w", err)
	}

	volumes, err := volume.NewStore(tc.DB, tc.LoggerOrDefault()).ListByNovel(ctx, nil, tc.NovelID)
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}
	volumeNames := make(map[int64]string, len(volumes))
	for _, v := range volumes {
		volumeNames[v.ID] = v.Name
	}

	data := PageMeta(result)
	data["content"] = formatChapterList(result.Items, volumeNames)

	return &ToolResult{
		Success: true,
		Data:    data,
	}, nil
}

func formatChapterList(chapters []chapter.Chapter, volumeNames map[int64]string) string {
	if len(chapters) == 0 {
		return "暂无章节。"
	}

	var sb strings.Builder
	var previousVolumeID *int64
	for i, ch := range chapters {
		if i == 0 || !sameVolumeID(previousVolumeID, ch.VolumeID) {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			if ch.VolumeID == nil {
				sb.WriteString("## 未分卷\n")
			} else {
				name, ok := volumeNames[*ch.VolumeID]
				if !ok {
					name = "未知分卷"
				}
				fmt.Fprintf(&sb, "## %s [volume_id:%d]\n", name, *ch.VolumeID)
			}
			previousVolumeID = ch.VolumeID
		}
		fmt.Fprintf(&sb, "- 第%d章《%s》 [chapter_id:%d] · %d 字\n", ch.ReadingNumber, ch.Title, ch.ID, ch.WordCount)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func sameVolumeID(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// ── list_volumes ───────────────────────────────────────

// ListVolumesArgs 是 list_volumes 的参数。
type ListVolumesArgs struct{}

// ListVolumesTool 获取小说全部卷列表（含 0 章空卷），用于在读写卷纲前确认真实卷 ID。
type ListVolumesTool struct{}

func (t *ListVolumesTool) Name() string { return "list_volumes" }
func (t *ListVolumesTool) Description() string {
	return "获取小说的全部卷列表，含卷 ID、卷名与卷纲文件路径。读写卷纲（volumes/id_{卷id}.md）前应先调用本工具确认卷 ID，避免用猜测的序号访问不存在的卷。"
}
func (t *ListVolumesTool) Category() ToolCategory { return CategoryNovelManagement }

func (t *ListVolumesTool) JSONSchema() json.RawMessage {
	return SchemaOf(ListVolumesArgs{})
}

func (t *ListVolumesTool) ExposeToLLM() bool { return true }
func (t *ListVolumesTool) NewArgs() any      { return &ListVolumesArgs{} }

func (t *ListVolumesTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	volumes, err := volume.NewStore(tc.DB, tc.LoggerOrDefault()).ListByNovel(ctx, nil, tc.NovelID)
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}

	return &ToolResult{
		Success: true,
		Data: map[string]any{
			"count":   len(volumes),
			"content": formatVolumeList(volumes),
		},
	}, nil
}

func formatVolumeList(volumes []volume.Volume) string {
	if len(volumes) == 0 {
		return "暂无卷。"
	}
	var sb strings.Builder
	for _, v := range volumes {
		fmt.Fprintf(&sb, "- [id:%d] %s · 卷纲：%s\n", v.ID, v.Name, git.VolumePath(v.ID))
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// ── 注册 ──────────────────────────────────────────────

// RegisterNovelTools 注册小说管理类工具。
func RegisterNovelTools(r *Registry) {
	r.Register(&GetChapterListTool{})
	r.Register(&ListVolumesTool{})
}
