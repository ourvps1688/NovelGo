# LLM 流式空闲超时熔断的重新设计（future 提案）

> 状态：提案（待排期）。对应现象：聊天/流水线弹窗「近 10 分钟 LLM 流式空闲超时 64 次 → 自动熔断暂停派发」，以及参考指南中 SSE 空闲超时处理的 4 招通用做法。
> 关键词：流式空闲超时、供应商熔断、自愈熔断器、思考模型误杀、SSE 心跳、阶段事件。

## 1. 背景与动机

近期多位用户（含开发者自测日志）多次遇到：

- 弹窗文案：**「供应商疑似挂起：近 10 分钟 LLM 流式空闲超时 64 次 → 自动熔断暂停派发」**，用户被强制要求「稍后重试」。
- DeepSeek 思考（reasoning）模型长推理时，连接保持但服务端长时间不吐 chunk，对话被看门狗误判挂起。
- 同一时间直连 chat 仍在狂打该供应商，10 分钟滚出截图里那个 **64 次**计数——但 chat 通道本身**并未被熔断**，于是「误熔断 + 持续制造假阳性」并存。

参考指南（用户附图）给出了业界处理 SSE 空闲超时的 4 招：

1. **SSE `: ` 心跳注释**填满空闲空隙，避免中间层（代理/网关）切断长间隙流。
2. **阶段事件（Progress Events）**：流已结束、agent 执行 tool 期间向前端持续发送状态，消除前端真空焦虑。
3. **代理/网关调优**：让 Nginx/ALB/Cloudflare 允许大模型长间隙（Idle Timeout 调大、关缓冲）。
4. **客户端活跃重置 + 断点续连**：任何字节到达即刷新空闲计时；长任务放宽空闲预算；按操作类型区分「真静默」与「正常长思考」。

本提案将上述 4 招落到 NovelGO 的桌面 Wails 中继架构，并**根本性修正当前熔断器的触发信号**。

## 2. 根因定位

已读源码确认当前链路：

| 环节 | 位置 | 行为 |
|---|---|---|
| 空闲看门狗 | `internal/llm/stream.go:71` `streamIdleTimeout = 3 * time.Minute` | SSE body **3 分钟内读不到任何字节** → `cancel(ctx)`（`kickReader`/`idleWatchdog` 见 `stream.go:83-136`） |
| 空闲计数 | `internal/llm/stream.go:738` `idleFires` 滑窗；`:750` `recordIdleFire()`；`:754` `RecentIdleTimeouts(window)` | 每次 idle 超时记一次，全**局**共享 |
| 熔断判定 | `internal/pipeline/provider_trip.go:27-31` `providerIdleWindow=10min` + `fixProviderIdleTrip=10` | `providerTripped()` 只看「近 10 分钟空闲超时 ≥ 10 次」→ 熔断 |
| 派发拦截 | `internal/pipeline/provider_trip.go:74` `dispatchSkill` | 熔断 → 返回 `errProviderTripped` 哨兵错误、零 agent 派发 |
| 观测落库 | `internal/activity/types.go:66` `ExecStatusTripped` + `store.go:185` `tripped_runs` 聚合 | 「tripped」状态进执行体观测 |

**核心问题：把「空闲超时」等同于「供应商挂起」。但二者不是一回事。**

1. **空闲超时是可恢复瞬态**，却被当成「供应商死了」的硬信号。DeepSeek 思考模型「连接保持但服务端不吐 chunk」（`docs/feat/v1.2.0/chat-interruption-error-feedback.md:121` 已记录）在纯推理静默 >3min 时必被看门狗误杀；RAG 检索、工具执行、长 CoT 同理。
2. **代理缓冲/网关切断**会把随产随送的流切成 >3min 的空隙（参考 doc 第 3/4 招），这是传输层问题，不是供应商死亡。
3. **信号源与消费者不一致**：熔断只在 `dispatchSkill`（pipeline）检查；直连 chat（`app/chat.go` → `agent.Run` → `ChatStream`）**不检查熔断**（grep `providerTripped|dispatchSkill|errProviderTripped|breaker` 在 `app/chat.go` 零匹配）。所以 chat 仍在狂打该供应商，10 分钟滚出截图里那个 **64 次**——pipeline 早被误熔断，chat 还在制造假阳性。
4. **全局计数**：`idleFires` 是全局滑窗，无法区分「某一家供应商真不稳」与「思考模型长静默」；且浏览器/localhost IPC 段本就不该有代理缓冲问题，却被一刀切。

## 3. 核心设计原则

> **把「空闲超时」从熔断触发信号里剥离开。**
> - 空闲超时 = **可恢复瞬态**：只走 transport/agent 退避重试（现有 `APIError.Retryable` + agent 重试已具备，见 `chat-interruption-error-feedback.md` P2），**绝不**触发熔断。
> - 熔断只由**硬失败**触发：连接拒绝、`>=5xx`、首字节超时（`ResponseHeaderTimeout` 已设 10m，`stream.go:53`）、以及「**零字节且远超思考大预算的总静默**」（见 §5）。
> - 熔断必须是**自愈三态**（Closed→Open→Half-Open），不再依赖用户手点「重试」。

## 4. 参考方案对照表（4 招 → NovelGO 落点）

| 参考 doc 方案 | 适用场景 | NovelGO 落点（桌面 Wails 中继架构） |
|---|---|---|
| **① SSE `: ` 心跳** | 中间层（代理/网关/Nginx 缓冲）切断长间隙流 | NovelGO 后端是「供应商→前端」的**中继服务器**。在**后端→前端**这段 SSE 上主动发 `: keep-alive` 注释心跳（每 10–15s），保证浏览器/localhost IPC 不被任何中间层切断；并把供应商的 `reasoning` delta 转成前端可见的「思考中」进度，消除前端真空。供应商→Go 这段无法强迫对方发心跳，但 `ResponseHeaderTimeout=10m` + `http.Client.Timeout=0`（已有纪律）已足够。 |
| **② 阶段事件（Progress Events）** | 流已结束、tool 执行期间前端真空 | agent 执行 tool / RAG 检索期间，复用既有 `statesync:progress` 范式（`internal/mcp_tools/rw_tools.go:907`）向前端持续发 `stage: retrieving / tool_exec / reasoning`。既消焦虑，也让「等待」对前端透明。 |
| **③ 代理/网关调优** | 公司代理 / 套了 Nginx/Cloudflare | 桌面直连通常无 Nginx；但若走公司代理（`http.ProxyFromEnvironment` 已启用），按 doc 第 3 招让代理允许大模型长间隙（ALB Idle Timeout 调大）。NovelGO 自身对外响应（若套网关）加 `X-Accel-Buffering: no`。 |
| **④ 客户端活跃重置 + 按操作放宽预算** | 思考模型长推理被误杀 | `kickReader`（`stream.go:130-136`）已正确「任何字节到达都刷新 last」，缺的是**按操作类型放宽空闲预算**：对 `SupportsThinking` 模型 / 长工具场景，把 3min 固定预算改为「有 reasoning 则放宽，纯 content 真空才收紧」。agent 层已是「只重试 LLM 调用本身、messages 不变」（doc P2），方向正确，无需重写。 |

## 5. 熔断器重设计（核心：自愈三态 + 按 provider 隔离）

### 5.1 状态机

```
       硬失败累计 ≥ 阈值(建议 5/2min)
  ┌──────────┐  ───────────────────────────►  ┌──────────┐
  │  CLOSED   │                                   │   OPEN   │
  │ 正常派发  │  ◄──── 探针成功(1~2 次连续成功) ────  │ 拒绝派发  │
  └──────────┘                                   └────┬─────┘
        ▲                                                │ 冷却到期
        │ 探针失败                                        │ (建议 30~60s)
        │                                                ▼
        │                                        ┌──────────┐
        └──────────── 探针失败回 Open ────────────│ HALF-OPEN │
                 (探针失败)                        │ 放行探针  │
                                                  └──────────┘
```

要点：

- **Closed**：正常派发，累计硬失败计数。
- **Open**：拒绝派发（返回 `errProviderTripped`），但**不再弹「请稍后重试」强阻塞**——冷却到期后**自动**进入 Half-Open。
- **Half-Open**：放行少量探针请求；成功则回 Closed（计数清零），失败则回 Open（重置冷却）。**无需用户手点「重试」即可自愈**。

### 5.2 与现有实现的差异

| 维度 | 现状 | 改造后 |
|---|---|---|
| 触发信号 | 空闲超时 ≥10/10min | **硬失败**（连接拒绝 / `>=5xx` / 首字节超时 / 零字节超思考大预算） |
| 状态 | 只有「是否熔断」布尔 | 三态 + 自动半开探针 |
| 粒度 | 全局 `idleFires` | **每 provider 独立** breaker 实例（某一家挂了自动回退其他家） |
| 覆盖范围 | 仅 `dispatchSkill`（pipeline） | **chat 与 pipeline 共用**同一 breaker（下沉到 LLM 调用入口） |
| 自愈 | 依赖用户手点重试 | 30~60s 后自动 Half-Open 探针恢复 |
| UI 文案 | 「请稍后重试」 | 显示状态(Open/Half-Open)、冷却剩余秒数、命中 provider、原因分类 |

## 6. 阈值、度量与可观测

- **信号分离是核心改动**：新增 `hardFailure` 计数（连接拒绝 / `>=5xx` / 首字节超时 / 「零字节且超过思考大预算」）。`fixProviderIdleTrip=10/10min`（`provider_trip.go:31`）这个过于敏感的阈值**废弃**——空闲超时本就频繁，不该触发熔断；改硬失败阈值（建议 **5 次 / 2min**）。空闲超时不进熔断滑窗，只进日志做观察（沿用 `idleFires` 做长期观测，但不再驱动 breaker）。
- **每 provider 独立状态**：把全局 `idleFires` 改为按 `providerName` 索引的 breaker 实例，支持自动回退其他供应商（对应参考 doc「切换模型/供应商」）。
- **chat 与 pipeline 共用**：把熔断检查下沉到 LLM 调用入口（或在 `dispatchSkill` 之外加一道共享判定），避免 chat 继续狂打已 Open 的供应商、滚出假阳性 64 次。
- **思考预算**：`SupportsThinking` 模型纯 content 真空超过「思考大预算」（建议 8~10min，明显大于正常长推理）才计入「真静默」候选；期间只要有 reasoning delta 到达即刷新计时（复用 `kickReader` 逻辑）。
- **UI 信息增强**：对话框显示当前状态（Open/Half-Open）、冷却剩余秒数、命中 provider、原因分类（硬失败 vs 空闲）；「重试」按钮语义变为「立即触发 Half-Open 探针」；但正常情况 30~60s 后**自动**自愈，无需手点。
- **观测对齐**：`activity.ExecStat` 的 `tripped` 状态（`types.go:66`）与 breaker 状态对齐，便于长期观测哪家供应商真不稳定（`tripped_runs` 聚合，`store.go:185`）。

## 7. 实施拆解（WBS）

| 步骤 | 内容 | 优先级 | 关键文件 |
|---|---|---|---|
| 1 | **信号分离**：`providerTripped()` 改为基于硬失败计数；删除 `fixProviderIdleTrip=10/10min` 作为熔断条件，空闲超时仅作日志观察 | P0 | `internal/pipeline/provider_trip.go:27-60` |
| 2 | **三态熔断器**：新增 `breaker` 包（或并入 `provider_trip.go`），实现 Closed/Open/Half-Open + 冷却与自动半开探针（按 provider 索引） | P0 | 新增 `internal/llm/breaker.go` 或 `internal/pipeline/breaker.go` |
| 3 | **硬失败识别**：在 `stream.go` 把首字节超时 / `>=5xx` / 连接拒绝 / 零字节超思考大预算归类为 `hardFailure`，并暴露给 breaker（现有 `APIError.Retryable` 可复用） | P0 | `internal/llm/stream.go:108-121, 338-344, 524` |
| 4 | **chat 与 pipeline 共用 breaker**：把熔断检查下沉到 LLM 调用入口，`app/chat.go` 的 `agent.Run` 调用链也过 breaker，避免 chat 绕过 | P1 | `app/chat.go` + 共享 breaker 入口 |
| 5 | **空闲预算按模型放宽**：`SupportsThinking` 模型纯 content 真空放宽到思考大预算；有 reasoning delta 即刷新计时 | P1 | `internal/llm/stream.go:71,83-136` |
| 6 | **后端→前端心跳**：中继 SSE 加 `: keep-alive` 注释心跳（10~15s），并把 reasoning delta 转前端「思考中」进度 | P2 | 后端 SSE 发射 + 前端订阅 |
| 7 | **阶段事件填空闲**：agent 执行 tool / RAG 检索期间复用 `statesync:progress` 发 `stage` 事件 | P2 | `internal/mcp_tools/rw_tools.go:907` 范式 + agent 层 |
| 8 | **代理/网关调优**：若套网关加 `X-Accel-Buffering: no`；文档说明公司代理场景的 Idle Timeout 调大 | P3 | 部署文档 / 配置 |
| 9 | **UI 信息增强**：弹窗显示状态/冷却/provider/原因分类；「重试」按钮变「立即半开探针」 | P2 | 前端熔断弹窗组件 |
| 10 | **度量对齐**：`ExecStat.tripped` 与 breaker 状态对齐；保留 `idleFires` 作长期观测 | 配套 | `internal/activity/types.go:66`, `store.go:185` |

**实施优先级建议**：P0（信号分离 + 三态 breaker + 硬失败识别）→ P1（chat/pipeline 共用 + 空闲预算放宽）→ P2（心跳 + 阶段事件 + UI 增强）→ P3（代理调优）。

## 8. 关键验收点 / 测试覆盖

1. 注入空闲超时**不会**再熔断，只触发 transport/agent 重试退避。
2. 只有注入硬失败才能熔断；熔断后 30~60s **自动** Half-Open 探针恢复，无需手点「重试」。
3. 同一 provider 的 chat 与 pipeline 熔断状态一致（chat 不再独自累积假阳性 64 次）。
4. 长思考 / thinking 模型（`SupportsThinking`）不再误熔断——空闲预算放宽 + 后端→前端心跳 + 阶段事件填充。
5. `activity.ExecStat` 的 `tripped` 状态与 breaker 状态对齐，便于长期观测哪家供应商真不稳定。
6. 单元测试：硬失败累计达阈值→Open；Open 冷却期→Half-Open→探针成功→Closed；探针失败→回 Open；空闲超时**不**改变 breaker 状态；按 provider 隔离互不干扰。

## 9. 风险与权衡

1. **阈值选择**：硬失败 5/2min 可能仍偏敏感（高峰期 429/503 密集）。建议做成可配置，初值保守，观测后调。
2. **思考大预算误判**：若某供应商真「连 reasoning 都不吐」地永久挂起，需靠「零字节超思考大预算」兜底；预算过小会误杀长推理，过大则真挂起发现慢。建议 8~10min 初值。
3. **chat 共用 breaker 的回退体验**：chat 被 breaker 拦下时需有清晰文案（「供应商 X 暂不可用，已自动切换/稍后重试」），避免用户以为软件坏了。
4. **自愈探针的副作用**：Half-Open 探针仍是真实 LLM 调用（花钱/耗额度）。建议限制探针并发与频率，避免 Open 期间被打爆。
5. **向后兼容**：`ExecStatusTripped` 状态枚举保留，仅语义从「空闲超时熔断」收窄为「硬失败熔断」，旧数据无需迁移。
6. **空闲超时从熔断剥离后的盲区**：长静默但非思考模型的场景（如供应商真死、零字节）仍须靠首字节超时 + 硬失败兜底覆盖，不能只依赖 idle watchdog。

## 10. 关键文件速查

| 文件 | 行号 | 作用 |
|---|---|---|
| `internal/llm/stream.go` | 71 | `streamIdleTimeout = 3min` 空闲看门狗预算（待按模型放宽） |
| `internal/llm/stream.go` | 83-136 | `idleWatchdog` + `kickReader`（任何字节刷新 last，复用） |
| `internal/llm/stream.go` | 108-121, 338-344, 524 | 硬失败识别点（5xx / SSE 中断 / idleTimeoutError） |
| `internal/llm/stream.go` | 738, 750, 754 | `idleFires` 滑窗、`recordIdleFire`、`RecentIdleTimeouts`（改观测用） |
| `internal/pipeline/provider_trip.go` | 27-31 | `providerIdleWindow=10min` + `fixProviderIdleTrip=10`（待废弃作熔断条件） |
| `internal/pipeline/provider_trip.go` | 40, 45-60 | `errProviderTripped` 哨兵、`providerTripped()` 单源判定（重构为三态） |
| `internal/pipeline/provider_trip.go` | 74 | `dispatchSkill` 唯一的派发口（接入共享 breaker） |
| `app/chat.go` | — | 直连 chat 入口（确认**未**检查 breaker，P1 需接入） |
| `internal/activity/types.go` | 66 | `ExecStatusTripped` 观测状态（与 breaker 对齐） |
| `internal/activity/store.go` | 185 | `tripped_runs` 聚合取数口 |
| `internal/mcp_tools/rw_tools.go` | 907 | `statesync:progress` 阶段事件范式（P2 复用） |
| `docs/feat/v1.2.0/chat-interruption-error-feedback.md` | 121 | DeepSeek 思考模型「连接保持不吐 chunk」根因记录（本提案前置背景） |

## 11. 修订历史

- **v1（初稿）**：根因定位（空闲超时被误当供应商挂起、chat 绕过 breaker、全局计数），提出信号分离 + 自愈三态 + 按 provider 隔离 + 参考 doc 4 招对照 + 实施 WBS + 验收点。
