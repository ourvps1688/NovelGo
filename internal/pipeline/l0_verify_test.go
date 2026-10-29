package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/quality"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
	"log/slog"
)

// l0TestService 构造一个最小可用的 pipeline.Service（内存 SQLite），仅迁移 chapters 表；
// 角色/声线/境界/题材卡等台账表缺失时，tryL0Verify 的装配函数会优雅返回空，与生产中台账为空同口径。
func l0TestService(t *testing.T) *Service {
	t.Helper()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}))
	return NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
}

// TestClassifyMechanicalChecks 验证关键词→L0 检查项映射与语义排除。
func TestClassifyMechanicalChecks(t *testing.T) {
	cases := []struct {
		area, desc string
		want       []string
	}{
		{"字数", "本章字数不足", []string{quality.CheckWordCount}},
		{"章尾钩子", "结尾缺少悬念", []string{quality.CheckTailHook}},
		{"AI味", "套话油腻机翻", []string{quality.CheckAIFlavor}},
		{"称呼混用", "同人异称别名混乱", []string{quality.CheckNameDrift}},
		{"死人出镜", "已死角色复活出镜", []string{quality.CheckDeadAppearance}},
		{"战力越级", "境界碾压低阶越阶", []string{quality.CheckPowerUpset, quality.CheckPowerCrush}},
		{"文风漂移", "语感笔调语调漂移", []string{quality.CheckStyleDrift}},
	}
	for _, c := range cases {
		checks, ambiguous := classifyMechanicalChecks(c.area, c.desc, "")
		require.False(t, ambiguous, "机械类工单不应判歧义: %q", c.area)
		for _, w := range c.want {
			require.True(t, checks[w], "工单 %q 应映射到 %s", c.area, w)
		}
	}

	// 语义矛盾信号 → 视为歧义，保守走 LLM。
	_, ambiguous := classifyMechanicalChecks("逻辑矛盾", "情节前后冲突人设崩塌", "")
	require.True(t, ambiguous, "含语义矛盾词应判歧义")

	// 空 area/desc → 非机械类。
	checks, ambiguous := classifyMechanicalChecks("", "", "")
	require.Nil(t, checks)
	require.False(t, ambiguous)

	// 无关键词命中（也不歧义）→ 非机械类，走 LLM。
	checks, ambiguous = classifyMechanicalChecks("其他", "一段普通描述", "")
	require.False(t, ambiguous)
	require.Empty(t, checks)
}

// TestL0Verdict 验证纯判定函数三分支：仍命中→退回待修、零命中且全可判定→自动确认、零命中但有缺失项→保守退回 LLM。
func TestL0Verdict(t *testing.T) {
	eff := map[string]bool{quality.CheckWordCount: true}

	// 1) 命中有效检查项 → decided=true, verified=false
	decided, verified, hit := l0Verdict([]quality.Finding{{Check: quality.CheckWordCount}}, eff, true)
	require.True(t, decided)
	require.False(t, verified)
	require.Equal(t, []string{quality.CheckWordCount}, hit)

	// 2) 有效项零命中且 allVerifiable → 自动确认通过（免 LLM）
	decided, verified, _ = l0Verdict(nil, eff, true)
	require.True(t, decided)
	require.True(t, verified)

	// 3) 有效项零命中但 !allVerifiable（有候选项因依赖缺失无法复核）→ 保守退回 LLM
	decided, verified, _ = l0Verdict(nil, eff, false)
	require.False(t, decided)
	require.False(t, verified)

	// 4) 命中项不在 effective 内 → 忽略，不影响判定
	decided, verified, hit = l0Verdict([]quality.Finding{{Check: quality.CheckTailHook}}, eff, true)
	require.True(t, decided)
	require.True(t, verified)
	require.Empty(t, hit)
}

// TestTryL0VerifyRouting 验证各类「走原 LLM 复验路径」的守卫分支（decided=false）。
func TestTryL0VerifyRouting(t *testing.T) {
	svc := l0TestService(t)
	ctx := context.Background()
	stub := func(novelID, chapterID int64) (string, bool) { return strings.Repeat("文", 1500), true }

	// 语义歧义 → 走 LLM
	decided, _, _ := svc.tryL0Verify(ctx, 1, "r1", PipelineIssue{Area: "逻辑矛盾", Desc: "情节冲突"}, stub)
	require.False(t, decided, "语义歧义应退回 LLM")

	// 空 area/desc → 走 LLM
	decided, _, _ = svc.tryL0Verify(ctx, 1, "r1", PipelineIssue{}, stub)
	require.False(t, decided, "空工单应退回 LLM")

	// 仅 style_drift 但无声线基线（依赖缺失→effective 空）→ 走 LLM
	decided, _, _ = svc.tryL0Verify(ctx, 1, "r1", PipelineIssue{Area: "文风漂移", Desc: "语感笔调漂移"}, stub)
	require.False(t, decided, "依赖缺失应退回 LLM")

	// 机械命中但无章节作用域 → 走 LLM
	decided, _, _ = svc.tryL0Verify(ctx, 1, "r1", PipelineIssue{Area: "字数", Desc: "字数偏短", ChapterIDsJSON: "[]"}, stub)
	require.False(t, decided, "无章节作用域应退回 LLM")
}

// TestTryL0VerifyWordCountPass 字数达标 → 确定性自动确认通过（免 LLM）。
func TestTryL0VerifyWordCountPass(t *testing.T) {
	svc := l0TestService(t)
	ctx := context.Background()
	ch := chapter.Chapter{NovelID: 1, SortOrder: 1, Title: "第一章"}
	require.NoError(t, svc.store.db.Create(&ch).Error)

	long := strings.Repeat("文", 1500) // 1500 rune → WordCount 1500 >= 2000*0.7=1400 阈值
	stub := func(novelID, chapterID int64) (string, bool) { return long, true }

	decided, verified, note := svc.tryL0Verify(ctx, 1, "r1",
		PipelineIssue{Area: "字数", Desc: "本章字数不足", ChapterIDsJSON: "[1]"}, stub)
	require.True(t, decided, "应确定性判定")
	require.True(t, verified, "字数达标应自动确认通过")
	require.Contains(t, note, "自动确认通过")
}

// TestTryL0VerifyWordCountFail 字数仍偏短 → 确定性判定未修复退回待修。
func TestTryL0VerifyWordCountFail(t *testing.T) {
	svc := l0TestService(t)
	ctx := context.Background()
	ch := chapter.Chapter{NovelID: 1, SortOrder: 1, Title: "第一章"}
	require.NoError(t, svc.store.db.Create(&ch).Error)

	short := strings.Repeat("文", 500) // 500 rune < 1400 → word_count 命中
	stub := func(novelID, chapterID int64) (string, bool) { return short, true }

	decided, verified, note := svc.tryL0Verify(ctx, 1, "r1",
		PipelineIssue{Area: "字数", Desc: "本章字数不足", ChapterIDsJSON: "[1]"}, stub)
	require.True(t, decided, "应确定性判定")
	require.False(t, verified, "字数仍偏短应判定未修复")
	require.Contains(t, note, "退回待修")
}
