package quality

// benchmark_book.go —— P2-3 对标书库：赛道标杆书结构特征样本（钩子类型/爽点类型/
// 节奏形态）的存储、粘贴解析与「对标书库摘要」块渲染。
//
// 契约要点（勿改）：
//   - 样本形态：一本书一行，字段序「书名｜钩子类型｜爽点类型｜节奏形态｜赛道｜备注」，
//     分隔符半/全角竖线或 Tab；书名必填，钩子/爽点/节奏至少一项非空（结构特征样本
//     缺特征=无证据价值，宁缺勿错跳过并回报）；赛道/备注可选；
//   - 表头行（书名/标题/作品名/书名·标题）跳过并回报；无法解析/超列数的行跳过并回报；
//   - 幂等：同 (novel_id, title) upsert，最新粘贴胜（自然键=书名，对标书以书名标识）；
//   - 消费：RenderBenchmarkBlock 渲染「对标书库摘要」块注入签约体检提示词，
//     dim2（题材契合 fit）评分证据从纯模型记忆「对标 XX 书」变为样本库结构特征举证。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// BenchmarkBook 一本赛道标杆书的结构特征样本（人工录入）。
type BenchmarkBook struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID      int64     `gorm:"column:novel_id;not null;uniqueIndex:idx_benchmark_book_title,priority:1" json:"novel_id"`
	Title        string    `gorm:"column:title;not null;uniqueIndex:idx_benchmark_book_title,priority:2" json:"title"`
	HookType     string    `gorm:"column:hook_type" json:"hook_type"`
	PleasureType string    `gorm:"column:pleasure_type" json:"pleasure_type"`
	RhythmShape  string    `gorm:"column:rhythm_shape" json:"rhythm_shape"`
	Track        string    `gorm:"column:track" json:"track"`
	Notes        string    `gorm:"column:notes" json:"notes"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (BenchmarkBook) TableName() string { return "benchmark_books" }

// BenchmarkBookRow 是粘贴解析的预览行（未落库，无 novel 归属）。
type BenchmarkBookRow struct {
	Title        string `json:"title"`
	HookType     string `json:"hook_type"`
	PleasureType string `json:"pleasure_type"`
	RhythmShape  string `json:"rhythm_shape"`
	Track        string `json:"track"`
	Notes        string `json:"notes"`
}

// benchmarkBookMaxFields 字段数上限：书名/钩子/爽点/节奏/赛道/备注 共 6 列。
const benchmarkBookMaxFields = 6

// benchmarkFieldSepRe 字段分隔：半/全角竖线或 Tab（特征短语可含逗号/空格，
// 不认逗号/空白为分隔，宁缺勿错防误切）。**单个分隔符不合并**——列间空字段
// 有位置语义（如「书名｜钩子｜｜节奏」=爽点缺位），合并会错位。
var benchmarkFieldSepRe = regexp.MustCompile(`[|｜\t]`)

// benchmarkBookHeaders 表头首字段识别（整行跳过，防止表头被存成样本）。
var benchmarkBookHeaders = map[string]bool{
	"书名": true, "标题": true, "作品名": true, "书名/标题": true, "书名/作品名": true,
}

// ParseBenchmarkBooks 解析粘贴的对标书文本为预览行集合。
// 返回 (解析成功的行, 被跳过的原始行)；空文本报错，全跳过不报错（行集为空）。
// 同书名重复行以最后一条为准（与 upsert「最新粘贴胜」一致）。
func ParseBenchmarkBooks(text string) ([]BenchmarkBookRow, []string) {
	var rows []BenchmarkBookRow
	var skipped []string
	seen := map[string]int{} // title → rows 下标（同书名后行覆盖）
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := benchmarkFieldSepRe.Split(line, -1)
		// 只去首尾空字段（行首/行尾分隔符噪声）；列间空字段保留——
		// 位置语义（钩子/爽点/节奏列）靠下标对齐，空列=该特征缺位。
		clean := make([]string, 0, len(fields))
		for _, f := range fields {
			if f = strings.TrimSpace(f); f != "" || len(clean) > 0 {
				clean = append(clean, f)
			}
		}
		for len(clean) > 0 && clean[len(clean)-1] == "" {
			clean = clean[:len(clean)-1]
		}
		if len(clean) == 0 {
			continue
		}
		if benchmarkBookHeaders[clean[0]] {
			skipped = append(skipped, raw)
			continue
		}
		if len(clean) > benchmarkBookMaxFields {
			skipped = append(skipped, raw)
			continue
		}
		row := BenchmarkBookRow{Title: clean[0]}
		if len(clean) > 1 {
			row.HookType = clean[1]
		}
		if len(clean) > 2 {
			row.PleasureType = clean[2]
		}
		if len(clean) > 3 {
			row.RhythmShape = clean[3]
		}
		if len(clean) > 4 {
			row.Track = clean[4]
		}
		if len(clean) > 5 {
			row.Notes = clean[5]
		}
		// 结构特征样本必带至少一项特征（钩子/爽点/节奏），且书名必填。
		if row.Title == "" ||
			(row.HookType == "" && row.PleasureType == "" && row.RhythmShape == "") {
			skipped = append(skipped, raw)
			continue
		}
		if idx, ok := seen[row.Title]; ok {
			rows[idx] = row
			continue
		}
		seen[row.Title] = len(rows)
		rows = append(rows, row)
	}
	return rows, skipped
}

// ReplaceBenchmarkBooks 幂等覆盖写入一批对标书样本：同 (novel_id, title) upsert
// （最新粘贴胜）。书名必填，钩子/爽点/节奏至少一项非空（宁缺勿错）。
func (s *Store) ReplaceBenchmarkBooks(ctx context.Context, novelID int64, rows []BenchmarkBook) error {
	if novelID <= 0 {
		return errors.New("quality store: novel_id 缺失")
	}
	if len(rows) == 0 {
		return errors.New("quality store: 对标书样本行集为空")
	}
	for i := range rows {
		r := &rows[i]
		r.Title = strings.TrimSpace(r.Title)
		r.HookType = strings.TrimSpace(r.HookType)
		r.PleasureType = strings.TrimSpace(r.PleasureType)
		r.RhythmShape = strings.TrimSpace(r.RhythmShape)
		r.Track = strings.TrimSpace(r.Track)
		r.Notes = strings.TrimSpace(r.Notes)
		if r.Title == "" {
			return fmt.Errorf("quality store: 对标书样本第 %d 行缺书名", i+1)
		}
		if r.HookType == "" && r.PleasureType == "" && r.RhythmShape == "" {
			return fmt.Errorf("quality store: 对标书样本第 %d 行缺结构特征（钩子/爽点/节奏至少一项）", i+1)
		}
		if r.NovelID == 0 {
			r.NovelID = novelID
		}
		if r.NovelID != novelID {
			return fmt.Errorf("quality store: 对标书样本第 %d 行 novel_id 不一致", i+1)
		}
	}
	if err := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "novel_id"}, {Name: "title"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"hook_type", "pleasure_type", "rhythm_shape", "track", "notes", "updated_at",
			}),
		}).
		Create(&rows).Error; err != nil {
		return fmt.Errorf("quality store: upsert 对标书样本: %w", err)
	}
	return nil
}

// ListBenchmarkBooks 列出某书全部对标书样本，按录入顺序（id）升序。
func (s *Store) ListBenchmarkBooks(ctx context.Context, novelID int64) ([]BenchmarkBook, error) {
	var rows []BenchmarkBook
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("id").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("quality store: list 对标书样本: %w", err)
	}
	return rows, nil
}

// DeleteBenchmarkBook 删除单条对标书样本（novel_id 限定防跨书误删）。
func (s *Store) DeleteBenchmarkBook(ctx context.Context, novelID, id int64) error {
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND id = ?", novelID, id).
		Delete(&BenchmarkBook{}).Error; err != nil {
		return fmt.Errorf("quality store: delete 对标书样本: %w", err)
	}
	return nil
}

// DeleteBenchmarkBooks 清空某书全部对标书样本（面板「清空」用）。
func (s *Store) DeleteBenchmarkBooks(ctx context.Context, novelID int64) error {
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Delete(&BenchmarkBook{}).Error; err != nil {
		return fmt.Errorf("quality store: delete 对标书样本: %w", err)
	}
	return nil
}

// 摘要块体量封顶（提示词体量可控；截断如实标注）。
const (
	benchmarkBlockMaxBooks      = 20  // 最多列 20 本
	benchmarkBlockMaxFieldRunes = 120 // 单字段最多 120 rune
)

// RenderBenchmarkBlock 把对标书样本清单渲染为「对标书库摘要」文本块。
//
// 渲染纪律：
//   - 每本一行：书名 [赛道]：钩子/爽点/节奏（+备注），字段体量封顶截断如实标注；
//   - 体量封顶：benchmarkBlockMaxBooks 本，超出截断并标注余量；
//   - 尾部证据纪律行：dim2（题材契合 fit）评分以样本结构特征举证，
//     不得仅凭模型记忆写「对标 XX 书」；
//   - 空清单返回空串——无样本=不注入（提示词与无该块逐字一致）。
func RenderBenchmarkBlock(rows []BenchmarkBook) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "【对标书库摘要】赛道标杆书结构特征样本（钩子类型/爽点类型/节奏形态），"+
		"题材契合（fit）维度评分举证依据（共 %d 本）：\n", len(rows))
	shown := rows
	truncated := 0
	if len(rows) > benchmarkBlockMaxBooks {
		shown = rows[:benchmarkBlockMaxBooks]
		truncated = len(rows) - benchmarkBlockMaxBooks
	}
	for _, r := range shown {
		b.WriteString(benchmarkBookLine(r) + "\n")
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "- …（余 %d 本略，体量封顶）\n", truncated)
	}
	b.WriteString("证据纪律：题材契合（fit）的「与同题材标杆异同」须以本摘要样本的钩子/爽点/节奏" +
		"结构特征逐条对照举证，不得仅凭印象写「对标 XX 书」；样本缺位的特征如实说明，宁缺勿错。")
	return strings.TrimRight(b.String(), "\n")
}

// benchmarkBookLine 渲染单本样本行。
func benchmarkBookLine(r BenchmarkBook) string {
	line := fmt.Sprintf("- 《%s》", benchmarkField(r.Title))
	if track := benchmarkField(r.Track); track != "" {
		line += fmt.Sprintf("[%s]", track)
	}
	var parts []string
	if hook := benchmarkField(r.HookType); hook != "" {
		parts = append(parts, "钩子="+hook)
	}
	if pleasure := benchmarkField(r.PleasureType); pleasure != "" {
		parts = append(parts, "爽点="+pleasure)
	}
	if rhythm := benchmarkField(r.RhythmShape); rhythm != "" {
		parts = append(parts, "节奏="+rhythm)
	}
	if len(parts) == 0 {
		// 防御渲染（存储层已保证至少一项特征）：如实标注缺位，不编造。
		line += "：结构特征未录入"
	} else {
		line += "：" + strings.Join(parts, "；")
	}
	if notes := benchmarkField(r.Notes); notes != "" {
		line += "；备注=" + notes
	}
	return line
}

// benchmarkField 归一化单字段展示（去空白 + rune 级截断，空=省略该段）。
func benchmarkField(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) > benchmarkBlockMaxFieldRunes {
		return string(r[:benchmarkBlockMaxFieldRunes]) + "…"
	}
	return s
}
