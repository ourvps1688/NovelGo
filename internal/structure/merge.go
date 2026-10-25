package structure

import "fmt"

// MergeFindings 合并多组告警并重排 S 系编号：组内顺序保持，跨组按传入组序，
// 编号 S1..Sn 连续唯一。各检查器独立产出 S1..（单独使用即正确）；跨检查器
// 汇入同一「结构告警」块时由本函数统一编号，保证确定性（同输入同输出）。
func MergeFindings(groups ...[]StructFinding) []StructFinding {
	var out []StructFinding
	for _, g := range groups {
		for _, f := range g {
			f.ID = fmt.Sprintf("S%d", len(out)+1)
			out = append(out, f)
		}
	}
	return out
}
