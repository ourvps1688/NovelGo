package quality

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// observability_wiring_test.go —— #114 P2 静态接线闸。
// 与 finding_round_wiring_test.go 同风格：读源码断言（Windows CRLF 归一），
// 锁住「技能账本本轮身份 → 供应商信号落库 → 只读出口 → 前端消费面」全链，
// 并明令零后台自动修复、零新增执行体。

func readObservabilitySource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("读取源码失败 %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestObservabilityWiredEndToEnd(t *testing.T) {
	checks := []struct {
		path string
		want []string
	}{
		{"finding_key.go", []string{"FindingKeysMarker", "【本章目标发现项】"}},
		{"../activity/context_keys.go", []string{"WithTurnFindingKeys", "turnFindingFromContext"}},
		{"../activity/skill_invocation.go", []string{
			"TurnID", "FindingKeys string", "turnFindingFromContext(ctx)",
			"last_turn_id", "finding_keys", "Group(\"skill, channel\")",
		}},
		{"../activity/llm_signal.go", []string{
			"LLMSignalDaily", "llm_signal_daily", "task_kind",
			"AddLLMSignal", "SummarizeLLMSignals", "count + excluded.count",
		}},
		{"../llm/signal.go", []string{
			"SetLLMSignalSink", "RecordLLMSignal",
			"SignalTruncatedByLength", "SignalToolArgsInvalid",
			"SignalEmptyStream", "SignalBreakerOpen",
		}},
		{"../llm/types.go", []string{"TaskKind"}},
		{"../llm/stream.go", []string{
			"TaskKindFromOptions(opts)", "parseSSE(ch, resp, providerName, model, taskKind)",
			"SignalTruncatedByLength", "SignalToolArgsInvalid",
			"SignalEmptyStream", "SignalBreakerOpen",
		}},
		{"../llm/generate.go", []string{"SignalBreakerOpen", "TaskKindGenerate"}},
		{"../agent/agent.go", []string{"callOpts.TaskKind = llm.NormalizeTaskKind(opts.AgentType)"}},
		{"../migrate/migrate.go", []string{"&activity.LLMSignalDaily{}"}},
		{"../../app/chat.go", []string{"activity.WithTurnFindingKeys(ctx, turnID, findingKeysJSONFromMessage(input.Message))"}},
		{"../../app/finding_prescription_inject.go", []string{
			"findingKeysJSONFromMessage", "quality.MarshalFindingKeys", "quality.FindingKeysMarker",
			`HasPrefix(key, "l0|")`, `HasPrefix(key, "l1|")`,
		}},
		{"../../app/handler.go", []string{"llm.SetLLMSignalSink", "a.activity.AddLLMSignal"}},
		{"../../app/activity.go", []string{"GetLLMSignalStats", "SummarizeLLMSignals"}},
		{"../pipeline/detect_status.go", []string{"FindingKeys []string", "out.FindingKeys = round.CurrentKeys"}},
		{"../../frontend/src/components/skill/DetectionWorkflowView.tsx", []string{
			"archive.finding_keys", "findingKeysTitle", "targetKeys",
		}},
		{"../../frontend/src/components/skill/SkillUsagePanel.tsx", []string{
			"last_turn_id", "finding_keys",
		}},
		{"../../frontend/src/components/skill/LLMSignalPanel.tsx", []string{
			"GetLLMSignalStats", `["feature-usage"`, "llm-signal-panel",
		}},
		{"../../frontend/src/components/skill/SkillMatrixView.tsx", []string{"LLMSignalPanel"}},
		{"../../frontend/src/i18n/locales/zh-CN.json", []string{"findingKeysTitle", "\"signals\"", "【本章目标发现项】"}},
		{"../../frontend/src/i18n/locales/en.json", []string{"findingKeysTitle", "\"signals\"", "【本章目标发现项】"}},
		{"../../frontend/src/lib/wailsjs/go/models.ts", []string{
			"last_turn_id: number;", "finding_keys: string;", "export class LLMSignalStat",
		}},
		{"../../frontend/src/lib/wailsjs/go/app/App.d.ts", []string{"GetLLMSignalStats"}},
	}
	for _, c := range checks {
		src := readObservabilitySource(t, c.path)
		for _, want := range c.want {
			assert.Contains(t, src, want, "%s 缺少接线符号 %q", c.path, want)
		}
	}
}

func TestObservabilityNeverAutoFixesOrUploads(t *testing.T) {
	banned := []string{
		"StartIssueFixTickets", "StartManualIssueFixTickets",
		"taskKindIssueFix", "onAutoFix", "autoFix", "RewriteItem",
	}
	for _, rel := range []string{
		"../activity/llm_signal.go", "../activity/context_keys.go",
		"../llm/signal.go", "../llm/stream.go", "../llm/generate.go",
		"../../app/finding_prescription_inject.go",
		"../../frontend/src/components/skill/LLMSignalPanel.tsx",
	} {
		src := readObservabilitySource(t, rel)
		for _, b := range banned {
			assert.NotContains(t, src, b, "%s 不得出现自动修复符号 %q", rel, b)
		}
	}
	// 度量纯本地：信号通道只发标识，不携带正文/错误原文。
	sig := readObservabilitySource(t, "../llm/signal.go")
	assert.NotContains(t, sig, "Content string")
	assert.NotContains(t, sig, "http.Post")
	// render 纯度：新面板不得在渲染期读时钟。
	panel := readObservabilitySource(t, "../../frontend/src/components/skill/LLMSignalPanel.tsx")
	assert.NotContains(t, panel, "Date.now()")
	assert.NotContains(t, panel, "new Date()")
}
