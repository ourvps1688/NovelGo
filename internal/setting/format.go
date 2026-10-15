package setting

import (
	"fmt"
	"sort"
	"strings"

	"github.com/novelgo/novelgo/internal/llm"
)

// SettingsTokenBudget 是注入 system 消息时设定的总软上限（token）。
// 设计依据：12k 占 200k 上下文窗口 6%，中文 12k 约等于 6-9k 字，能装较完整的世界观/力量体系。
// 注意：本预算是「分桶后各分类预算之和」，不是单分类上限。
const SettingsTokenBudget = 12000

// settingCategoryWeights 是分类的预算权重。根基类（世界观/力量体系/历史/地理）权重更高，
// 保证即使「角色/物品」等高频类目条目再多，根基设定也按权重分到足额预算，不会被挤出。
// 未在此显式列出的分类走 foundationalKeyword 关键词兜底或直接取默认权重。
var settingCategoryWeights = map[string]int{
	"世界观":  4,
	"力量体系": 3,
	"历史":   3,
	"地理":   2,
	"地图":   2,
	"角色":   1,
	"物品":   1,
}

// foundationalKeywords 关键词兜底：分类写法有变体时（如「世界观设定」「修仙力量体系」
// 「修炼境界」），只要包含这些词就视为根基类，给予高权重保护，避免被高频类挤掉。
var foundationalKeywords = []string{
	"世界观", "力量", "体系", "历史", "地理", "地图", "功法", "修炼", "境界",
}

// settingCategoryDefaultWeight 是未命中显式权重与关键词兜底的分类默认权重。
const settingCategoryDefaultWeight = 1

// settingCategoryWeight 返回某分类的预算权重。
func settingCategoryWeight(raw string) int {
	cat := strings.TrimSpace(raw)
	if w, ok := settingCategoryWeights[cat]; ok {
		return w
	}
	for _, kw := range foundationalKeywords {
		if strings.Contains(cat, kw) {
			return 3
		}
	}
	return settingCategoryDefaultWeight
}

// FormatSettings 格式化设定列表为可读文本，每条带 id 前缀。
// 用于 NovelProfile 注入 system 消息，agent 看到后可通过 id 调 upsert_setting 更新。
// 空 items 返回空字符串。
//
// 排序：调用方（NovelProfile）SQL 已按 updated_at DESC 排序——最近活跃在前。
//
// 分桶预算（P3）：按 Category 分桶，各桶按权重比例瓜分 SettingsTokenBudget 总预算；
// 桶内按 updated_at DESC 保留最近活跃的，超出本桶预算则截断该桶剩余条目。
// 这样根基类（世界观/力量体系/历史/地理，权重更高）即使面对大量「角色/物品」条目，
// 也始终分到足额预算，不会被挤出导致「吃书」。末尾对超出本桶预算的分类追加提示。
// 返回 (text, truncatedCount)，truncatedCount 为被截断的条目总数（跨桶汇总）。
func FormatSettings(items []SettingItem) (string, int) {
	if len(items) == 0 {
		return "", 0
	}

	// 1. 按分类分桶
	type bucket struct {
		category string
		weight   int
		items    []SettingItem
	}
	bucketsByName := make(map[string]*bucket, 8)
	order := make([]string, 0, 8)
	for _, it := range items {
		cat := strings.TrimSpace(it.Category)
		if cat == "" {
			cat = "其他"
		}
		b, ok := bucketsByName[cat]
		if !ok {
			b = &bucket{category: cat, weight: settingCategoryWeight(cat)}
			bucketsByName[cat] = b
			order = append(order, cat)
		}
		b.items = append(b.items, it)
	}

	// 2. 按比例分配总预算：每桶预算 = 总预算 × 本桶权重 / 总权重
	totalWeight := 0
	for _, cat := range order {
		totalWeight += bucketsByName[cat].weight
	}

	// 3. 稳定输出顺序：权重降序，权重相同按分类名升序——根基类排在前面（注意力/缓存友好）
	sort.SliceStable(order, func(i, j int) bool {
		wi, wj := bucketsByName[order[i]].weight, bucketsByName[order[j]].weight
		if wi != wj {
			return wi > wj
		}
		return order[i] < order[j]
	})

	var b strings.Builder
	b.WriteString("【设定】\n")

	totalTruncated := 0
	var notes []string
	for _, cat := range order {
		bb := bucketsByName[cat]
		// 桶内按 updated_at DESC（最近活跃在前），保证保留的是近期维护过的条目
		sort.SliceStable(bb.items, func(i, j int) bool {
			return bb.items[i].UpdatedAt.After(bb.items[j].UpdatedAt)
		})
		bucketCap := SettingsTokenBudget * bb.weight / totalWeight
		if bucketCap < 1 {
			bucketCap = 1
		}

		written := 0
		usedInBucket := 0
		for _, it := range bb.items {
			line := fmt.Sprintf("- [setting_id:%d | %s] %s\n", it.ID, bb.category, it.Content)
			lineTokens, _ := llm.CountTokens(line)
			// 单条就超桶预算（异常长条目）：最多写首条，避免独占，其余截断
			if lineTokens > bucketCap {
				if written == 0 {
					b.WriteString(line)
					written++
				}
				break
			}
			if usedInBucket+lineTokens > bucketCap {
				break
			}
			b.WriteString(line)
			usedInBucket += lineTokens
			written++
		}

		if written < len(bb.items) {
			dropped := len(bb.items) - written
			totalTruncated += dropped
			notes = append(notes, fmt.Sprintf("「%s」分类超出其预算上限，已保留该分类最近活跃的 %d 条、截断 %d 条", cat, written, dropped))
		}
	}

	if totalTruncated > 0 {
		b.WriteString("\n⚠️ 设定总量较大，已按分类分桶保留（根基类如世界观/力量体系优先）：\n")
		for _, n := range notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		b.WriteString("请优先用 upsert_setting 合并同分类的已有条目（基于 setting_id 更新 content），避免预算持续膨胀。\n")
	}

	return b.String(), totalTruncated
}

// CountSettingsTokens 估算设定列表全量 token 数（不截断），用于 App 层报告超预算状态。
// 与 FormatSettings 输出格式一致，但不做截断，返回完整文本的 token 数。
// 用于 GetNovelSettings 返回 token_count + over_budget，前端可据此显示但不阻塞。
func CountSettingsTokens(items []SettingItem) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	var b strings.Builder
	b.WriteString("【设定】\n")
	for _, item := range items {
		fmt.Fprintf(&b, "- [setting_id:%d | %s] %s\n", item.ID, item.Category, item.Content)
	}
	n, err := llm.CountTokens(b.String())
	if err != nil {
		return 0, fmt.Errorf("count setting tokens: %w", err)
	}
	return n, nil
}
