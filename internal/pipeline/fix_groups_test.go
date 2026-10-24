package pipeline

// R1b（作用域分组并行 + 修复日志共享，提案 §4.3-2/3，F2 根治）验收测试：
//   ① planFixGroups：作用域 token（章节∪台账维度）连通分量分组——相交/传递相连的同组
//      串行，不相交的分组（组间可并行）；全空作用域进 global 共享串行组。
//   ② fixLogBlock / fixPrompt / blueprintFixPrompt：空日志不注入（逐字同旧文），
//      非空日志渲染「本轮修复日志」块。
//   ③ 服务级：不相交分组真正并行派发（rendezvous 会合断言，串行实现必超时失败）；
//      同组跨批注入前批修复日志（消除重复勘察）。

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/novelgo/novelgo/internal/contextasm"
	"github.com/stretchr/testify/require"
)

// TestPlanFixGroups 验证作用域连通分量分组：token 相交（含传递相连）同组、
// 不相交分组、全空作用域共享 global 串行组、组序=首现次序、组内保序。
func TestPlanFixGroups(t *testing.T) {
	issues := []AuditIssue{
		{ID: "A", ChapterIDs: []int64{1}, Dimension: "none"},      // ch:1
		{ID: "B", ChapterIDs: []int64{1, 2}, Dimension: "none"},   // ch:1,ch:2 → 连 A
		{ID: "C", ChapterIDs: []int64{5}, Dimension: "none"},      // ch:5 → 独立
		{ID: "D", Dimension: "character"},                         // dim:character → 独立
		{ID: "E", ChapterIDs: []int64{9}, Dimension: "character"}, // ch:9+dim:character → 连 D
		{ID: "F", ChapterIDs: []int64{2}, Dimension: "timeline"},  // ch:2 → 连 A/B；dim:timeline
		{ID: "G", Dimension: "timeline"},                          // dim:timeline → 传递连 A/B/F
		{ID: "H"},                                                 // 全空 → global
		{ID: "I"},                                                 // 全空 → global，连 H
	}
	groups := planFixGroups(issues)
	require.Equal(t, [][]string{
		{"A", "B", "F", "G"}, // ch:1—ch:2—dim:timeline 传递相连
		{"C"},                // 独立章节
		{"D", "E"},           // dim:character 维度相连（E 的 ch:9 不并入别组）
		{"H", "I"},           // 全空作用域共享 global 串行组
	}, idsOfGroups(groups), "组间按 token 连通分量划分，组序=首现次序，组内保输入序")

	// 单条 / 空输入边界。
	require.Equal(t, [][]string{{"X"}}, idsOfGroups(planFixGroups([]AuditIssue{{ID: "X"}})))
	require.Empty(t, planFixGroups(nil))
}

// idsOfGroups 把分组结果拍平为各组 id 列表（断言可读性）。
func idsOfGroups(groups [][]AuditIssue) [][]string {
	var out [][]string
	for _, g := range groups {
		ids := make([]string, 0, len(g))
		for _, it := range g {
			ids = append(ids, it.ID)
		}
		out = append(out, ids)
	}
	return out
}

// TestPlanFixGroupsNoneDimensionOnlyChapterTokens 验证 dimension=none 且有章节时
// 只用 ch token（不与同为 none 的其它章节组相连，也不与 dim:X 组相连）。
func TestPlanFixGroupsNoneDimensionOnlyChapterTokens(t *testing.T) {
	issues := []AuditIssue{
		{ID: "A", ChapterIDs: []int64{1}, Dimension: "none"},
		{ID: "B", ChapterIDs: []int64{5}, Dimension: "none"},
		{ID: "C", Dimension: "none"}, // 全空 → global，不与 A/B 相连
	}
	require.Equal(t, [][]string{{"A"}, {"B"}, {"C"}}, idsOfGroups(planFixGroups(issues)))
}

// TestFixLogBlockAndPromptInjection 验证修复日志块：空日志不注入（fixPrompt /
// blueprintFixPrompt 与旧文一致，不含任何日志痕迹）；非空日志按完成序渲染进两个修复提示词。
func TestFixLogBlockAndPromptInjection(t *testing.T) {
	require.Empty(t, fixLogBlock(nil), "空日志必须渲染为空串（P0 逐字降级纪律）")

	entries := []FixLogEntry{
		{ID: "I1", Note: "角色弧线缺口已补进卷纲"},
		{ID: "I3", Note: "时间线锚点同步到第4卷"},
	}
	block := fixLogBlock(entries)
	require.Contains(t, block, "【本轮修复日志")
	require.Contains(t, block, "- I1：角色弧线缺口已补进卷纲")
	require.Contains(t, block, "- I3：时间线锚点同步到第4卷")
	require.Less(t, strings.Index(block, "I1"), strings.Index(block, "I3"), "日志应按完成序渲染")

	issues := []AuditIssue{{ID: "I2", Level: "P1", Area: "节奏", Desc: "x"}}
	emptyFix := fixPrompt(1, issues, "摘要", nil, contextasm.ContextBlocks{})
	require.NotContains(t, emptyFix, "【本轮修复日志", "空日志不注入任何日志痕迹")
	require.NotContains(t, emptyFix, "角色弧线缺口已补进卷纲")
	loggedFix := fixPrompt(1, issues, "摘要", entries, contextasm.ContextBlocks{})
	require.Contains(t, loggedFix, "【本轮修复日志")
	require.Contains(t, loggedFix, "- I1：角色弧线缺口已补进卷纲")

	emptyBlueprint := blueprintFixPrompt(1, issues, "摘要", nil)
	require.NotContains(t, emptyBlueprint, "【本轮修复日志")
	loggedBlueprint := blueprintFixPrompt(1, issues, "摘要", entries)
	require.Contains(t, loggedBlueprint, "【本轮修复日志")
	require.Contains(t, loggedBlueprint, "- I3：时间线锚点同步到第4卷")
}

// TestServiceFixGroupsRunParallel 验收「作用域分组并行」（R1b，F2 根治）：
// 不相交分组（ch:1 / ch:5）的修复会话必须并行派发——两批修复在会合点互相等待，
// 若实现回退为串行，首批会等不到第二批而超时，timedOut 断言立即标红。
func TestServiceFixGroupsRunParallel(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"两组待修","issues":[` +
		`{"id":"I1","level":"P0","area":"设定自洽","desc":"第1章冲突","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[1]},` +
		`{"id":"I2","level":"P1","area":"节奏爽点","desc":"第5章拖沓","fix_type":"ledger","target_scope":"active_volume","dimension":"none","direction":"forward_fix","chapter_ids":[5]}]}`
	secondAudit := `{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`

	var arriveMu sync.Mutex
	arrived := 0
	timedOut := false
	bothArrived := make(chan struct{})
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "signability-fix":
			arriveMu.Lock()
			arrived++
			if arrived == 2 {
				close(bothArrived)
			}
			arriveMu.Unlock()
			// 会合点：两个不相交分组的修复会话互相等到齐才放行。
			select {
			case <-bothArrived:
			case <-time.After(5 * time.Second):
				arriveMu.Lock()
				timedOut = true
				arriveMu.Unlock()
			}
			return SkillOutput{Text: "已修复"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 901, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS5, StatusRunning, string(ctxtJSON)))

	svc.executeFix(run.RunID, 901)

	arriveMu.Lock()
	gotArrived, gotTimeout := arrived, timedOut
	arriveMu.Unlock()
	require.Equal(t, 2, gotArrived, "两个不相交分组应各派发一次修复会话")
	require.False(t, gotTimeout, "两组修复会话未在会合点同时到达——分组并行未生效（退回串行？）")
	got, gerr := svc.store.GetByRunID(ctx, run.RunID)
	require.NoError(t, gerr)
	require.Equal(t, StageS4, got.CurrentStage, "全部批次成功后应进复评")
	require.Equal(t, StatusAwaitingGate, got.Status)
}

// TestServiceFixLogSharedAcrossBatches 验收「修复日志共享」（R1b，F2）：同组内前批
// 回执的 log 摘要注入后批提示词（消除重复勘察），首批无日志时提示词不留任何日志痕迹。
func TestServiceFixLogSharedAcrossBatches(t *testing.T) {
	db := newTestDB(t)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	ctx := context.Background()

	// I1(P0)/I2(P1) 同维度（dim:character）→ 同组串行两小批（planFixBatches 语义保留）。
	auditJSON := `{"verdict":"needs_fix","score":18,"summary":"同组两批","issues":[` +
		`{"id":"I1","level":"P0","area":"人设卖点","desc":"主角动机缺失","fix_type":"ledger","target_scope":"active_volume","dimension":"character","direction":"forward_fix"},` +
		`{"id":"I2","level":"P1","area":"人设爽点","desc":"配角记忆点弱","fix_type":"ledger","target_scope":"active_volume","dimension":"character","direction":"forward_fix"}]}`
	secondAudit := `{"verdict":"pass","score":30,"summary":"修复后通过","issues":[]}`

	var mu sync.Mutex
	var prompts []string
	svc.skillRunner = func(_ context.Context, name string, input SkillInput) (SkillOutput, error) {
		switch name {
		case "signability-audit":
			return SkillOutput{Text: "复评：\n```json\n" + secondAudit + "\n```"}, nil
		case "signability-fix":
			mu.Lock()
			prompts = append(prompts, input.Prompt)
			first := len(prompts) == 1
			mu.Unlock()
			if first {
				// 首批回执带 log 摘要（R1b 回执扩展字段）。
				return SkillOutput{Text: "已修复。\n```json\n{\"fixed\":[\"I1\"],\"skipped\":[]," +
					"\"log\":[{\"id\":\"I1\",\"note\":\"主角动机已补进人物小传第三节\"}]}\n```"}, nil
			}
			return SkillOutput{Text: "已修复"}, nil
		default:
			return SkillOutput{Text: "ok"}, nil
		}
	}

	run, err := svc.StartPipeline(ctx, 902, "new", nil, false)
	require.NoError(t, err)
	ctxt := GateContext{Audit: auditJSON, AuditStage: StageS4}
	ctxtJSON, merr := json.Marshal(ctxt)
	require.NoError(t, merr)
	require.NoError(t, svc.store.SetStageStatus(ctx, run.RunID, StageS5, StatusRunning, string(ctxtJSON)))

	svc.executeFix(run.RunID, 902)

	mu.Lock()
	got := append([]string(nil), prompts...)
	mu.Unlock()
	require.Len(t, got, 2, "同组两小批串行派发")
	require.NotContains(t, got[0], "【本轮修复日志", "首批（无前批日志）不应有日志痕迹")
	require.Contains(t, got[1], "【本轮修复日志", "后批应注入同轮修复日志")
	require.Contains(t, got[1], "主角动机已补进人物小传第三节", "后批应看到前批的修复摘要")
	require.Contains(t, got[1], "- I1：", "日志条目应带 issue id")
}
