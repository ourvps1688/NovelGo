package mcp_tools

// #119 契约：字数本地确定性判定不再触发整章自动重写；写后维护子 Agent
// 有独立预算、脱离主 chat 可见流。纯函数单测 + 静态全链锁防回潮。

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/novelgo/novelgo/internal/quality"
)

func platformWindow() quality.ChapterWordsTarget {
	return quality.ChapterWordsTarget{
		Min: 3000, Max: 4000, Target: 4000,
		Source: "platform_card", Label: "平台约束卡 3000–4000 字",
	}
}

func TestWordShortfallPrescription(t *testing.T) {
	w := platformWindow()
	assert.Empty(t, wordShortfallPrescription(3000, w), "恰好达标不处方")
	assert.Empty(t, wordShortfallPrescription(3500, w), "超额不处方")

	got := wordShortfallPrescription(1585, w) // 2026-11-14 第一遍真实字数
	assert.Contains(t, got, "1585")
	assert.Contains(t, got, "3000–4000")
	assert.Contains(t, got, "1415", "差额必须由区间下限本地算出")
	assert.Contains(t, got, w.WindowName(), "口径名必须取自单源")
	assert.Contains(t, got, "chat", "补写必须走 chat，不得后台自动改稿")
	assert.Contains(t, got, "不会整章退回重写")
	assert.Contains(t, got, "不会自动补写")
	assert.Contains(t, got, "请把该差额如实告知作者")
	assert.NotContains(t, got, "系统已退回重写")

	// 无区间（default 口径）时回落既有评分门禁，逐字不变。
	noWindow := quality.ChapterWordsTarget{Target: 2000, Source: "default"}
	assert.Empty(t, wordShortfallPrescription(500, noWindow))
}

func TestDetachedPostWriteContextIsIndependentAndBounded(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	bg, cancelBG := detachedPostWriteContext(parent)
	defer cancelBG()
	require.NoError(t, bg.Err(), "主 ctx 已取消，写后维护仍应能收口")
	deadline, ok := bg.Deadline()
	require.True(t, ok, "必须有独立超时，不得无限续命")
	assert.InDelta(t, postWriteSubAgentTimeout.Seconds(), time.Until(deadline).Seconds(), 2)
}

func TestDetachedPostWriteSubAgentMarksRequest(t *testing.T) {
	var got SubAgentRequest
	wrapped := detachedPostWriteSubAgent(func(_ context.Context, req SubAgentRequest) (string, error) {
		got = req
		return "ok", nil
	})
	require.NotNil(t, wrapped)
	report, err := wrapped(context.Background(), SubAgentRequest{
		AgentType: "chapterci", NovelID: 27, MaxTurns: 12,
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", report)
	assert.True(t, got.Detached)
	assert.Equal(t, "chapterci", got.AgentType, "其余字段逐字不动")
	assert.Equal(t, 12, got.MaxTurns)
	assert.Nil(t, detachedPostWriteSubAgent(nil))
}

func TestNoAutoRewriteAndDetachedWiring(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(name)
		require.NoError(t, err)
		return string(b)
	}
	rw := read("rw_tools.go")
	shortAt := strings.Index(rw, "if shortfall := wordShortfallPrescription")
	require.GreaterOrEqual(t, shortAt, 0, "字数本地判定接线缺失")
	incAt := strings.Index(rw, "attempts.Increment(")
	require.Greater(t, incAt, shortAt, "字数不足必须在重试计数/退回重写之前放行")
	seg := rw[shortAt:incAt]
	assert.Contains(t, seg, "return &score, nil")
	assert.NotContains(t, seg, "ErrKindQuality", "字数分支绝不能产出退回重写结果")
	assert.Contains(t, rw, "章节字数不足，放行不退回整章重写")
	// 非字数质量问题仍保留原门禁。
	assert.Contains(t, rw, "系统已退回重写")
	// 处方必须透出到 chat（quality_report 前端零消费面），且只限成章写。
	assert.Contains(t, rw, `data["word_shortfall"] = p`)
	assert.Contains(t, rw, `ref.IsNew || a.ChangeType == "full_replace"`)

	policy := read("post_write_policy.go")
	for _, want := range []string{
		"context.WithoutCancel(ctx)", "context.WithTimeout",
		"postWriteSubAgentTimeout", "req.Detached = true",
	} {
		assert.Contains(t, policy, want, "写后维护策略缺少 "+want)
	}

	ci := read("chapter_ci.go")
	assert.Contains(t, ci, "detachedPostWriteContext(ctx)")
	assert.Contains(t, ci, "detachedPostWriteSubAgent(tc.RunSubAgent)")
	assert.NotContains(t, ci, "runChapterL1(context.WithoutCancel(ctx)")
	assert.NotContains(t, rw, "runStateSync(context.WithoutCancel(ctx)")
	assert.Contains(t, ci, "chapter-ci 写后维护超时收口")
	assert.Contains(t, rw, "state-sync 写后维护超时收口")

	assert.Contains(t, read("base.go"), "Detached bool")
	pres := read("word_count_prescription.go")
	for _, banned := range []string{
		"RunSubAgent", "Schedule", "Dispatch", "Upload", "autoFix",
	} {
		assert.NotContains(t, pres, banned, "字数处方不得含派发/自动修复/上传")
	}
}
