# Agent Loop 设计文档

## 概述

Agent Loop 是对话系统的编排核心——接收消息列表，调用 LLM 流式接口，解析 tool_calls 并执行工具，将结果追加回消息列表，循环直到 LLM 不再调用工具或达到上限。

与 Python `core/agent_loop.py` 逻辑等价。Go 版无需 Python 的 5 个回调——async SQLAlchemy session 管理、消息打标、display 生成等 Python 必须的外部依赖，在 Go 中或不需要或由 loop 内置。事件推送经 `event.Emitter` 接口注入（由 app 提供 wails 实现），领域包不直接 import Wails（见 AGENTS.md 分层约束）。

## 整体流程

```
app chat handler:

  1. sess := sessionStore.DB.First(&Session{}, "session_id = ?", id)
  2. apiMsgs := sessionStore.GetMessagesForAPI(sessionID, sess.ActiveVersion)
  3. toolDefs := registry.OpenAI(toolAllowlist)
  4. system2 := agentcfg.System2(db, novelID)
  5. messages := append([system1, system2], apiMsgs...)

  6. turnID := sessionStore.NextTurn(ctx, sessionID)
  7. agent := &Agent{llm: llmClient, registry: registry, db: db, logger: log}
  8. result := agent.Run(ctx, RunOptions{
        TurnID: turnID, SessionID: sessionID, NovelID: novelID,
        Messages: messages, AllowedTools: mainAgentAllowlist, AgentType: "main", ...
     })
  9. sessionStore.UpdateSessionUsage(sessionID, ...)
```

## Agent 结构

```go
type Agent struct {
    llm      *llm.Client
    registry *mcp_tools.Registry
    db       *gorm.DB
    logger   *slog.Logger
}

type RunOptions struct {
    TurnID           int             // 当前 turn 编号，调用方 NextTurn() 后传入
    SessionID        string
    NovelID          int64
    Messages         []map[string]any
    AllowedTools     map[string]bool // 工具白名单，Tools 在 Run() 内部通过 registry.OpenAI() 生成
    ActiveVersion    int
    Model            *llm.ModelInfo   // 模型完整信息（ID + ContextWindow 等），调用方从 Provider 查
    AgentType        string          // "main" 或子 agent 类型（review / memory / statesync / signability / fix / …）
    ParentTurnID     *int            // 子 agent 指向触发它的主 turn，主 agent 为 nil
    MaxTurns         int             // 默认 50
    MaxContextTokens int             // 默认 800000
}

type AgentLoopResult struct {
    FinalText string
    TurnCount int
}
```

Python 的 5 个回调在 Go 中消解为直接调用：

| Python 回调 | Go 等价 | 原因 |
|---|---|---|
| `tool_call_handler` | `a.registry.Execute(ctx, name, rawArgs, tc, opts.AllowedTools)` | Registry 是 Agent 字段，白名单通过 RunOptions 注入 |
| `display_handler` | `a.buildDisplay(name, args, phase)` | 主和子逻辑完全相同，内置为 Agent 方法 |
| `on_message` | `a.db.Create(&msg)` + 内存追加 | 持久化就一行，不需要注入不同实现 |
| `on_usage` | 调用方从 `EventUsage` 自行 `UpdateSessionUsage` | 与循环逻辑无关，事后处理 |
| `ws_manager.send_personal_message` | 经注入的 `event.Emitter.Emit("agent:"+turnID, event)` | 领域包不得 import Wails，事件走 `internal/event` 接口注入（由 app 提供 wails 实现） |

### 事件推送

Agent 经注入的 `event.Emitter.Emit("agent:"+strconv.Itoa(turnID), event)` 发布事件，无回调（事件发射器由 `agent.New` 注入，默认 `event.NopEmitter`）。子 agent 时事件名用 `parentTurnID`，前端在对应的主 turn 分组下渲染子 agent 行为。

### turn_id 替代 task_id

Python 用 `task_id`（随机 UUID）关联事件到前端。Go 中所有消息和事件已天然绑定 `turn_id`（session 自增计数器），因此 `RunOptions.TurnID` 直接作为事件关联标识。

### 主 Agent / 子 Agent 复用

同一个 `Agent.Run()` 被主和子共用。差异在 `RunOptions` 字段值，不在代码路径：

| | 主 Agent | 子 Agent（review / memory / statesync 等） |
|---|---|---|
| `AgentType` | `"main"` | `"review"` / `"memory"` / `"statesync"` 等 |
| `ParentTurnID` | `nil` | `&parentTurnID` |
| `AllowedTools` | mainAgentAllowlist | reviewAllowlist / memoryAllowlist |
| 事件推送 | `event.Emitter.Emit("agent:"+turnID, ...)` | `event.Emitter.Emit("agent:"+parentTurnID, ...)` |
| Messages | session 历史 | 独立的 `[system, user]` |

## ToolContext 和 ToolResult

```go
type ToolContext struct {
    DB      *gorm.DB
    NovelID int64
    ToolID  string
}

type ToolResult struct {
    Success  bool
    Data     map[string]any
    Error    string
    ErrKind  string            // "system" 表示系统异常（DB/网络），"" 表示业务错误
    Metadata map[string]any
    Inject   []InjectMessage   // 工具返回的额外上下文消息
}

type InjectMessage struct {
    Role    string // 始终为 "user"（中间插入 system 不符合 API 规范）
    Content string // 追加时以 <system-reminder> 包裹，LLM 区分系统注入与用户输入
}
```

## DisplayPhase

展示文本分五阶段（Python 同样，Go 增加了 `PhaseCancelled`）：

```go
type DisplayPhase int

const (
    PhaseSelected  DisplayPhase = iota // tool_call_start，工具被选中
    PhaseExecuting                     // 参数解析完毕，开始执行
    PhaseCompleted                     // 执行成功
    PhaseFailed                        // 执行失败
    PhaseCancelled                     // 用户取消
)

type DisplayInfo struct {
    DisplayText  string
    ActivityKind string
    Metadata     map[string]any
}
```

`buildDisplay` 是 Agent 的私有方法，主和子逻辑完全相同，loop 内置。需要查 DB 获取名称时直接使用 `a.db`。

## AgentEvent — 实时推送前端

前端需要感知 6 类实时事件。Go 版用 `emitEvent(AgentEvent)` 回调替代 Python 的 WebSocket push：

```go
type AgentEventType int

const (
    EventThinking     AgentEventType = iota // reasoning_content 流式文本
    EventThinkingDone                       // 思考阶段结束
    EventContent                            // 正文流式文本
    EventToolCall                           // 工具调用状态变化（selected/executing/completed/failed/loop_detected）
    EventUsage                              // 每次 LLM 调用的 token 用量
    EventError                              // 不可恢复错误
)

type AgentEvent struct {
    TurnID    int               // 当前 turn 编号
    Type      AgentEventType
    Data      string            // thinking / content 文本 chunk
    ToolName  string            // EventToolCall 时
    ToolID    string            // EventToolCall 时  
    Phase     string            // "selected" | "executing" | "completed" | "failed" | "cancelled" | "loop_detected"
    ToolArgs  map[string]any    // 工具参数快照
    Result    *ToolResult       // 执行完毕后携带
    Display   *DisplayInfo      // buildDisplay 产出
    Usage     map[string]any    // EventUsage 时，含 usage_ratio / detail 分角色计数
    FinalText string            // EventDone / 中断时
    Error     error             // EventError 时
}
```

与 Python 事件的对应关系：

| Python WebSocket `type` | Go AgentEvent |
|---|---|
| `thinking_chunk` | `EventThinking`（Data = chunk） |
| `thinking_done` | `EventThinkingDone` |
| `content_chunk` | `EventContent`（Data = chunk） |
| `tool_call` (phase=selected/executing/completed/failed) | `EventToolCall`（Phase 区分） |
| `system_warning` | 不单独发事件，注入 system 消息后由 `persistMsg` 持久化，前端从消息列表获取 |
| `usage` | `EventUsage`（Usage = API raw + usage_ratio + detail） |

## 核心循环

```
func (a *Agent) Run(ctx, opts RunOptions) (AgentLoopResult, error):
    loopCount := 0
    fullResponse := ""
    responseBuffer := ""
    thinkingBuffer := ""
    isThinking := false
    recentPatterns := []string{}
    failCnt := map[string]int{}
    runningTokens := initRunningTokens(opts.Messages)

    while loopCount < opts.MaxTurns:
        toolOutputs := []toolOutput{}
        pendingInjects := map[string][]InjectMessage{}

        tools := a.registry.OpenAI(opts.AllowedTools)
        for event := range a.llm.ChatStream(ctx, opts.Messages, tools, opts.Model.ID, nil):
            select case <-ctx.Done():
                // 中断时保存 partial
                if responseBuffer != "" || thinkingBuffer != "":
                    a.appendMsg("assistant", responseBuffer,
                        {thinking_content: thinkingBuffer}, opts)
                return AgentLoopResult{fullResponse, loopCount}, ctx.Err()

            switch event.Type:
            case EventThinking:
                isThinking = true
                thinkingBuffer += event.Data
                a.emitter.Emit("agent:"+turnID, AgentEvent{Type: EventThinking, Data: event.Data})

            case EventContent:
                if isThinking { a.emitter.Emit(...EventThinkingDone); isThinking = false }
                responseBuffer += event.Data
                fullResponse += event.Data
                a.emitter.Emit("agent:"+turnID, AgentEvent{Type: EventContent, Data: event.Data})

            case EventToolCallStart:
                if isThinking { a.emitter.Emit(...EventThinkingDone); isThinking = false }
                display := a.buildDisplay(name, nil, PhaseSelected)
                a.emitter.Emit("agent:"+turnID, AgentEvent{
                    Type: EventToolCall, ToolName: name, ToolID: id,
                    Phase: "selected", Display: display})

            case EventToolCallEnd:
                display := a.buildDisplay(name, args, PhaseExecuting)
                a.emitter.Emit("agent:"+turnID, AgentEvent{
                    Type: EventToolCall, ToolName: name, ToolID: id,
                    Phase: "executing", Display: display})

                tc := mcp_tools.ToolContext{DB: a.db, NovelID: opts.NovelID, ToolID: id}
                result := a.registry.Execute(ctx, name, rawArgs, tc, opts.AllowedTools)

                phase := "completed"
                if !result.Success { phase = "failed" }
                display = a.buildDisplay(name, args, phase)
                a.emitter.Emit("agent:"+turnID, AgentEvent{
                    Type: EventToolCall, ToolName: name, ToolID: id,
                    Phase: phase, Result: result, Display: display})

                // 失败计数
                if !result.Success && result.ErrKind == "system":
                    failCnt[name]++
                else:
                    failCnt[name] = 0
                if failCnt[name] >= 3:
                    a.appendMsg("system", "工具 "+name+" 已连续失败 3 次，已被禁用", nil, opts)

                if result.Inject != nil:
                    pendingInjects[id] = result.Inject

                toolOutputs = append(toolOutputs, {name, id, rawArgs, result})

            case EventUsage:
                a.updateUsage(event.Usage, runningTokens, opts)

        // ---- 流结束 ----
        if len(toolOutputs) == 0:
            if isThinking { a.emitter.Emit(...EventThinkingDone) }
            break

        // 1. assistant + tool_calls
        a.appendMsg("assistant", responseBuffer,
            {tool_calls: buildToolCalls(toolOutputs), thinking_content: thinkingBuffer}, opts)

        // 2. tool 结果
        for _, to := range toolOutputs:
            a.appendMsg("tool", to.resultJSON(),
                {tool_call_id: to.id, tool_name: to.name}, opts)

        // 3. inject（role=user，<system-reminder> 包裹；to_frontend 由 appendMsg 的 role=="assistant" 规则排除）
        for _, to := range toolOutputs:
            for _, inj := range pendingInjects[to.id]:
                a.appendMsg("user", "<system-reminder>\n"+inj.Content+"\n</system-reminder>", nil, opts)

        // 4. 死循环检测
        if isStuckLoop(recentPatterns, toolOutputs, loopCount):
            a.appendMsg("system", "检测到重复调用模式，请基于已有信息直接输出", nil, opts)
            a.emitter.Emit("agent:"+turnID,
                AgentEvent{Type: EventToolCall, Phase: "loop_detected"})

        // 5. token 预算 → 超限时触发压缩（暂占位）
        if sumRunningTokens(runningTokens) > opts.MaxContextTokens:
            a.compress(opts)  // TODO: 上下文压缩，暂时占位

        thinkingBuffer = ""
        responseBuffer = ""
        loopCount++

    return AgentLoopResult{fullResponse, loopCount}, nil
```

## appendMsg — 消息构造与持久化

Loop 内部统一的追加入口，覆盖 loop 产生的所有消息（assistant / tool / system）。两个 bool 完全由 `RunOptions` 和 `role` 推导：

```go
func (a *Agent) appendMsg(role, content string, extra map[string]any, opts RunOptions) {
    msg := &session.Message{
        SessionID:    opts.SessionID,
        TurnID:       opts.TurnID,
        AgentType:    opts.AgentType,
        ParentTurnID: opts.ParentTurnID,
        Role:         role,
        Content:      content,
        ExtraMetadata: extra,
        Version:      opts.ActiveVersion,
        ToAPI:        opts.AgentType == "main",  // 主全 true，子全 false
        ToFrontend:   role == "assistant",        // 只有 assistant 展示
    }
    a.db.Create(&msg)
    opts.Messages = append(opts.Messages, msg.ToAPIFormat())
    runningTokens[role] += llm.CountMessageTokens(apiFormat)
}
```

| role | ToAPI | ToFrontend | 说明 |
|------|-------|------------|------|
| assistant | AgentType=="main" | **true**（永远） | loop 产生的正常响应文本 |
| tool | AgentType=="main" | **false**（永远） | 工具执行结果，前端不渲染 |
| system | AgentType=="main" | **false**（永远） | 安全警告、预算警告 |
| user (inject) | AgentType=="main" | **false**（永远） | 隐式排除——loop 不产生普通 user 消息，唯一 user 是 inject |

`role == "assistant"` 规则天然覆盖所有情况：assistant 给前端展示，tool/system/inject-user 均不展示。普通用户输入在 loop 外由调用方自行构造，不走 appendMsg。

主 agent 全部 `ToAPI = true`，子 agent 全部 `ToAPI = false`。`ToFrontend` 只有 assistant 为 true，tool 和 system 前端不需要显示。

## 辅助函数（抽离到独立文件）

循环体涉及较多细节，以下抽为独立方法避免 Run 方法膨胀：

| 方法 | 职责 |
|------|------|
| `buildDisplay(name, args, phase) *DisplayInfo` | 展示文本生成，内置默认实现 |
| `updateUsage(apiUsage, runningTokens, opts)` | 计算 usage_ratio + 分角色计数 → 持久化 + 推送前端 |
| `initRunningTokens(msgs)` | 逐消息 token 计数初始化 |
| `isStuckLoop(patterns, outputs, loopCount)` | 死循环检测 |
| `buildToolCalls(outputs)` | tool_calls JSON 数组构造 |
| `compress(opts)` | 上下文压缩，超预算时调用（暂占位） |

## 安全机制

| 机制 | 逻辑 | 触发后动作 |
|------|------|-----------|
| 工具失败降级 | 同工具连续系统异常（ErrKind="system"）3 次 | persist system 警告（to_api=true, to_frontend=false）。业务错误（ErrKind=""）不计数——LLM 换个参数就能成功 |
| 死循环检测 | 最近 4 轮 ≤2 种模式 + 全是只读工具 + turn≥4 | persist system 警告 + push loop_detected 事件 |
| Token 预算 | runningTokens > maxContextTokens | 触发 `compress()` 压缩上下文（暂占位，后续接 session 压缩逻辑） |
| 取消 | ctx.Done() 在每收到 SSE event 时检查 | 返回当前 partial 文本 |

只读工具集合：`search_story_memory`、`get_timeline`、`get_chapter_content`、`get_chapter_list`、`get_characters`、`get_locations`、`get_novel_info`、`get_creative_profile`、`get_story_arcs`、`get_story_state`、`get_reader_perspective`

## 与 Python 的差异

| | Python | Go |
|---|---|---|
| 回调数量 | 5 个（tool_call / display / on_args / on_message / on_usage） | 0 个 |
| 工具执行 | `tool_call_handler` 闭包（注入 session、allowed 等） | `a.registry.Execute()`，Agent 持有 registry + allowedTools |
| 消息持久化 | `on_message` 闭包 → session_manager | `a.db.Create(&msg)`，Agent 持有 db |
| 展示文本 | `display_handler` 闭包 | `a.buildDisplay()` 内置方法 |
| 事件推送 | `ws_manager.send_personal_message` import 后直接调 | `event.Emitter.Emit` 注入（`internal/event` 接口，由 app 提供 wails 实现） |
| 消息打标 | `on_message` 包装函数给 dict 塞 `source`/`agent_type` | `Message.AgentType` / `ParentTurnID` 构造时直接赋值 |
| 事件关联 | 随机 `task_id` UUID | 自增 `turn_id` |
| 取消 | `asyncio.Event` | `context.Context` |
| 子 agent 消息可见性 | `to_api = true`（污染上下文） | `to_api = false`（隔离） |

## 与子 Agent 的关系

Python 中 `run_subagent` 工具复用了 `run_agent_loop`，传不同的 system prompt + 工具白名单 + max_turns。Go 同样复用——创建 Agent 实例时设不同的 `allowedTools` + `AgentType` + `ParentTurnID`，`Run` 方法完全相同。

## Message Schema 调整

### agent_type 升级为顶层字段

Python 把 `source: "subagent"` 和 `agent_type` 塞在 `extra_metadata` JSON 里——无法查询、无法索引、每次要 Unmarshal。Go 版在 `session.Message` 上新增列：

```go
type Message struct {
    // ... 原有字段 ...
    AgentType    string `gorm:"column:agent_type;not null;default:'main';index" json:"agent_type"`
    // "main" 或子 agent 类型（review / memory / statesync / signability / fix / …）
}
```

### parent_turn_id

子 agent 的消息记录其触发来源，用于审计和前端按 turn 分组渲染：

```go
    ParentTurnID *int  `gorm:"column:parent_turn_id;index" json:"parent_turn_id,omitempty"`
    // 主 agent 消息为 NULL，子 agent 消息指向触发它的主 turn
```

### 子 agent 消息 to_api = false

Python 把子 agent 的全部内部 tool call 历史不加区分地喂回主 agent 的下一轮上下文，浪费大量 token。Go 版子 agent 消息的可见性：

| 消息 | to_api | to_frontend | 说明 |
|------|--------|-------------|------|
| 子 agent 内部消息（system/user/tool_call/tool_result） | false | true | 前端嵌套渲染子 agent 行为，但不污染主 LLM 上下文 |
| 子 agent final_text 消息 | false | true | `persistMsg` 单独持久化一条 role="assistant"、agent_type="review" 的消息，前端渲染报告内容 |
| run_subagent 工具结果 | true | false | 主 LLM 看到 report 文本，前端不需要看原始 JSON |

两条路径各一条消息：主 LLM 通过工具结果看到子 agent 报告，前端通过 `to_frontend = true` 的消息渲染完整嵌套视图。不重复。

## 与其他模块的关系

| 模块 | 关系 |
|------|------|
| `llm` | Agent 持有 `*llm.Client`，调 `ChatStream` |
| `mcp_tools` | Agent 持有 `*Registry` + `allowedTools`，调 `Execute` 和 `OpenAI` |
| `session` | Agent 直接 `db.Create(&msg)`，调用方负责 `GetMessagesForAPI` / `NextTurn` / `UpdateSessionUsage` |
| `agentcfg` | 调用方从 `system1.go` 取 system prompt + 工具白名单，从 `system2.go` 取上下文快照 |
| `app` | `chat.go` 创建 Agent 实例，组装 `RunOptions`，调 `Run` |

## 实现路线

### 前置：Message Schema

1. `session.Message` 新增 `AgentType string` + `ParentTurnID *int` 列
2. `migrate` 中 AutoMigrate 更新

### Agent Loop 主文件

3. `internal/agent/events.go` — `AgentEventType` + `AgentEvent` + `AgentLoopResult` 类型定义
4. `internal/agent/agent.go` — `Agent` struct + `RunOptions` + `New()` + `Run()` 核心循环
5. `internal/agent/display.go` — `buildDisplay(name, args, phase) *DisplayInfo`
6. `internal/agent/safety.go` — `isStuckLoop()` + `toolPattern()` + `allReadOnly()`
7. `internal/agent/tokens.go` — `initRunningTokens()` + `enrichUsage()` + `sumRunningTokens()`

### 对接 app 层

8. `app/chat.go` — 实现 `Chat()` Wails 绑定：组装 messages → 创建 Agent → 调 `Run`
