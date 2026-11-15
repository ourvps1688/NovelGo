package quality

// stale_word_count.go —— #130：重新判分剔除发现描述里的「历史字数」陈述。
//
// 背景（2026-11-15 17:00 用户实测）：第 7 章补写到 3135 字后点「重新判分」，
// 判官仍按 1879 字、差约 1100 字扣分。根因不是字数统计口径：rescore.go 用
// text.ComputeStats 现场算 3135，但 L1 子 Agent 会把语料里标注的
// 「权威字数 N 字（word_count）」写进 finding.Desc；检测超时按 #115 纪律
// 「保留上一轮 L1」时这份 desc 不会刷新，BuildPrescriptions 又把 L1 desc
// 原样塞进 [pacing] 处方的 Current，而判官提示词明令「不得自行更换数字」，
// 于是判官只能照抄上一轮正文的历史字数。
//
// 修法：只在「以当前落盘正文为准」的链路（重新判分）注入处方前，把 desc 里
// 与本次现场 wordCount 冲突的字数陈述替换成本地确定性判定，并逐字保留 desc
// 中与字数无关的真实发现内容（pacing/台账矛盾等仍需判官核验）。
//
// 纯函数、零 LLM、零写操作：不改 Check/ID/Status/refs，不改 finding_round 与
// score_events 的 key 快照，不改 BuildPrescriptions 本身（chat 改稿消息与
// 评分门禁的处方文案逐字保持），也不引入任何自动修复或自动派发。

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// wordNumberRE 只取 3–5 位数字；hasStaleWordNumber 再按区间端点/目标值过滤，
// 避免把「3000–4000 字」这类仍有效的口径数字误判成历史实测值。
var wordNumberRE = regexp.MustCompile(`\d{3,5}`)

// ReconcileStaleWordCountFindings 返回一份「只改 Desc」的新发现清单。
// 未命中历史字数陈述的发现逐字保持；调用方传入的 slice 及其元素不被修改。
func ReconcileStaleWordCountFindings(findings []Finding, wordCount int, words ChapterWordsTarget) []Finding {
	out := make([]Finding, len(findings))
	copy(out, findings)
	for i := range out {
		out[i].Desc = reconcileStaleWordCountDesc(out[i].Desc, wordCount, words)
	}
	return out
}

func reconcileStaleWordCountDesc(desc string, wordCount int, words ChapterWordsTarget) string {
	if desc == "" {
		return desc
	}
	parts := splitDescSentences(desc)
	var b strings.Builder
	replaced := false
	for _, part := range parts {
		if isWordCountStatement(part) && hasStaleWordNumber(part, wordCount, words) {
			if !replaced {
				// 一条 desc 里若有多句历史字数陈述，只注入一次权威判定，避免重复。
				b.WriteString(currentWordCountNote(wordCount, words))
				replaced = true
			}
			continue
		}
		b.WriteString(part)
	}
	if !replaced {
		return desc
	}
	return b.String()
}

// splitDescSentences 按中文句号/分号/换行切句，分隔符跟随前句保留，
// 保证未改动的句子拼接后与原文逐字一致。
func splitDescSentences(desc string) []string {
	out := make([]string, 0, 4)
	var cur strings.Builder
	for _, r := range desc {
		cur.WriteRune(r)
		if r == '。' || r == '；' || r == '\n' {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// isWordCountStatement 判定一句是否在陈述「字数实测值/差额」。
// 只认确定性标记，不按「有数字就是字数」猜，避免误伤段位、台账 id 等。
func isWordCountStatement(s string) bool {
	if strings.Contains(s, "word_count") || strings.Contains(s, "权威字数") ||
		strings.Contains(s, "尚差") || strings.Contains(s, "字下限") {
		return true
	}
	return strings.Contains(s, "低于") && strings.Contains(s, "字")
}

// hasStaleWordNumber 判断这句里是否残留与当前正文不符的历史字数数字。
// 有效口径数字（区间端点、目标值）与当前现场 wordCount 都不算陈旧；
// 小于 500 的数字（段号、台账 id、命中次数）一律忽略。
func hasStaleWordNumber(s string, wordCount int, words ChapterWordsTarget) bool {
	lo, hi, ok := words.EffectiveLengthWindow()
	if !ok {
		lo, hi = 0, 0
	}
	target := words.EffectiveTarget()
	for _, m := range wordNumberRE.FindAllString(s, -1) {
		n, err := strconv.Atoi(m)
		if err != nil || n < 500 {
			continue
		}
		if n == wordCount || n == lo || n == hi || n == target {
			continue
		}
		return true
	}
	return false
}

// currentWordCountNote 给出唯一的权威字数表述：全部数字来自当前落盘正文与本书
// 生效字数口径，禁止再引用发现描述里的任何历史数字。
func currentWordCountNote(wordCount int, words ChapterWordsTarget) string {
	lo, hi, ok := words.EffectiveLengthWindow()
	if !ok {
		return fmt.Sprintf("字数以当前落盘正文现场判定：%d 字（word_count）；发现描述中的历史字数已过期，不得作为验收数字。", wordCount)
	}
	if wordCount < lo {
		return fmt.Sprintf("字数以当前落盘正文现场判定：%d 字（word_count），%s %d–%d 字，尚差 %d 字；发现描述中的历史字数已过期，不得作为验收数字。",
			wordCount, words.WindowName(), lo, hi, lo-wordCount)
	}
	return fmt.Sprintf("字数以当前落盘正文现场判定：%d 字（word_count），已落在 %s %d–%d 字区间内；发现描述中的历史字数已过期，不得作为验收数字。",
		wordCount, words.WindowName(), lo, hi)
}
