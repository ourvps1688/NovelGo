package quality

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// finding_round_wiring_test.go —— #114 P1 静态接线闸。
// 与 prescription_wiring_test.go 同风格：读源码断言（Windows CRLF 归一），
// 锁住「写侧推进 → 读侧透出 → 评分事件快照 → 前端展示/失效」全链，
// 并明令零后台自动修复。

func readFindingRoundSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读取源码失败 %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestFindingRoundWiredEndToEnd(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"types.go", []string{"FindingRound string", `column:finding_round`}},
		{"store.go", []string{"withFindingRound", "NextFindingRound", `"finding_round"`, "UpsertL0", "UpsertL1"}},
		{"finding_key.go", []string{"func FindingKeys", "func DiffFindingKeys", "findingKeysForScope"}},
		{"finding_round.go", []string{"func NextFindingRound", "PendingL1", "FindingGuardRounds", "GuardKeys"}},
		{"score_event.go", []string{"FindingKeys string", "MarshalFindingKeys", "ParseFindingKeys"}},
		{"../pipeline/detect_status.go", []string{"ResolvedCount", "PersistedCount", "NewCount", "GuardKeys", "ParseFindingRound(row.FindingRound)"}},
		{"../pipeline/score_trend.go", []string{"FindingKeys []string", "ParseFindingKeys(ev.FindingKeys)"}},
		{"../mcp_tools/chapter_prescription.go", []string{"findingKeysForChapter", "quality.FindingKeys"}},
		{"../mcp_tools/rw_tools.go", []string{"pendingFindingKeys", "findingKeysForChapter(ctx, tc, ch.ID)", "quality.MarshalFindingKeys(findingKeys)"}},
		{"../../frontend/src/components/skill/DetectionWorkflowView.tsx", []string{
			"archive.resolved_count", "archive.guard_keys", "roundGuardHint", "fixPromptRoundGuard",
			`["score-trend"]`, `["score-history"]`,
		}},
		{"../../frontend/src/components/skill/ScoreTrendPanel.tsx", []string{
			"e.finding_keys", "prev?.finding_keys", "hasSnapshot", "findingDiff.resolved", "findingDiff.noSnapshot",
		}},
		{"../../frontend/src/i18n/locales/zh-CN.json", []string{"findingCounts", "roundGuardHint", "fixPromptRoundGuard", "findingDiff"}},
		{"../../frontend/src/i18n/locales/en.json", []string{"findingCounts", "roundGuardHint", "fixPromptRoundGuard", "findingDiff"}},
		{"../../frontend/src/lib/wailsjs/go/models.ts", []string{"resolved_count: number;", "guard_keys: string[];", "finding_keys: string[];"}},
	}
	for _, c := range checks {
		src := readFindingRoundSource(t, c.path)
		for _, want := range c.want {
			assert.Contains(t, src, want, "%s 缺少接线符号 %q", c.path, want)
		}
	}
}

func TestFindingRoundNeverAutoFixes(t *testing.T) {
	banned := []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets",
		"taskKindIssueFix", "onAutoFix", "autoFix", "RewriteItem",
	}
	for _, rel := range []string{
		"finding_key.go", "finding_round.go", "store.go",
		"../mcp_tools/chapter_prescription.go",
		"../pipeline/detect_status.go",
		"../../frontend/src/components/skill/ScoreTrendPanel.tsx",
	} {
		src := readFindingRoundSource(t, rel)
		for _, b := range banned {
			assert.NotContains(t, src, b, "%s 不得出现自动修复符号 %q", rel, b)
		}
	}
}

func TestDetectorAndFindingRoundShareOneIdentity(t *testing.T) {
	// 身份口径单源：检测侧与评分事件快照都走 FindingKeys，不得另造第二套 key。
	for _, rel := range []string{
		"../mcp_tools/chapter_prescription.go",
		"../pipeline/detect_status.go",
	} {
		src := readFindingRoundSource(t, rel)
		assert.NotContains(t, src, "C%d", "%s 不得用顺编 ID 造第二套身份", rel)
		assert.NotContains(t, src, "L1-%d", "%s 不得用顺编 ID 造第二套身份", rel)
	}
	// 写侧只在一处推进跨轮快照（Store 的两个 Upsert 出口）。
	store := readFindingRoundSource(t, "store.go")
	assert.Equal(t, 2, strings.Count(store, "s.withFindingRound(ctx, e,"),
		"UpsertL0/UpsertL1 各一处，不得出现第三个写口")
}
