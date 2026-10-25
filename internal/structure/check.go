package structure

import (
	"fmt"
	"sort"
)

// CheckForeshadowLifecycle 对伏笔投影做 lifecycle 确定性检查，返回 S 系告警清单
// （提案 §8.2「伏笔 lifecycle 表」：埋超预期收章未收=烂尾告警、收未埋=无源告警）。
//
// 三类告警：
//   - 超期未收（CheckForeshadowOverdue）：pending 且 target>0 且当前进度已过预期收章；
//   - 无收点规划（CheckForeshadowNoTarget）：pending 且 target<=0（没规划回收位置）；
//   - 收未埋=无源（CheckForeshadowUnsourced）：resolved 且无埋设章记录。
//
// 确定性：按条目 ID 升序遍历、每条按固定检查顺序产出，同输入同输出；
// abandoned（作者显式弃坑）与已回收条目不告警。nowReadingNumber<=0（尚无章节）
// 时超期判定自然不触发，其余检查照常。
func CheckForeshadowLifecycle(entries []ForeshadowEntry, nowReadingNumber int) []StructFinding {
	sorted := append([]ForeshadowEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var findings []StructFinding
	add := func(check, desc string, entryID int64) {
		findings = append(findings, StructFinding{
			ID:    fmt.Sprintf("S%d", len(findings)+1),
			Check: check,
			Level: FindingLevelDefault,
			Desc:  desc,
			Ref:   fmt.Sprintf("time_entries:%d", entryID),
		})
	}
	for _, e := range sorted {
		switch e.Status {
		case "pending":
			switch {
			case e.TargetReadingNumber <= 0:
				add(CheckForeshadowNoTarget,
					fmt.Sprintf("伏笔「%s」待收但未规划回收位置，烂尾风险", e.Title), e.ID)
			case nowReadingNumber > e.TargetReadingNumber:
				add(CheckForeshadowOverdue,
					fmt.Sprintf("伏笔「%s」预期第 %d 章前回收，当前进度第 %d 章仍待收，烂尾风险",
						e.Title, e.TargetReadingNumber, nowReadingNumber), e.ID)
			}
		case "resolved":
			if e.SourceChapterID == nil {
				add(CheckForeshadowUnsourced,
					fmt.Sprintf("伏笔「%s」已标记回收但无埋设章记录（收未埋=无源）", e.Title), e.ID)
			}
		}
	}
	return findings
}
