package app

// P1 WBS T3.1：Voice 台账查询与人工校正 API。
// 台账真理源在 internal/voice（voice_dimensions 表，书级十维）；此处只做
// 查询视图（含空维度占位）与人工校正入口（source=manual），不掺业务逻辑。

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/voice"
)

// VoiceDimensionView 是查询 API 的单维视图（WBS T3.1「含空维度占位视图」）：
// 已落库维度带完整字段（Present=true）；未落库维度以空占位呈现
// （Present=false，Content 等为空——不造默认值，宁缺勿错）。
type VoiceDimensionView struct {
	Dimension string    `json:"dimension"`
	Present   bool      `json:"present"`
	Content   string    `json:"content"`
	Metrics   string    `json:"metrics"`
	Evidence  string    `json:"evidence"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GetVoiceLedger 列出某书 Voice 台账：按 §0.1 固定维度顺序返回十维视图，
// 未落库维度以空占位呈现（空台账即十项全为 Present=false）。
func (a *App) GetVoiceLedger(novelID int64) ([]VoiceDimensionView, error) {
	if novelID <= 0 {
		return nil, fmt.Errorf("voice: 缺少 novel_id")
	}
	rows, err := voice.NewStore(a.db, a.logger).List(a.ctx, novelID)
	if err != nil {
		return nil, err
	}
	byDim := make(map[string]voice.Entry, len(rows))
	for _, r := range rows {
		byDim[r.Dimension] = r
	}
	items := make([]VoiceDimensionView, 0, 10)
	for _, d := range voice.Dimensions() {
		view := VoiceDimensionView{Dimension: string(d)}
		if r, ok := byDim[string(d)]; ok {
			view = VoiceDimensionView{
				Dimension: string(d),
				Present:   true,
				Content:   r.Content,
				Metrics:   r.Metrics,
				Evidence:  r.Evidence,
				Source:    r.Source,
				UpdatedAt: r.UpdatedAt,
			}
		}
		items = append(items, view)
	}
	return items, nil
}

// UpsertVoiceDimensionInput 是人工校正入口入参（source=manual）。
type UpsertVoiceDimensionInput struct {
	NovelID   int64  `json:"novel_id"`
	Dimension string `json:"dimension"`
	Content   string `json:"content"`
	Metrics   string `json:"metrics"`  // 可空；非空须为合法 JSON
	Evidence  string `json:"evidence"` // 可空；非空须为合法 JSON
}

// UpsertVoiceDimension 人工校正单条维度台账（source=manual，(novel_id,dimension)
// 幂等覆盖——含双写落库的行也可人工覆盖校正）。返回落库后的稳定快照。
func (a *App) UpsertVoiceDimension(input UpsertVoiceDimensionInput) (*voice.Entry, error) {
	if err := validateVoiceJSONField("metrics", input.Metrics); err != nil {
		return nil, err
	}
	if err := validateVoiceJSONField("evidence", input.Evidence); err != nil {
		return nil, err
	}
	store := voice.NewStore(a.db, a.logger)
	err := store.Upsert(a.ctx, &voice.Entry{
		NovelID:   input.NovelID,
		Dimension: input.Dimension,
		Content:   input.Content,
		Metrics:   strings.TrimSpace(input.Metrics),
		Evidence:  strings.TrimSpace(input.Evidence),
		Source:    string(voice.SourceManual),
	})
	if err != nil {
		return nil, err
	}
	// 回读稳定快照：Upsert 冲突更新路径下 Create 不回填 ID/时间戳。
	got, err := store.Get(a.ctx, input.NovelID, voice.Dimension(input.Dimension))
	if err != nil {
		return nil, err
	}
	return got, nil
}

// validateVoiceJSONField 校验可空 JSON 字段：空串放行，非空须为合法 JSON（坏数据宁缺勿错）。
func validateVoiceJSONField(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if !json.Valid([]byte(value)) {
		return fmt.Errorf("voice: %s 须为合法 JSON", name)
	}
	return nil
}
