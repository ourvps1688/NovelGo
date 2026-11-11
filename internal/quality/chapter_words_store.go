package quality

// chapter_words_store.go —— ChapterWordsTarget 的 DB 装配口（#113）。
//
// 只做两件事：读本书检查器配置里的平台 profile、读偏好自由文本（平台卡/题材卡），
// 再交给纯函数 ResolveChapterWords 做唯一一次决策。不 import preference 包，
// 用原生列名取 content，避免 llm/preference/quality 之间出现循环依赖。
//
// 降级纪律：任一步读取失败都不报错、按已拿到的信息解析；全失败回落 default，
// 与既有缺省行为逐字一致（宁缺勿错）。

import (
	"context"
)

// ChapterWords 解析本书当前生效的单章字数口径（检测侧 L0、写作侧 system、
// 前端质量档案展示共用同一结果）。novelID<=0 时直接返回 default。
func (s *Store) ChapterWords(ctx context.Context, novelID int64) ChapterWordsTarget {
	if s == nil || s.DB == nil || novelID <= 0 {
		return ResolveChapterWords(nil, nil)
	}
	var profile *PlatformProfile
	if cfg, err := s.GetCheckerConfig(ctx, novelID); err == nil && cfg != nil {
		profile = cfg.Profile()
	}
	var contents []string
	// 与 NovelProfile 同源：is_global 或 novel_id 命中的偏好文本，按模板 marker 识别。
	_ = s.DB.WithContext(ctx).
		Table("preference_items").
		Where("is_global = ? OR novel_id = ?", true, novelID).
		Pluck("content", &contents).Error
	return ResolveChapterWords(profile, contents)
}
