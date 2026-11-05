package app

// rhythm_inject.go —— P2-2 平台化写作模板的写章注入（生成侧消费）。
//
// 命中写章意图 + CheckerConfig 已配平台 profile 且 profile 指名节奏模板时，
// 把 scene-beats「平台节奏模板」小节（skill.RhythmBlock 确定性提取）以
// <system-reminder> 注入本轮（与 autoInject/preCtxInject 同源同持久化语义）。
//
// 缺省纪律：非写章意图 / 未配 profile / 未指名模板 / 提取失败 → 空串不注入，
// 生成提示词与无模板口径逐字一致（缺省不模板）。

import (
	"context"
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/skill"
)

// buildRhythmInject 构建平台节奏模板块。返回空串表示不注入；
// 返回的 error 仅用于调用方记日志，不影响主流程（降级与「不注入」等价）。
func (a *App) buildRhythmInject(ctx context.Context, novelID int64, message string) (string, error) {
	// 与 preCtxInject 同口径：仅非斜杠消息 + 写章意图（斜杠走 skill 自身正文）。
	if strings.HasPrefix(message, "/") {
		return "", nil
	}
	if !isChapterWritingIntent(message) {
		return "", nil
	}
	cfg, err := quality.NewStore(a.db, a.logger).GetCheckerConfig(ctx, novelID)
	if err != nil {
		return "", fmt.Errorf("rhythm: 取 CheckerConfig 失败: %w", err)
	}
	profile := cfg.Profile()
	if profile == nil || profile.RhythmTemplate == "" {
		return "", nil
	}
	block := skill.RhythmBlock(profile.RhythmTemplate)
	if block == "" {
		return "", nil
	}
	return "<system-reminder>\n" + block + "\n</system-reminder>", nil
}
