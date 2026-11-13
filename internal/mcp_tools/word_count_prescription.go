package mcp_tools

// word_count_prescription.go —— #119 P1：字数的本地确定性判定与处方单源。
//
// 2026-11-14 实测：第一遍正文 1585 字（平台约束卡 3000–4000），判官因「字数
// 达标度」打 58 分，评分门禁据此把整章退回重写，多花 17 分钟只换来 2694 字
// ——仍差 306 字，却因 81 分放行。字数不是主观质量问题，是可以用
// text.ComputeStats + ChapterWordsTarget 本地算清的确定性事实（#105 纪律），
// 不该由 LLM 判官发现、更不该触发一次整章自动重写（与「所有修复都走 chat」相悖）。
//
// 这里只产出「差多少字、怎么补」的处方文本，不派发、不改稿、不改阈值。

import (
	"fmt"

	"github.com/novelgo/novelgo/internal/quality"
)

// wordShortfallPrescription 返回字数不足时的确定性处方；达标或区间不可用返回空串。
// 调用方拿到空串时评分门禁行为逐字保持原样（含非字数质量问题的退回重写）。
func wordShortfallPrescription(wordCount int, words quality.ChapterWordsTarget) string {
	lo, hi, ok := words.EffectiveLengthWindow()
	if !ok || wordCount >= lo {
		return ""
	}
	return fmt.Sprintf(
		"【字数未达标（本地确定性判定）】实测 %d 字，%s %d–%d 字，尚差 %d 字。\n"+
			"本轮正文已放行落盘，系统不会整章退回重写、也不会自动补写；请在 chat 中定点补写差额"+
			"（优先扩写已有场景的动作与对白细节，不要另起新线、不要推翻已写内容），"+
			"补写后再重新检测本章。请把该差额如实告知作者并结束本轮。判官评分仅供参考，字数一律以本判定为准。",
		wordCount, words.WindowName(), lo, hi, lo-wordCount)
}
