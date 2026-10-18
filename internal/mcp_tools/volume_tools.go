package mcp_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/git"
	"github.com/novelgo/novelgo/internal/volume"
)

// ── create_volume ────────────────────────────────────

// CreateVolumeItem 是 create_volume 的单条参数。
type CreateVolumeItem struct {
	Name           string  `json:"name" jsonschema:"required,description=卷名称" validate:"required"`
	BeforeVolumeID *int64  `json:"before_volume_id" jsonschema:"description=插入到该卷之前；不传则追加到小说末尾"`
	Outline        *string `json:"outline" jsonschema:"description=该卷的卷纲摘要（Markdown），创建时一并写入 volumes/id_{id}.md"`
}

// CreateVolumeArgs 是 create_volume 的参数。
type CreateVolumeArgs struct {
	Volumes []CreateVolumeItem `json:"volumes" jsonschema:"required,description=要创建的卷列表（1-20个）" validate:"required,min=1,max=20,dive"`
}

// CreateVolumeTool 创建新卷并可选地写入卷纲。
type CreateVolumeTool struct{}

func (t *CreateVolumeTool) Name() string { return "create_volume" }
func (t *CreateVolumeTool) Description() string {
	return "批量创建卷（1-20个）并可选地写入卷纲。保证原子性，失败时全部回滚。" +
		"name 必填；before_volume_id 可调整卷序（插入到目标卷之前，不传则追加末尾）；" +
		"outline 非空时把卷纲摘要写入 volumes/id_{新卷id}.md。蓝图规划（S2）阶段用此工具落库卷结构。"
}
func (t *CreateVolumeTool) Category() ToolCategory { return CategoryNovelManagement }

func (t *CreateVolumeTool) JSONSchema() json.RawMessage { return SchemaOf(CreateVolumeArgs{}) }
func (t *CreateVolumeTool) ExposeToLLM() bool           { return true }
func (t *CreateVolumeTool) NewArgs() any                { return &CreateVolumeArgs{} }

func (t *CreateVolumeTool) Execute(ctx context.Context, args any, tc ToolContext) (*ToolResult, error) {
	a := args.(*CreateVolumeArgs)

	store := volume.NewStore(tc.DB, tc.LoggerOrDefault())

	var ids []int64
	var placed []volume.Volume
	err := tc.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, item := range a.Volumes {
			name := item.Name
			v, err := store.Place(ctx, tx, volume.PlaceInput{
				NovelID:        tc.NovelID,
				Name:           &name,
				BeforeVolumeID: item.BeforeVolumeID,
			})
			if err != nil {
				return err
			}
			ids = append(ids, v.ID)
			placed = append(placed, *v)
		}
		return nil
	})
	if err != nil {
		return &ToolResult{
			Success: false,
			Error:   fmt.Sprintf("创建卷失败: %s", err),
			ErrKind: ErrKindBusiness,
		}, nil
	}

	// 写卷纲文件（非事务，失败仅告警，不影响卷记录落库）。
	for i, v := range placed {
		outline := a.Volumes[i].Outline
		if outline == nil || *outline == "" {
			continue
		}
		if werr := writeVolumeOutline(tc.NovelID, v.ID, *outline); werr != nil {
			tc.LoggerOrDefault().Warn("写入卷纲文件失败", "volume_id", v.ID, "err", werr)
		}
	}

	return &ToolResult{
		Success: true,
		Data:    map[string]any{"ids": ids, "count": len(ids)},
	}, nil
}

// writeVolumeOutline 把卷纲内容写入小说仓库的 volumes/id_{id}.md。
func writeVolumeOutline(novelID, volumeID int64, content string) error {
	dir := config.NovelDirPath(novelID)
	full := filepath.Join(dir, git.VolumePath(volumeID))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("创建卷纲目录失败: %w", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return fmt.Errorf("写入卷纲文件失败: %w", err)
	}
	return nil
}

// ── 注册 ──────────────────────────────────────────────

// RegisterVolumeTools 注册卷管理类工具。
func RegisterVolumeTools(r *Registry) {
	r.Register(&CreateVolumeTool{})
}
