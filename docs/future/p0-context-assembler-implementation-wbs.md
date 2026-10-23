# P0「接线」—— 统一上下文装配器 · 分阶段实施计划细化（WBS）

> 承接 `ultimate-form-integration-plan.md` P0 阶段（杠杆最大、闭环"冗余感"的一刀）。
> 顺序铁律：先定死契约 → 装配器核心 → 流水线注入 → chat 单源化 → intent 轻量 → 全量验证收尾。
> 每个任务标注「文件触点 / 交付物 / 验收 / 依赖 / 风险」，未过验收不得进入下一任务。
> **范围铁律**：P0 只接线、不结构化、不动前端；**无 voice / 无 intent 时输出与旧行为逐字一致**（降级测试锁定）。

---

## 0. 契约定案（冻结 · 实现唯一依据）

### 0.1 三块契约（ContextBlocks）

| 块 | 语义 | P0 内容源 | 终局源（P1+，非本 WBS） |
|----|------|-----------|--------------------------|
| `Facts` | 对不对：已冻结卷既定事实 | `snapshot.EstablishedFactsText`（**原样复用，不动**） | 同左 |
| `Voice` | 像不像：文风锚定 | `agentcfg.BuildPersonaSystemText` 产出的 persona/心智文本（**现成源，直接接**） | Voice 台账（P1 结构化） |
| `Intent` | 该不该：创作意图锚定 | book-pitch 应用产物 → 退化为当前卷纲摘要 → 无则空（T4.1 探查定源） | Intent 统一面板（P3） |

### 0.2 注入纪律（四条，平移卷级 WBS 2.3 已验证契约，勿改）

1. **有则注入**：块内容非空 → 按 §0.3 块格式渲染注入；
2. **无则降级为空串**：provider 为 nil / 返回空 / 返回 error → 该块不注入，**最终提示词与当前输出逐字一致**（error 只记日志，不阻断、不改文案）；
3. **预算截断**：见 §0.4，截断时块内标注「（已截断）」；
4. **坏数据宁缺勿错**：渲染失败整块放弃，绝不注入半截/乱码内容。

### 0.3 块格式定案（bibleBlock 范式扩展）

- **Facts 块**：`bibleBlock` 现有文案**一字不动**（【已冻结卷既定事实（系列圣经快照）】+ 尾注）。
- **Voice 块**（新 `voiceBlock`）：
  ```
  【写作人格 · 文风锚定】
  <purpose 相关引导语（§0.6 定案）>
  <心智正文（provider 返回值）>
  ```
  心智正文含 `PersonaHonestyNote` 诚实边界声明（由 provider 端保证，装配器不再重复拼）。
- **Intent 块**（新 `intentBlock`）：
  ```
  【创作意图锚定】
  <book-pitch/卷纲摘要正文>
  以上是本书的创作意图与卖点定位，写作与校验以此为方向锚定；与之冲突时以意图为准调整执行细节，不得偏离卖点定位。
  ```
- **块顺序固定**：`Facts → Voice → Intent`（硬事实在前、软约束在后）。
- **chat 路径块头不改**：chat 的 persona 常驻块继续用 `PersonaMarker`（【写作人格 · Persona】），**去重/持久化语义零变化**（T3 只做内容渲染单源化）。

### 0.4 预算定案

| 块 | 上限 | 超限行为 |
|----|------|----------|
| Facts | 24KB（现状不变，`snapshot.BibleText` 既有预算） | 既有表粒度截断，不动 |
| Voice | 8KB | 头部保留、尾部截断 + 标注「（已截断）」 |
| Intent | 8KB | 同上 |

总预算不设硬顶（三块独立上限，facts 优先级天然由在前保证）；purpose 不改变预算（简化，避免组合爆炸）。

### 0.5 purpose × 块矩阵（P0 适用范围）

| purpose | Facts | Voice | Intent | 说明 |
|---------|-------|-------|--------|------|
| `rewrite`（S3 重写） | ✅ | ✅ | ✅ | **核心受益者**，文风锚定的第一现场 |
| `audit`（S4 体检 / 完本审） | ✅ | ✅ | ❌ | voice 仅作**参照**；P0 不加文风漂移维度（P2 范围） |
| `fix`（S5 修复） | ✅ | ✅ | ❌ | 防止台账修复把文风写歪 |
| `patrol`（一致性巡检） | ✅ | ✅ | ❌ | 张力参照 |
| `chat`（对话写作） | —（走 Profile/State） | ✅（现状 persona，语义不变） | ❌（P3） | 只做内容单源化 |

### 0.6 Voice 引导语定案（按 purpose，替换 BuildPersonaSystemText 中的 chat 专属引导语拼装）

| purpose | 引导语 |
|---------|--------|
| `chat` | 「以下创作心智将指导你起草本轮新章节。请在保持故事世界一致性的前提下，以该作者的叙事习惯、表达指纹与创作决策启发式来写作，而不是简单模仿或套用公式。」（**与现状逐字一致**，golden 锁定） |
| `rewrite` | 「以下创作心智是本书的文风基准。重写时保持该叙事习惯、表达指纹与创作决策启发式，**不得因重写造成文风漂移**；解决既有问题优先于改写风格。」 |
| `audit` / `fix` / `patrol` | 「以下创作心智是本书的文风基准，本轮仅作文风参照（不单独扣分）；修复与校验不得引入与之冲突的文风。」 |

### 0.7 Provider 契约

```go
// internal/contextasm（新包）
type TextProvider func(ctx context.Context, novelID int64) (string, error)
```

- pipeline.Service 新增 `SetVoiceProvider` / `SetIntentProvider`；**`SetBibleProvider` 名字与签名保持不变**（兼容既有测试与 app 注入）。
- app 层（`handler.go`）接线：
  - voice：`agentcfg.BuildPersonaSystemText(personasetting.NewStore(...).Get(...), a.skill, novelID)`——persona 未配置/解析失败 → 空串（既有容错）；
  - intent：T4.1 探查后定源。
- 容错语义与 `bibleProvider` 完全同构（`establishedFactsText` helper 的 nil/err → "" 模式照抄）。

---

## 1. 阶段 1：装配器核心（internal/contextasm）

### T1.1 新建 `internal/contextasm` 包

- **内容**：`ContextBlocks{Facts, Voice, Intent string}`；`Assembler{facts, voice, intent TextProvider}` + `Build(ctx, novelID, purpose) (ContextBlocks, error)`（error 仅聚合日志用，块级失败置空）；块渲染器 `RenderFacts/RenderVoice/RenderIntent`（Facts 渲染直接委托 pipeline 既有 `bibleBlock` 文案常量——**文案单源**，或把 `bibleBlock` 迁入本包由 pipeline 调用，二选一，倾向后者并保持函数名）；§0.4 预算截断助手。
- **文件触点**：新 `internal/contextasm/{asm.go, blocks.go, budget.go}`；`internal/pipeline/service.go:2243 bibleBlock`（迁移或委托）。
- **验收**：包可独立编译；`go test ./internal/contextasm/...` 全绿。
- **依赖**：§0 契约。
- **风险**：与 pipeline 的 import 方向——contextasm **不得反向 import pipeline**（纯文本包，无重依赖）。

### T1.2 装配器单测（降级纪律的机械锁定）

- **内容**：①三 provider 全 nil → `Build` 输出空串且各 Render 返回空；②单块非空 → 只渲染该块；③多块顺序固定 Facts→Voice→Intent；④超预算截断 + 「（已截断）」标注；⑤provider 返回 error → 该块为空、其余块正常；⑥`purpose` 变化只影响 Voice 引导语（§0.6 表驱动测试）。
- **文件触点**：`internal/contextasm/*_test.go`。
- **验收**：上述 6 类用例全绿；**逐字断言**（string equal，非 contains）。
- **依赖**：T1.1。

---

## 2. 阶段 2：流水线注入（voice 进重写/体检/修复/巡检）

### T2.1 pipeline Service 装配接入

- **内容**：`service.go` 新增 `voiceProvider / intentProvider` 字段 + setter（同 `SetBibleProvider` 范式，service.go:144-147 照抄）；新增 `buildBlocks(ctx, novelID) contextasm.ContextBlocks` helper（替代各调用点直调 `s.establishedFactsText`）；`establishedFactsText`（service.go:155-166）保留为 facts 内部实现或删除并入。
- **文件触点**：`internal/pipeline/service.go:49-66（字段）/:144-166（setter+helper）/:813/:1251/:1595/:1706`、`internal/pipeline/patrol.go:47`。
- **验收**：编译过；既有测试全绿（此步不改提示词输出）。
- **依赖**：T1.1。

### T2.2 五个提示词构造函数改走块装配

- **内容**：`chapterRewritePrompt`（service.go:1610）/ `signabilityPrompt`（:2256）/ `fixPrompt`（:2291）/ `finalAuditPrompt`（:1752）/ `patrolPrompt`（patrol.go:25）签名由 `bibleText string` 改为 `blocks contextasm.ContextBlocks`（或加尾参 `voiceText, intentText`，与现 `bibleText` 尾参风格一致——**推荐前者**，一处收口）；各函数尾部 `bibleBlock(bibleText)` 改为 `blocks.Render(purpose)`。`blueprintAuditPrompt`/`blueprintFixPrompt` **不动**（蓝图级无文风诉求，P0 不扩）。
- **文件触点**：上述 5 函数及其调用点（T2.1 列出行号）。
- **验收**：blocks 仅 Facts 非空时，输出与改造前**逐字一致**（golden 对照测试）；Facts+Voice 时输出含 §0.3 Voice 块且顺序正确。
- **依赖**：T2.1。
- **风险**：`fixPrompt`/`signabilityPrompt` 内嵌 JSON 契约片段（contract.go 单源）在前、块在尾——保持"块只追加在末尾"的既有格局，勿把块插进 JSON 契约前后文之间。

### T2.3 app 层接线

- **内容**：`app/handler.go`（参照 :439 `SetBibleProvider` 注入点）追加 `SetVoiceProvider`（persona 解析复用 `personasetting.Store.Get` + `agentcfg.BuildPersonaSystemText`）与 `SetIntentProvider`（T4.1 定源后接；P0 前半段可先只接 voice，intent 留 nil 降级）。
- **文件触点**：`app/handler.go:427-445 一带`；`app/persona_api.go`（复用）。
- **验收**：`wails dev` 跑一轮 S3 重写，提示词含/不含 Voice 块行为符合 §0.2。
- **依赖**：T2.2。

### T2.4 流水线降级 golden 测试

- **内容**：新增测试锁定降级契约：①voice/intent provider 为 nil → 五类提示词输出与本 WBS 改造前基线**逐字一致**（基线字符串固化在测试里）；②voice 非空 → 只在尾部追加 Voice 块（Facts 块与前文不动）；③intent 同理。
- **文件触点**：`internal/pipeline/service_test.go`（或新 `context_blocks_test.go`）。
- **验收**：三类用例全绿，逐字断言。
- **依赖**：T2.2。

---

## 3. 阶段 3：chat 内容单源化（零行为变化）

### T3.1 `BuildPersonaSystemText` 渲染单源化

- **内容**：`agentcfg.BuildPersonaSystemText`（persona.go:32-50）的拼装逻辑改为委托 `contextasm.RenderVoice(purpose=chat)`（引导语按 §0.6 chat 行**逐字保留**；`PersonaMarker` 与 `PersonaHonestyNote` 位置/文案不变）。**msg.Persona 结构、每轮注入去重（`messagesContainPersona`）、持久化语义全部零改动。**
- **文件触点**：`internal/agentcfg/persona.go:32-50`。
- **验收**：新增 golden 测试断言改造前后输出**逐字一致**；`persona_chat_test.go` 全绿。
- **依赖**：T1.1。
- **风险**：chat 去重靠 `PersonaMarker` 前缀匹配——marker 文案改动即破坏去重，**禁止改**。

### T3.2 chat 侧回归

- **内容**：跑 `go test ./app/... ./internal/agentcfg/...`；手工过一遍新会话常驻块 + 旧会话每轮注入两条路径。
- **验收**：测试全绿；两条路径注入次数与改造前一致（无重复、无缺失）。
- **依赖**：T3.1。

---

## 4. 阶段 4：intent 轻量接线

### T4.1 intent 源探查与定源

- **内容**：探查 book-pitch 应用产物的持久化位置（`useBookPitchStore` 应用后落哪张表/字段）与卷纲文本可得性；定源优先级：book-pitch 应用产物 → 当前卷纲摘要（`volumes` 卷纲 markdown，截断）→ 空。产出 5 行以内定源结论记入本节。
- **文件触点**：`app/`（book-pitch 相关 handler）、`internal/pipeline`（不涉）。
- **验收**：定源结论写入本文件；provider 实现 + 单测（有源/无源两路）。
- **依赖**：T2.3（接线位）。
- **风险**：book-pitch 可能仅存前端 store 未落库——若如此，P0 intent 源退化为卷纲摘要，book-pitch 落库列为 P3 提示项（**不阻塞 P0**）。
- **定源结论（2026-10-23 探查）**：
  1. book-pitch **已落库**（G1 风险解除）：终稿字段双通道落库——`novels` 表（title/description/genre，`UpdateNovel`）+ `preference_items` 表（平台约束/创作约束/标签/方向/一句话卖点五类 category，`persistPitchPreferences`→`CreatePreference`，novel 级）。
  2. 卷纲文本可得：`volumes/id_{id}.md`（`git.ReadFile(novelID, git.VolumePath(id))`），「当前卷」=最新写就章节所属卷（同 pipeline.currentVolumeScope 口径）。
  3. provider（`app/intent.go`）定源优先级：book-pitch 应用产物（novels 字段 + 五类偏好，同类多条取最新终稿，非 pitch 分类不混入）→ 当前卷纲正文 → 空串降级；预算截断由装配器 fitBudget 统一负责。
  4. `useBookPitchStore` 仅存前端会话态（incubatorId/appliedPitchSig），非持久化源，不作 intent 源。

### T4.2 rewrite 路径注入 intent

- **内容**：仅 `chapterRewritePrompt` 按 §0.5 矩阵启用 Intent 块（audit/fix/patrol 不注）。
- **验收**：intent 空 → 逐字一致（沿用 T2.4 golden）；intent 非空 → 块序 Facts→Voice→Intent。
- **依赖**：T4.1。

---

## 5. 阶段 5：全量验证与收尾

### T5.1 全量门禁

- **内容**：`go test ./...`；`golangci-lint run`（gofmt+lint 钩子同源）；前端四道闸（prettier --check / i18n:check / eslint / build）——**前端零改动，四道闸应原样通过**（防误伤）。
- **验收**：全绿；`git diff` 确认前端源码零触碰。
- **依赖**：T4.2。

### T5.2 主观 A/B 验收（P0 唯一的"人感受"验收）

- **内容**：同一章节分别在 voice 开/关下跑 S3 重写，对比产出：①文风锚定语句生效、叙事习惯可见保持；②关 voice 时与 P0 前行为无差别。结论记入本节（3 行内）。
- **验收**：A/B 结论写入本文件。
- **依赖**：T5.1。

### T5.3 提交推送与收尾

- **内容**：按「一任务一提交」提交（含测试）；`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master` + `git status -sb` 核验；`.workbuddy/memory` 日志 + `MEMORY.md` 摘要（P0 完成状态与经验坑）。
- **验收**：远端同步；任务账销清。
- **依赖**：T5.2。

---

## 6. 横切关注点（贯穿）

- **降级逐字一致是本 WBS 的最高验收标准**：任何一步输出了"多出来的字"（哪怕注释性文案），视为 P0 bug——它会让"无 voice=旧行为"的兼容承诺失效。
- **文案单源**：块文案（facts/voice/intent 的块头、引导语、尾注）只允许存在于 contextasm 一处；pipeline/agentcfg 不得自带副本。
- **注入只追加在末尾**：三块统一追加在提示词尾部，不插入 JSON 契约片段/方法论引用之间（既有格局不破）。
- **AGPL/NOTICE 义务不涉**（本 WBS 无新依赖）；模块路径 `github.com/novelgo/novelgo` 不动。

## 7. 与既有代码的衔接（避免打架）

| 既有资产 | 本 WBS 的关系 |
|----------|----------------|
| `SetBibleProvider` / `establishedFactsText`（service.go:144-166） | **名字签名不变**；内部并入装配器，测试兼容 |
| `bibleBlock`（service.go:2243）文案 | 迁入/委托 contextasm 后**文案逐字不变** |
| `BuildPersonaSystemText` + `PersonaMarker` 去重 | 只换拼装实现，**输出逐字不变**、marker 禁改 |
| 卷级 WBS 2.3 注入纪律 | 四条纪律原样平移为 §0.2，机械测试锁定 |
| `blueprintAuditPrompt`/`blueprintFixPrompt` | **不扩**（蓝图级无文风诉求） |
| S5 修复有界化 / FixStates / RewriteBacklog | 零触碰（本 WBS 只改提示词组装，不动流转） |

## 8. 完备性核对

### 8.1 需求 → 任务覆盖矩阵

| P0 需求（源自整合规划 §5） | 覆盖任务 |
|---------------------------|----------|
| ContextAssembler v1（facts/voice/intent 契约） | T1.1/T1.2 |
| v1 voice 取现有 persona/心智文本 | T2.3 |
| S3 重写注入 voice | T2.2（rewrite）+ T2.3 |
| S4 体检 / S5 修复注入 voice | T2.2（audit/fix/patrol）|
| BuildSystemMessages 改走装配器 | T3.1/T3.2 |
| 验收①文风可辨识改善 | T5.2 |
| 验收②无 voice 逐字一致 | T1.2/T2.4/T3.1 golden |
| 验收③既有测试全绿 | T5.1 |

### 8.2 疏漏清单（立项时已识别，执行中发现新项追加于此）

| # | 疏漏/风险 | 处置 |
|---|-----------|------|
| G1 | book-pitch 可能未落库，intent 无源 | T4.1 探查；无源则退化卷纲摘要，不阻塞 |
| G2 | audit 注入 voice 可能诱导 agent"按文风扣分" | §0.6 引导语明示"仅作参照不扣分"；发现误用再收紧 |
| G3 | 多书并发下 provider 解析成本 | persona 解析为按次读 store，量小；如成热点再缓存（P1 顺带） |
