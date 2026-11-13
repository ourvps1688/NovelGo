package quality

// tension_block.go —— 「情绪曲线摘要」块（P0-2 dim4 节奏/爽点证据）。
//
// 把一书的张力曲线（chapter_tension，L0 基线 + L1 标注）渲染为按章一行的摘要块，
// 附进签约体检提示词（signabilityPrompt structBlock 之后）供 dim4（节奏爽点）评分
// 举证——「曲线截图式证据」，不替代逐章通读。

import (
	"fmt"
	"sort"
	"strings"
)

// 摘要块体量封顶（提示词体量可控；截断如实标注）。
const (
	tensionBlockMaxSegs     = 16  // 每章 tension 值串最多列 16 段
	tensionBlockMaxChapters = 120 // 最多列 120 章
)

// RenderTensionBlock 把一书的张力曲线行渲染为「情绪曲线摘要」文本块。
//
// 渲染纪律：
//   - 合并：按章分组后经 EffectiveTensionCurve 合并（同 seg L1 优先）；
//     章序按阅读序号升序（同序按 chapter_id），同输入同输出；
//   - 每章一行：tension 值串 + 情绪峰 segs + 最长拖沓 run + 开篇坡度升幅
//     （阈值口径与 tension_checks.go 同源）；
//   - 体量封顶：每章 tensionBlockMaxSegs 段、全书 tensionBlockMaxChapters 章，
//     截断如实标注；
//   - 空清单返回空串——无曲线=不注入（提示词与无该块逐字一致）。
func RenderTensionBlock(rows []ChapterTension) string {
	if len(rows) == 0 {
		return ""
	}
	// 按章分组。
	groups := map[int64][]ChapterTension{}
	for _, r := range rows {
		groups[r.ChapterID] = append(groups[r.ChapterID], r)
	}
	type chapterRef struct {
		chapterID int64
		reading   int
		rows      []ChapterTension
	}
	refs := make([]chapterRef, 0, len(groups))
	for id, rs := range groups {
		rn := 0
		for _, r := range rs {
			if r.ReadingNumber > rn {
				rn = r.ReadingNumber
			}
		}
		refs = append(refs, chapterRef{chapterID: id, reading: rn, rows: rs})
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].reading != refs[j].reading {
			return refs[i].reading < refs[j].reading
		}
		return refs[i].chapterID < refs[j].chapterID
	})

	var b strings.Builder
	b.WriteString("【情绪曲线摘要】各章张力曲线合并快照（L0 基线与 L1 标注同段取 L1，tension/emotion 0-100；" +
		"开篇坡度只取 L0 基线、按峰值/升幅双通路与检测判定同源；供节奏/爽点维度举证参照，不替代逐章通读）：\n")
	shown := refs
	truncated := 0
	if len(refs) > tensionBlockMaxChapters {
		shown = refs[:tensionBlockMaxChapters]
		truncated = len(refs) - tensionBlockMaxChapters
	}
	for _, ref := range shown {
		// #116：坡度是 L0 确定性判定，摘要这一项不能吃 L1 主观标注（旧链路
		// 用合并曲线算，l0/l1 两套数会让界面同时显示「不足」与「达标」）。
		// 其余项仍按 EffectiveTensionCurve 的 dim4 举证口径不变。
		var l0Rows []ChapterTension
		for _, r := range ref.rows {
			if r.Source == TensionSourceL0 {
				l0Rows = append(l0Rows, r)
			}
		}
		b.WriteString(tensionChapterLine(ref.reading, EffectiveTensionCurve(ref.rows), l0Rows) + "\n")
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "- …（余 %d 章略，体量封顶）\n", truncated)
	}
	return strings.TrimRight(b.String(), "\n")
}

// tensionChapterLine 渲染单章摘要行：tension 值串/情绪峰 segs/拖沓/开篇坡度。
// rows 为该章合并后的有效曲线（EffectiveTensionCurve 语义，seg 升序）；
// slopeRows 是同章 L0 基线行（空时回落 rows），供坡度项与检测判定同源。
func tensionChapterLine(reading int, rows, slopeRows []ChapterTension) string {
	pts := TensionPointsFromRows(rows)
	slopePts := pts
	if len(slopeRows) > 0 {
		slopePts = TensionPointsFromRows(slopeRows)
	}
	// tension 值串（每章 tensionBlockMaxSegs 段封顶）。
	vals := make([]string, 0, len(pts))
	for i, pt := range pts {
		if i >= tensionBlockMaxSegs {
			break
		}
		vals = append(vals, fmt.Sprintf("%.0f", pt.Tension))
	}
	valText := strings.Join(vals, "/")
	if len(pts) > tensionBlockMaxSegs {
		valText += "/…"
	}

	// 情绪峰 segs（climax 段全列，章内一般个位数）。
	var climaxSegs []string
	for _, pt := range pts {
		if NormalizeBeatType(pt.BeatType) == BeatTypeClimax {
			climaxSegs = append(climaxSegs, fmt.Sprintf("%d", pt.Seg))
		}
	}
	climaxText := "无"
	if len(climaxSegs) > 0 {
		climaxText = strings.Join(climaxSegs, ",")
	}

	// 最长拖沓 run（连续平坦段 ≥ pacingDragMinRun 才标注）。
	dragText := "无"
	if s, e, run := longestFlatRun(pts); run >= pacingDragMinRun {
		dragText = fmt.Sprintf("seg %d–%d", s, e)
	}

	// 开篇加压（与 openingSlopeFinding 共用 openingSlopeMetrics / openingSlopeOK）。
	// 显示「最高升幅」但达标判定同时看峰值，避免与写作规范方向相反。
	slopeText := "n/a"
	if rise, peak, segs := openingSlopeMetrics(slopePts); len(segs) >= 2 {
		slopeText = fmt.Sprintf("%+.1f", rise)
		if !openingSlopeOK(rise, peak) {
			slopeText += "（不足）"
		}
	}

	return fmt.Sprintf("- #%d｜tension %s（%d 段）｜情绪峰 seg %s｜拖沓 %s｜开篇坡度 %s",
		reading, valText, len(pts), climaxText, dragText, slopeText)
}
