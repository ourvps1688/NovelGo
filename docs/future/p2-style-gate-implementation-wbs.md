# P2「质检扩展（文风进闸门，闭 W1 彻底）」—— 漂移检测 · 双快照 · 风格回滚 · 分阶段实施计划细化（WBS）

> 承接 `ultimate-form-integration-plan.md` P2 阶段（W1「流水线不认文风」彻底闭合；P1 G2 预留「阈值属 P2 漂移检测」销账）。
> 顺序铁律：先定死契约 → 确定性检测器 → S4 接线与路由 → 体检技能文风维度 → 双快照/风格回滚 → 验收收尾。
> 每个任务标注「文件触点 / 交付物 / 验收 / 依赖 / 风险」，未过验收不得进入下一任务。
> **范围铁律**：P2 只做文风漂移检测进闸门 + G_freeze 双快照语义/风格回滚 + retcon 风格案路由；**前端零改动**（UI 属 P3）；**V 系一律 warning 级、不阻断、不自动改写**（「只报告起步」plan 非目标 #5）；**无基线/无文本/坏数据 → 零 issue**（降级纪律原样延续）；A1–A7 勿回退。

---

## 0. 契约定案（冻结 · 实现唯一依据）

### 0.1 漂移检测契约（确定性数值比对，非 LLM）

- **输入**：①当前章正文 → `style.ComputeStats`（`internal/style/utils.go`，确定性统计）；②风格基线 = `voice_dimensions.metrics` JSON（P1 落库产物，键= `voice.MetricKey*` 八键，**复用常量禁改**）。
- **比对纯函数**：`internal/voice/drift.go` 新增 `DetectDrift(baseline, current map[string]float64) []DriftFinding`。逐键比对，命中条件统一为 `|current−baseline| > max(abs_tol, rel_tol×|baseline|)`。
- **阈值表单源**（`internal/voice/drift.go` 常量表，保守初值只报明显偏离，P4 度量后调参）：

| metric 键 | rel_tol | abs_tol |
|---|---|---|
| `avg_sent_len` | 25% | 3 |
| `sent_len_std_dev` | 30% | — |
| `short_sent_pct` | — | 8（百分点） |
| `quote_density` | — | 3（百分点） |
| `comma_density` / `period_density` | 40% | 0.5（百分点） |
| `exclaim_density` / `question_density` | 40% | 0.3（百分点） |

- **宁缺勿错跳过规则**：基线键缺失 / 当前值算不出 / metrics JSON 坏 → **该键跳过**；基线整体为空 → **零 issue**（不造默认基线）；全书无 voice 落库 → 检测不运行（与旧行为逐字一致）。
- **检测范围** = S4 体检同作用域章节（卷级 scoped run 扫当前卷；S6 终检扫全本），逐章独立出 issue（issue 原子化纪律：>2 章必拆，禁 meta-issue）。

### 0.2 V 系 issue 契约（ID 命名空间与字段映射）

- **ID 前缀 `V` 单源**（`issueSampleStyle = issueSample{ID:"V1", Level:"P1", Area:"文风漂移·章节#12"}`，`internal/pipeline/contract.go` 追加，体例同 `issueSampleDrift`）。**命名空间裁决**：`D` 系已归属 G3 蓝图漂移巡检（`driftPatrolPrompt`/`issueSampleDrift`），文风漂移不得复用 D，防同 run 混淆。
- **Level 恒为 `P1`**（plan「warning 级起步」映射到既有二级：P0=阻断/P1=提升；不上 error/P0）。
- **三枚举照常**（plan「issue 沿用三枚举字段」）：
  - `TargetScope`：章节所在卷已冻结 → `frozen_volume`；活跃卷 → `active_volume`。
  - `Dimension`：恒 `none`（文风非四维 series bible 事实；同 signability-audit「纯质量问题填 none」纪律）。
  - `Direction`：`frozen_volume` → `retcon_candidate`（写保护命中）；`active_volume` → `forward_fix`（缺省）。
- `fix_type` 恒 `body`（偏离在正文，消解=章节重写或作者改基线）；`Area` 形如「文风漂移·章节#N」；`Desc` 含指标名/基线值/当前值/容差。
- 检测器产出与 LLM 侧（T3.1）产出**同契约**；派发豁免以 ID 前缀 `V` 为判别（issue 不加新字段，三枚举沿用）。

### 0.3 分级纪律与派发契约（「只报告不改写」×「自动进路由」消解，本 WBS 核心裁决）

| 约束（plan 原文） | 落点（本 WBS 定案） |
|---|---|
| 「warning 级起步，不阻断」 | Level=P1；**V 系不翻转 verdict**（drift-only 体检 verdict 保持 pass，不触发收口修复循环）；不进 40 分评分 |
| 「只报告不改写（A5 纪律）」 | ①检测器零写入（纯函数，无任何写句柄，机械保证）；②**自动派发豁免**：自动重写派发与自动修复批次派发一律跳过 V 系（阈值未成熟期自动改写=误报骚扰，plan 非目标 #5） |
| 「漂移 issue 自动进既有修复/重写路由」 | V 系经 `bodyIssuesOf`/`retconItemsOf` **自然收口**进既有容器（body→RewriteBacklog、write-protected/retcon_candidate→RetconBacklog→handoff 立案），零新路由通道；作者**显式推进**（手动重写入口/写作页重写/复评/手动立案）不受豁免限制 |
| A6 有界化勿回退 | 全 V 系 RewriteBacklog **不得触发自动重写循环**：handoff 判定只计可派发项，全豁免 → 走手动交接收口（paused + note「文风漂移项待作者裁决」），测试锁定 |

- LLM 侧（T3.1）V 系输出同样受豁免；即便 LLM 误翻 verdict，豁免谓词兜底零自动改写（有界化三层防线）。

### 0.4 G_freeze 双快照契约（风格随卷冻结）

- **「双快照」语义裁决 = 冻结快照双层钉住**：G_freeze 的 committed snapshot **同事务**同时钉住事实层（10 表+正文清单）与风格层（voice_dimensions，P1 已入 registry 采集）。**不拆两条快照记录**——拆分会动 `DiffToPrevious` 基线语义与 uk 唯一键，违背 A2「机制零改动」；plan 风险「双快照拖慢冻结」的对策（风格量小、同事务、uk 冲突重试）已由 registry 采集路径原样满足（风格行 ≤10/书）。
- **风格回滚锚点 = 冻结快照本身**（其 voice_dimensions 行）；书级 style 后续演进（再提取/人工修订）不改写任何已冻卷快照（快照不可变，语义测试钉死）。
- **新增 `snapshot.RestoreStyle(snapshotID)`**：按表回滚，只还原 `voice_dimensions`（内部复用既有单表 restore 管线：DELETE+raw SQL INSERT、schema 反射、时间戳保真、绕开 temporal/oplog 回调——A2 纪律原样）；**防误伤事实层**（voice 为书级行，全量 Restore 会把全书事实表打回快照态，风格回滚不得动事实）。
- 既有全量 `Restore` 与 capture/diff/registry **零改动**；P1 测试（freeze payload 含 voice 行）是回归防线，不得回退。

### 0.5 retcon 风格回改契约（A3 零新流程）

- `RetconCase` 增可选 `kind` 列（枚举单源：`fact`（缺省）/`style`；存量行回填 fact）；**A3 状态机零改动**（`state.go` 迁移图/终态封死不触碰，`state_test` 既有全绿=机械证明）。
- **立案**：`CreateInput.Kind` 可选；V 系 issue 走 `handoffRetconToUser` 自动立案时带 `kind=style`（同 issue 未终态幂等去重沿用）；前端手动立案缺省 fact（风格案选择器属 P3）。
- **回滚执行体按 kind 路由**：`style` → `RestoreStyle`（只还原 voice 行）；`fact` → 既有全量 `Restore`。BeginEdit 回滚点固化 / Record 差异摘要（`FormatDelta` 已可读 voice 行，P1 T4.2）/ Record·Rollback 终态前各 Commit 一份 canon——**全部沿用不改**。
- **testhelpers 必须与 handler.go 同形装配全部钩子**（现有三钩子 + 新增风格 Restorer；漏装=nil receiver panic，既有坑位勿回退）。

### 0.6 降级纪律（P0 四条平移 + P2 增补一条）

1. 有则注入；2. 无则降级且输出与旧行为逐字一致；3. 预算截断照旧；4. 坏数据宁缺勿错（渲染失败整块放弃）。
5. **（P2 新增）无基线/无文本/坏 metrics → 检测跳过、零 issue**——不产出任何猜测性漂移，旧体检输出逐字不变。

---

## 1. 阶段 1：漂移检测器（确定性数值）

### T1.1 阈值表与 DetectDrift 纯函数

- **内容**：新 `internal/voice/drift.go`——阈值表（§0.1，常量单源）+ `DriftFinding{Metric, Baseline, Current, Tol}` + `DetectDrift` 纯函数（逐键比对/宁缺勿错跳过/空基线零输出）。
- **文件触点**：新 `internal/voice/drift.go` + `drift_test.go`。
- **验收**：单测——超限命中（rel/abs 两种容差路径）、限内不报、缺键跳过、坏 JSON/空基线零输出、`MetricKey*` 八键全表覆盖。
- **依赖**：§0.1 契约。
- **风险**：阈值初值宁宽勿窄（只报明显偏离）；键名一律引用 `voice.MetricKey*` 常量，禁字符串字面量两处各写。

### T1.2 采样装配与 V 系 issue 成形

- **内容**：`internal/pipeline` 新 `drift.go`——章节文本→`style.ComputeStats`→八键 current map 装配；基线读取装配（注入 provider，§0.2）；`DriftFinding[]`→`AuditIssue` 成形（V 系 ID 顺编、三枚举映射、fix_type=body）；`issueSampleStyle` 常量入 `contract.go`。
- **文件触点**：新 `internal/pipeline/drift.go` + `drift_test.go`；`internal/pipeline/contract.go`（常量）。
- **验收**：成形单测——ID 前缀/Level/三枚举映射（active vs frozen 卷两路）/Desc 含四要素；空 findings 零 issue。
- **依赖**：T1.1。
- **风险**：V 系 ID 顺编避免与 LLM issue ID 冲突（保留前缀命名空间即可，同 run 内不混排断言）。

---

## 2. 阶段 2：S4 接线与路由

### T2.1 executeAudit 漂移检测接线

- **内容**：`executeAudit` 在 `parseFreshAudit` 后追加 V 系 issue（检测范围=同作用域章节，卷级扫当前卷/S6 扫全本）；注入 `chapterTextReader`/`voiceMetricsProvider`（app 层用 `git.ReadFile(novelID, git.ChapterPath(id))` 与 `voice.Store` 装配，体例同 `SetVolumeFreezer`/`SetBibleProvider`）；**verdict 不翻转**（V 系不改写 LLM verdict）。
- **文件触点**：`internal/pipeline/service.go`（executeAudit 落点 ~840–900 行一带）、`app/handler.go`（两 provider 注入）。
- **验收**：接线测试——①有基线+超限章 → audit.Issues 尾部含 V 系；②无基线/无 provider → issue 零新增、verdict 与旧行为一致（降级逐字）；③drift-only → verdict 不翻转。
- **依赖**：T1.2。
- **风险**：provider 为 nil/出错一律降级零 issue（宁缺勿错），不得阻断体检主链。

### T2.2 路由收口与派发豁免

- **内容**：V 系经既有 `bodyIssuesOf`/`retconItemsOf` 自然收口（**路由容器零改动**）；新增 V 系派发豁免谓词（`isStyleExempt(id)`，ID 前缀判别），挂入自动重写派发过滤与自动修复批次派发过滤；`finishFixHandoffRewrite` handoff 判定只计可派发项，全豁免 → 手动交接收口（note「文风漂移项待作者裁决」）。
- **文件触点**：`internal/pipeline/service.go`（派发过滤 ~1144/1433–1444 一带 + handoff 判定）。
- **验收**：路由测试——①V 系 body → RewriteBacklog、V 系 retcon_candidate → RetconBacklog（自动进路由 ✓）；②自动派发循环跳过 V 系（零自动改写断言，A5 机械保证）；③全 V 系 RewriteBacklog 不触发自动重写循环、走手动交接。
- **依赖**：T2.1。
- **风险**：豁免谓词勿误伤非 V 系（普通 I 系 issue 派发行为逐字不变=回归防线）；死循环防护是 P1「完成但零修复」教训的正向迁移，测试必锁。

---

## 3. 阶段 3：体检技能文风维度（LLM 主观侧）

### T3.1 signability-audit.md 新增文风漂移维度

- **内容**：技能增「文风漂移」检查节：对照 Voice 契约（P0 已注入 audit 提示词）审读章节文风偏移，输出 V 系 issue（warning 口径、/40 评分结构不动、不翻 verdict 提示语）；主观品味类偏移（用词/味道/腔调）同 V 系输出。
- **文件触点**：`internal/skill/builtin/signability-audit.md`。
- **验收**：技能文本审查——/40 结构与既有输出格式逐字不破（B 系对齐勿回退）；V 系示例走 `auditFormatLine` 全量契约；既有解析测试全绿。
- **依赖**：T2.2。
- **风险**：LLM 把文风项记进评分或 P0 级——提示语显式禁止；即便发生，豁免谓词兜底零自动改写。

### T3.2 契约与解析兼容验证

- **内容**：`issueSampleStyle` 接线 `auditFormatLine`（driftPatrolPrompt 同款体例）；`parseFreshAudit`/陈旧 run 判定（`issueContractGroups` 宁松勿杀纪律）对 V 系兼容；`contract_test.go` 补例。
- **文件触点**：`internal/pipeline/contract.go`、`contract_test.go`。
- **验收**：contract 测试含 `issueSampleStyle` schema 片段断言（体例同 `issueSampleDrift`/`issueSamplePatrol` 既有断言）；陈旧判定不误伤 V 系。
- **依赖**：T3.1。

---

## 4. 阶段 4：双快照语义与风格回滚

### T4.1 双快照语义固化与 RestoreStyle

- **内容**：`snapshot.RestoreStyle(snapshotID)`（§0.4，复用单表 restore 管线只动 voice_dimensions）；语义测试钉死「冻结双层钉住/风格回滚不动事实层/冻结后书级 style 演进不改冻结快照」。
- **文件触点**：`internal/snapshot/restore.go`（或新 `style.go`）+ `service_test.go` 语义测试。
- **验收**：三态测试——①freeze 后改书级 voice 行 → 冻结快照内容不变（不可变性）；②RestoreStyle 还原 voice 行且**事实表逐行不变**（防误伤断言）；③全量 Restore 行为与既有逐字一致（回归）。
- **依赖**：T2.2（不强依赖阶段 3，可并行）。
- **风险**：RestoreStyle 勿复用全量 Restore 的整包语义（必须按表过滤）；raw SQL Exec/时间戳保真纪律原样平移。

### T4.2 retcon 风格回改（kind 路由）

- **内容**：`RetconCase.Kind` 列（fact 缺省/style，migrate 加列回填）；`CreateInput.Kind`；`handoffRetconToUser` 对 V 系立案带 style；Rollback 按 kind 路由执行体（style→RestoreStyle / fact→全量 Restore）；handler.go + testhelpers 同形装配风格 Restorer。
- **文件触点**：`internal/retcon/case.go`、`service.go`、`store.go`/migrate；`internal/pipeline/service.go`（handoff）；`app/handler.go` + `app/testhelpers`（同形装配）。
- **验收**：风格案全链测试（candidate→approved→editing→recorded→rolled_back，Rollback 只动 voice 行）；A3 `state_test` 既有全绿（零改动机械证明）；存量 fact 案行为逐字不变。
- **依赖**：T4.1。
- **风险**：testhelpers 漏装新钩子=nil panic（既有坑位，测试先行）；kind 迁移勿动状态机。

---

## 5. 阶段 5：验收与收尾

### T5.1 双验收（P2 验收标准本体）

- **内容**：端到端验收测试：①**漂移 issue 自动进既有修复/重写路由**（V 系自动分流进 RewriteBacklog/RetconBacklog→立案，且自动派发豁免断言）；②**冻结卷风格可回滚**（style retcon 全链 Rollback 后 voice 行回冻结态、事实层不动）。结论记入本节（3 行内）。
- **验收**：双联断言全绿 + 结论写入本文件。
- **依赖**：T4.2。
- **结论**：（待 T5.1 执行时填写）

### T5.2 全量门禁

- **内容**：`go test ./...`（瞬态判定纪律：单项+包复跑绿即收口）；`golangci-lint run`；前端四道闸（prettier/i18n:check/eslint/build）——**前端零改动，四道闸原样通过**（防误伤）。
- **验收**：全绿；`git diff` 确认 `frontend/` 源码零触碰。
- **依赖**：T5.1。

### T5.3 提交推送与收尾

- **内容**：按「一任务一提交」提交（含测试）；`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master` + `git status -sb` 核验；`.workbuddy/memory` 日志 + `MEMORY.md` 摘要。
- **验收**：远端同步；任务账销清。
- **依赖**：T5.2。

---

## 6. 横切关注点（贯穿）

- **降级零 issue 是本 WBS 最高验收标准**：无基线/无文本/坏数据时体检输出与旧行为逐字一致——多一条 V 系 issue 即 bug。
- **A5「只报告不改写」机械保证**：检测器纯函数零写入 + V 系自动派发豁免 + verdict 不翻转，三层各有测试锁定；勿以「顺手修一下」破坏。
- **枚举单源**：`MetricKey*` 八键、issue 三枚举、V/D/T/I ID 命名空间、retcon kind 枚举、阈值表，各只允许一处定义。
- **A1–A7 勿回退**：状态机/快照事务纪律（raw SQL restore）/retcon 单向无环/审计严重度分界/S5 有界化/写保护双通道全部原样。
- **前端零改动**：V 系 issue 经既有 IssueTags/面板可见，风格案经既有 RetconPanel 走 A3；UI 增强属 P3。
- **AGPL/NOTICE 义务不涉**（无新依赖）；模块路径不动。

## 7. 与既有代码的衔接（避免打架）

| 既有资产 | 本 WBS 的关系 |
|----------|----------------|
| P1 voice_dimensions/metrics（`internal/voice`） | 基线真理源；`MetricKey*` 八键复用禁改 |
| `style.ComputeStats`（`internal/style`） | 采样统计复用（键与 MetricKey 对齐，P1 G2 预留兑现） |
| issue 全量契约 + 三枚举 | 沿用零改动；V 系新前缀（**D 归 G3 蓝图漂移**，勿混用） |
| `bodyIssuesOf`/`retconItemsOf`/`planFixBatches` | 自然收口零改动；仅派发过滤加 V 系豁免谓词 |
| 自动重写回路（RewriteAgent/autoRewriteCap=2） | 派发过滤加豁免；handoff 判定防全豁免死循环 |
| G_freeze / snapshot（A2） | 双层钉住语义固化 + RestoreStyle 按表回滚；capture/diff/全量 Restore 零改动 |
| retcon（A3） | 零新流程；+kind 列与回滚执行体路由 |
| contextasm（P0）/Voice 注入 | 零改动（audit/rewrite 已带 Voice 上下文，T3.1 复用） |
| 一致性巡检（3.3，T 系 issue） | 零触碰（张力报告独立通道） |
| S5 修复闭环 / 修复回执 / FixStates | 零触碰（V 系 fix_type=body 本就不进 S5 批次） |
| 前端面板（阶段 4 UI） | 零改动 |

## 8. 完备性核对

### 8.1 需求 → 任务覆盖矩阵

| P2 需求（源自整合规划 §5） | 覆盖任务 |
|---------------------------|----------|
| S4 新增文风漂移维度（warning 起步） | T1.1/T1.2（确定性）+ T3.1（LLM）+ T2.1（接线） |
| issue 沿用三枚举（forward_fix/retcon_candidate） | T1.2（成形映射）+ T2.2（路由） |
| 风格快照随卷冻结（G_freeze 双快照） | T4.1 |
| retcon 支持风格回改（A3 零新流程） | T4.2 |
| **验收①**：漂移 issue 自动进既有修复/重写路由 | T2.2 + T5.1 |
| **验收②**：冻结卷风格可回滚 | T4.1/T4.2 + T5.1 |
| （plan 风险）误报骚扰 / 双快照性能 | §0.3 分级纪律 / §0.4 同事务裁决 |

### 8.2 疏漏清单（立项时已识别，执行中发现新项追加于此）

| # | 疏漏/风险 | 处置 |
|---|-----------|------|
| G1 | V 系误报骚扰（阈值未成熟） | warning + verdict 不翻转 + 自动派发豁免（只报告起步）；阈值 P4 调参 |
| G2 | 全 V 系 RewriteBacklog 触发自动重写死循环 | handoff 判定只计可派发项，全豁免 → 手动交接收口（测试锁定） |
| G3 | ID 命名空间冲突（D=蓝图漂移 G3 已占） | V 系新前缀单源（issueSampleStyle）；D/T/I 归属不变 |
| G4 | 双快照拖慢冻结 / 拆记录动 DiffToPrevious | 双层同快照同事务（registry 携带，风格行 ≤10/书）；不拆两条快照记录 |
| G5 | 风格回滚误伤事实层（voice 为书级行） | RestoreStyle 按表只动 voice_dimensions；retcon 按 kind 路由 |
| G6 | retcon 新钩子 testhelpers 漏装 → nil panic | T4.2 同形装配 + 测试先行（既有坑位） |
| G7 | LLM 侧 V 系输出不稳定（ID/字段缺漏/误进评分） | issueSampleStyle 走全量契约 + parseFreshAudit 宁松勿杀兼容 + 豁免谓词兜底 |
| G8 | 无风格基线（未提取/未落库） | 检测跳过零 issue（降级纪律第 5 条） |
| G9 | 手动立案无风格选择（前端零改动约束） | 自动立案（V 系 handoff）带 kind=style；手动 UI 选择器属 P3 |
