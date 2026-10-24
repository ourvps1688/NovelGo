package chapter

import (
	"context"

	"gorm.io/gorm"
)

// CreatedHook 是章节记录创建成功后的外围维护钩子——
// 当前用途：回填章纲任务卡的 chapter_id（timeline.Store.BindChapterID）。
//
// 设计注记：Store 实例在 rw_tools 等调用点按需临时新建，实例字段覆盖不到全部
// 创建路径，因此钩子是进程级单例，由 app 启动期注入一次。tx 是章节创建所处的
// 事务（可空）：绑定写入走同一事务，创建回滚时绑定一并回滚（原子一致）。
// 纪律：钩子内失败只自行告警，绝不影响章节创建；notifyChapterCreated 兜底 recover。
type CreatedHook func(ctx context.Context, tx *gorm.DB, novelID, chapterID int64, readingNumber int)

// createdHook 进程级单例钩子（nil=未注入，跳过）。
var createdHook CreatedHook

// SetCreatedHook 注入/清除章节创建回填钩子（app 启动期调用；传 nil 清除，测试用）。
func SetCreatedHook(h CreatedHook) { createdHook = h }

// notifyChapterCreated 在章节创建成功后触发回填钩子：阅读序号查询失败或钩子自身
// 异常都只记日志（降级零报错），不影响章节创建（宁缺勿错）。
func (s *Store) notifyChapterCreated(ctx context.Context, tx *gorm.DB, novelID, chapterID int64) {
	if createdHook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.logger.Warn("章节创建回填钩子异常（已忽略）", "novel_id", novelID, "chapter_id", chapterID, "panic", r)
		}
	}()
	readingNumber, err := s.GetReadingNumberByID(ctx, tx, novelID, chapterID)
	if err != nil {
		s.logger.Warn("章节创建回填：阅读序号查询失败（跳过）", "novel_id", novelID, "chapter_id", chapterID, "err", err)
		return
	}
	createdHook(ctx, tx, novelID, chapterID, readingNumber)
}
