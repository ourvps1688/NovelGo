package app

// A 方案契约：写后零后台 LLM。台账同步只能由作者显式点「同步本章台账」触发，
// 执行体复用 S0 对齐 / chat 显式工具共用的 RunStateSyncForChapter，不新建后台链。

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSyncChapterLedgerIsExplicitOnly(t *testing.T) {
	src := readAppSource(t, "chapter_ledger_sync.go")
	assert.Contains(t, src, "func (a *App) SyncChapterLedger(novelID, chapterID int64) error")
	assert.Contains(t, src, "mcp_tools.RunStateSyncForChapter(")
	assert.Contains(t, src, "a.runBackgroundSubAgent")
	assert.Contains(t, src, "context.WithTimeout(a.ctx, chapterLedgerSyncTimeout)")
	for _, banned := range []string{
		"ScheduleChapterCI(", "scheduleStateSync(", "stateSyncDebouncer",
		"Detached:", "go func(", "RunChapterCIForChapter(", "RunDetectionTask(",
	} {
		assert.NotContains(t, src, banned, "显式同步不得复活写后自动链：%s", banned)
	}

	// 写后钩子仍只做本地 L0，绝不在这里触发台账同步或 L1 复核。
	rw := readAppSource(t, "../internal/mcp_tools/rw_tools.go")
	assert.NotContains(t, rw, "stateSyncDebouncer", "写后钩子不得复活去抖派发")
	assert.Contains(t, rw, "ScheduleChapterCI(ctx, tc, ch, proposed)")
}

func TestSyncChapterLedgerBindingsAndCopyAreWired(t *testing.T) {
	js := readAppSource(t, "../frontend/src/lib/wailsjs/go/app/App.js")
	assert.Contains(t, js, "export function SyncChapterLedger(arg1, arg2)")
	dts := readAppSource(t, "../frontend/src/lib/wailsjs/go/app/App.d.ts")
	assert.Contains(t, dts, "export function SyncChapterLedger(arg1:number,arg2:number):Promise<void>;")

	view := readAppSource(t, "../frontend/src/components/skill/DetectionWorkflowView.tsx")
	assert.Contains(t, view, "SyncChapterLedger(novelId, effChapterId)")
	assert.Contains(t, view, "syncLedgerHint")

	// 面向 Agent 的提示词不得再说「后台自动同步」。
	identity := readAppSource(t, "../internal/agentcfg/identity.go")
	assert.NotContains(t, identity, "由后台自动完成")
	assert.NotContains(t, identity, "由后台同步保证")
	assert.Contains(t, identity, "不会**自动派发 statesync 子 Agent")
}
