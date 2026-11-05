package quality

// P1-2 真实数据对账：七猫创作者后台逐章真实数据（完读率/追读率）人工粘贴录入。
//
// 契约要点（勿改）：
//   - 数据形态：粘贴表格文本，每行「章号 [完读率] [追读率]」——章号支持纯数字或
//     「第N章/第N话/章N/N章/第N」，率支持带/不带 %（半/全角），分隔符 Tab/逗号/
//     分号/顿号/竖线/空白均可；表头与无法解析的行跳过并回报（宁缺勿错不硬造）；
//   - 率归一：带 % 原样取用；不带 % 且 0<v≤1 视为小数（×100），否则视为百分数。
//     归一后区间 [0,100]，越界行跳过；
//   - 章号=全局阅读序号（reading_number，1-based，与工单 chapter_ids 同口径），
//     本层只存 ReadingNumber，由上层解析为 chapter_id 落库；
//   - 幂等：同 (novel_id, chapter_id) upsert，最新粘贴覆盖（batch 仅作来源标签）。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// PlatformMetric 一章的平台真实数据（七猫后台，人工粘贴）。
type PlatformMetric struct {
	ID            int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	NovelID       int64 `gorm:"column:novel_id;not null;uniqueIndex:idx_platform_metric_chapter,priority:1" json:"novel_id"`
	ChapterID     int64 `gorm:"column:chapter_id;not null;uniqueIndex:idx_platform_metric_chapter,priority:2" json:"chapter_id"`
	ReadingNumber int   `gorm:"column:reading_number;not null" json:"reading_number"`
	// CompletionRate/RetentionRate 为完读率/追读率（归一后百分比 0-100）。
	// 缺失列存 NULL（nil），消费侧据此区分「未录入」与「0%」。
	CompletionRate *float64  `gorm:"column:completion_rate" json:"completion_rate"`
	RetentionRate  *float64  `gorm:"column:retention_rate" json:"retention_rate"`
	Batch          string    `gorm:"column:batch" json:"batch"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 返回物理表名。
func (PlatformMetric) TableName() string { return "platform_metrics" }

// PlatformMetricRow 是粘贴解析的预览行（未落库，无 novel/chapter 归属）。
type PlatformMetricRow struct {
	ReadingNumber  int      `json:"reading_number"`
	CompletionRate *float64 `json:"completion_rate"`
	RetentionRate  *float64 `json:"retention_rate"`
}

// 章号形态：纯数字 / 第N章 / 第N话 / 章N / N章 / 第N。
var platformChapterNoRe = regexp.MustCompile(`^(?:第)?(\d+)(?:章|话)?$|^章(\d+)$`)

// 字段分隔：Tab/逗号/分号/顿号/竖线/空白（连续视为一个分隔）。
var platformFieldSepRe = regexp.MustCompile(`[\t,;，；、|]+|\s+`)

// platformRateRe 率字段：数字 + 可选半/全角百分号。
var platformRateRe = regexp.MustCompile(`^(\d+(?:\.\d+)?|\.\d+)\s*[%％]?$`)

// parsePlatformChapterNo 解析章号字段（全局阅读序号）；无法解析返回 0。
func parsePlatformChapterNo(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	m := platformChapterNoRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	digits := m[1]
	if digits == "" {
		digits = m[2]
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// parsePlatformRate 解析率字段并归一到百分比 [0,100]。
// 带 % 原样取用；不带 % 且 0<v≤1 视为小数 ×100；越界返回 error（宁缺勿错）。
func parsePlatformRate(s string) (float64, error) {
	s = strings.TrimSpace(s)
	m := platformRateRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("quality: 非法平台数据率 %q", s)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("quality: 非法平台数据率 %q: %w", s, err)
	}
	hadPercent := strings.ContainsAny(s, "%％")
	if !hadPercent && v > 0 && v <= 1 {
		v *= 100
	}
	if v < 0 || v > 100 {
		return 0, fmt.Errorf("quality: 平台数据率越界 %q（归一后 %.2f）", s, v)
	}
	return v, nil
}

// ParsePlatformMetrics 解析粘贴的平台数据文本为预览行集合。
// 返回 (解析成功的行, 被跳过的原始行)；空文本报错，全跳过不报错（行集为空）。
// 同章重复行以最后一条为准（与 upsert「最新粘贴胜」一致）。
func ParsePlatformMetrics(text string) ([]PlatformMetricRow, []string) {
	var rows []PlatformMetricRow
	var skipped []string
	seen := map[int]int{} // reading_number → rows 下标（同章后行覆盖）
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := platformFieldSepRe.Split(line, -1)
		// 去掉分隔产生的空字段（行首/行尾分隔）。
		clean := make([]string, 0, len(fields))
		for _, f := range fields {
			if f = strings.TrimSpace(f); f != "" {
				clean = append(clean, f)
			}
		}
		if len(clean) == 0 {
			continue
		}
		no := parsePlatformChapterNo(clean[0])
		if no == 0 {
			skipped = append(skipped, raw)
			continue
		}
		if len(clean) < 2 {
			skipped = append(skipped, raw)
			continue
		}
		completion, err := parsePlatformRate(clean[1])
		if err != nil {
			skipped = append(skipped, raw)
			continue
		}
		var retention *float64
		if len(clean) >= 3 {
			v, err := parsePlatformRate(clean[2])
			if err != nil {
				skipped = append(skipped, raw)
				continue
			}
			retention = &v
		}
		row := PlatformMetricRow{
			ReadingNumber:  no,
			CompletionRate: &completion,
			RetentionRate:  retention,
		}
		if idx, ok := seen[no]; ok {
			rows[idx] = row
			continue
		}
		seen[no] = len(rows)
		rows = append(rows, row)
	}
	return rows, skipped
}

// ReplacePlatformMetrics 幂等覆盖写入一批逐章平台数据：同 (novel_id, chapter_id)
// upsert（最新粘贴胜），batch 仅作来源标签记录。
func (s *Store) ReplacePlatformMetrics(ctx context.Context, novelID int64, batch string, rows []PlatformMetric) error {
	if novelID <= 0 {
		return errors.New("quality store: novel_id 缺失")
	}
	if len(rows) == 0 {
		return errors.New("quality store: 平台数据行集为空")
	}
	for i := range rows {
		r := &rows[i]
		if r.ChapterID <= 0 {
			return fmt.Errorf("quality store: 平台数据第 %d 行缺 chapter_id", i+1)
		}
		if r.NovelID == 0 {
			r.NovelID = novelID
		}
		if r.NovelID != novelID {
			return fmt.Errorf("quality store: 平台数据第 %d 行 novel_id 不一致", i+1)
		}
		if r.CompletionRate != nil && (*r.CompletionRate < 0 || *r.CompletionRate > 100) {
			return fmt.Errorf("quality store: 平台数据第 %d 行完读率越界", i+1)
		}
		if r.RetentionRate != nil && (*r.RetentionRate < 0 || *r.RetentionRate > 100) {
			return fmt.Errorf("quality store: 平台数据第 %d 行追读率越界", i+1)
		}
		r.Batch = batch
	}
	if err := s.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "novel_id"}, {Name: "chapter_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"reading_number", "completion_rate", "retention_rate", "batch", "updated_at",
			}),
		}).
		Create(&rows).Error; err != nil {
		return fmt.Errorf("quality store: upsert 平台数据: %w", err)
	}
	return nil
}

// ListPlatformMetrics 列出某书全部平台数据行，按全局阅读序号升序。
func (s *Store) ListPlatformMetrics(ctx context.Context, novelID int64) ([]PlatformMetric, error) {
	var rows []PlatformMetric
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Order("reading_number, id").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("quality store: list 平台数据: %w", err)
	}
	return rows, nil
}

// DeletePlatformMetrics 清空某书全部平台数据（面板「清空重录」用）。
func (s *Store) DeletePlatformMetrics(ctx context.Context, novelID int64) error {
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ?", novelID).
		Delete(&PlatformMetric{}).Error; err != nil {
		return fmt.Errorf("quality store: delete 平台数据: %w", err)
	}
	return nil
}
