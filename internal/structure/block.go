package structure

import (
	"fmt"
	"strings"
)

// RenderBlock 把结构告警清单渲染为「结构告警」文本块（R2b-1：组装进 L2 体检输入与
// 档案汇总快审输入，提案 §8.5 结构索引全量）。
//
// 渲染纪律：
//   - 零告警返回空串——零提示（渲染输出与无该块逐字一致，降级契约）；
//   - 确定性：按传入顺序渲染（编号由检查器按条目 ID 定序分配），描述压单行防破版式；
//   - 只读汇编不改写：告警原样投影，不做聚合判断（裁决留给 L2 汇总）。
func RenderBlock(findings []StructFinding) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【结构告警】长程结构索引层确定性检查（伏笔 lifecycle）产出的风险告警" +
		"（只报告不改写，供体检汇总裁决参照；请对告警点做定向核验）：\n")
	for _, f := range findings {
		fmt.Fprintf(&b, "  - %s[%s] %s（time_entries:%d）\n", f.ID, f.Check, oneLine(f.Desc), f.EntryID)
	}
	return strings.TrimRight(b.String(), "\n")
}

// oneLine 把多行文本压成单行（块内每条告警一行，描述不破版式）。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
