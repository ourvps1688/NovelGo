package app

import (
	"context"
	"fmt"
	"time"

	"github.com/novelgo/novelgo/internal/mcp_tools"
)

// chapterLedgerSyncTimeout 是单次显式台账同步的执行上限。
// 与 A 方案的「零后台自动 LLM」并不冲突：这里没有写后钩子、没有去抖器、
// 没有独立后台预算或续命逻辑，只在作者点按钮时跑一次，到点即如实收口。
const chapterLedgerSyncTimeout = 15 * time.Minute

// SyncChapterLedger 是作者显式触发的单章台账同步入口（A 方案）。
//
// 写章落盘不再自动派发 statesync 子 Agent，台账因此不会自己更新；本方法把
// 「同步本章」变成界面上可见、可控的一次操作。它只委托既有的
// mcp_tools.RunStateSyncForChapter（S0 对齐 / chat 显式工具共用同一执行体），
// 不改章节正文、不触发 L1、不自动重试、不写任何后台任务。
func (a *App) SyncChapterLedger(novelID, chapterID int64) error {
	if novelID <= 0 || chapterID <= 0 {
		return fmt.Errorf("小说与章节不能为空")
	}
	if a.db == nil || a.agent == nil || a.llmClient == nil {
		return fmt.Errorf("服务未初始化")
	}
	ctx, cancel := context.WithTimeout(a.ctx, chapterLedgerSyncTimeout)
	defer cancel()
	return mcp_tools.RunStateSyncForChapter(ctx, a.db, a.skill, a.runBackgroundSubAgent,
		a.logger, "manual-chapter-ledger-sync", novelID, chapterID)
}
