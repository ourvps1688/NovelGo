package mcp_tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/volume"
)

// ── move_chapter ────────────────────────────────────

// MoveChapterArgs 是 move_chapter 的参数。
type MoveChapterArgs struct {
	ChapterID       int64  `json:"chapter_id" jsonschema:"required,description=要移动的章节稳定 ID（get_chapter_list 返回的 [chapter_id]；不是「第N章」序号，不要推算）"`
	TargetVolumeID  *int64 `json:"target_volume_id" jsonschema:"description=目标卷 ID（list_volumes 的 [id:N]）；传 0 表示移入未分卷。与 before_chapter_id 至少传一个；同时传时 before 章必须属于目标卷"`
	BeforeChapterID *int64 `json:"before_chapter_id" jsonschema:"description=插入到该章之前（稳定 ID，与目标卷同组）。不传则追加到目标卷末尾；只传本项时目标组=该章所在组（含未分卷）"`
}

// MoveChapterTool 移动既有章节到目标卷的指定位置（换卷/卷内重排/移入未分卷）。
// 对应前端拖拽换卷的 PlaceChapter 能力；LLM 建章进错卷（如裸 chapters/new.md
// 误入最后一卷）后用本工具纠正。移动只改 volume_id/sort_order，不动正文文件。
type MoveChapterTool struct{}

func (t *MoveChapterTool) Name() string { return "move_chapter" }
func (t *MoveChapterTool) Description() string {
	return "移动既有章节到目标卷的指定位置（换卷、卷内重排、移入未分卷）。放在 before_chapter_id 指定章节之前，" +
		"或追加到 target_volume_id 目标卷末尾。调整在一个事务内完成，不改正文文件；「第N章」展示序号随移动实时重算。" +
		"建章进错卷（例如多卷书误用裸 chapters/new.md 被拒或落错卷）后，用本工具把章节移到正确卷。"
}
func (t *MoveChapterTool) Category() ToolCategory { return CategoryNovelManagement }
func (t *MoveChapterTool) JSONSchema() json.RawMessage {
	return SchemaOf(MoveChapterArgs{})
}
func (t *MoveChapterTool) ExposeToLLM() bool { return true }
func (t *MoveChapterTool) NewArgs() any      { return &MoveChapterArgs{} }

func (t *MoveChapterTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*MoveChapterArgs)

	src, err := getChapterRecord(ctx, tc.DB, tc.NovelID, a.ChapterID)
	if err != nil {
		return nil, fmt.Errorf("query chapter: %w", err)
	}
	if src == nil {
		return &ToolResult{Success: false, Error: fmt.Sprintf("章节不存在: id=%d", a.ChapterID)}, nil
	}
	if a.TargetVolumeID == nil && a.BeforeChapterID == nil {
		return &ToolResult{Success: false, Error: "须指定 target_volume_id（目标卷，0=未分卷）或 before_chapter_id（插入到哪一章之前）"}, nil
	}
	if a.BeforeChapterID != nil && *a.BeforeChapterID == a.ChapterID {
		return &ToolResult{Success: false, Error: "before_chapter_id 不能是章节自身"}, nil
	}

	// 解析目标组：显式 target_volume_id 优先（0=未分卷）；缺省时跟随 before 章所在组。
	var target *int64
	if a.TargetVolumeID != nil {
		if *a.TargetVolumeID == 0 {
			target = nil // 显式移入未分卷组
		} else {
			if _, err := volume.NewStore(tc.DB, tc.LoggerOrDefault()).GetByID(ctx, nil, tc.NovelID, *a.TargetVolumeID); err != nil {
				if errors.Is(err, volume.ErrNotFound) {
					return &ToolResult{Success: false, Error: err.Error()}, nil
				}
				return nil, fmt.Errorf("query volume: %w", err)
			}
			target = a.TargetVolumeID
		}
	}
	if a.BeforeChapterID != nil {
		before, berr := getChapterRecord(ctx, tc.DB, tc.NovelID, *a.BeforeChapterID)
		if berr != nil {
			return nil, fmt.Errorf("query before chapter: %w", berr)
		}
		if before == nil {
			return &ToolResult{Success: false, Error: fmt.Sprintf("before 章节不存在: id=%d", *a.BeforeChapterID)}, nil
		}
		if a.TargetVolumeID == nil {
			target = before.VolumeID // 跟随 before 章所在组（nil=未分卷）
		} else if !sameVolumeID(before.VolumeID, target) {
			return &ToolResult{Success: false, Error: "before 章节不属于目标卷"}, nil
		}
	}

	chStore := chapter.NewStore(tc.DB, tc.LoggerOrDefault())
	placed, err := chStore.Place(ctx, nil, chapter.PlaceInput{
		NovelID:         tc.NovelID,
		SourceChapterID: &a.ChapterID,
		TargetVolumeID:  target,
		BeforeChapterID: a.BeforeChapterID,
	})
	if err != nil {
		return nil, err
	}

	readingNumber, rerr := chStore.GetReadingNumberByID(ctx, nil, tc.NovelID, placed.ID)
	if rerr != nil {
		return nil, fmt.Errorf("get reading number: %w", rerr)
	}
	data := map[string]any{
		"chapter_id":     placed.ID,
		"title":          placed.Title,
		"sort_order":     placed.SortOrder,
		"reading_number": readingNumber,
	}
	if placed.VolumeID != nil {
		data["volume_id"] = *placed.VolumeID
	} else {
		data["volume_id"] = nil
	}
	// 移动不改文件内容，但目录归属/阅读序号已变：以 metadata_only 事件通知前端刷新章节目录。
	emitFileMetadataChanged(ctx, tc.NovelID, git.ChapterPath(placed.ID))
	return &ToolResult{Success: true, Data: data}, nil
}

// ── 注册 ──────────────────────────────────────────────

// RegisterChapterTools 注册章节结构管理类工具。
func RegisterChapterTools(r *Registry) {
	r.Register(&MoveChapterTool{})
}
