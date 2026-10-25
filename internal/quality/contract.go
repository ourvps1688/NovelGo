package quality

// contract.go —— chapter-ci 技能 L1 回执 JSON 契约（单源，R2 提案 §4.2）。
//
// LLM 输出的 L1 回执契约在此唯一定义：字段名、检查项枚举、格式串、解析、
// 档案发现投影全部由本文件派生。skill md（../skill/builtin/chapter-ci.md）
// 输出格式节必须原样引用 L1ReceiptFormatLine()，契约测试（contract_test.go）
// 强制同步——新增/修改字段只改这里 + skill md 示例，不再逐处手抄 schema 字符串。

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// L1SchemaVersion 是 L1 回执契约的版本号。每次给回执增删字段或变更语义时 +1。
const L1SchemaVersion = 1

// L1 检查项枚举（回执 check 字段取值单源；与 Summary.ByCheck 键同域）。
const (
	L1CheckContinuity  = "continuity"   // 一致性/连贯（与前章结尾/台账是否矛盾）
	L1CheckPacing      = "pacing"       // 节奏（爽点断档/拖沓）
	L1CheckForeshadow  = "foreshadow"   // 伏笔 lifecycle（埋/收/烂尾）
	L1CheckReaderState = "reader_state" // 读者认知态（已知/误知/剧透）
	L1CheckTaskCard    = "task_card"    // 本章计划卡（钩子/爽点/人设时刻/推进）兑现
)

// L1CheckValid 判断 L1 检查项枚举值是否合法。
func L1CheckValid(s string) bool {
	switch s {
	case L1CheckContinuity, L1CheckPacing, L1CheckForeshadow, L1CheckReaderState, L1CheckTaskCard:
		return true
	}
	return false
}

// L1Finding 是 L1 回执里的一条发现（只报告不改写，warning 级起步由档案层统一置位）。
type L1Finding struct {
	// ID 是档案内稳定键（L1 系：L1-1/L1-2/…）；空缺由投影层补号。
	ID string `json:"id"`
	// Check 是检查项标识（L1Check* 枚举）。
	Check string `json:"check"`
	// Desc 是发现描述。
	Desc string `json:"desc"`
	// ChapterIDs 是涉及章节全局阅读序号。
	ChapterIDs []int64 `json:"chapter_ids,omitempty"`
	// LedgerRefs 是影响/涉及的台账条目。
	LedgerRefs []string `json:"ledger_refs,omitempty"`
}

// L1Receipt 是 chapter-ci 技能的回执契约形。
type L1Receipt struct {
	Findings []L1Finding `json:"findings"`
	Summary  string      `json:"summary"`
}

// L1ReceiptFormatLine 生成 L1 回执 JSON 格式串（skill md 输出格式节原样引用，契约测试锁定）。
func L1ReceiptFormatLine() string {
	return `{"findings":[{"id":"L1-1","check":"continuity|pacing|foreshadow|reader_state|task_card","desc":"...","chapter_ids":[12],"ledger_refs":[]}],"summary":"一句话小结"}`
}

// l1JSONBlockRe 匹配 ```json 或裸 ``` 围栏内的 JSON 块（回执在最终回复末尾）。
var l1JSONBlockRe = regexp.MustCompile("(?s)```(?:json)?[ \t]*\n?(\\{.*?\\})\n?```")

// ParseL1Receipt 从 LLM 输出提取末尾 ```json 回执块并解析（多个块时取最后一个可解析的）。
// 降级纪律：找不到回执块 / 解析失败 → 返回 error，由调用方静默降级（零提示零报错）。
func ParseL1Receipt(out string) (*L1Receipt, error) {
	matches := l1JSONBlockRe.FindAllStringSubmatch(out, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		var r L1Receipt
		if err := json.Unmarshal([]byte(matches[i][1]), &r); err != nil {
			continue
		}
		return &r, nil
	}
	return nil, errors.New("quality contract: 未找到可解析的 L1 回执 JSON 块")
}

// ToFindings 把回执发现投影为档案发现：Level 恒 warning（P1）、Status 恒 pending
// （只报告不改写，修复状态由后续修复路由流转）；ID 空缺按序补 L1-n；
// 作用域空缺回填本章阅读序号（readingNumber>0 时）。
func (r *L1Receipt) ToFindings(readingNumber int) []Finding {
	if r == nil || len(r.Findings) == 0 {
		return nil
	}
	out := make([]Finding, 0, len(r.Findings))
	for i, f := range r.Findings {
		id := f.ID
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("L1-%d", i+1)
		}
		it := Finding{
			ID:         id,
			Check:      f.Check,
			Desc:       f.Desc,
			ChapterIDs: f.ChapterIDs,
			LedgerRefs: f.LedgerRefs,
			Level:      FindingLevelDefault,
			Status:     FindingStatusPending,
		}
		if len(it.ChapterIDs) == 0 && readingNumber > 0 {
			it.ChapterIDs = []int64{int64(readingNumber)}
		}
		out = append(out, it)
	}
	return out
}
