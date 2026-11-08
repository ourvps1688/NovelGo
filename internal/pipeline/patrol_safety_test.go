package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/novelgo/novelgo/internal/chapter"
	"github.com/novelgo/novelgo/internal/character"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/volume"
	"github.com/stretchr/testify/require"
)

// ── 安全审核定位单测（#101）────────────────────────────────────────────────

func safetyTestUnits(n int, triggers map[int]string) []patrolUnit {
	units := make([]patrolUnit, 0, n)
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("name=角色%d; note=常规设定", i+1)
		if tr, ok := triggers[i]; ok {
			content = tr
		}
		units = append(units, patrolUnit{
			Kind:    "ledger",
			Label:   fmt.Sprintf("台账 characters 第 %d 行", i+1),
			Content: content,
		})
	}
	return units
}

// markerProbe 是按内容子串判定拦截的假上游，控制探针恒为 clean。返回调用计数指针，
// 用于断言二分探测的调用次数上界（绝不因书长线性放大上游请求）。
func markerProbe(triggers ...string) (patrolSafetyProbe, *int) {
	calls := 0
	return func(_ context.Context, content string) patrolProbeState {
		calls++
		if content == patrolSafetyControlProbe {
			return probeClean
		}
		for _, tr := range triggers {
			if strings.Contains(content, tr) {
				return probeBlocked
			}
		}
		return probeClean
	}, &calls
}

func TestIsLLMSafetyError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   bool
		reason string
	}{
		{"nil", nil, false, ""},
		{"空闲超时不是安全错误", &llm.APIError{Kind: "idle_timeout", Message: "9m0s 内未收到任何字节"}, false, ""},
		{"普通网络错误", errors.New("connection reset by peer"), false, ""},
		{"普通业务错误", fmt.Errorf("skill 执行失败: %w", errors.New("boom-xyz")), false, ""},
		{"APIError 上游 body 含过滤标记", fmt.Errorf("call failed: %w", &llm.APIError{
			StatusCode: 400, Message: "content_filter triggered: high risk"}), true, "网关内容过滤拦截"},
		{"错误链含策略违规", fmt.Errorf("skill %q 执行失败: %w", "consistency-patrol",
			errors.New("request violates our content policy")), true, "请求命中内容策略"},
		{"中文安全审核", errors.New("上游安全审核未通过"), true, "安全审核拦截"},
		{"中文敏感内容", errors.New("检测到敏感内容"), true, "命中敏感内容"},
		{"高危不等同审核命中", errors.New("disk usage high risk of failure"), false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := isLLMSafetyError(tc.err)
			require.Equal(t, tc.want, got)
			if tc.want {
				require.Equal(t, tc.reason, reason)
			}
		})
	}
}

func TestLocalizeSafetyTriggersSingleHit(t *testing.T) {
	units := safetyTestUnits(12, map[int]string{5: "BLOCK-ME-ALPHA"})
	probe, calls := markerProbe("BLOCK-ME-ALPHA")

	got := localizeSafetyTriggers(context.Background(), units, probe)

	require.Len(t, got.Hits, 1, "应精确定位到唯一命中的台账行")
	require.Equal(t, "台账 characters 第 6 行", got.Hits[0].Label)
	require.Contains(t, got.Hits[0].Content, "BLOCK-ME-ALPHA")
	require.Empty(t, got.Unconfirmed)
	require.LessOrEqual(t, *calls, 12, "二分探测不得退化成逐条全量调用")
	require.Contains(t, got.Note, "探针仅重放本次请求内", "#104 起探针也重放提示词/工具返回块")
}

func TestLocalizeSafetyTriggersTwoHits(t *testing.T) {
	units := safetyTestUnits(16, map[int]string{1: "BLOCK-ONE", 13: "BLOCK-TWO"})
	probe, calls := markerProbe("BLOCK-ONE", "BLOCK-TWO")

	got := localizeSafetyTriggers(context.Background(), units, probe)

	require.Len(t, got.Hits, 2)
	require.Equal(t, "台账 characters 第 2 行", got.Hits[0].Label)
	require.Equal(t, "台账 characters 第 14 行", got.Hits[1].Label)
	require.LessOrEqual(t, *calls, 24)
}

func TestLocalizeSafetyTriggersAllClean(t *testing.T) {
	units := safetyTestUnits(8, nil)
	probe, _ := markerProbe()

	got := localizeSafetyTriggers(context.Background(), units, probe)

	require.Empty(t, got.Hits)
	require.Empty(t, got.Unconfirmed)
	require.Contains(t, got.Note, "逐段探测未发现单独触发审核的片段", "整体语境/网关规则触发须诚实说明")
	require.Empty(t, got.Hints, "语料不含高风险词面时不应凭空造提示")
}

func TestLocalizeSafetyTriggersControlBlocked(t *testing.T) {
	units := safetyTestUnits(6, map[int]string{2: "BLOCK-ME"})
	calls := 0
	probe := func(_ context.Context, _ string) patrolProbeState {
		calls++
		return probeBlocked // 连控制探针都拦：网关/账户级风控
	}

	got := localizeSafetyTriggers(context.Background(), units, probe)

	require.Empty(t, got.Hits, "全局拦截时不得把任一单元误报为命中")
	require.Contains(t, got.Note, "控制探针")
	require.Equal(t, 1, calls, "控制探针被拦后应立即止损，不再细分")
}

func TestLocalizeSafetyTriggersUnknownSingles(t *testing.T) {
	units := safetyTestUnits(3, nil)
	calls := 0
	probe := func(_ context.Context, content string) patrolProbeState {
		calls++
		if content == patrolSafetyControlProbe {
			return probeClean
		}
		return probeUnknown // 供应商断连/限流：不可判定
	}

	got := localizeSafetyTriggers(context.Background(), units, probe)

	require.Empty(t, got.Hits, "unknown 绝不能被当成 clean 而漏报")
	require.Len(t, got.Unconfirmed, 3, "不可判定的单条须逐一列为待人工核查")
}

func TestLocalizeSafetyTriggersBudgetCap(t *testing.T) {
	units := safetyTestUnits(100, nil)
	probe, calls := markerProbe()
	unknownProbe := func(ctx context.Context, content string) patrolProbeState {
		_ = probe(ctx, content) // 计数
		if content == patrolSafetyControlProbe {
			return probeClean
		}
		return probeUnknown
	}

	got := localizeSafetyTriggers(context.Background(), units, unknownProbe)

	require.Equal(t, patrolSafetyMaxProbes, *calls, "探针数必须硬顶在预算帽")
	require.Contains(t, got.Note, "定位预算或时间已用尽")
}

func TestScanLocalSafetyHints(t *testing.T) {
	units := []patrolUnit{
		{Kind: "ledger", Label: "台账 characters 第 1 行", Content: "name=林川; note=主角"},
		{Kind: "chapter", Label: "第2章《夜色》", Content: "描写中含血腥场面"},
		{Kind: "ledger", Label: "台账 time_entries 第 4 行", Content: "event=自残念头; scope=future"},
	}
	got := scanLocalSafetyHints(units)
	require.Len(t, got, 2)
	require.Equal(t, "第2章《夜色》", got[0].Label)
	require.Equal(t, "台账 time_entries 第 4 行", got[1].Label)
}

func TestSafetyLocateResultRender(t *testing.T) {
	hints := safetyTestUnits(patrolSafetyRenderCap+3, nil)
	got := safetyLocateResult{
		Hits:        []patrolUnit{{Kind: "ledger", Label: "台账 characters 第 7 行", Content: "name=某人; note=违规片段"}},
		Unconfirmed: []patrolUnit{{Kind: "chapter", Label: "第3章《雨》", Content: "正文"}},
		Hints:       hints,
		Note:        "定位说明占位",
	}.render()

	require.Contains(t, got, "【触犯内容自动定位】")
	require.Contains(t, got, "台账 characters 第 7 行")
	require.Contains(t, got, "摘录：")
	require.Contains(t, got, "【未能确认的片段】")
	require.Contains(t, got, "【词面提示】")
	require.Contains(t, got, "另有 3 条未逐条展示", "渲染条数帽生效")
	require.Contains(t, got, "定位说明：定位说明占位")
}

// TestNewPatrolSafetyProbeOverHTTP 覆盖生产探针：真实 HTTP 客户端对 400 content_filter、
// 软拒绝语、正常响应分别判定 blocked / blocked / clean；未配置直连模型时恒为 unknown。
func TestNewPatrolSafetyProbeOverHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(string(body), "content_filter"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"content_filter: high risk"}}`))
		case strings.Contains(string(body), "REFUSAL-MARK"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"I'm sorry, but I cannot assist"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
		}
	}))
	defer srv.Close()

	client := llm.NewClient(map[string]llm.Provider{
		"probe-http": {
			Name: "probe-http", ChatURL: srv.URL,
			Models: []llm.ModelInfo{{ID: "mock", ContextWindow: 4096, MaxOutputTokens: 64}},
		},
	}, slog.Default())

	svc := &Service{llmClient: client, defaultProv: "probe-http", defaultModel: "mock"}
	probe := svc.newPatrolSafetyProbe()
	require.Equal(t, probeClean, probe(context.Background(), patrolSafetyControlProbe))
	require.Equal(t, probeBlocked, probe(context.Background(), "content_filter"))
	require.Equal(t, probeBlocked, probe(context.Background(), "REFUSAL-MARK"))

	noClient := (&Service{}).newPatrolSafetyProbe()
	require.Equal(t, probeUnknown, noClient(context.Background(), "anything"), "无直连模型时须优雅降级")
}

// ── 巡检失败态集成回归 ────────────────────────────────────────────────────

// patrolSafetyTestService 造一本带章节 + 台账行的测试书，保证巡检确实拼入了语料单元。
func patrolSafetyTestService(t *testing.T, novelID int64) *Service {
	t.Helper()
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&chapter.Chapter{}, &volume.Volume{}, &character.Character{}))
	chStore := chapter.NewStore(db, slog.Default())
	_, err := chStore.Create(context.Background(), nil, novelID, nil, "第一章")
	require.NoError(t, err)
	require.NoError(t, db.Table("characters").Create(map[string]any{
		"novel_id": novelID, "name": "林川", "description": "描写中含血腥场面",
	}).Error)
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())
	svc.SetChapterTextReader(func(context.Context, int64, int64) (string, error) {
		return "夜色沉沉，城中灯火未熄。", nil
	})
	return svc
}

func readPatrolSummary(t *testing.T, svc *Service, runID string) string {
	t.Helper()
	got, err := svc.store.GetByRunID(context.Background(), runID)
	require.NoError(t, err)
	var ctxt GateContext
	require.NoError(t, json.Unmarshal([]byte(got.ContextJSON), &ctxt))
	var report AuditData
	require.NoError(t, json.Unmarshal([]byte(ctxt.PatrolReport), &report))
	return report.Summary
}

// TestPatrolRefusalSummaryLocatesContent 回归 #101：通用 high risk 拒绝语之外，
// 失败态必须给出可操作的定位结果（词面提示/说明 + 具体台账行），且不再把排查甩给用户。
func TestPatrolRefusalSummaryLocatesContent(t *testing.T) {
	svc := patrolSafetyTestService(t, 731)
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 731, "new", nil, false)
	require.NoError(t, err)

	capturePatrolEmit(t)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{Text: "I'm sorry, but I cannot continue: graphic depiction in chapter 12 violates our content policy."}, nil
	}
	svc.runConsistencyPatrol(ctx, run.RunID, run.NovelID)

	summary := readPatrolSummary(t, svc, run.RunID)
	require.Contains(t, summary, "模型拒绝了本次请求")
	require.Contains(t, summary, "chapter 12", "原始拒绝语仍须回显")
	require.Contains(t, summary, "【词面提示】", "探测不可用时须给出本地降级提示")
	require.Contains(t, summary, "台账 characters 第 1 行", "必须点名是哪张台账哪一行")
	require.Contains(t, summary, "定位说明：")
	require.NotContains(t, summary, "系统无法自动定位是哪一章命中")
	require.NotContains(t, summary, "需由你定位")
}

// TestPatrolAPIErrorSummaryKeepsGatewayDetail 回归：以 error 形式返回的内容审核拦截，
// 既有实现截成 200 字并误归因「供应商无响应」——现在必须完整回显网关原文并走定位。
func TestPatrolAPIErrorSummaryKeepsGatewayDetail(t *testing.T) {
	svc := patrolSafetyTestService(t, 732)
	ctx := context.Background()
	run, err := svc.StartPipeline(ctx, 732, "new", nil, false)
	require.NoError(t, err)

	capturePatrolEmit(t)
	svc.skillRunner = func(_ context.Context, _ string, _ SkillInput) (SkillOutput, error) {
		return SkillOutput{}, fmt.Errorf("skill %q 执行失败: %w", "consistency-patrol",
			&llm.APIError{StatusCode: 400, Message: "content_filter: flagged by moderation, 台账行含违规内容"})
	}
	svc.runConsistencyPatrol(ctx, run.RunID, run.NovelID)

	summary := readPatrolSummary(t, svc, run.RunID)
	require.Contains(t, summary, "网关/API 错误原文")
	require.Contains(t, summary, "content_filter")
	require.Contains(t, summary, "【词面提示】")
	require.NotContains(t, summary, "供应商长时间无响应")
}

// TestClassifyPatrolFailureSafetyError 兜底：安全错误即使漏到分类函数，也不得误归因超时。
func TestClassifyPatrolFailureSafetyError(t *testing.T) {
	verdict, summary := (&Service{}).classifyPatrolFailure(
		&llm.APIError{StatusCode: 400, Message: "content_filter: high risk"})
	require.Equal(t, "error", verdict)
	require.Contains(t, summary, "上游内容审核拦截")
	require.Contains(t, summary, "content_filter")
	require.NotContains(t, summary, "供应商长时间无响应")
}

// TestBuildPatrolCorpusWithUnitsMatchesCorpus 回归：可定位单元必须与真正发出的语料
// 同源同截断，否则二分探测会报出「从未发给上游」的条目（误定位）。
func TestBuildPatrolCorpusWithUnitsMatchesCorpus(t *testing.T) {
	svc := patrolSafetyTestService(t, 733)
	corpus, units := svc.buildPatrolCorpusWithUnits(context.Background(), 733, "run-locate")
	require.NotEmpty(t, units)
	for _, u := range units {
		require.Contains(t, corpus, u.Content, "每个单元的 Content 都应出现在拼入的语料中")
	}
	var labels []string
	for _, u := range units {
		labels = append(labels, u.Label)
	}
	require.Contains(t, strings.Join(labels, "|"), "台账 characters 第 1 行")
}

// TestDumpLedgersTruncationUnitsMatchWrittenText 回归：台账预算截断时，回收的可定位单元
// 必须与真正写进语料的行逐条一致——多报会把没发给上游的内容误报成命中，少报会漏掉
// 真正触犯的行（中文按字节/按 rune 混算是历史隐患）。
func TestDumpLedgersTruncationUnitsMatchWrittenText(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.AutoMigrate(&character.Character{}))
	longText := strings.Repeat("描写与设定细节 ", 60)
	for i := 0; i < patrolLedgerRows; i++ {
		require.NoError(t, db.Table("characters").Create(map[string]any{
			"novel_id": 734, "name": fmt.Sprintf("角色%d", i), "description": longText,
		}).Error)
	}
	svc := NewService(db, nil, nil, nil, "", "", context.Background(), slog.Default())

	ledger, units := svc.dumpLedgers(context.Background(), 734)

	require.NotEmpty(t, units)
	require.Less(t, len(units), patrolLedgerRows, "长台账应触发预算截断")
	require.Contains(t, ledger, "台账快照因预算已截断")
	written := 0
	for _, line := range strings.Split(ledger, "\n") {
		if strings.HasPrefix(line, "- ") {
			written++
		}
	}
	require.Equal(t, written, len(units), "写入行数必须等于回收单元数")
	for _, u := range units {
		require.Contains(t, ledger, u.Content)
	}
}
