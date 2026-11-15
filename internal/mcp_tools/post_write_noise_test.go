package mcp_tools

// A 方案契约：写后链零后台 LLM 派发。
//
// 旧 #119/#121/#122 的 Detached 独立预算、去抖器串行化、失败不补跑策略已随
// 「撤销后台自动 LLM」整链退役（post_write_policy.go 删除）。这里锁住新不变量：
// 写稿落盘只做本地确定性 L0，L1 / 台账同步只由用户显式触发，调度脚手架不得回潮。

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readWriteChainSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取写后链源文件 %s", rel)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// TestNoBackgroundLLMAfterChapterWrite 写钩子不得再挂任何子 Agent / 去抖 / 后台预算。
func TestNoBackgroundLLMAfterChapterWrite(t *testing.T) {
	rw := readWriteChainSource(t, "rw_tools.go")
	ci := readWriteChainSource(t, "chapter_ci.go")
	for _, banned := range []string{
		"scheduleStateSync(", "stateSyncDebouncer", "chapterCIDebouncer",
		"detachedPostWriteContext(", "detachedPostWriteSubAgent(",
		"runChapterL1(", "runStateSync(", "newPostWriteDebouncer(",
		"stateSyncDebounce", "chapterCIDebounce",
	} {
		assert.NotContains(t, rw, banned, "rw_tools.go 不得复活后台派发/调度：%s", banned)
		assert.NotContains(t, ci, banned, "chapter_ci.go 不得复活后台派发/调度：%s", banned)
	}
	// 显式执行体必须仍在：L1 复核由用户点「重新检测本章」触发，台账同步由 S0/chat 触发。
	assert.Contains(t, ci, "func RunChapterCIForChapter(")
	assert.Contains(t, rw, "func RunStateSyncForChapter(")
	assert.Contains(t, rw, "ScheduleChapterCI(ctx, tc, ch, proposed)")
	// 已退役的调度脚手架文件不得复活。
	_, err := os.Stat("post_write_policy.go")
	assert.True(t, os.IsNotExist(err), "post_write_policy.go 已退役，不得重建")
}

// TestChapterWriteHookIsPureLocalL0 写钩子函数体零派发、零 goroutine、零 LLM。
func TestChapterWriteHookIsPureLocalL0(t *testing.T) {
	ci := readWriteChainSource(t, "chapter_ci.go")
	start := strings.Index(ci, "func ScheduleChapterCI(")
	require.GreaterOrEqual(t, start, 0)
	rest := ci[start:]
	end := strings.Index(rest, "\n}\n")
	require.Greater(t, end, 0)
	body := rest[:end]
	assert.Contains(t, body, "runChapterL0(ctx, tc, ch.ID, content)")
	for _, banned := range []string{
		"RunSubAgent", "RunChapterCIForChapter", "go func",
		"Schedule(", "time.AfterFunc", "context.WithoutCancel",
	} {
		assert.NotContains(t, body, banned, "写钩子必须纯本地：%s", banned)
	}
}

// TestL0RecalcInvalidatesPreviousL1 按正文版本记账：L0 重算成功即作废旧版 L1。
func TestL0RecalcInvalidatesPreviousL1(t *testing.T) {
	ci := readWriteChainSource(t, "chapter_ci.go")
	assert.Contains(t, ci, "qs.InvalidateL1(ctx, tc.NovelID, chapterID)")
	st := readWriteChainSource(t, "../quality/store.go")
	assert.Contains(t, st, "func (s *Store) InvalidateL1(")
	assert.Contains(t, st, `"l1_status":   L1StatusPending`)
	assert.Contains(t, st, `"l1_findings": MarshalFindings(nil)`)
}

// TestNoAutoFixAfterWrite 纯本地链不得夹带自动修复 / 自动派发 / 上传。
func TestNoAutoFixAfterWrite(t *testing.T) {
	src := readWriteChainSource(t, "rw_tools.go") +
		readWriteChainSource(t, "chapter_ci.go") +
		readWriteChainSource(t, "../quality/store.go")
	for _, banned := range []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets", "autoFix",
		"StartPipeline", "Upload", "http.Post",
	} {
		assert.NotContains(t, src, banned, "写后链不得含自动修复/执行体/上传：%s", banned)
	}
}
