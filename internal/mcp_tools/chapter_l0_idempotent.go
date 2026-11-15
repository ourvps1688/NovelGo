package mcp_tools

// chapter_l0_idempotent.go —— #129 写后 L0 重算的「同正文幂等短路」。
//
// 写章一个 turn 里模型会连续多次 edit，每次落盘都会走 ScheduleChapterCI →
// runChapterL0：UpsertL0（含 finding_round 轮次推进、checked_at 刷新）、
// RefreshTensionCurve（整段作废旧 L1 曲线）、情绪账工单整批替换。同一份落盘
// 正文被多条写后链重复处理时，这些副作用完全相同却要重复落库，还会把刚算出的
// resolved 冲成空（#124 的同轮保护只能挡住轮次，挡不住重复写入本身）。
//
// 这里只在「同一进程、同一章、同一份正文内容」下短路整套副作用：
//   - 指纹取正文内容的 sha256，不是字数/指标推断，绝不把改过的正文当成没变；
//   - 只有 L0 落档、张力曲线、工单三步全部成功才登记指纹，失败可自愈；
//   - 进程重启后缓存为空，第一次必定重算，DB 仍是真相源；
//   - RecalcChapterL0FromDisk（用户显式「重新检测本章」）不经过本短路，
//     永远按当前落盘正文强制重算，闭环语义逐字不变。
//
// 边界（如实说明）：连续多次**不同内容**的 edit 中间态仍会各算一次——它们确实
// 对应不同正文，合并需要把同步 L0 改成异步去抖，属另一项语义改动，本轮不做。
// 纯本地、零 LLM、零上传、零自动修复、不新增任何执行体。

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"gorm.io/gorm"
)

var (
	chapterL0FingerprintMu sync.Mutex
	chapterL0Fingerprints  = map[string][32]byte{}
)

// chapterL0CacheKey 把 DB 实例身份并入 key：生产里 app 注入的是同一个 *gorm.DB
// 指针，缓存正常共享；测试里每个用例各开一个内存库，互不误短路。
// DB 为 nil（降级路径）时退化为 "0x0"，同样不会跨实例串味。
func chapterL0CacheKey(db *gorm.DB, novelID, chapterID int64) string {
	return fmt.Sprintf("%p:%d:%d", db, novelID, chapterID)
}

// chapterL0AlreadyComputed 判断该章这份正文是否已经完整重算过 L0。
// 命中即跳过整套写后 L0 副作用；未命中时调用方负责执行并在成功后登记。
func chapterL0AlreadyComputed(db *gorm.DB, novelID, chapterID int64, content string) bool {
	fp := sha256.Sum256([]byte(content))
	key := chapterL0CacheKey(db, novelID, chapterID)
	chapterL0FingerprintMu.Lock()
	defer chapterL0FingerprintMu.Unlock()
	prev, ok := chapterL0Fingerprints[key]
	return ok && prev == fp
}

// chapterL0MarkComputed 只在整套 L0 副作用成功后登记指纹。
func chapterL0MarkComputed(db *gorm.DB, novelID, chapterID int64, content string) {
	fp := sha256.Sum256([]byte(content))
	key := chapterL0CacheKey(db, novelID, chapterID)
	chapterL0FingerprintMu.Lock()
	defer chapterL0FingerprintMu.Unlock()
	chapterL0Fingerprints[key] = fp
}

// resetChapterL0Fingerprints 供同包测试隔离用；生产代码不调用。
func resetChapterL0Fingerprints() {
	chapterL0FingerprintMu.Lock()
	defer chapterL0FingerprintMu.Unlock()
	chapterL0Fingerprints = map[string][32]byte{}
}
