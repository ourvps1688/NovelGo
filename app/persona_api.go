package app

import (
	"github.com/novelgo/novelgo/internal/personasetting"
)

// GetPersonaSetting 返回本书的写作人格（persona）设置；未配置返回默认
// （Enabled=false，即普通模式）。
func (a *App) GetPersonaSetting(novelID int64) (*personasetting.PersonaSetting, error) {
	return personasetting.NewStore(a.db, a.logger).Get(a.ctx, novelID)
}

// SavePersonaSettingInput 是保存 persona 设置的入参。
type SavePersonaSettingInput struct {
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"` // "skill" | "author"
	Ref     string `json:"ref"`  // 技能名 或 规范 source_author
}

// SavePersonaSetting 保存本书的写作人格设置（按 novel_id 幂等 upsert）。
// Enabled=false 表示普通模式（关闭 persona）。
func (a *App) SavePersonaSetting(novelID int64, input SavePersonaSettingInput) error {
	p := &personasetting.PersonaSetting{
		NovelID: novelID,
		Enabled: input.Enabled,
		Kind:    input.Kind,
		Ref:     input.Ref,
	}
	return personasetting.NewStore(a.db, a.logger).Save(a.ctx, p)
}
