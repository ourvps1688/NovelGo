package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	wails "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/agent"
	"github.com/novelgo/novelgo/internal/agentcfg"
	"github.com/novelgo/novelgo/internal/config"
	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/session"
	"github.com/novelgo/novelgo/internal/skill"
)

// ChatInput 是一次对话请求的入参。
type ChatInput struct {
	SessionID       string               `json:"session_id"` // 空=新建会话
	NovelID         int64                `json:"novel_id"`
	Message         string               `json:"message"`
	ProviderName    string               `json:"provider_name"`     // "deepseek"
	ModelID         string               `json:"model_id"`          // "deepseek-v4-pro"
	ReasoningEffort string               `json:"reasoning_effort"`  // "high" | "max" | ""
	Persona         *agentcfg.PersonaRef `json:"persona,omitempty"` // 写作人格（创作心智）引用；nil=普通模式
}

// ChatResult 是一次对话请求的返回值。
type ChatResult struct {
	SessionID string `json:"session_id"`
	TurnID    int    `json:"turn_id"`
	FinalText string `json:"final_text"`
}

// mainChatTimeout 是单次主对话 LLM 调用的整体超时上限。
// 目的：防止 provider 假死（连接挂起、长时间无 token 返回）时本 turn 永久阻塞 session
// （此前只能杀进程才能恢复）。对正常长文本生成（通常数分钟）足够宽松；真正卡死会在该
// 上限被安全中断，并回写为 interrupted 事件而非崩溃。
const mainChatTimeout = 30 * time.Minute

// Chat 是对话入口。Wails 绑定，前端调用后同步执行，期间通过 EventsEmit 推流。
func (a *App) Chat(input ChatInput) (*ChatResult, error) {
	// 1. 验证模型（最早失败，不污染 DB）
	model, ok := a.llmClient.ProviderModel(input.ProviderName, input.ModelID)
	if !ok {
		return nil, fmt.Errorf("模型未找到: %s/%s", input.ProviderName, input.ModelID)
	}

	// 整体超时保护：在 app 生命周期 ctx 之上叠加 30 分钟上限，防止 provider 假死时
	// 本 turn 永久阻塞（取消/超时都会让 agent.Run 返回，最终回写为 interrupted 事件）。
	ctx, cancel := context.WithTimeout(a.ctx, mainChatTimeout)
	defer cancel()

	// 2. 加载或创建 Session
	sess, isNew, err := a.loadOrCreateSession(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("session 初始化失败: %w", err)
	}

	a.agent.Cancel(sess.SessionID)
	a.agent.RegisterCancel(sess.SessionID, cancel)
	defer func() {
		if ctx.Err() == nil {
			a.agent.UnregisterCancel(sess.SessionID)
		}
	}()

	// 3. 新会话自动生成标题（异步，与 agent LLM 调用并发）
	if isNew && sess.Title == "" {
		go a.generateTitle(sess.SessionID, input.Message, input.ProviderName, input.ModelID)
	}

	// 4. 获取下一个 turn ID
	turnID, err := a.session.NextTurn(ctx, sess.SessionID)
	if err != nil {
		return nil, fmt.Errorf("获取 turn ID 失败: %w", err)
	}

	// 5. 提交用户在对话间隙的手动编辑（git 收尾已下沉到 commit.Service，best-effort 不阻塞对话）
	s := a.settingsSnapshot()
	a.commitService.RecordUserChanges(ctx, input.NovelID, s.GitName, s.GitEmail, sess.SessionID, turnID)
	defer a.commitService.RecordAIChanges(ctx, input.NovelID, s.GitName, s.GitEmail, sess.SessionID, turnID, model.Name)

	// 6. 解析 / 命令（skill 或 quick command），构建注入内容
	var slashInject string
	var injectName string
	if strings.HasPrefix(input.Message, "/") {
		parts := strings.Fields(input.Message)
		if len(parts) > 0 {
			cmdName := strings.TrimPrefix(parts[0], "/")
			if inj, name := a.resolveSlashCommand(input.NovelID, cmdName); inj != "" {
				slashInject = inj
				injectName = name
			}
		}
	}

	// 6.5 确定性意图预路由 + LLM 兜底：非斜杠消息按 useWhen 线索自动注入命中的 auto 技能
	// （复用 slash 同款 <system-reminder> 注入，逐轮写入；always 已常驻、manual 走建议）
	var autoInject string
	if !strings.HasPrefix(input.Message, "/") {
		autoInject = a.buildAutoSkillInject(ctx, input.NovelID, input.Message, input.ProviderName, input.ModelID)
	}

	// 7. 持久化本轮消息（事务化交由 session.Store.PersistTurnMessages：
	// System 消息 + slash inject + auto inject + 用户消息在单事务内原子写入）
	// system 消息在事务外构建（compress 模式），避免事务内读 DB 导致 SQLite 单连接死锁
	var sysMsgs *agentcfg.SystemMessages
	if isNew {
		var buildErr error
		sysMsgs, buildErr = agentcfg.BuildSystemMessages(ctx, a.db, input.NovelID, a.skill, input.Persona)
		if buildErr != nil {
			a.logger.Warn("构建 system 消息部分失败", "novel_id", input.NovelID, "err", buildErr)
		}
	}
	userMsg := &session.Message{
		SessionID:  sess.SessionID,
		TurnID:     turnID,
		Role:       "user",
		Content:    input.Message,
		Version:    sess.ActiveVersion,
		ToAPI:      true,
		ToFrontend: true,
		AgentType:  "main",
	}
	var sysContents []string
	if isNew && sysMsgs != nil {
		sysContents = []string{sysMsgs.Identity, sysMsgs.Always, sysMsgs.Catalog, sysMsgs.Profile, sysMsgs.State, sysMsgs.Persona}
	}
	if err := a.session.PersistTurnMessages(ctx, sess.SessionID, turnID, sess.ActiveVersion, sysContents, slashInject, autoInject, userMsg); err != nil {
		return nil, fmt.Errorf("持久化消息失败: %w", err)
	}
	if a.activity != nil {
		a.activity.AddActivity(ctx, activity.ActivityDelta{ConversationTurns: 1})
	}

	if injectName != "" {
		a.logger.Info("slash injected", "name", injectName, "session", sess.SessionID)
	}

	// 8. 构建消息列表：全部来自 DB（含系统消息/历史/用户消息）
	// runningTokens 由 loadAPIMessages 基于已持久化的 token_count 累加返回，避免重编码整段历史。
	messages, runningTokens, err := a.loadAPIMessages(ctx, sess.SessionID, sess.ActiveVersion)
	if err != nil {
		return nil, fmt.Errorf("加载 API 消息失败: %w", err)
	}

	// 8.5 persona 每轮注入（仅当 persona 引用有效且尚未常驻）：
	// 新会话 persona 已作为常驻块写入 DB（msg.Persona），loadAPIMessages 会读回，
	// 此处用 PersonaMarker 去重避免重复；已有会话（无此常驻块）则每轮注入，实现实时切换。
	messages = injectPersonaMessages(messages, runningTokens, input.Persona, a.skill, input.NovelID)

	// 9. 运行 Agent 循环（含 turn 级自动重放，见 chat_retry.go）
	wails.EventsEmit(ctx, "chat:started", map[string]any{
		"session_id": sess.SessionID,
		"turn_id":    turnID,
	})

	result, runErr := a.runTurnWithRetry(ctx, agent.RunOptions{
		TurnID:          turnID,
		RunningTokens:   runningTokens,
		SessionID:       sess.SessionID,
		NovelID:         input.NovelID,
		Messages:        messages,
		AllowedTools:    agentcfg.Allowlist(agentcfg.MainAgent),
		ActiveVersion:   sess.ActiveVersion,
		Model:           model,
		ProviderName:    input.ProviderName,
		AgentType:       "main",
		MaxTurns:        100,
		ReasoningEffort: input.ReasoningEffort,
	}, input.ProviderName, runningTokens)

	// 10. 最终回复已由 agent.Run() 内部 appendMsg 持久化，此处不重复存储
	if runErr != nil {
		a.logger.Error("对话失败", "err", runErr)
		eventType := session.EventSystemInterrupted
		if errors.Is(runErr, context.Canceled) {
			eventType = session.EventUserStopped
		} else {
			var apiErr *llm.APIError
			if errors.As(runErr, &apiErr) {
				eventType = session.EventError
			}
		}
		if err := a.session.AppendMessage(ctx, &session.Message{
			SessionID:  sess.SessionID,
			TurnID:     turnID,
			Role:       "system",
			EventType:  eventType,
			Content:    agent.FriendlyError(runErr),
			ToFrontend: true,
			ToAPI:      false,
			AgentType:  "main",
		}); err != nil {
			a.logger.Warn("持久化结局事件消息失败", "err", err)
		}
		// 统一 emit 结局事件：session.EventType 既是 DB 标记又是实时载荷，前端按同一套映射设 status
		wails.EventsEmit(ctx, "chat:turn_outcome", map[string]any{
			"turn_id":    turnID,
			"event_type": string(eventType),
			"reason":     agent.FriendlyError(runErr),
		})
		return &ChatResult{
			SessionID: sess.SessionID,
			TurnID:    turnID,
			FinalText: result.FinalText,
		}, fmt.Errorf("%s", agent.FriendlyError(runErr))
	}

	return &ChatResult{
		SessionID: sess.SessionID,
		TurnID:    turnID,
		FinalText: result.FinalText,
	}, nil
}

// loadOrCreateSession 加载已有 session 或创建新 session。
func (a *App) loadOrCreateSession(ctx context.Context, input ChatInput) (*session.Session, bool, error) {
	if input.SessionID != "" {
		if sess, err := a.session.GetSession(ctx, input.SessionID); err == nil {
			return sess, false, nil
		}
		// 会话不存在或读取失败 → 视为需新建（下方统一创建流程）
	}

	// 创建新 session
	sess := &session.Session{
		SessionID:       fmt.Sprintf("sess_%d_%x", input.NovelID, time.Now().UnixNano()),
		NovelID:         input.NovelID,
		Model:           input.ModelID,
		ReasoningEffort: input.ReasoningEffort,
	}
	if err := a.session.CreateSession(ctx, sess); err != nil {
		return nil, false, err
	}

	wails.EventsEmit(ctx, "chat:session_created", sess)
	return sess, true, nil
}

// messagesContainPersona 检测已加载的 API 消息中是否已存在 persona 常驻块
// （由新会话写入 DB 的 msg.Persona）。用于每轮注入去重，避免与常驻块重复。
func messagesContainPersona(messages []map[string]any) bool {
	for _, m := range messages {
		if role, _ := m["role"].(string); role != "system" {
			continue
		}
		if content, _ := m["content"].(string); strings.Contains(content, agentcfg.PersonaMarker) {
			return true
		}
	}
	return false
}

// injectPersonaMessages 在每轮消息前追加 persona 系统消息（若引用有效且尚未常驻）。
// 返回的消息切片保持 system 在前；已存在常驻块（PersonaMarker）时原样返回，避免重复。
// 新会话的首轮不依赖此函数（persona 已由 PersistTurnMessages 持久化为常驻块）。
func injectPersonaMessages(messages []map[string]any, runningTokens map[string]int, persona *agentcfg.PersonaRef, skillStore *skill.Store, novelID int64) []map[string]any {
	if persona == nil || messagesContainPersona(messages) {
		return messages
	}
	personaText := agentcfg.BuildPersonaSystemText(persona, skillStore, novelID)
	if personaText == "" {
		return messages
	}
	pm := map[string]any{"role": "system", "content": personaText}
	if n, terr := llm.CountMessageTokens(pm); terr == nil {
		runningTokens["system"] += n
	}
	return append([]map[string]any{pm}, messages...)
}

// loadAPIMessages 加载指定 version 的所有 to_api 消息，转为 map 格式。
// 出口防御：历史 DB 数据可能存在重复 tool_call_id（DeepSeek 偶发 bug + 早期未去重），
// 这里按 id 去重 assistant.tool_calls 和对应的 tool 消息，避免发给 DeepSeek 时触发 400。
func (a *App) loadAPIMessages(ctx context.Context, sessionID string, version int) ([]map[string]any, map[string]int, error) {
	msgs, err := a.session.GetMessagesForAPI(ctx, sessionID, version)
	if err != nil {
		return nil, nil, err
	}
	result := make([]map[string]any, 0, len(msgs))
	// tokens: 各角色 token 预算，优先累加持久化的 token_count，避免每轮重编码整段历史。
	tokens := map[string]int{"system": 0, "user": 0, "assistant": 0, "tool": 0}
	// validToolCallIDs: assistant 消息去重后的 tool_call id 集合
	// seenToolMsgIDs: 已保留的 tool 消息 tool_call_id 集合（每个 id 只保留一条 tool 消息）
	validToolCallIDs := make(map[string]bool)
	seenToolMsgIDs := make(map[string]bool)
	for _, m := range msgs {
		api := m.ToAPIFormat(a.logger)
		role, _ := api["role"].(string)

		// assistant 消息：去重 tool_calls，保留首次出现的 id
		if role == "assistant" {
			if tc, ok := api["tool_calls"].([]any); ok && len(tc) > 0 {
				seen := make(map[string]bool, len(tc))
				deduped := make([]any, 0, len(tc))
				for _, item := range tc {
					call, ok := item.(map[string]any)
					if !ok {
						deduped = append(deduped, item)
						continue
					}
					id, _ := call["id"].(string)
					if id != "" {
						if seen[id] {
							a.logger.Warn("duplicate tool_call_id in DB assistant tool_calls, skipping",
								"session_id", sessionID, "tool_call_id", id)
							continue
						}
						seen[id] = true
						validToolCallIDs[id] = true
					}
					deduped = append(deduped, item)
				}
				api["tool_calls"] = deduped
			}
		}

		// tool 消息：跳过没有对应 tool_call 的 orphan + 跳过重复 tool 消息
		if role == "tool" {
			if id, ok := api["tool_call_id"].(string); ok && id != "" {
				if !validToolCallIDs[id] {
					a.logger.Warn("orphan tool message without matching tool_call, skipping",
						"session_id", sessionID, "tool_call_id", id)
					continue
				}
				if seenToolMsgIDs[id] {
					a.logger.Warn("duplicate tool message in DB, skipping",
						"session_id", sessionID, "tool_call_id", id)
					continue
				}
				seenToolMsgIDs[id] = true
			}
		}

		result = append(result, api)
		// 仅对真正进入上下文的消息累加 token（与 InitRunningTokens 口径一致）。
		// 优先用持久化的 token_count，升级前写入的 0 值回退到实时编码。
		if _, ok := tokens[m.Role]; ok {
			n := m.TokenCount
			if n == 0 {
				n, _ = llm.CountMessageTokens(api)
			}
			tokens[m.Role] += n
		}
	}
	return result, tokens, nil
}

// generateTitle 用 LLM 为非流式调用生成对话标题（≤10 字）。
func (a *App) generateTitle(sessionID, userMessage, providerName, modelID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	messages := []map[string]any{
		{
			"role":    "system",
			"content": "基于用户消息，生成一个不超过10个字的对话标题。只需输出标题文本，不要添加引号、标点或者额外解释。",
		},
		{"role": "user", "content": userMessage},
	}

	title, err := a.llmClient.GenerateText(ctx, providerName, messages, modelID)
	if err != nil {
		a.logger.Warn("自动生成标题失败", "err", err)
		return
	}

	title = strings.TrimSpace(title)
	if len([]rune(title)) > 30 {
		title = string([]rune(title)[:30])
	}
	if title == "" {
		return
	}

	if err := a.session.UpdateSessionMeta(a.ctx, sessionID, title, "", ""); err != nil {
		a.logger.Warn("更新标题失败", "err", err)
		return
	}

	wails.EventsEmit(a.ctx, "chat:title_updated", map[string]any{
		"session_id": sessionID,
		"title":      title,
	})
}

// ApproveTool 前端调用，响应审批请求。
func (a *App) ApproveTool(toolID string, approved bool, feedback string) error {
	return a.approvals.Complete(toolID, approved, feedback)
}

// SetApprovalMode 前端调用，切换审批模式并持久化。"auto" 自动批准，"manual" 等待用户操作。
func (a *App) SetApprovalMode(mode string) error {
	a.approvals.SetMode(mode)
	return a.updateSettings(func(s *config.AppSettings) {
		s.ApprovalMode = mode
	})
}

// CancelChat 前端调用，取消一个正在进行的对话。
func (a *App) CancelChat(sessionID string) {
	a.agent.Cancel(sessionID)
}

// CompressInput 是手动压缩请求的入参。
type CompressInput struct {
	SessionID    string `json:"session_id"`
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
}

// CompressResult 是手动压缩请求的返回值。
type CompressResult struct {
	TurnID int `json:"turn_id"`
}

// CompressContext 手动触发上下文压缩。仅在无进行中 turn 时允许。
func (a *App) CompressContext(input CompressInput) (*CompressResult, error) {
	if a.agent.IsRunning(input.SessionID) {
		return nil, fmt.Errorf("对话进行中，无法手动压缩上下文，请等待当前对话完成")
	}

	// 1. 加载 Session
	sess, err := a.session.GetSession(context.Background(), input.SessionID)
	if err != nil {
		return nil, fmt.Errorf("session 不存在: %w", err)
	}

	// 2. 查找模型
	model, ok := a.llmClient.ProviderModel(input.ProviderName, input.ModelID)
	if !ok {
		return nil, fmt.Errorf("模型未找到: %s/%s", input.ProviderName, input.ModelID)
	}

	// 3. 获取下一个 turn ID（手动压缩独立成一个 turn）
	turnID, err := a.session.NextTurn(context.Background(), sess.SessionID)
	if err != nil {
		return nil, fmt.Errorf("获取 turn ID 失败: %w", err)
	}

	// 4. 构建消息列表：全部来自 DB
	// runningTokens 由 loadAPIMessages 基于已持久化的 token_count 累加返回，避免重编码整段历史。
	messages, runningTokens, err := a.loadAPIMessages(context.Background(), sess.SessionID, sess.ActiveVersion)
	if err != nil {
		return nil, fmt.Errorf("加载 API 消息失败: %w", err)
	}

	// 5. 创建上下文（含 cancel，支持打断）
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	a.agent.RegisterCancel(sess.SessionID, cancel)
	defer a.agent.UnregisterCancel(sess.SessionID)

	// 7. 执行压缩
	// 注意：Compress 走 GenerateText 单次调用，不进入 agent loop，不需要 MaxTurns
	opts := agent.RunOptions{
		TurnID:        turnID,
		RunningTokens: runningTokens,
		SessionID:     sess.SessionID,
		NovelID:       sess.NovelID,
		Messages:      messages,
		ActiveVersion: sess.ActiveVersion,
		Model:         model,
		ProviderName:  input.ProviderName,
		AgentType:     "main",
	}

	if err := a.agent.Compress(ctx, &opts, runningTokens); err != nil {
		return nil, err
	}
	return &CompressResult{TurnID: turnID}, nil
}
