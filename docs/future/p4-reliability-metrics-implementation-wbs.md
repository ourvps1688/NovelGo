# P4「可靠性与度量（闭 W5/W6）」—— 重试矩阵统一 · 熔断扩展 · 长跑可见性 · 本地使用度量 —— 分阶段实施计划细化（WBS）

> 承接 `ultimate-form-integration-plan.md` P4 阶段（W5「产品取舍无数据」、W6「供应商稳定性」闭合；§5-P4 三条承诺）。
> 顺序铁律：先收口供应商可靠性（重试矩阵→熔断扩展→长跑可见性）→ 本地使用度量（存储→埋点→消费面）→ 验收收尾。
> 每个任务标注「文件触点 / 交付物 / 验收 / 依赖 / 风险」，未过验收不得进入下一任务，一任务一提交一销账。
> **范围铁律**：P4 是可靠性与度量阶段，允许触碰 `internal/llm`、`internal/agent`、`internal/pipeline`、`internal/activity` 与对应 app 绑定/前端；**P0–P2 工件语义零改动**（`internal/contextasm`/`internal/voice`/`internal/snapshot`/`internal/retcon` 不进本 WBS 文件触点）；**度量纯本地不上传**（禁止任何网络上报路径）；**只报告不拦截**（可见性/熔断只影响派发决策，绝不新增强杀/阻断用户动作）；**desktop-first**（用户偏好：布局按桌面设计，不主动做移动端 parity）。
> **（可选）V3 跨书聚合产物直接进 Voice 层**：plan §5-P4 标注可选项，本 WBS **不立账不排期**（宁缺勿错，待度量数据与提取链路现状再议）。

---

## 0. 契约定案（冻结 · 实现唯一依据）

### 0.1 重试决策单源契约（供应商重试矩阵）

- **勘察结论（缺口实证）**：三条 LLM 请求路径的错误分类与重试信号不一致——
  - `internal/llm/stream.go`：网络错误分「永久（DNS NXDOMAIN / ECONNREFUSED → 不可重试）/ 瞬时（其余 → 可重试）」；HTTP 错误带 `parseRetryAfter`；
  - `internal/llm/generate.go`：网络错误包成**裸 `fmt.Errorf`（不是 APIError，agent 层判不可重试直接失败）**；HTTP 错误**无 Retry-After**；
  - `internal/llm/toolcall_nonstream.go`：网络错误一律 `Retryable:true`（无永久分类）；HTTP 错误**无 Retry-After**。
- **契约**：错误分类收敛为 llm 包**单源决策函数**（`apiErrorFromNetErr(err)` 网络错误分类 + `apiErrorFromHTTP(status, body, headers)`（statusRetryable × parseRetryAfter × ParseError 三合一）；三条路径全部改走单源函数。矩阵定案：

| 错误类 | Retryable | RetryAfter 来源 |
|---|---|---|
| DNS NXDOMAIN / ECONNREFUSED（永久网络） | false | — |
| 其余网络错误（超时/EOF/reset） | true | — |
| 流式/非流式空闲超时（idle watchdog） | true | — |
| HTTP 429 / 408 / 5xx | true | `Retry-After` 头（有则带上限） |
| HTTP 其余 4xx | false | — |
| tool_args_invalid（agent 层） | true（既有重试通道） | 固定退避 |

- **零改动基线**：agent 层重试协议（`maxRetries=8`/`backoffBase`/`computeBackoff`/`EventRetrying` 含 `ClearFromSeq`）**一字不动**——P4 只统一「错误怎么分类/怎么带 Retry-After」，不改「怎么重试」。
- **验收**：单测锁定矩阵全行（含 generate/toolcall_nonstream 两路径回归到与 stream 同判定）；既有 llm/agent 测试零回退。

### 0.2 熔断扩展契约（idle trip 全派发点）

- **勘察结论**：供应商熔断（`fixProviderIdleTrip=10` / 10 分钟窗口）**只挂在 S5 修复批次派发前**（`internal/pipeline/service.go:1312`）；其余 skillRunner 派发点（S2 蓝图 :284、S4 体检 :858/:1787/:2061、S3 逐章重写 :1678、G3 漂移巡检 :1708、一致性巡检 `patrol.go:47`）无熔断——供应商挂起时照常派发，白烧整轮 agent 墙钟。
- **契约**：抽共享判定（llm 包 `ProviderDegraded(window, threshold) int` 或 pipeline 内等价私有助手，**阈值与窗口常量单源**，语义与既有 `RecentIdleTimeouts(10*time.Minute) >= fixProviderIdleTrip` 逐字一致）；**全部 skillRunner 派发点**派发前判定，熔断=跳过派发、按该点**原有失败通道**落地（S5 批次计数/闸门、S3 逐章 `EmitRewriteProgress` error、S4 落 paused/闸门、巡检落失败 note——各点失败语义零改）；提示文案统一「疑似供应商挂起」口径（复用 idleNote 语义，附近期空闲超时次数）。
- **降级纪律（最高验收）**：判定自身异常 → **照常派发**（宁缺勿错，绝不误熔断）；窗口内超时为 0 → 行为与现状逐字一致。
- **验收**：①全派发点覆盖断言（源码/测试锁定 skillRunner 调用点均有过熔断判定）；②熔断触发时零 agent 派发 + 落点失败通道语义不变；③未触发时行为逐字一致（既有 pipeline 测试零回退）。

### 0.3 长跑子 Agent 任务可见性契约

- **勘察结论**：`SubagentCard` 有 streaming/done/failed/retrying 徽标，但长跑（`RunSubAgent` MaxTurns=100、长工具执行）期间只有静态 spinner——事件间隔数分钟时用户无法区分「还在干活」与「卡死」。`AgentEvent.Timestamp` 字段已有，心跳数据现成。
- **契约**：`SubagentCard` 增加**「已运行时长」时钟**（自该 sub_task 首事件 Timestamp 起算，秒级刷新）与**「最近活动 Xs 前」心跳**（最近一次事件 Timestamp 派生）；**事件协议零改动**（不新增事件类型/字段）；纯前端。
- **显示纪律**：只读可见性——不加超时强杀、不加拦截（Cancel 通道已有，不动）；多 sub_agent 并存时逐卡独立计时（按 `sub_task_id` 隔离）。
- **验收**：①长跑卡显示时长随时间走动、有事件流入时「最近活动」归零刷新；②完成/失败态停表定格；③两卡并存互不串扰；④无事件静默 ≥N 秒仅显示心跳数值，**零弹窗零报错**（降级纪律）。

### 0.4 本地使用度量契约（功能使用率 · 纯本地不上传）

- **勘察结论**：`internal/activity` 已有 `activity_daily`（产量/时长/对话轮次/工具调用）+ `llm_usage_daily`（token 用量），均「尽力记录、失败只告警」；**缺「功能使用率」维度**（哪些面板/动作真的被用——W5 取舍无数据的直接缺口），且两表**无只读消费面**。
- **契约**：
  - **存储**：新表 `feature_usage_daily`（`date` + `feature_key` 唯一键，`count` 累计），挂进 `internal/migrate/migrate.go` AutoMigrate 清单（与 activity 两表同模式——**不动版本化迁移，done 步数断言 12 不变**）；`activity.Store.AddFeatureUsage(ctx, featureKey)` 尽力记录（负数/空 key 拒绝、失败只告警，与 AddActivity 同纪律）。
  - **绑定**：`app.RecordFeatureUsage(feature string)`（零返回值，失败静默）。
  - **埋点**：前端 `lib/featureKeys.ts` **单源常量表**（key 枚举：面板打开按 PanelId、旅程卡点击、关键动作——提取/重写/体检/修复/冻结/巡检/retcon 立案）；埋点处调 `RecordFeatureUsage`，**失败静默零提示**。
  - **消费面**：`app.GetFeatureUsage(days int)` 只读聚合 API + ProfileView 增「功能使用率」区块（近 30 天，取舍规则供数据）。
- **隐私纪律（不可妥协）**：**纯本地不上传**——度量链路禁任何 fetch/上报/遥测端点（验收含 grep 断言）；不记录用户内容（只记 feature key + 次数）。
- **验收**：①记录→查询闭环（面板切换/动作触发后 `GetFeatureUsage` 计数正确累加）；②统计失败业务零感知（触发处不抛错、无提示）；③grep 断言无网络上报路径；④新表不影响既有迁移 done 步数断言（12）。

### 0.5 兼容与勿回退（P0–P3 成果 + A1–A7）

- **P0–P2 语义零改动**：`internal/contextasm`/`internal/voice`/`internal/snapshot`/`internal/retcon` 不进本 WBS 文件触点（P4 可靠性触点限 llm/agent/pipeline/activity）。
- **agent 重试协议零改动**（§0.1）；**S5 熔断语义零改动**（阈值/失败计数/闸门通道原样，只新增派发点，§0.2）。
- **PanelId 零删除 / 双速语义勿合并**（P3 纪律平移）：度量埋点不改路由、不合并 chat 与流水线语义。
- **主题纪律**（CI theme-check 教训 #39）：样式一律 `index.css` 皮肤 token，禁硬编码色值。
- **desktop-first**（用户偏好）：布局按桌面设计；移动端 parity 不主动提案。
- **wails 绑定纪律**：新增 Go 方法经 `wails generate module`（带 `CGO_CFLAGS=-IC:/tools/sqlite3-headers`）再生，禁手改生成文件以外的漂移。

### 0.6 降级纪律（P0 四条平移）

1. 有则注入（有事件才走心跳、有度量才显示区块）；2. 无则降级且界面与旧行为逐字一致（统计失败=业务零感知；熔断判定异常=照常派发；GetFeatureUsage 失败=空表静默）；3. 预算照旧；4. 坏数据宁缺勿错（负数/空 key 拒收，不写脏数据）。

---

## 1. 阶段 1：供应商可靠性（闭 W6）

### T1.1 重试矩阵单源统一

- **内容**：llm 包抽 `apiErrorFromNetErr` / `apiErrorFromHTTP` 单源决策函数（§0.1 矩阵）；`stream.go`/`generate.go`/`toolcall_nonstream.go` 三路径改走单源（generate 网络错误从裸 error 升级为 APIError 分类、两非流式路径补 Retry-After）；单测锁定矩阵全行。
- **文件触点**：`internal/llm/{stream.go,generate.go,toolcall_nonstream.go}` + 新 `internal/llm/retry_matrix.go`（或并入 types.go）+ 测试。
- **验收**：①矩阵单测全行锁定；②三路径同错误同判定（含回归测试）；③agent 层重试协议零 diff（`internal/agent` 不进触点）。
- **依赖**：§0.1。
- **风险**：generate.go 网络错误从「不可重试」变「可重试」是**行为增强**——依赖 agent 既有 8 次退避兜底，不引入新循环；toolcall_nonstream 补永久分类后 DNS 故障不再空转重试。

### T1.2 熔断扩展（idle trip 全派发点）

- **内容**：抽共享熔断判定（阈值/窗口常量单源，§0.2）；全部 skillRunner 派发点（S2 蓝图 / S3 逐章重写 / S4 体检与复评 / G3 漂移巡检 / 一致性巡检 / S5 修复既有）派发前判定；熔断=跳过派发走各点原失败通道，提示统一「疑似供应商挂起」口径。
- **文件触点**：`internal/llm/stream.go`（导出判定）或 `internal/pipeline/service.go` + `patrol.go`（派发点接线）+ 测试。
- **验收**：①全派发点覆盖断言；②熔断触发零派发 + 失败通道语义不变；③未触发逐字一致（既有 pipeline 测试零回退）。
- **依赖**：T1.1（判定复用 RecentIdleTimeouts）。
- **风险**：各派发点失败语义不同（S5 批次计数/闸门 vs S3 逐章进度 error vs S4 paused）——熔断只决定「不派发」，落地路径各接各的，勿统一成一种；S3 是逐章循环，熔断应**中断整个循环**而非逐章失败刷屏。

### T1.3 长跑子 Agent 任务可见性

- **内容**：`SubagentCard` 增「已运行时长」时钟 + 「最近活动 Xs 前」心跳（§0.3，事件 Timestamp 派生，零后端改动）；完成/失败定格停表；多卡按 `sub_task_id` 独立。
- **文件触点**：`frontend/src/components/chat/SubagentCard.tsx`（+ 必要时 `types.ts` 透传首/末事件时间）；i18n 双语。
- **验收**：§0.3 四条。
- **依赖**：无（可与 T1.1/T1.2 并行，串行执行纪律照旧）。
- **风险**：秒级刷新用 interval 需随卡卸载清理（泄漏）；静默期只显示数值，禁止 spinner 变体弹窗骚扰。

---

## 2. 阶段 2：本地使用度量（闭 W5）

### T2.1 功能使用度量存储与绑定

- **内容**：`feature_usage_daily` 表 + `activity.Store.AddFeatureUsage`（尽力记录纪律）+ migrate AutoMigrate 注册 + `app.RecordFeatureUsage` / `app.GetFeatureUsage(days)` 绑定（§0.4）。
- **文件触点**：`internal/activity/{types.go,store.go}` + 测试；`internal/migrate/migrate.go`（模型清单 +1）；`app/activity.go`；wails 绑定再生。
- **验收**：①记录→查询闭环单测；②负数/空 key 拒收、失败只告警；③迁移集成测试零回退（done 步数 12 不变）。
- **依赖**：§0.4。
- **风险**：只加 AutoMigrate 新表、不动版本化迁移（步数断言不破）；绑定再生带 CGO_CFLAGS。

### T2.2 前端埋点接入（纯本地）

- **内容**：`lib/featureKeys.ts` 单源 key 枚举；埋点三类：面板打开（PanelId 切换）、旅程卡点击、关键动作（提取/重写/体检/修复/冻结/巡检/retcon 立案）；`RecordFeatureUsage` 失败静默。
- **文件触点**：新 `frontend/src/lib/featureKeys.ts` + 测试；`views/WorkspaceView.tsx`（面板切换埋点）；`components/journey/*`（旅程卡）；关键动作挂点；i18n 无新增（key 为机器枚举）。
- **验收**：①埋点触发后计数可查（与 T2.1 闭环）；②统计失败业务零感知（不抛错零提示）；③grep 断言无网络上报（纯本地）。
- **依赖**：T2.1。
- **风险**：埋点密度——只记 key+次数不记内容；面板打开埋点防抖（同面板重复切换不刷量），切换语义以 `activePanel` 变化为准。

### T2.3 度量消费面（取舍规则供数据）

- **内容**：ProfileView 增「功能使用率」区块（近 30 天 top 功能计数，`GetFeatureUsage` 驱动）；空数据/加载失败静默空态（降级逐字一致）。
- **文件触点**：`components/profile/ProfileView.tsx`（+ `useFeatureUsage.ts` query hook，沿 `useWritingStats` 模式）；`lib/queryKeys.ts`、`lib/queryErrorToast.ts`（静默口径）；i18n 双语。
- **验收**：①区块渲染近 30 天计数；②无数据=静默空态零报错；③i18n:check 双语齐。
- **依赖**：T2.2。
- **风险**：消费面是「供取舍规则看的数据」，不做成运营大屏（宁缺勿错，克制呈现）。

---

## 3. 阶段 3：验收与收尾

### T3.1 双验收（P4 验收标准本体）

- **内容**：端到端验收测试：①**可靠性闭环**——重试矩阵全行一致 + 熔断全派发点覆盖 + 长跑可见性四条（§0.1/0.2/0.3 验收汇总为联测）；②**度量闭环**——「记录→查询」功能使用率闭环 + 纯本地 grep 断言（无上报路径）+ 统计失败业务零感知。结论记入本节（3 行内）。
- **验收**：双联断言全绿 + 结论写入本文件。
- **依赖**：T2.3。

### T3.2 全量门禁

- **内容**：前端四道闸（prettier/i18n:check/eslint 限 0 error/build）+ theme:check；`go build ./...` + `go test ./...`（瞬态判定纪律照旧）；`git diff` 确认 P0–P2 工件（`internal/contextasm`/`internal/voice`/`internal/snapshot`/`internal/retcon`）零触碰。
- **验收**：全绿；P0–P2 工件零 diff。
- **依赖**：T3.1。

### T3.3 提交推送与收尾

- **内容**：按「一任务一提交」提交（含测试）；`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master` + `git status -sb` 核验；`.workbuddy/memory` 日志 + `MEMORY.md` 摘要。
- **验收**：远端同步；任务账销清。
- **依赖**：T3.2。

---

## 4. 横切关注点（贯穿）

- **降级逐字一致是最高验收**（P0–P3 纪律平移）：熔断判定异常=照常派发；统计失败=业务零感知；心跳静默=只显示数值。任何新失败路径都不得弹错/阻断用户动作。
- **纯本地不上传是隐私红线**：度量链路零网络上报（grep 断言锁定）；只记 feature key + 次数，不记内容。
- **只报告不拦截**：可见性/熔断/度量均不做用户动作拦截；Cancel/暂停等既有控制通道零改动。
- **单源纪律**：重试矩阵决策函数、熔断阈值常量、feature key 枚举各只允许一处定义。
- **agent 重试协议零改动**：P4 不动 `internal/agent` 重试循环（maxRetries/backoff/EventRetrying）。
- **desktop-first + theme token**（用户偏好 + CI #39 教训）。

## 5. 需求覆盖映射（plan §5-P4 → 任务）

| P4 需求（源自整合规划 §5/§2-W5/W6） | 覆盖任务 |
|---|---|
| 供应商重试矩阵（W6） | T1.1 |
| 熔断完善·扩展现有 idle trip（W6） | T1.2 |
| 长跑子 Agent 任务可见性（W6） | T1.3 |
| 本地使用度量·功能使用率·纯本地（W5） | T2.1 / T2.2 |
| 为取舍规则供数据（W5） | T2.3 |
| 验收①可靠性闭环 / ②度量闭环 | T3.1 |
| （可选）V3 跨书聚合进 Voice 层 | **不立账**（可选项，待度量数据再议） |

## 6. 风险与对策（plan §7 平移 + 本 WBS 增补）

| 风险 | 对策 |
|---|---|
| 熔断误杀（判定抖动阻断正常任务） | 判定异常照常派发；阈值沿用既有 10 次/10 分钟语义不变；仅跳过派发不改失败语义 |
| 重试行为增强引发循环放大 | 只统一错误分类，agent 层 8 次退避协议零改动（测试锁定） |
| 度量被质疑隐私 | 纯本地红线 + grep 断言 + 不记内容；消费面明示数据仅存本机 |
| 埋点刷量/噪音 | 只记 key+次数；面板埋点防抖；动作埋点限关键动作 |
| 新表破坏迁移断言 | 只挂 AutoMigrate 清单不动版本化迁移（done 步数 12 断言回归） |
| 心跳刷新泄漏 | interval 随组件卸载清理；静默期零弹窗 |
