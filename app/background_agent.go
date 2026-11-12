package app

// 后台独立子 Agent 的模型配置单源（#115 P0）。
//
// 起因：pipeline / 编辑反馈等后台链路此前用 `agent.RunOptions{}` 调 RunSubAgent，
// 而 RunSubAgent 原样复制 parentOpts.Model；agent.Run 入口 `Model == nil` 立即返回
// "agent: Model is required in RunOptions"，被 FriendlyError 兜底成「对话出错，请重试」。
// 结果：章节手动检测（重新检测本章 / 启动检测）每次静默失败，L1 还被写成 skipped。
// 这里与 chat 主链路共用同一模型来源：SelectedModelKey → parseModelKey → ProviderModel。

import (
	"context"
	"errors"
	"fmt"

	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/mcp_tools"
	"github.com/novelgo/novelgo/internal/quality"
)

// errBackgroundModelUnavailable 是「后台子 Agent 拿不到模型」的可判定哨兵。
// 上层据此把失败归类为 model，前端提示去设置里检查模型，而不是笼统「稍后重试」。
var errBackgroundModelUnavailable = errors.New("后台子 Agent 模型不可用")

// backgroundRunOptions 按当前设置构造后台子 Agent 的最小必要 RunOptions。
// 每次派发现取，用户在设置里切换模型后立即生效；NovelID/SessionID/TurnID 由
// RunSubAgent 依请求或零值处理，保持既有后台行为不变。
func (a *App) backgroundRunOptions() (agent.RunOptions, error) {
	if a.llmClient == nil || a.agent == nil {
		return agent.RunOptions{}, fmt.Errorf("%w: LLM 客户端或 Agent 未初始化", errBackgroundModelUnavailable)
	}
	s := a.settingsSnapshot()
	provider, modelID := a.parseModelKey(s.SelectedModelKey)
	model, ok := a.llmClient.ProviderModel(provider, modelID)
	if !ok {
		return agent.RunOptions{}, fmt.Errorf("%w: %s/%s", errBackgroundModelUnavailable, provider, modelID)
	}
	return agent.RunOptions{
		Model:           model,
		ProviderName:    provider,
		ReasoningEffort: s.ReasoningEffort,
	}, nil
}

// runBackgroundSubAgent 是三条后台链路（chapterci / statesync / editorfeedback）
// 唯一派发入口，杜绝再出现空 RunOptions。
func (a *App) runBackgroundSubAgent(ctx context.Context, req mcp_tools.SubAgentRequest) (string, error) {
	opts, err := a.backgroundRunOptions()
	if err != nil {
		return "", err
	}
	return a.agent.RunSubAgent(ctx, opts, req)
}

// chapterDetectError 给章节检测失败附一个前端可读的稳定分类码。
// Error() 只回友好文案（会进日志与 LLM 上下文的边界由 RunSubAgent 守住），
// pipeline 侧用 errors.As 取 DetectReason()，不解析错误文本、不传 LLM 原文。
type chapterDetectError struct {
	code string
	err  error
}

func (e *chapterDetectError) Error() string        { return e.err.Error() }
func (e *chapterDetectError) Unwrap() error        { return e.err }
func (e *chapterDetectError) DetectReason() string { return e.code }

// classifyChapterDetectError 把 L1 失败归为 model / receipt / llm 三类。
func classifyChapterDetectError(err error) error {
	if err == nil {
		return nil
	}
	code := "llm"
	switch {
	case errors.Is(err, errBackgroundModelUnavailable):
		code = "model"
	case errors.Is(err, quality.ErrL1ReceiptNotFound):
		code = "receipt"
	}
	return &chapterDetectError{code: code, err: err}
}
