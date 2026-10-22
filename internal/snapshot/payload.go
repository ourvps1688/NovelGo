package snapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// SchemaVersion payload 结构版本。模型演进时递增；旧 payload 仍可解码
// （缺字段=零值），保证历史快照长期可回滚。
const SchemaVersion = 1

// ChapterManifestItem 正文清单条目（只读上下文：冻结时刻「写到哪了」）。
// 按章节 id 升序；阅读序由 (volume_id, sort_order) 承载（阅读序展示号不持久化）。
// 不参与回滚——正文以 Git 文件为准，Restore 只动台账表。
type ChapterManifestItem struct {
	ID        int64  `gorm:"column:id"         json:"id"`
	VolumeID  *int64 `gorm:"column:volume_id"  json:"volume_id,omitempty"`
	SortOrder int    `gorm:"column:sort_order" json:"sort_order"`
	Title     string `gorm:"column:title"      json:"title"`
	WordCount int    `gorm:"column:word_count" json:"word_count"`
}

// Payload 快照正文：series bible 全量台账 + 正文清单的 JSON 序列化。
//
// Tables 键=表名（registry 全集，空表也占键），值=按 id 升序的行数组。
// 序列化确定性：map 键由 encoding/json 排序、行按 id 定序、结构体字段定序——
// 相同库态两次采集产生逐字节相同的 payload（供 diff/留痕稳定比较）。
type Payload struct {
	SchemaVersion   int                        `json:"schema_version"`
	NovelID         int64                      `json:"novel_id"`
	VolumeID        int64                      `json:"volume_id"`
	SnapshotVersion int                        `json:"snapshot_version"`
	CommittedAt     time.Time                  `json:"committed_at"`
	ChapterManifest []ChapterManifestItem      `json:"chapter_manifest"`
	Tables          map[string]json.RawMessage `json:"tables"`
}

// capturePayload 采集该小说当前台账全量状态为 payload。须在事务内调用，
// 保证与冻结写入看到同一数据视图（「冻结即串行提交」）。
func capturePayload(ctx context.Context, tx *gorm.DB, novelID, volumeID int64, version int, committedAt time.Time) (*Payload, error) {
	p := &Payload{
		SchemaVersion:   SchemaVersion,
		NovelID:         novelID,
		VolumeID:        volumeID,
		SnapshotVersion: version,
		CommittedAt:     committedAt,
		Tables:          make(map[string]json.RawMessage, len(registry)),
	}

	for _, spec := range registry {
		rows := spec.newRows()
		if err := tx.WithContext(ctx).Where("novel_id = ?", novelID).Order("id ASC").Find(rows).Error; err != nil {
			return nil, fmt.Errorf("snapshot: 采集 %s: %w", spec.name, err)
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			return nil, fmt.Errorf("snapshot: 序列化 %s: %w", spec.name, err)
		}
		p.Tables[spec.name] = raw
	}

	manifest := make([]ChapterManifestItem, 0)
	if err := tx.WithContext(ctx).Table("chapters").
		Select("id", "volume_id", "sort_order", "title", "word_count").
		Where("novel_id = ?", novelID).Order("id ASC").
		Scan(&manifest).Error; err != nil {
		return nil, fmt.Errorf("snapshot: 采集正文清单: %w", err)
	}
	p.ChapterManifest = manifest
	return p, nil
}

// marshal 把 payload 序列化为确定性 JSON（表名键排序 + 行按 id 定序）。
func (p *Payload) marshal() (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("snapshot: 序列化 payload: %w", err)
	}
	return string(raw), nil
}

// parsePayload 解析快照行 payload。schema 演进容忍：未知字段忽略、缺字段=零值。
func parsePayload(raw string) (*Payload, error) {
	var p Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("snapshot: 解析 payload: %w", err)
	}
	return &p, nil
}
