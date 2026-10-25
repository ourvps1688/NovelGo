package quality

// 关键章定向深读清单（R2b-9，提案 §8.5 三层输入第三层）：
// L2 深审重定位为「结构索引全量 + 质量档案全序列 + 关键章定向深读」——
// 抽样深读只负责验证发现层疑点，不负责发现。关键章从质量档案确定性信号挑选：
//   - 有未修复发现的章（L0/L1 Finding 待修=明确疑点）；
//   - 黄金三章（阅读序 1..GoldenThreeCount，§9.3 开篇预算倾斜）。
//
// 纪律：纯函数零副作用；确定性（阅读序升序、原因定序合并）；
// 空清单=空块零提示（提示词与无该块逐字一致）。

import (
	"fmt"
	"sort"
	"strings"
)

// GoldenThreeCount 是黄金三章的章数（§9.3 开篇预算倾斜：前三章逐字精读）。
const GoldenThreeCount = 3

// 关键章入选原因（渲染文案单源，定序合并）。
const (
	keyChapterReasonOpenFindings = "未修复发现"
	keyChapterReasonGoldenThree  = "黄金三章"
)

// KeyChapter 是关键章定向深读清单的一项。
type KeyChapter struct {
	// ReadingNumber 是章节全局阅读序号（1-based）。
	ReadingNumber int
	// Reasons 是入选原因（定序去重：未修复发现在前、黄金三章在后）。
	Reasons []string
}

// PickKeyChapters 从质量档案全序列挑选关键章（定向深读验证清单）。
// 信号：①有未修复发现（L0/L1 Finding 状态为待修/空）；②黄金三章（阅读序 1..goldenN，
// goldenN 非正=不挑黄金三章，由调用方显式控制）。无任何信号=空清单（nil，零提示）。
func PickKeyChapters(rows []ChapterQuality, goldenN int) []KeyChapter {
	byReading := map[int]*KeyChapter{}
	ensure := func(reading int) *KeyChapter {
		if k, ok := byReading[reading]; ok {
			return k
		}
		k := &KeyChapter{ReadingNumber: reading}
		byReading[reading] = k
		return k
	}
	addReason := func(k *KeyChapter, reason string) {
		for _, r := range k.Reasons {
			if r == reason {
				return
			}
		}
		k.Reasons = append(k.Reasons, reason)
	}
	for _, row := range rows {
		if hasOpenFindings(row) {
			addReason(ensure(row.ReadingNumber), keyChapterReasonOpenFindings)
		}
	}
	for i := 1; i <= goldenN; i++ {
		addReason(ensure(i), keyChapterReasonGoldenThree)
	}
	if len(byReading) == 0 {
		return nil
	}
	out := make([]KeyChapter, 0, len(byReading))
	for _, k := range byReading {
		out = append(out, *k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReadingNumber < out[j].ReadingNumber })
	return out
}

// hasOpenFindings 判断档案行是否有未修复发现（L0/L1 任一侧，状态待修或空）。
func hasOpenFindings(row ChapterQuality) bool {
	for _, f := range ParseFindings(row.L0Findings) {
		if f.Status == "" || f.Status == FindingStatusPending {
			return true
		}
	}
	for _, f := range ParseFindings(row.L1Findings) {
		if f.Status == "" || f.Status == FindingStatusPending {
			return true
		}
	}
	return false
}

// RenderKeyChapterBlock 把关键章清单渲染为「关键章定向深读」文本块
// （R2b-9 §8.5 三层输入第三层，注入 L2 体检提示词）。
//
// 渲染纪律（与 RenderLedgerBlock 同族）：
//   - 空清单返回空串——零提示（提示词与无该块逐字一致）；
//   - 确定性：按阅读序号升序，原因按传入顺序（PickKeyChapters 已定序）；
//   - 只读汇编：清单原样投影，不做取舍判断（裁决留给 L2）。
func RenderKeyChapterBlock(items []KeyChapter) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【关键章定向深读】以下章节请逐字深读核验——定向深读是三层输入的验证层：" +
		"只负责验证「质量档案汇总」与「结构告警」（发现层）锁定的疑点是否成立（举证或排除），" +
		"不承担全书发现职责；深读中确证的硬伤照常入 issues 并注明章节：\n")
	for _, it := range items {
		fmt.Fprintf(&b, "  - 第 %d 章（%s）\n", it.ReadingNumber, strings.Join(it.Reasons, "、"))
	}
	return strings.TrimRight(b.String(), "\n")
}
