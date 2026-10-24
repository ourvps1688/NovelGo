package timeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChapterTaskCard 是「章纲任务卡」——每章写作前必须立的结构化契约（四件套）。
//
// 设计动机（docs/future/pipeline-core-redesign-proposal.md §9.1）：长篇网文的质量
// 主要「规划出来」而非「检查出来」——每章为什么写、写什么，从技能文案约定升为
// 机器可验契约。四件套：
//   - HookTask    钩子任务：本章埋/收哪个坑 + 章尾断章钩设计（追读的生命线）
//   - BeatTask    爽点任务：爽点类型与情绪落点（情绪曲线的落点）
//   - MomentTask  人设时刻：哪位角色的高光/反差时刻（人设卖点的兑现）
//   - AdvanceTask 推进任务：推进哪条主线 / 卷纲功能位（宏观结构的位置）
//
// 约定：
//   - (novel_id, target_reading_number) 唯一——一章一卡，重复立卡整卡覆盖（无历史留痕，
//     与 ChapterPlan 同思路：卡表达「当前规划状态」而非历史记录）。
//   - 四件套齐全才可落库（Validate 缺一拒收）：机器校验是本表存在的意义。
//   - TargetReadingNumber 与 time_entries 的 target_reading_number 同口径：阅读序号锚点，
//     未来章节尚无 ID 时也可立卡；章节重组后由 LLM 实时校准（与伏笔同一维护纪律）。
//   - ChapterID 可空：立卡时尚无章节记录，审批建章后由章节创建回填钩子
//     （chapter.SetCreatedHook → Store.BindChapterID）绑定。
//   - 属规划数据而非系列圣经事实，不进 snapshot 冻结/审计范围。
type ChapterTaskCard struct {
	ID                  int64     `gorm:"column:id;primaryKey;autoIncrement"          json:"id"`
	NovelID             int64     `gorm:"column:novel_id;not null;uniqueIndex:idx_task_cards_novel_reading" json:"novel_id"`
	TargetReadingNumber int       `gorm:"column:target_reading_number;not null;uniqueIndex:idx_task_cards_novel_reading" json:"target_reading_number"`
	ChapterID           *int64    `gorm:"column:chapter_id;index"                      json:"chapter_id,omitempty"`
	HookTask            string    `gorm:"column:hook_task;not null"                    json:"hook_task"`
	BeatTask            string    `gorm:"column:beat_task;not null"                    json:"beat_task"`
	MomentTask          string    `gorm:"column:moment_task;not null"                  json:"moment_task"`
	AdvanceTask         string    `gorm:"column:advance_task;not null"                 json:"advance_task"`
	Source              string    `gorm:"column:source"                                json:"source"` // "ai" | "user"
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime"           json:"created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at;autoUpdateTime"           json:"updated_at"`
}

// TableName 指定 GORM 表名。
func (ChapterTaskCard) TableName() string { return "chapter_task_cards" }

// taskCardField 是四件套的字段定义（错误提示单源，字段名与 json/列名对齐）。
type taskCardField struct {
	Name  string // 错误提示里的字段名（中文+json 键）
	Value *string
}

// Validate 校验任务卡契约：TargetReadingNumber >= 1，且四件套齐全（TrimSpace 后非空）。
// 缺失时错误信息逐项列出缺失字段——这是机器可验契约的拒收口径。
func (c *ChapterTaskCard) Validate() error {
	if c.TargetReadingNumber < 1 {
		return fmt.Errorf("章纲任务卡缺阅读序号（target_reading_number 须 >= 1）")
	}
	fields := []taskCardField{
		{Name: "钩子任务(hook_task)", Value: &c.HookTask},
		{Name: "爽点任务(beat_task)", Value: &c.BeatTask},
		{Name: "人设时刻(moment_task)", Value: &c.MomentTask},
		{Name: "推进任务(advance_task)", Value: &c.AdvanceTask},
	}
	var missing []string
	for _, f := range fields {
		trimmed := strings.TrimSpace(*f.Value)
		*f.Value = trimmed
		if trimmed == "" {
			missing = append(missing, f.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("章纲任务卡四件套缺 %s（钩子/爽点/人设时刻/推进 必须齐全，缺一拒收）", strings.Join(missing, "、"))
	}
	return nil
}

// SaveTaskCard 立/覆盖某章任务卡（(novel_id, target_reading_number) 唯一，重复调用整卡覆盖）。
// 先过 Validate：四件套不齐直接拒收，不落库。
func (s *Store) SaveTaskCard(ctx context.Context, card *ChapterTaskCard) error {
	if err := card.Validate(); err != nil {
		return err
	}
	return s.DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "novel_id"}, {Name: "target_reading_number"}},
		DoUpdates: clause.AssignmentColumns([]string{"chapter_id", "hook_task", "beat_task", "moment_task", "advance_task", "source", "updated_at"}),
	}).Create(card).Error
}

// GetTaskCard 取某章任务卡。不存在时返回 gorm.ErrRecordNotFound（调用方按需转业务文案）。
func (s *Store) GetTaskCard(ctx context.Context, novelID int64, readingNumber int) (*ChapterTaskCard, error) {
	var card ChapterTaskCard
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND target_reading_number = ?", novelID, readingNumber).
		First(&card).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

// BindChapterID 回填任务卡的 chapter_id（ChapterID 回填钩子：章节创建后按阅读序号绑定）。
// 只填「尚未绑定」的卡（chapter_id IS NULL）——回填语义，绝不覆盖既有绑定（宁缺勿错）。
// tx 可空：传入时绑定与章节创建同事务（原子生效，创建回滚时绑定一并回滚）。
// 绑定成功返回 true；该阅读序号无卡或已绑定返回 false。落库失败返回 error（调用方尽力降级）。
func (s *Store) BindChapterID(ctx context.Context, tx *gorm.DB, novelID, chapterID int64, readingNumber int) (bool, error) {
	if readingNumber < 1 || chapterID <= 0 {
		return false, nil
	}
	db := s.DB
	if tx != nil {
		db = tx
	}
	res := db.WithContext(ctx).Model(&ChapterTaskCard{}).
		Where("novel_id = ? AND target_reading_number = ? AND chapter_id IS NULL", novelID, readingNumber).
		Update("chapter_id", chapterID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// GetTaskCardByChapterID 按已绑定的 chapter_id 取任务卡。
// 未绑定 / 不存在时返回 gorm.ErrRecordNotFound（调用方可再按阅读序号兜底）。
func (s *Store) GetTaskCardByChapterID(ctx context.Context, novelID, chapterID int64) (*ChapterTaskCard, error) {
	var card ChapterTaskCard
	if err := s.DB.WithContext(ctx).
		Where("novel_id = ? AND chapter_id = ?", novelID, chapterID).
		First(&card).Error; err != nil {
		return nil, err
	}
	return &card, nil
}

// HasAnyTaskCard 判断本书是否已启用任务卡工作流（存在任一卡）。
// 供「宁可漏拦」口径使用：纯即兴写作（整本零卡）不打扰。
func (s *Store) HasAnyTaskCard(ctx context.Context, novelID int64) (bool, error) {
	var n int64
	if err := s.DB.WithContext(ctx).Model(&ChapterTaskCard{}).
		Where("novel_id = ?", novelID).Limit(1).Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
