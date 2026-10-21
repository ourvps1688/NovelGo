# 卷级作用域流水线 —— 分阶段实施计划细化（WBS）

> 承接原 RFC 的分阶段路径（原文件已归档至 `archive/volume-scoped-pipeline-and-ledger-rfc.md`，其独有论据并入本文件附录 A），本文件把它细化为**可执行工作分解**。
> 顺序铁律：先定死数据模型（作用域 / 维度 / 快照）→ 再做路由与写保护 → 再台账结构化 → 再审批与巡检 → 最后前端。
> 每个阶段都标注「文件触点 / 交付物 / 验收标准 / 依赖 / 风险」，未过验收不得进入下一阶段。

---

## 0. 全局数据字典（贯穿所有阶段，先定死避免返工）

所有枚举值封闭、字段结构化（非自由文本）。这是"周全"的落点——四维缺一，前向修复就退化成瞎改。

### 0.1 issue 的作用域 / 维度 / 方向
| 字段 | 类型 | 枚举 | 说明 |
|------|------|------|------|
| `target_scope` | enum | `active_volume` / `frozen_volume` / `none` | issue 指向哪一卷；`none`=全书级/不涉具体卷 |
| `dimension` | enum | `character` / `locus_arc` / `timeline` / `reader_pov` / `none` | 归属哪一维；`none`=不属四维的纯质量问题（节奏/爽点/文笔等） |
| `direction` | enum | `forward_fix` / `retcon_candidate` | 默认 `forward_fix`，硬矛盾才 `retcon_candidate` |

### 0.2 series bible 四维结构化字段
> 四维每条目均嵌入 §0.3 的 `temporal` 时间戳块（生成 / 维护 / 变革 + 变更履历）。
**角色 `character`**
- `id`, `name`, `aliases[]`
- `identity`: `{ gender, birth_year|age, appearance_anchors[] }`（锚点例：「左手刀疤」「左撇子」）
- `status`: `{ alive:bool, location_ref, faction, role }`
- `capability`: `{ ceiling_desc, known_skills[], sealed:bool }`（封禁/封印态，防后期偷偷加挂）
- `relations[]`: `{ target_id, type: mentor|rival|lover|kin }`
- `voice_traits[]`（软约束，供一致性检查）

**地点 / 弧线 `locus_arc`**
- `locations[]`: `{ id, name, controller, props[], state: intact|destroyed|blocked }`
- `character_arcs[]`: `{ char_id, stage: start|turning|goal, current }`
- `plot_threads[]`: `{ id, type: mystery|foreshadow, status: open|closed|suspended, expected_payoff_volume }`（**open 线程禁止静默丢弃**）
- `theme_arcs[]`: `{ motif }`

**时间线 `timeline`**
- `absolute_anchors[]`: `{ label, era|date, character_ages{}, season }`
- `sequence[]`: `{ event, order }`
- `gaps[]`: `{ after_volume, duration }`（卷间空白期显式标注，防推算漂移）

**读者视角 `reader_pov`**
- `pov_roster[]`: `{ char_id, mode: limited|omniscient }`
- `knowledge_boundaries[]`: `{ char_id, timepoint, facts_known[], facts_unknown[] }`
- `reader_facts[]`（读者已知态，状态机，禁无 retcon 撤销）: `{ fact_id, kind: known|suspense|misconception, revealed_to_reader:bool, lifecycle, temporal }`
  - **已知 `known`**：`lifecycle.revealed_at`（揭示给读者的时点）
  - **悬念 `suspense`**：`lifecycle.raised_at`（埋设）→ `lifecycle.payoff_at`（回收，null=未回收）+ `expected_payoff_volume`
  - **误解 `misconception`**：`lifecycle.formed_at`（形成）→ `lifecycle.corrected_at`（纠正，null=持续中）

### 0.3 条目时间戳 `temporal`（四维每条目通用，回答"什么时候改变了"）
四维（角色 / 地点·弧线 / 时间线 / 读者视角）的**每一条目**都嵌入 `temporal` 块：
- `created_at`: ISO8601 **生成时间**；`created_in`: `{ volume_id, chapter_id }`（从哪章产生）
- `updated_at`: ISO8601 **维护时间**（任何一次落盘 / 整理都更新，含格式调整）
- `changed_at`: ISO8601 **变革时间**（语义值最后一次**实质变更**，区别于维护——只在事实真变时才更新）
- `changed_in`: `{ volume_id, chapter_id }`（变革定位：发生在哪卷哪章）
- `history[]`: **变更履历**（append-only，只增不删），每条 `{ at, field, old, new, reason, source: auto|author|retcon, volume_id, chapter_id }`

语义要点：
- 真正回答"什么时候改变了"的是 `history[]` + `changed_at`；仅 `updated_at` 只能看出"最后动过"，看不出"变了什么、何时变的"。
- 卷冻结后对冻结卷条目的改动，在 `history[]` 里 `source` 自动标 `retcon`，供一致性巡检回溯张力、供作者审查"吃书"。
- 读者视角的戏剧生命周期（`lifecycle`）与通用 `temporal` 并存：前者管叙事起止（埋设 / 回收 / 揭示 / 纠正），后者管维护与变更审计。

### 0.4 字段终审定案（冻结 · 实现唯一依据）

> 2026-10-21 字段终审，映射真实表结构后定案。四个岔路口已定（D1–D4），子决策已厘清（R1–R4）。**实现以本节为准**；§0.2/0.3 为概念依据，与本节冲突处以本节为准。

**决策定案**
- **D1 四维判定字段 → 新增结构化列**（机器可校验，支撑前向修复）：identity / status / capability / voice_traits / knowledge_boundaries / pov_roster 等判定用字段落独立列/枚举，不塞自由文本。
- **D2 时间线 chronology → 新增 `chronology_anchors` 表**：绝对锚点/角色年龄/季节/卷间间隙独立建表（代码原 timeline 包实为伏笔，不可复用）。
- **D3 卷快照 → 新 GORM 表 `volume_snapshots`**：进 migrate 清单，便于 diff/回滚/查询。
- **D4 issue.dimension 结构化做路由，area 降为展示标签**：保留 area 向后兼容，dimension 为机器归属。

**子决策厘清**
- **R1 relations**：保留 `relation_describe` 自由文本为主（尊重代码"弃枚举"初衷）+ 新增 `relation_kind` 软枚举（mentor|rival|lover|kin|other，可空）供一致性判定（如"仇敌不得无故变恋人"）。
- **R2 lifecycle = 故事时间（章节引用），temporal = 墙钟审计时间**：reader_facts 的 raised_at/payoff_at/revealed_at/formed_at/corrected_at 映射既有 `planted_chapter_id`/`revealed_chapter_id`（章节=故事时间）；temporal 的 created_at/changed_at 等为墙钟审计。二者并存不混。
- **R3 knowledge_boundaries 粒度 = per (char_id, fact_id) + as_of_chapter**：新表 `character_fact_knowledge`（哪位 POV 角色自某章起知道/不知道某条 reader_fact）。
- **R4 issue 不带 temporal**：issue 是 run 级临时态，已有 FixStates 生命周期，不入 temporal/history（temporal 只给长期台账条目）。

**新增列 / 表清单（按维度）**
| 表 | 新增列 / 表 | 说明 |
|----|-----------|------|
| `characters` | +aliases(JSON), gender, birth_year, appearance_anchors(JSON), alive(bool), location_ref, faction, role, capability_ceiling, sealed(bool), voice_traits(JSON), volume_id, changed_at, changed_in_volume_id, changed_in_chapter_id | known_skills 复用既有 `abilities` |
| `character_relations` | +relation_kind(软枚举), volume_id, changed_at | relation_describe 仍为主 |
| `locations` | +state(enum), controller, volume_id, changed_at, changed_in_* | |
| `story_arcs` | +char_id(角色弧), motif(主题弧), volume_id, changed_at | plot_threads 对齐 time_entries/reader_perspectives |
| `arc_nodes` | +volume_id, changed_at | |
| `time_entries`（伏笔） | +volume_id, changed_at | 归属 locus_arc.plot_threads |
| **`chronology_anchors`（新表）** | id, novel_id, label, anchor_type(era\|date\|age\|season\|gap), era_date, character_ages(JSON), season, after_volume, duration, volume_id, temporal | 时间线维（绝对锚点/年龄/间隙） |
| `reader_perspectives` | +volume_id, changed_at | lifecycle 映射 planted/revealed_chapter_id |
| **`character_fact_knowledge`（新表）** | char_id, fact_id, knows(bool), as_of_chapter_id | knowledge_boundaries |
| **`pov_roster`（新表）** | char_id, mode(limited\|omniscient), volume_id | POV 名册 |
| **`ledger_change_history`（新表）** | entry_table, entry_id, field, old, new, reason, source(auto\|author\|retcon), volume_id, chapter_id, at | temporal.history[]（参考 operation_log） |
| `volumes` | +frozen(bool), frozen_at, snapshot_version | 冻结态（供 G_freeze/写保护） |
| **`volume_snapshots`（新表）** | id, novel_id, volume_id, snapshot_version, committed_at, payload(JSON), diff_ref | 卷快照 |
| `AuditIssue` | +target_scope, dimension, direction | area 降为展示标签（D4） |

**枚举冻结**
`target_scope`: active_volume|frozen_volume|none · `dimension`: character|locus_arc|timeline|reader_pov|none（末位 none=纯质量问题逃生口，2026-07 阶段 1 增补） · `direction`: forward_fix|retcon_candidate · `relation_kind`: mentor|rival|lover|kin|other · `location.state`: intact|destroyed|blocked · `chronology.anchor_type`: era|date|age|season|gap · `pov mode`: limited|omniscient · `history.source`: auto|author|retcon · `reader type`: known|suspense|misconception（已存在）

**Phase 0 出口 gate 达成**：四维封闭字段定义 ✅、存储形态（扩 GORM/SQLite + 新表）✅、快照模型（volume_id + snapshot_version + committed_at）✅。**可进入阶段 1。**

---

## 1. 阶段 0：评审定稿

- **目标**：把 §0 数据字典定死，冻结枚举与字段，作为后续唯一事实源。
- **任务**：
  1. 评审 RFC + 本 WBS；确认四维字段够不够用（尤其读者视角的 `knowledge_boundaries` 粒度）。
  2. 决定 series bible 存储形态：**SQLite 表**（推荐，便于机器校验）还是结构化 JSON 文件（便于 diff/快照）。
  3. 定快照版本模型（`volume_id` + `snapshot_version` + `committed_at`）。
- **交付**：定稿 RFC + 数据字典（本文件 §0）+ 存储选型结论。
- **验收**：四维均有封闭字段定义；存储形态与快照模型敲定。
- **依赖**：无。
- **风险**：字段粒度定太粗（如把读者知识边界写成自由文本）会让阶段 2 无法机器校验——本阶段必须逐字段锁死。

---

## 2. 阶段 1：最小闭环（作用域路由 + 写保护）

**目标**：跨卷 issue 不再自动改冻结卷，前向适配为默认——先用最小改动根除"后卷改前卷"。

| 子任务 | 内容 | 文件触点（据现有代码） | 验收 |
|--------|------|------------------------|------|
| 1.1 issue 加字段 | `target_scope` + `dimension` + `direction` | `signability-audit.md`（输出格式）→ audit JSON schema → `store.go`（Issue 结构体） | 每条 issue 必带三字段 |
| 1.2 S4 审计改造 | 只扫当前卷 N，跨卷问题标注 `target_scope` + `dimension` | `signability-audit.md` | 审计范围=当前卷；跨卷问题有归属 |
| 1.3 S5 写保护 | `target_scope=frozen_volume` 时不派发自动修复 | `service.go`（`executeFix` 路由） | 冻结卷正文不被自动改动 |
| 1.4 前向默认 | 默认 `forward_fix`；仅硬矛盾且无法前向消解才 `retcon_candidate` | `service.go` 路由 + `signability-fix.md` | 跨卷问题以改当前卷为主 |

- **交付**：带作用域的 issue 流 + S5 冻结卷写保护 + 前向默认路由。
- **依赖**：阶段 0 枚举定稿。
- **风险**：`executeFix` 现有"批次失败计数 / 供应商熔断"等逻辑要与新路由叠加，避免写保护绕过前次的有界化闸门。

---

## 3. 阶段 2：台账结构化 + 快照

**目标**：扁平台账 → 四维结构化 series bible；每卷冻结提交一份**已提交快照**，作为后续卷的既定事实。

- **任务**：
  1. **schema 落地**：按 §0.2 建四维表（或结构化 JSON schema），带外键/引用（如 `char_id`、`volume_id`）。
  2. **快照机制**：卷冻结（G_freeze）时提交 `committed snapshot`；支持 diff + 回滚（retcon 受控编辑靠它留痕）。
  3. **S5 读快照**：修复/生成时把最新快照作为"既定事实"喂给 prompt（前向修复的依据）。
  4. **台账审计**：冻结前校验圣经自洽（防垃圾进垃圾出）。
- **文件触点**：`store.go`（新表/结构）+ `service.go`（冻结提交快照、S5 读快照）+ 新增台账 schema 定义文件。
- **交付**：四维 series bible + 版本快照 + 冻结提交逻辑。
- **验收**：每卷有 `committed snapshot`；S5 前向修复以快照为唯一依据；快照 diff 可回滚。
- **依赖**：阶段 1（作用域已能区分活跃/冻结卷）。
- **风险**：两卷并行写（少见）时快照序列化冲突——本阶段定"冻结即串行提交"规避。

---

## 4. 阶段 3：retcon 闸门 + 一致性巡检

- **目标**：把"回改前卷"变成作者显式决策；用建议性巡检兜底累计漂移。

| 子任务 | 内容 | 文件触点 | 验收 |
|--------|------|----------|------|
| 3.1 retcon 闸门 | 状态机（候选→审批→受控编辑→留痕→回滚点） | `service.go` 新闸门 + 状态持久化 | retcon 必须显式审批，非循环自动 |
| 3.2 受控编辑留痕 | 基于快照 diff 的编辑，可回滚 | 快照机制（阶段 2） | 每次 retcon 有 diff + 回滚点 |
| 3.3 一致性巡检 | 全书"张力报告" agent，只报告不改写 | 新巡检 agent + 报告视图 | 只出建议性报告，不自动改 |

- **交付**：retcon 审批闸门 + 巡检报告。
- **依赖**：阶段 2（快照提供回滚与 diff 基础）。
- **风险**：retcon 触发条件判断不准（把可前向的误升 retcon）——需在 prompt 里明确"硬矛盾且无法前向消解"的判定问句。

---

## 5. 阶段 4：前端

- **任务**：
  1. 卷管理 UI（卷列表、状态、冻结标识）。
  2. retcon 审批弹窗（展示 diff、风险、确认/取消）。
  3. 一致性巡检报告视图（张力清单，只读）。
  4. 前向适配提示（issue 带 `direction` 标签展示）。
- **文件触点**：`frontend/src/components/pipeline/`（新增卷管理/retcon/巡检组件）+ `usePipeline.ts`（事件/状态）+ i18n（`zh-CN.json`/`en.json`）。
- **交付**：卷管理 + retcon 弹窗 + 巡检视图。
- **验收**：作者可看卷状态、可审批 retcon、可看巡检报告；通过前端硬闸门（prettier / i18n:check / eslint / build）。
- **依赖**：阶段 1–3 的后端接口与状态。

---

## 6. 横切关注点（贯穿）

- **向后兼容**：单卷书当"1 卷书"运行；book-run 下挂 scoped sub-run，未分卷项目走旧路径，行为不变。
- **测试**：每阶段配单测（issue 字段、路由写保护、快照 diff、retcon 状态机）；沿用 `go test ./internal/pipeline/` + 前端四道闸门。
- **迁移**：既有项目首次加载时按"单卷 + 无快照"初始化，不强制重建台账。
- **里程碑建议**：阶段 1 一个 MR（最小可验证）；阶段 2 一个 MR（数据模型）；阶段 3、4 各一个 MR，便于回滚。

---

## 7. 与前次改动的衔接（避免打架）

- 前次 S5 有界化（`FixBatchFailures` 批次失败计数 / 供应商熔断 `fixProviderIdleTrip` / issue 原子化）与本次写保护**叠加**，不替代：写保护拦"改冻结卷"，有界化拦"无限重试"，两道闸各管一段。
- issue 原子化（单 issue 限 5 台账 / 2 章）与 `dimension` 字段互补：原子化管粒度，`dimension` 管归属，都进 S4 输出格式。

---

## 附录 A：背景与设计依据（源自原 RFC，已归档）

> 本附录为原 RFC 的独有论据，与 §0–§7 互补：正文讲"怎么做"（数据字典 / 任务 / 验收），本附录讲"为什么"（问题、架构、护栏、开放问题）。原 RFC 已归档至 `archive/volume-scoped-pipeline-and-ledger-rfc.md`，保留可回溯性，不再是执行依据。

### A.1 问题复述
当前 S0–S6 以"整本书"为唯一对象运行：S4 扫全书、S5 无差别"回头改"。流水线**没有"作用域"和"定稿"概念**——issue 不带"指向哪一卷、那一卷是否定稿"的信息，于是只要体检说第一卷有问题，不管写完没有，S5 都会去改它。这正是"写第二卷又翻第一卷重改"的必然结果，违背长篇"一卷一卷定稿"的写作逻辑。

### A.2 核心架构（5 点概念模型）
1. **卷级流水线实例**：每卷独立 scoped sub-run（挂 book-run 下），审计范围天然=当前卷。
2. **冻结闸门 G_freeze**：一卷过 S4+S5 后进只读冻结态；正文不可再自动改，台账贡献固化为 committed ledger snapshot。
3. **滚动审计**：写第 N 卷，S4 只扫第 N 卷，依据 1..N-1 卷快照；前卷是既定事实，第 N 卷来适配（而非反过来）。
4. **Issue 分流**：目标=活跃卷→S5 正常修；目标=冻结卷→绝不自动回写，默认前向适配，仅硬矛盾且无法前向消解才升 retcon 审批闸门（作者显式拍板，批准后受控留痕编辑）。
5. **台账版本化**：每卷冻结提交快照，当前卷在其上扩展；不断增长的快照即"系列圣经"，前卷权威性由它保证，审计无动机重写前卷（已与自身快照自洽）。

### A.3 四维为何缺一不可（rationale + 一致性规则）
前向修复能否成立完全取决于台账完整性；任一维缺失就退化成瞎改或被迫回头。
- **角色**：冻结身份/状态/能力上限/关系/语音；规则——已死不得复活（除非 retcon）、外貌/关系锚点不可静默改。例：N 卷写"右手有疤"而圣经记左手→改 N 卷。
- **地点/弧线**：地点状态（毁/封锁/完好）+ 角色弧/情节弧/主题弧；规则——open 线程须携带或显式关闭，禁止静默丢弃（最易漏的跨卷 bug）。
- **时间线**：绝对锚点 + 序列 + 间隙标记；规则——禁模型自行推算（最高发跨卷 bug 源）。例：N 卷"三年后"与圣经"五年"冲突→改 N 卷。
- **读者视角**：POV 名册 / 知识边界 / 读者已知态；规则——"读者当前相信 X"是事实，不能无 retcon 撤销；限知 POV 不得写出不可能知道的信息。防意外剧透与"不可能知识"。

### A.4 跨卷 Issue 路由矩阵（四维 × 修复方向）
每条跨卷 issue 标注 `target_scope` + 维度 + 方向；默认前向适配，仅硬矛盾且无法前向消解才升 retcon。

| 维度 | 典型跨卷问题 | 默认方向 | 升 retcon 的条件 |
|------|--------------|----------|------------------|
| 角色 | N 卷外貌/关系与圣经冲突 | 前向（改 N） | 需让已死角色复活 / 推翻核心身份 |
| 地点/弧线 | N 卷引用已毁地点为完好 / 丢弃 open 线程 | 前向（改 N） | 需改写前卷地理结局以服务新剧情 |
| 时间线 | N 卷时长/年龄与锚点冲突 | 前向（改 N） | 需推翻已确立的大时间框架 |
| 读者视角 | 限知角色泄露未知 / 撤销已揭示真相 | 前向（改 N） | 需"吃书"重设读者已知态 |

### A.5 边角与护栏
- **累计漂移**：只允许前向适配久了会失真 → 全书"一致性巡检"作建议性报告兜底（只报张力、不自动改写，改不改由作者定）。
- **作者主动 retcon**：审批闸门是逃生舱，显式 + 留痕，非循环自动触发。
- **晚期发现的跨卷正文 bug**：归 retcon 候选，非静默回写前面卷。
- **台账自身可错**：冻结前"台账审计"校验圣经自洽，防垃圾进垃圾出。

### A.6 预期收益与开放问题
**收益**：根除"后卷改前卷"怪圈（写保护 + 前向默认 + retcon 显式）；S5 体量变小自洽，缓解"无限重试"；系列圣经成可复用资产（利于续作 / 改编）。

**开放问题（待阶段 2/3 细化）**：
- 快照合并冲突：两卷并行写时快照如何序列化（本 WBS §2 暂定"冻结即串行提交"规避）。
- retcon 受控编辑的留痕与回滚（建议基于快照 diff，见 §4）。
- 一致性巡检的触发频率与成本（建议卷末 + 手动触发，而非实时）。

---

## 附录 B：代码现状对齐与计划校准（2026-10-21 只读审计）

> 基于对当前代码的审计，校准 §0–§7 与附录 A 的假设。核心结论：**不必新建"卷"抽象、不必从零建台账**——两者代码里已有，缺的是"卷级作用域语义"和"台账的归属/时间/四维字段"。

### B.1 已存在、可直接复用（降低工作量）
- **卷抽象**：`volumes` 表 + `chapters.volume_id`（可空 FK）+ 卷纲 Git 文件 `volumes/id_{id}.md` + MCP/Wails/前端（`components/volume/VolumeRail.tsx`）全套已上线；流水线已有卷边界检测（`crossedVolumeBoundary`，`internal/pipeline/service.go` L339）。→ 阶段 1/2 直接做"卷级语义"，无需建卷模型。
- **台账已是结构化 SQLite 表**：`characters`/`character_relations`/`locations`/`location_relations`/`story_arcs`/`arc_nodes`/`time_entries`/`reader_perspectives`（后者已有 `type=known|suspense|misconception`，与本设计一致）。→ 阶段 2 是"扩展既有表"，不是从零建 schema。
- **fix_type 二分路由已上线**：body→S3 重写（`service.go` L798）、ledger→S5 批次（L823）。→ `direction` 是叠加在 `executeFix` 上的第三根轴。
- **schema 演进守卫先例**：`auditHasFixType`（`service.go` L742）拒陈旧体检。→ 新增 `target_scope` 可仿照做旧数据兼容。
- **通用审计日志**：`operation_log`（含 patch 差分）≈ `temporal.history[]` 的现成机制，可参考。

### B.2 缺口（= 要新建 / 扩展的）
| 项 | 现状 | 要做 |
|----|------|------|
| 卷级语义 | 无（全库无 freeze/snapshot/retcon） | G_freeze 冻结、卷级 scoped sub-run、卷快照 |
| 台账归属 | 台账各表**无 volume_id** | 加 volume_id，才能按卷定位 |
| 四维字段 | 松散自由文本（如 `Character.Description`） | 补结构化：capability / voice_traits / knowledge_boundaries / reader_facts 状态机 |
| temporal | 无 | created_at / updated_at / changed_at / changed_in + history[]（参考 operation_log） |
| issue 字段 | `AuditIssue` 仅 id/level/area/desc/fix_type | 加 target_scope / dimension / direction / temporal |
| 卷快照 | 无落点 | 新 GORM 表（进 `migrate.go` L84 清单）或沿卷纲 Git 文件先例 |

### B.3 计划校准（据现实调整）
1. **阶段 0 存储选型基本落定**：台账四维 = **扩展现有 GORM/SQLite 表**（加 volume_id + temporal + 四维字段），非 JSON、非新库；卷快照二选一（新表 / Git 文件），**倾向新 GORM 表**进 migrate 清单（便于 diff/回滚）；`context_json` 留给 run 级临时态。
2. **阶段 1 工作量上调**：issue 加字段要**同步 4 处硬编码 JSON schema**（`signability-audit.md` L76 + `service.go` L1330/L1565/L1768）+ `parseAudit` + `AuditIssue` struct + 前端 type（`types/pipeline.ts` L52），并仿 `auditHasFixType` 做兼容守卫——机械工作比原估多。
3. **阶段 2 大幅简化**：从"建四维 schema"改为"扩展既有 8 张台账表"（加 volume_id / temporal / 补字段）+ 新增快照表；`reader_perspectives` 的 known/suspense/misconception 已在，只需补 lifecycle 时间戳。
4. **direction 合并点**：在 `executeFix` 的 body 判出（L798）/ ledger 判入（L823）处叠加 forward_fix / retcon_candidate 判定，与 fix_type 合并路由。
5. **顺序微调**：因卷模型已在，阶段 1 的"卷级语义"可与 issue 加字段并行起步，整体风险低于原估。

---

## 8. 完备性核对与疏漏补强（2026-10-21 审查）

> 审查目标：确认四阶段（0 评审 / 1 最小闭环 / 2 台账+快照 / 3 retcon+巡检 / 4 前端）能覆盖文档提出的所有问题。结论：**主链路（后卷改前卷 → 冻结 + 前向 + retcon）覆盖完整，但有 7 处"说了要解决、却无阶段认领"的疏漏**，已按下表补强归属到对应阶段。

### 8.1 需求 → 阶段 覆盖矩阵
| # | 文档提出的问题 / 需求 | 原覆盖 | 审查结论 |
|---|----------------------|--------|----------|
| 1 | 后卷改前卷（核心） | 1.3 写保护 + 1.4 前向 + 3.1 retcon | 覆盖 |
| 2 | 卷级作用域 / 定稿 | 1.2 S4 只扫当前卷 | 缺 scoped-sub-run 运行模型（G1） |
| 3 | 台账四维完整 | 2.1 schema | 覆盖（阶段 2 已简化为扩表） |
| 4 | temporal 时间戳 / 变革追溯 | 2 schema + 3 retcon 标记 | 缺写路径维护机制（G3） |
| 5 | 读者 已知/悬念/误解 生命周期 | 2 schema（lifecycle 字段） | 缺 lifecycle 采集（G4） |
| 6 | 跨卷 issue 路由 | 1.1 + 1.4 + 3.1 | 覆盖 |
| 7 | 冻结闸门 G_freeze | 2.2 假设"卷冻结时" | 缺冻结态定义/置位 + 闸门接入（G2） |
| 8 | 滚动审计（S4 依据前卷快照） | 1.2 + 2.3 | 2.3 只写 S5 读快照，缺 S4 读快照（G5） |
| 9 | 台账版本化 / 快照 | 2.2 | 覆盖 |
| 10 | retcon 审批（显式+留痕+回滚） | 3.1 + 3.2 | 覆盖 |
| 11 | 一致性巡检（累计漂移兜底） | 3.3 | 覆盖 |
| 12 | 台账审计（冻结前自洽） | 2.4 | 覆盖 |
| 13 | 向后兼容（单卷书） | §6 | 覆盖 |
| 14 | 存量迁移 / backfill | §6（泛化） | 缺台账加 volume_id/temporal 回填（G6） |
| 15 | 一致性规则强制点 | 散布 A.3 | 未落到任务（G7，轻） |

### 8.2 疏漏清单（gap log）
- **G1｜scoped sub-run 运行模型**：架构点 1（每卷独立 scoped sub-run 挂 book-run）是"卷级作用域"的地基，但四阶段无一任务构建 run 的卷级定界/层级。
- **G2｜G_freeze 冻结态与闸门**：1.3 写保护要读"卷是否冻结"、2.2 假设"卷冻结时提交快照"，但"冻结态字段 + 何时置位 + G_freeze 接入 orchestrator"无归属。
- **G3｜temporal/history 写路径维护**：阶段 2 只加字段 schema，但"每次台账写操作维护 created/updated/changed + 追加 history[] + 判定语义变更 vs 格式调整"这一机制无任务。
- **G4｜读者 lifecycle 时间戳采集**：lifecycle 字段已定义，但"埋设/回收/揭示/纠正 何时被写入（谁触发 payoff_at/corrected_at）"无任务。
- **G5｜S4 读快照**：2.3 只说"S5 读快照"，而滚动审计（A.2 点 3）要求 S4 也以 1..N-1 卷快照为校验依据。
- **G6｜存量迁移 backfill**：§6 只说"按单卷初始化"，未覆盖 8 张台账表加 volume_id/temporal 的旧数据回填（含四维结构化字段从自由文本填充）。
- **G7（轻）｜一致性规则强制点**："已死不复活 / open 线程不丢 / 读者已知态不撤销 / 限知 POV 不泄露"的强制/检测点未明确归属。

### 8.3 补强任务（分阶段归属，并入 §2–§5 执行）
- **→ 阶段 1 增补**
  - **1.5 卷级作用域运行模型**：book-run 挂 scoped sub-run，run 按 `volume_id` 定界；`Orchestrator.Advance` 与 run 结构支持卷级范围。触点 `internal/pipeline/orchestrator.go` + `store.go`。验收：每卷有独立 scoped run，S4 范围=当前卷。（闭 G1）
  - **1.6 卷冻结态 + 置位规则**：Volume 加 `frozen/status` 字段，定义何时置冻结（默认：卷过 S4+S5 后）；1.3 写保护读此标记。触点 `internal/volume/types.go` + `service.go`。验收：卷可标冻结，S5 据此拒写。（闭 G2 前半）
- **→ 阶段 2 增补**
  - **2.5 G_freeze 闸门接入 orchestrator**：卷过 S4+S5 后自动置冻结 + 触发快照提交（把 2.2 的"卷冻结时"落成自动闸门）。触点 `orchestrator.go` + `service.go`。（闭 G2 后半）
  - **2.6 temporal/history 写路径接入**：所有台账写操作维护 created_at/updated_at/changed_at + 追加 history[]；判定"语义变更 vs 格式调整"；复用/参考 `operation_log`。触点 8 张台账表的 store 写方法。验收：任意台账改动能查到 changed_at + history。（闭 G3）
  - **2.7 读者 lifecycle 时间戳采集**：埋设/回收/揭示/纠正 触发写入 raised_at/payoff_at/revealed_at/corrected_at（S4 检测 + 作者确认双通道）。触点 reader store + S4 prompt。（闭 G4）
  - **2.8 存量迁移/backfill**：8 张台账表加 volume_id/temporal 的默认值与回填（旧条目 volume_id=NULL→"无卷"，temporal 取迁移时刻；四维结构化字段从自由文本尽力填充）；进 `migrate.go`/版本化迁移。验收：旧书加载不炸、字段有默认。（闭 G6）
  - **2.3 修订**：S4 **也读**快照（依据 1..N-1 卷快照校验），不止 S5。（闭 G5）
- **→ 阶段 0 增补**
  - 定 issue `area`（自由文本）与 `dimension`（枚举）关系（dimension 为结构化归属、area 降级为展示标签或弃用）；定 issue 是否也带 temporal。
- **→ 阶段 3 增补（轻）**
  - 明确一致性规则强制点：预防在 S4 prompt + 生成约束，检测在一致性巡检，越界在 S5 写保护拦。（闭 G7）

> 补强后覆盖结论：上述 7 项疏漏全部归属到阶段 0/1/2/3；四阶段 + 补强任务可覆盖文档提出的全部问题。阶段 4（前端）无疏漏。
