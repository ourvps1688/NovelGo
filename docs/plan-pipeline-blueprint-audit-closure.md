# NovelGo 流水线闭环补全 — 开发计划（A+B 方案）

> 配套设计评审结论：当前流水线「骨架」已成型，但**蓝图阶段缺判→修闭环**、**审计非确定性（仅抽样）且缺读者视角维度**、**针对性修复停留在改章症状层、无法真正协调蓝图/角色/弧线/时间线/读者视角**。
> 本文档把评审结论落为可执行计划：A 方案补蓝图判→修闭环，B 方案把审计升 8 维（含读者视角）+ 全台账一致性核查，并把修复升级为「蓝图驱动向下传播」。
> 约定同 `pipeline-development-plan.md`：界面中文、文案必须进 i18n；前端改后需 `npm run build` + 重启 `wails dev`；Go 提交前过 gofmt + golangci-lint + `prettier --check` + `i18n:check`。

---

## 0. 代码事实表（开工前必读）

| 关注点 | 真实位置 | 对本次的影响 |
|---|---|---|
| 阶段/闸门常量 | `internal/pipeline/orchestrator_c.go:5` | 新增 `GateG1b`；**复用** `StageS2` 承载蓝图体检/修复（不新增阶段常量） |
| 唯一流转源 | `internal/pipeline/orchestrator.go:33` `Orchestrator.Advance` | A 方案在 `Advance` 的 `StageS2` 分支内区分「生成/体检/修复」 |
| 蓝图生成 | `service.go:190` `executeBlueprint` | 改为「生成卷 → 进蓝图体检」两步 |
| 体检执行体 | `service.go:437` `executeAudit` | 技能名按 `ctxt.AuditKind` 选择 |
| 修复执行体 | `service.go:605` `executeFix` | 技能名 + issue 来源按 `ctxt.AuditKind` 选择 |
| 闸门发射 | `service.go:521` `emitGateDecision` | 按 `ctxt.AuditKind` 分支：蓝图→G1/G1b，签约→G4/G终 |
| 上下文载体 | `store.go:48` `GateContext` | 新增 `AuditKind`、`BlueprintAudit` 字段 |
| 技能→Agent 映射 | `skillrun.go:27` `skillAgentType` | 新增 `blueprint-audit`/`blueprint-fix` 分支 |
| Agent 白名单 | `identity.go:75/86/103` | 新增 `BlueprintFixAgent` 限权白名单 |
| 蓝图/签约/修复技能 | `internal/skill/builtin/{story-blueprint,signability-audit,signability-fix}.md` | B 方案改后两者；A 方案新增 `blueprint-audit.md`/`blueprint-fix.md` |
| 前端蓝图视图 | `frontend/src/components/pipeline/BlueprintWorkflowView.tsx` | 支持 G1b + 复用 `AuditResultView`/`FixProgressView` |
| 共享修复清单 | `frontend/src/components/pipeline/SignabilityAuditView.tsx` `FixProgressView` | 抽 `AuditResultView` 共用；标题随 `AuditKind` 变化 |
| 共享活动面板 | `frontend/src/components/pipeline/StageActivityPanel.tsx` | 蓝图生成/体检/修复阶段复用 |

---

## 1. 现状闭环缺口（设计评审结论，逐条对应）

| 阶段 | 现状 | 缺口 |
|---|---|---|
| S1 孵化 | `book-pitch` 生成设定 | 无判→修（产物即蓝图输入，合理） |
| **S2 蓝图** | `executeBlueprint` 建卷+写卷纲 → 直接落 **G1 确认闸**（continue/modify/abort），**无体检、无修复** | **最大缺口**：蓝图质量无人判，用户只能盲点 continue |
| S3 写作 | 每章 6 维评分门禁 + 里程碑 G3b（可进体检） | 已有判→修入口 |
| S4/S4b 签约体检 | `signability-audit` **只读 + 抽样 3–4 章**，7 维，产出 issue | 非确定性；**缺读者视角（已知/悬念/误解）维度**；**不查全台账一致性** |
| S5 修复 | `signability-fix` 按 issue 改章/补卷纲/同步台账 | **症状式**：只 patch 章节正文，结构性问题（世界/弧线/卷纲）**不向上回写设计台账再向下传播** |
| S6 完本终检 | `signability-audit` 全本 + G终 | 同 S4 维度缺口 |

**用户核心质疑的回答**（针对性修复能否真正协调五大台账）：
> 当前不能。S5 修复只改章节正文，蓝图/角色/弧线/时间线/读者视角之间靠 `statesync` 被动回填，无主动一致性核查、无结构变更向下传播。
> **B 方案**补两件事：①审计新增「全台账一致性」维度，显式扫描 timeline/arcs/reader_perspective 逐条判断 伏笔 resolved / 弧线 completed / 认知自洽，产出**指向具体 entry ID** 的 issue；②修复新增「蓝图驱动修复」方法论——宏观 issue 先改设计台账（卷纲/弧线），再传播到受影响章节，而非只贴章。

---

## 2. 阶段闭环总览（A+B 落地后）

```
S1 孵化 ──▶ S2 蓝图生成 ──▶ [蓝图体检 blueprint-audit]
                                 │ needs_fix
                                 ▼
                            [G1b 闸: fix/continue/abort]
                                 │ fix
                                 ▼
                            [S5 修复 blueprint-fix] ──▶ 复评 ──┐
                                 └──────────────────────────┘
                                 │ pass
                                 ▼
                            [G1 闸: continue/modify/abort] ──continue──▶ S3 写作
                                                                          │
                                          S3 ──(黄金三章/里程碑)──▶ S4 签约体检
                                                                          │ needs_fix
                                                                          ▼
                                                                     [G4 闸] ─▶ S5 签约修复 ─▶ 复评
                                                                          │
                                          S3 ──(用户 finish_book)──▶ S6 完本终检
                                                                          │ needs_fix
                                                                          ▼
                                                                     [G终 闸] ─▶ S5 签约修复 ─▶ 复评
```

`S5` 是**通用修复阶段**：签约修复与蓝图修复共用 `executeFix`，仅由 `ctxt.AuditKind` 区分技能与 issue 来源。

---

## 3. A 方案：蓝图体检 / 修复闭环

### 3.1 状态机变更
- 新增网关常量 `GateG1b = "G1b"`（`orchestrator_c.go` 常量块）。
- `GateContext`（`store.go`）新增两字段：
  ```go
  // AuditKind 标记当前体检/修复的种类："blueprint" 走蓝图技能，""（默认）走签约技能。
  AuditKind string `json:"audit_kind,omitempty"`
  // BlueprintAudit 是蓝图体检（blueprint-audit）产出的结构化结果 JSON，与签约 Audit 分字段存放，避免互相覆盖。
  BlueprintAudit string `json:"blueprint_audit,omitempty"`
  ```
- 复用 `StageS2` 作为蓝图「生成 → 体检 → 修复」的宿主阶段：**不新增阶段常量**，体检期间 `StageS2=running`，修复期间 `StageS2` 经 `enterFix` 切到 `S5`，复评回 `S2`。

### 3.2 `service.go` 改动
1. **`executeBlueprint`**（`:190`）改尾段：生成卷成功 → 不再直接 `emitGate(G1)`，改为调用 `enterBlueprintAudit(run)`。
2. **新增 `enterBlueprintAudit(ctx, run)`**：置 `ctxt.AuditKind="blueprint"`、`ctxt.AuditStage=StageS2`、`ctxt.BlueprintAudit=""`；`SetStageStatus(S2, running)`；`emitStage(running)`；`go executeAudit(runID, novelID)`。
3. **`executeAudit`**（`:437`）技能名选择：
   ```go
   skill := "signability-audit"
   if ctxt.AuditKind == "blueprint" { skill = "blueprint-audit" }
   ```
   并把结果写回 `ctxt.BlueprintAudit`（blueprint）或 `ctxt.Audit`（signability）。
4. **`executeFix`**（`:605`）issue 来源与技能名选择：
   ```go
   auditJSON := ctxt.Audit
   if ctxt.AuditKind == "blueprint" { auditJSON = ctxt.BlueprintAudit }
   auditData := parseAudit(auditJSON)
   // 修复技能：
   skill := "signability-fix"
   if ctxt.AuditKind == "blueprint" { skill = "blueprint-fix" }
   ```
5. **`emitGateDecision`**（`:521`）顶部按 `AuditKind` 分支：
   ```go
   if ctxt.AuditKind == "blueprint" {
       if audit.Verdict == "pass" {
           emitGate(GateG1, [continue, modify, abort])   // 蓝图确认
       } else {
           emitGate(GateG1b, [fix, continue, abort])     // 蓝图修复闸
       }
       return
   }
   ```
6. **auto_run 保护**：`executeAudit` 的自动裁决块（`:490`）条件改为 `run.AutoRun && auditStage != StageS6 && ctxt.AuditKind != "blueprint"`——蓝图体检**必须人工确认**（与 G1 原语义一致，不让自动修复绕过蓝图审阅）。

### 3.3 `orchestrator.go` 改动
- `Advance` 的 `case StageS2`（`advanceS2`）扩展为统一处理蓝图宿主阶段：
  - `decision=="fix"` → `enterFix(ctx, run)`（S5，蓝图修复技能由 `AuditKind` 驱动）。
  - `decision=="continue"` → `setS3Running(ctx, run)`（从 G1 通过 或 G1b 接受已知问题，均进入写作）。
  - `decision=="modify"` → 重新 `executeBlueprint`（重生成 + 复体检）。
  - `decision=="abort"` → `abortRun`。
- `advanceS1`（`:81`）无需改（仍 `executeBlueprint`）。

### 3.4 `skillrun.go` + `identity.go` 改动
- `skillAgentType` 新增：
  ```go
  case "blueprint-audit": return agentcfg.SignabilityAgent   // 复用只读白名单
  case "blueprint-fix":   return agentcfg.BlueprintFixAgent  // 新增限权 Agent
  ```
  `skillAgentTypeString` 同步加 `blueprint-fix → "blueprintfix"`。
- `identity.go` 新增 `BlueprintFixAgent` 枚举值 + `blueprintFixAgentTools`（限权白名单，**只改卷纲、不碰章节正文**）：
  ```go
  var blueprintFixAgentTools = []string{
      "read", "list_volumes", "get_chapter_list",
      "get_characters", "get_character_relations", "get_locations",
      "get_timeline", "get_story_arcs", "get_reader_perspective",
      "search_story_memory",
      "edit",          // 仅写 volumes/id_{id}.md 卷纲
      "create_volume", // 补卷
  }
  ```
  （不含 `edit chapters/`、不含 `create_character/update_character/update_*` 等状态写工具——蓝图修复只动卷纲与卷结构，角色/弧线的结构性改动由 B 方案的「签约修复蓝图驱动」在 S5 签约修复时处理。）

### 3.5 新增技能文件
- **`internal/skill/builtin/blueprint-audit.md`**（mode: manual，复用 SignabilityAgent 只读白名单）：
  - 立场：只读体检，专判**蓝图层**问题——立意清晰度、卷纲节奏地图（每卷主题/冲突推进/爽点密度/起止节点）、主支线编织、全局悬念、人物弧总览、卷间衔接。
  - 只读工具：`list_volumes` + `get_chapter_list` + `get_story_arcs` + `get_characters` + `get_reader_perspective`（看蓝图与已落设定是否自洽）。
  - 输出同 `signability-audit` 的 JSON 契约（`verdict`/`score`/`summary`/`issues[]`），`score` 为蓝图专属 6 维（/30），P0=阻断立项、P1=提升。
- **`internal/skill/builtin/blueprint-fix.md`**（mode: manual，BlueprintFixAgent）：
  - 立场：执行员，严格按蓝图 issue 清单改 `volumes/id_{id}.md` 卷纲 / `create_volume` 补卷；**不编辑章节正文**、不删数据、幂等、不派子 Agent。

### 3.6 前端改动
- **抽 `AuditResultView`**（从 `SignabilityAuditView` 提出）：渲染 verdict + score + issue 清单（P0/P1 分组），供 S4 与蓝图 G1b 共用。
- **`BlueprintWorkflowView.tsx`**：
  - S2 running 且 `AuditKind=="blueprint"` → 复用 `StageActivityPanel`（显示「蓝图体检中…」）。
  - 收到 `gate_id==GateG1b` → 渲染 `AuditResultView` + 「进入修复 / 接受并继续 / 中止」按钮。
  - 收到 `gate_id==GateG1` → 渲染蓝图确认（继续/重生成/中止）。
  - 修复阶段（`AuditKind=="blueprint"` 且 S5）→ 渲染 `FixProgressView`（标题传「蓝图修复」）。
- **`FixProgressView`** 增加 `title`/`auditKind` prop，标题在「签约修复 / 蓝图修复」间切换；其余逐项状态徽章逻辑不变。
- **i18n**（`zh-CN.json` / `en.json`）：新增 `blueprint.auditTitle`、`blueprint.fixTitle`、`gates.g1b.{summary,fix,continue,abort}`、`audit.blueprintVerdict` 等。

### 3.7 A 方案测试
- `service_test.go`：
  - `TestServiceBlueprintAuditPass`：`executeBlueprint` 后 stub `blueprint-audit` 返回 `pass` → 断言落 `GateG1` 于 `StageS2`。
  - `TestServiceBlueprintAuditFixLoop`：stub 首轮 `needs_fix`、次轮 `pass`，`blueprint-fix` 返回 ok → 断言经 S5 回到 `GateG1`。
  - `TestServiceBlueprintAuditAutoRunSkipped`：`AutoRun=true` 时蓝图体检仍落闸门而非自动修复。
- `skillrun_test.go`：断言 `blueprint-audit`→`SignabilityAgent` 白名单、`blueprint-fix`→`BlueprintFixAgent` 白名单（无 `edit` 章节/`create_character` 等）。

---

## 4. B 方案：审计升 8 维（含读者视角）+ 全台账一致性 + 蓝图驱动修复

### 4.1 `signability-audit.md` 改动
1. **新增第 8 维「读者视角（已知/悬念/误解）」**（1–5 分 + 证据）：
   - 检查 `get_reader_perspective` 条目是否自洽——读者「已知信息 / 悬念 / 被误导」三类认知是否清晰分层；关键 reveal 是否先埋后收；是否存在「读者比角色知道得多/少」导致的情绪失效。
   - 致命伤新增：核心悬念被提前剧透、读者认知与叙事脱节、误解线无回收。
2. **新增「全台账一致性核查」章节**（改变「仅抽样」的非确定性）：
   - 在抽样读完 3–4 章后，**额外全量扫描设计台账**：`get_timeline`（每条 entry：planted → resolved？有无悬空伏笔）、`get_story_arcs`（每条 arc：status=completed/in_progress/dropped？node 是否连贯）、`get_reader_perspective`（每条 entry：resolved？）。
   - 产出**指向具体 entry ID** 的 issue（如 `{"id":"I9","level":"P1","area":"长线牵引","desc":"timeline#42 伏笔未回收"}`），而非泛泛而谈。
   - 控制成本：逐条扫描时只标记**具体不一致**，不重复读同一资源。
3. **评分分母 /35 → /40**：8 维 ×5；verdict 规则改为「存在 P0 → needs_fix；无 P0 且 score ≥ 29/40 → pass」（保持约 71% 通过线，与旧 25/35 比例一致）。更新技能内 verdict 说明文字。
   - 注：Go 侧 `executeAudit`/`emitGateDecision` 不硬编码分母，`AuditData.Score` 仅为 int，门槛由技能文本自述，故 B 此处**不改 Go 逻辑**，只改技能 markdown（降低回归风险）。

### 4.2 `signability-fix.md` 改动
- 新增 **「蓝图驱动修复」方法论**（回答用户「能否真正协调五大台账」）：
  - 修复前先判 issue 层级：**结构性 issue**（世界/卷纲/弧线/角色体系）→ 先改设计台账（`update_*`/`create_*` 对应卷纲、story_arcs、characters），**再**用 `edit` 把变更传播到受影响章节；**症状性 issue**（单章正文瑕疵）→ 直接 `edit` 该章。
  - 强调：结构性改动必须「设计台账先变 → 章节后变」，保证蓝图/角色/弧线/时间线/读者视角五者随修复同步收敛，而非只贴章导致台账与正文再次漂移。
- 其余约束（不派子 Agent、不删数据、幂等、先查后改）不变。

### 4.3 B 方案测试
- `signability-audit` 为只读技能，主要验证在技能文本与抽查输出；增加一个 `parseAudit` 单测确保 8 维 score 正确落库（可选）。
- `signability-fix` 同上，靠行为约束；可加一条说明性测试（非阻断）。

---

## 5. 跨方案共享改动汇总（一次性落地，避免双源）

| 文件 | 改动 |
|---|---|
| `orchestrator_c.go` | 新增 `GateG1b` |
| `store.go` | `GateContext` 加 `AuditKind`、`BlueprintAudit` |
| `service.go` | `executeBlueprint`→`enterBlueprintAudit`；`executeAudit`/`executeFix` 按 `AuditKind` 选技能与 issue 源；`emitGateDecision` 按 `AuditKind` 分支（G1/G1b vs G4/G终）；auto_run 排除 blueprint |
| `orchestrator.go` | `advanceS2` 统一处理蓝图宿主阶段（fix/continue/modify/abort） |
| `skillrun.go` | `skillAgentType` 加 `blueprint-audit`/`blueprint-fix` |
| `identity.go` | 新增 `BlueprintFixAgent` + 限权白名单 |
| 技能 | 新增 `blueprint-audit.md`、`blueprint-fix.md`；改 `signability-audit.md`（8 维 + 一致性）、`signability-fix.md`（蓝图驱动） |
| 前端 | 抽 `AuditResultView`；`BlueprintWorkflowView` 支持 G1b + 复用面板；`FixProgressView` 标题随 `AuditKind`；i18n 新增键 |
| 测试 | 蓝图审计/修复闭环 3 例 + skill→Agent 映射 2 例 |

---

## 6. 任务拆分与提交策略（建议两个独立 commit，便于分别回滚）

**Commit 1 — A：蓝图体检/修复闭环**
- 新建 `blueprint-audit.md`、`blueprint-fix.md`
- `identity.go` 新增 `BlueprintFixAgent` + 白名单；`skillrun.go` 映射
- `store.go` 加 `AuditKind`/`BlueprintAudit`；`orchestrator_c.go` 加 `GateG1b`
- `service.go` `executeBlueprint`/`enterBlueprintAudit`/`executeAudit`/`executeFix`/`emitGateDecision` 改造
- `orchestrator.go` `advanceS2` 扩展
- 前端 `AuditResultView` 抽取 + `BlueprintWorkflowView` + `FixProgressView` 标题 + i18n
- 测试 `TestServiceBlueprintAuditPass` / `FixLoop` / `AutoRunSkipped` + `skillrun_test` 映射断言

**Commit 2 — B：审计升 8 维 + 蓝图驱动修复**
- 改 `signability-audit.md`（第 8 维 + 全台账一致性 + /40）
- 改 `signability-fix.md`（蓝图驱动修复方法论）
- i18n 新增第 8 维标签
- （可选）`parseAudit` 8 维单测

> A 不动签约逻辑，B 不动蓝图逻辑；二者在 `service.go` 的 `AuditKind` 分流处正交汇合，回滚任一半不影响另一半。

---

## 7. 风险与回滚

| 风险 | 缓解 |
|---|---|
| A 改动 S2→S3 关键入口，可能回归蓝图确认 | 保留 G1 语义；新增 3 个闭环测试；A 单独 commit 可整体回退 |
| B 让审计从「抽样」变「全台账扫描」，LLM 调用/成本上升、issue 数增多 | 仅一致性维度全扫，章节维度仍抽样；issue 指向具体 entry，便于精准修复；B 单独 commit 可回退 |
| 蓝图修复限权过严导致无法修结构性蓝图问题 | `BlueprintFixAgent` 已含 `edit`(卷纲) + `create_volume` + 全部只读上下文工具；结构性跨台账改动本就由 B 的签约修复承接 |
| 前端 S5 同时承载签约/蓝图修复，标题误显 | `FixProgressView` 标题随 `AuditKind` 切换，已在 A 内处理 |

---

## 8. 验收标准

- [ ] 跑通全流程：S1→S2 生成卷→蓝图体检（needs_fix）→G1b→S5 蓝图修复→复评 pass→G1→S3。
- [ ] 蓝图体检 `pass` 时直接落 G1，不经修复。
- [ ] `AutoRun` 下蓝图体检仍停闸门，不自动修复。
- [ ] 签约体检输出含第 8 维「读者视角」且 score 分母 /40；全台账一致性 issue 带具体 entry ID。
- [ ] 签约修复对结构性 issue 先改设计台账再传播到章节（抽查修复日志可见 `update_*` 先于 `edit` 章节）。
- [ ] `go build` / `golangci-lint` / `prettier --check` / `i18n:check` 全绿；新增测试通过。
- [ ] 前端三处界面（S2 生成/体检/修复、G1b、S5 修复清单）语言与 S5 既有界面一致，活动区不压缩，修复清单每项带 待修复/正在修复/已修复 状态。

---

## 9. 已知边界与后续增强（A+B 不阻塞，3 个软缺口）

A+B 落地后，流水线在**你提出的诉求范围（五台账能真正协调）内是完整闭环**。但以下 3 处属于「工程权衡」而非设计缺陷，当前版本**不阻塞 A+B**，列为后续增强项供决策。

| # | 软缺口 | 代码事实 | 影响 | 处理建议 |
|---|---|---|---|---|
| G1 | 签约审计仍是**非确定性抽样** | `signability-audit.md` 仅抽样读 3–4 章；B 方案补的「全台账一致性」只扫设计台账，**章节正文内容不保证逐章审到** | 漏审章节可能带隐患进入终检；非 100% 确定性 | 作为 A+B 之后**独立增强项**单独排期（全本逐章审计会显著抬高 LLM 成本，需你拍板） |
| G2 | S3 评分是「**配置开关 + 有放弃路径**」 | `writingconfig.ScoreThreshold` + `AutoScoreEnabled`（`internal/writingconfig/types.go:9`）控制是否启用；`rw_tools.go:maybeScoreChapter` 重试耗尽后**带低分放行**（`quality_score` 记录但不阻断），**不升级到流水线级修复** | 持续写不好的章可能漏到 S4；评分关闭时整环失效 | 可选：重试耗尽→升级为 S4 类体检 issue，而非静默放行；并默认开启 `AutoScoreEnabled` |
| G3 | 写作中途**蓝图漂移不自动回灌** | 进入 S3 后，S5 修复暴露的蓝图不一致靠 B 方案「结构化 issue 手动改设计台账再传播」；`statesync` 仅**被动回填**，无自动检测+重同步 | 蓝图改了，已写章节不会自动提醒重读，存在再次漂移风险 | 可选：S5 修复完成后触发一次「轻量蓝图一致性巡检」，标出与当前卷纲冲突的章节 |

**G1 的取舍说明**：B 方案把审计从「纯抽样」推进到「抽样 + 全台账一致性全扫」，已是性价比最高的收敛点。要彻底消除抽样不确定性，需把章节正文审计也改全本逐章，成本与延迟显著上升——故建议作为独立增强项，不在 A+B 内一并做。

**与核心诉求的关系**：G1/G2/G3 都不影响「五台账能协调」这一 A+B 已解决的目标——它们关乎**覆盖度与自愈性**，而非**机制缺失**。A+B 先把机制补完，G1–G3 按成本优先级后续迭代即可。

### 9.1 落地状态（2026-10-21，三个软缺口已全部实现）

| # | 落地内容 | 关键改动 |
|---|---|---|
| G1 | **签约审计消除抽样**：`signability-audit.md` 工作流改为「全本逐章通读」——黄金三章逐字精读，其余章节按 3–5 章一批依次读完，**不得抽样跳章**；「全台账一致性核查」措辞同步。Go 侧 `signabilityPrompt` / `stockAuditPrompt` / `finalAuditPrompt` 提示词同步改为逐章通读。**代价**：体检 LLM 成本随章节数线性上升（此前 3–4 章封顶），为消除不确定性的主动选择。 | `internal/skill/builtin/signability-audit.md`（version 2）、`internal/pipeline/service.go` 三处提示词 |
| G2 | **评分放弃路径收敛**：① `get_chapter_list` 输出对已落库 `quality_score` 的章节追加「质检 N」标注；② `signability-audit.md` 一致性核查新增「低分放行章节核查」——凡质检评分偏低（如 <60）或带低分放行报告的章节**必产一条 P1 issue**，评分门禁重试耗尽放行的章节不再静默漏过，下一次 S4/S6 体检必然暴露。`AutoScoreEnabled` 默认开启（`writingconfig` 默认值已为 true，无需改动）。 | `internal/mcp_tools/novel_tools.go`、`internal/skill/builtin/signability-audit.md` |
| G3 | **蓝图漂移轻量巡检**：S5 修复（签约/蓝图均适用）完成后自动触发一次 `blueprint-drift-check`（新只读技能，复用 SignabilityAgent 白名单，MaxTurns=8）：逐卷读当前卷纲，对照已写章节细纲与关键正文，标出剧情矛盾/承诺未兑现/顺序错位的章节；结论写入 `GateContext.DriftReport` 并追加到复评闸门 summary（非阻断、不评分；失败仅记日志）。无已写章节（纯蓝图阶段）自动跳过，节省调用。 | `internal/skill/builtin/blueprint-drift-check.md`（新增）、`internal/pipeline/{service,store,skillrun}.go` |

**测试**：`TestServiceDriftPatrolAfterFix`（修复→巡检→DriftReport 落库→闸门 summary 携带巡检结论）、`TestServiceDriftPatrolSkippedWhenNoChapters`（无章节跳过巡检）、`TestSkillAgentTypeBlueprintMappings`（drift-check 映射断言）。
