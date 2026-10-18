# NovelGo 创作流水线 — 具体开发计划

> 源设计文档：`docs/pipeline-orchestration-design.md`（C→A 演进，5 PR 拆分，两条硬约束，续写/导入模式，运行控制）
> 本文档是**可执行的开发计划**：把设计文档的每个 PR 拆成「建哪些文件 / 改哪些函数 / 写哪些测试 / 如何验收」，按依赖顺序排好。
> 约定：中文界面（中文文案必须进 i18n，否则 `i18n:check` 门禁会红）；前端改动需 `npm run build` 并重启 `wails dev` 才在 webview 生效；Go 提交前过 gofmt + golangci-lint。

---

## 0. 代码事实表（开工前必读，避免找错位置）

| 关注点 | 真实位置 | 说明 |
|---|---|---|
| MCP 工具注册入口 | `internal/mcp_tools/registry.go:5` `RegisterAllTools` | 新增工具须在此加一行 `RegisterXxxTools(r)` |
| 主 agent 工具白名单 | `internal/agentcfg/identity.go:18` `mainAgentTools` | 新工具须在此数组加名字，否则 agent 调不到 |
| 子 agent 枚举 | `internal/mcp_tools/subagent_tools.go:12` `AgentType` | **仅 `memory`/`review`/`statesync`**，无 `blueprint`/`signability` |
| 评分门禁范式 | `internal/mcp_tools/rw_tools.go:989` `maybeScoreChapter`；调用点 `:432`；门禁注释 `:429` | 流水线级「门」的样板 |
| 状态同步自动派发 | `internal/mcp_tools/rw_tools.go:907` `scheduleStateSync` / `:925` `runStateSync` | 已发 `statesync:progress` 全局事件（照搬范式） |
| 卷创建缺口 | `internal/volume/store.go:52` `Volume.Place`（内部，未暴露 MCP） | PR2 须新增 `create_volume` 工具 |
| 全局事件范式 | `frontend/src/hooks/useStateSyncProgress.ts` + `wails.EventsEmit` | 前端 `EventsOn` 订阅模板 |
| DB 表注册 | `internal/migrate/migrate.go` `models` 切片（约 `:90-113`） | 新表 = 在此切片加 `&pipeline.PipelineRun{}`，`AutoMigrate` 自动建表 |
| skill 路由 | `internal/skill/router.go`（仅「消息→skill 名」） | 无程序化执行入口（§13 要补） |
| skill 内容 | `internal/skill/builtin/*.md`（book-pitch / story-blueprint / signability-audit） | 内容已就绪，缺确定性触发 |
| Wails 前端绑定 | `app` 层 service（与现有 ChatService 同 Bind 模式） | 后端方法经 `application.Bind` 暴露给前端 |

---

## 1. 跨 PR 公共基建（PR1 之前先落地，全部 PR 复用）

> 这一步不单独成 PR，作为 PR1 的第一批提交；但**结构必须先行**，否则后续 PR 各自重复造。

### 1.1 `pipeline_runs` 表 + 模型 + 仓储
- 新建 `internal/pipeline/store.go`：
  ```go
  type PipelineRun struct {
      ID            int64          `gorm:"primaryKey;autoIncrement"`
      NovelID       int64          `gorm:"index;not null"`
      RunID         string         `gorm:"uniqueIndex;not null"`
      Mode          string         `gorm:"not null;default:new"` // new | continue
      StartChapter  *int           // 续写模式起始章号
      CurrentStage  string         `gorm:"not null"`            // S0|S1..S6|S3_loop
      Status        string         `gorm:"not null"`            // pending|running|paused|awaiting_gate|finished|aborted
      AutoRun       bool           `gorm:"not null;default:false"`
      ContextJSON   string         `gorm:"type:text"`
      CreatedAt     time.Time
      UpdatedAt     time.Time
  }
  func (PipelineRun) TableName() string { return "pipeline_runs" }

  type Store struct { db *gorm.DB }
  func NewStore(db *gorm.DB) *Store
  func (s *Store) Create(ctx, run *PipelineRun) error
  func (s *Store) GetByNovel(ctx, novelID int64) (*PipelineRun, error)
  func (s *Store) GetByRunID(ctx, runID string) (*PipelineRun, error)
  func (s *Store) SetStatus(ctx, runID, status, contextJSON string) error
  func (s *Store) AppendGateDecision(ctx, runID, gateID, decision, feedback string) error
  ```
- 在 `internal/migrate/migrate.go` 的 `models` 切片追加 `&pipeline.PipelineRun{}`（约 `:112` 前）。
- 测试：`internal/pipeline/store_test.go` —— 覆盖 Create / GetByNovel / 状态流转 / ContextJSON 读写往返。

### 1.2 事件常量 + 发射助手
- 新建 `internal/pipeline/events.go`：
  ```go
  const (
      EvStage   = "pipeline:stage"   // {run_id, novel_id, stage, status, note}
      EvGate    = "pipeline:gate"    // {run_id, novel_id, stage, gate_id, summary, options}
      EvControl = "pipeline:control" // 前端→后端：{run_id, cmd: start|pause|resume|stop}
      EvControlAck = "pipeline:control:ack" // 后端→前端：状态变更确认
  )
  // EmitStage / EmitGate 内部调 wails.EventsEmit(ctx, ...)，照搬 runStateSync 写法
  ```
- 复用 `statesync:progress` 既有的 `ctx` 取法（从 `ToolContext` / `app` 上下文取 wails ctx）。

### 1.3 `RunSkill` 基础设施（§13 方案乙，PR2/PR3 共用，此处先行）
- 新建 `internal/pipeline/skillrun.go`：
  ```go
  // RunSkill 把 builtin skill 正文作 system prompt，挂载指定工具白名单，
  // 跑一次性 agent run，返回产物文本。
  func RunSkill(ctx context.Context, name string, input SkillInput) (SkillOutput, error)
  type SkillInput struct {
      NovelID   int64
      Prompt    string            // 用户/编排给该 skill 的具体任务
      Whitelist []string          // 该 skill 允许调用的工具
      MaxTurns  int
  }
  type SkillOutput struct { Text string; Artifacts map[string]string }
  ```
- 实现：加载 `internal/skill/builtin/<name>.md` → 构造 system prompt（参考 `app/skill_audit_e2e_test.go` 的 `sysPrompt = 方法论 + sk.Content` 范式）→ 复用现有 agent loop（`internal/agent` 的 `Run`，传不同 `allowedTools` + `AgentType` + `ParentTurnID`，见 `internal/agent/DESIGN.md:368`）→ 回收产物。
- 各 skill 的白名单常量集中在 `internal/agentcfg/identity.go` 新增（见 PR2/PR3）。

> **为什么先行**：PR2 的 S2 蓝图、PR3 的 S4 签约体检都依赖「确定性触发 skill」。不在 PR1 做，PR2 会卡住。

> **实现状态（PR1 实际交付，2026-10-18）**：PR1（提交 `6885897`）以**单轮 `llmClient.GenerateText` 骨架**实现 `RunSkill`，**并非**多轮 agent loop；C 期 orchestrator（`orchestrator_c.go`）亦未调用 `RunSkill`。多轮化升级与 S2 接线不归入 1.0，而是作为 **PR2 §3.0 的前置缺口**在 PR2 内补完，对外接口签名保持不变。

---

## 2. PR1 — 闭环跑通 + 运行控制（C 期 MVP，已落地 2026-10-18）

**目标**：用户点「继续」即推进；运行中可随时暂停/恢复/停止，状态持久化可续跑。最小可见闭环：`S1(book-pitch) → G1 → S3(单章含评分门禁+statesync) → awaiting_gate`。

### 后端
| 文件 | 动作 | 关键函数 / 说明 |
|---|---|---|
| `app` 层新增 `internal/pipeline/service.go` 并 Bind | 新增 | `StartPipeline(novelID)`, `GetPipelineState(novelID)`, `ResumePipeline(runID, decision, feedback)`, `ControlPipeline(runID, cmd)`；内部用 `Store` + `events.Emit*` |
| `internal/pipeline/service.go` | 实现 | `StartPipeline`：建 `PipelineRun{Mode:new, CurrentStage:S1, Status:running}` → `EmitStage(S1, running)`。`ResumePipeline`：按 `decision` 推进 `CurrentStage` 并落库 + `AppendGateDecision`。`ControlPipeline`：`pause/resume/stop` → 改 `Status`（paused/aborted/running）并 `EmitControlAck` |
| `internal/pipeline/orchestrator_c.go`（C 期轻量驱动） | 新增 | 仅做「阶段→下一个阶段」的查表推进 + 在闸门点置 `awaiting_gate` 等前端回 `ResumePipeline`。**不含自主 agent 调用**（C 期实际执行走前端 UI/skill） |

### 前端
| 文件 | 动作 | 说明 |
|---|---|---|
| `frontend/src/types/pipeline.ts` | 新增 | `PipelineRun` / `PipelineStage` / `GatePayload` / `ControlCmd` 类型 |
| `frontend/src/hooks/usePipeline.ts` | 新增 | `EventsOn(EvStage/EvGate/EvControlAck)` 订阅，暴露 `stage/status/gate`；提供 `resume(decision)` / `control(cmd)` 调后端 |
| `frontend/src/components/pipeline/PipelineWizard.tsx` | 新增 | 左侧线性进度条 S1→S2→S3→S4→S5→S6；按 `stage` 高亮；包裹现有 UI（S1→`BookPitchWorkflowView`） |
| `frontend/src/components/pipeline/PipelineGateCard.tsx` | 新增 | 监听 `EvGate`，渲染「继续 / 修改 / 终止」三按钮 → `resume()` |
| `frontend/src/components/pipeline/PipelineControlBar.tsx` | 新增 | 暂停/恢复/停止浮钮 → `control()`；订阅 `EvControlAck` 同步状态 |
| `WorkspaceView.tsx` | 改 | 在「工作流」入口接入 `PipelineWizard`（参考 `:163` `activeWorkflow`） |
| `frontend/src/locales/zh-CN.json` / `en.json` | 改 | 新增 `pipeline.*` 文案键（i18n:check 硬门禁） |

### 硬约束落实（PR1 即可验证）
- **章细纲前置（§2①）**：C 期在向导层拦截——S3 进入 `edit` 前若本章无章细纲，禁用「下一章」（向导层 `useStateSyncProgress` 同模式加 `useChapterPlan`）。
- **台账实时维护（§2②）**：C 期向导在 `statesync:progress != done` 前禁用「下一章」操作（照搬 `useStateSyncProgress` 的订阅 + 60s 安全网）。

### 测试与验收
- 后端单测：`service_test.go` —— `StartPipeline` 建记录并发事件；`ResumePipeline` 推进阶段；`ControlPipeline` 置 paused/aborted 后 `GetPipelineState` 反映；重启后 `GetByRunID` 能恢复。
- 手动验收：起 `wails dev` → 工作流入口 → 走完 S1→G1→S3 单章（评分门禁触发、statesync 落库）→ 点暂停/恢复/停止均生效 → 关 app 重开能续跑。
- CI 门禁：prettier / i18n:check / eslint(0 error) / `npm run build` / `go build ./...` / gofmt + golangci-lint。

---

## 3. PR2 — 自动建卷 + S2 蓝图节点（落库，已落地 2026-10-18）

**目标**：S2 真正完成「自动建卷 / 写卷纲落库」（不只是 markdown）；G2 确认。

### 3.0 PR2 前置缺口（源于 PR1 实际交付，须在 §3.1/§3.2 之前补）

> 原计划 §1.3 假定 `RunSkill` 在 1.0 基建即实现为 `internal/agent.Run` 多轮循环；但 PR1 实际以单轮 `GenerateText` 骨架落地（见 `internal/pipeline/skillrun.go` 注释），且 C 期 orchestrator 不调用它。这两项是 PR2 的硬前置，必须先补：

- **T1 升级 `RunSkill` 为多轮 agent loop**：内部从 `llmClient.GenerateText` 改为 `internal/agent.Run`（传 `input.Whitelist` + `AgentType` + `ParentTurnID`），对外签名 `RunSkill(ctx, skillStore, llmClient, provider, model, name, input)` 不变。升级不影响 PR1——C 期从不调用 `RunSkill`。
- **T2 接线 orchestrator S2 → RunSkill**：在 S2 节点调用 `RunSkill(ctx, "story-blueprint", input{Whitelist: blueprintAgentTools})`，产物（卷划分 JSON + 经工具落库的卷记录）写回 `ContextJSON`。

### PR2 内部子任务顺序（建议，依赖序）

1. **T1** 升级 `RunSkill` 多轮化（见 §3.0）。
2. **T2** orchestrator S2 节点接线 `RunSkill` + 产物落库（见 §3.0）。
3. **T3** 新增 `create_volume` MCP 工具（`internal/mcp_tools/volume_tools.go`）+ 在 `registry.go` / `identity.go`(`mainAgentTools`) 注册（见 §3.1）。
4. **T4** S2 执行+落库：story-blueprint 产出卷划分 → 逐卷 `create_volume(outline)` → `EmitStage(S2,done)` → `EmitGate(G2)`（见 §3.2）。
5. **T5** 前端 `BlueprintWorkflowView.tsx` + G2 确认卡 + `skillrun_test.go`（mock 白名单越权被拒）。

### 3.1 `create_volume` MCP 工具（PR2 硬前置，对应 T3）
| 文件 | 动作 | 说明 |
|---|---|---|
| `internal/mcp_tools/volume_tools.go` | 新增 | `RegisterVolumeTools(r)`；`create_volume` 工具：`CreateVolumeArgs{Name string; BeforeVolumeID *int64; Outline *string}`；实现调 `volume.NewStore(ctx).Place(...)` 建卷记录，若 `Outline` 非空同步写 `volumes/id_{新卷id}.md`（复用现有文件 write 通道 `rw_tools.go:36`） |
| `internal/mcp_tools/registry.go:5` | 改 | `RegisterAllTools` 内加 `RegisterVolumeTools(r)` |
| `internal/agentcfg/identity.go:18` | 改 | `mainAgentTools` 追加 `"create_volume"` |
| （可选）`internal/mcp_tools/volume_tools.go` | 加 `update_volume` | 重命名/移动卷，供 S2/S5 调整 |

- 测试：`volume_tools_test.go` —— `create_volume` 建卷记录 + 写 `volumes/id_{id}.md`；`BeforeVolumeID` 插序正确；白名单生效（mainAgentTools 含 `create_volume`）。

### 3.2 S2 蓝图节点（确定性触发 story-blueprint）
- `internal/agentcfg/identity.go` 新增 `blueprintAgentTools`（一次性 agent run 白名单）：含 `create_volume` / `read` / `list_volumes` / 卷纲 write 通道工具 / `get_chapter_list` 等，**不含 `edit` 章节正文**（蓝图期不写正文）。
- `internal/pipeline/skillrun.go` 的 `RunSkill` 被 S2 调用：`RunSkill(ctx, "story-blueprint", input{Whitelist: blueprintAgentTools, ...})`。
- S2 编排（`orchestrator_c.go` 或 PR5 的 `orchestrator.go`）：story-blueprint 产出卷划分 → 逐卷 `create_volume(outline)` 落库 → `EmitStage(S2, done)` → `EmitGate(G2)`。
- 前端 `BlueprintWorkflowView.tsx`（新增）：调 story-blueprint、展示卷结构与卷纲、G2 确认卡。
- 测试：`skillrun_test.go` —— `RunSkill("story-blueprint", ...)` 调 `create_volume` 且产物含卷结构 JSON；mock 工具白名单验证越权调用被拒。

---

## 4. PR3 — S4a 签约评审 + S5 修复闭环（已落地 2026-10-18）

**目标**：写完黄金三章 → S4a 签约体检 → 不过则 S5 按 issue list 修复 → 复评，直到通过或用户强制继续。

### 4.1 S4 签约评审节点
- `internal/agentcfg/identity.go` 新增 `signabilityAgentTools`：只读台账 + `read` + `get_chapter_list`/`list_volumes` 等（体检是评估，不写）。
- S4 调用 `RunSkill(ctx, "signability-audit", input{Whitelist: signabilityAgentTools})` → 产出 **issue list（结构化 JSON：id / 层级 / 描述 / 严重度）** + 通过/不通过判定。
- `orchestrator`（C/A 共用）收到「不通过」→ 进 S5；收到「通过」→ 继续。

### 4.2 S5 修复闭环（共享）
- S5 = `RunSkill` 跑一个「修复 agent」循环：读 issue list → 调 `edit` / 卷纲工具 / `update_chapter_plan`（章细纲）/ `statesync` 复查 → 修复后回到 S4 复评。
- **S5 修复 agent 白名单**：含 `edit` / `update_chapter_plan` / 卷纲 write / `create_volume`（补卷）/ 全部 statesync 写工具 / `read` 类只读。
- **终止条件（§10.7）**：复评通过 → 出循环；复评不通过 → 继续修；用户可「强制继续」（`ResumePipeline(decision=force_continue)` 跳过）。
- **安全检查点（§5.1）**：S5 每轮迭代前轮询 `Status==paused/aborted`，命中即挂起持久化。
- 测试：`signability_audit_e2e`（扩展现有 `app/skill_audit_e2e_test.go`）—— 注入样例 issue list，验证 S5 修复后复评通过；验证 paused 时 S5 中断可续。

---

## 5. PR4 — G3b 里程碑闸 + 卷边界 S4b 复评（已落地 2026-10-19）

**目标**：长线写作的多节点质量闸门就位；解决「写完多章仍停在逐章写作、无法再触发签约体检」的卡点。

### 5.1 设计要点（相对原 §5 的修正）
- **触发驱动**：C 期架构下，S3 写作本身走前端 chat，流水线只在用户点「继续/检查进度」时评估一次。因此 G3b 闸在 `ResumePipeline` 的 S3 分支按**当前章节数**判定，而非后台监听每章落库。
- **里程碑判定**（满足任一即弹 G3b 并给 `audit_now` 选项）：
  1. `当前章数 - ContextJSON.last_audit_chapter >= 5`（每 5 章复检一次）；
  2. **跨卷边界**：最新两章的 `volume_id` 不同（即刚进入新卷）。
- **首检**：`章数 >= 3 && !S4Audited` → 走原黄金三章首检（进入 S4，reason=golden3），与 PR3 行为一致。
- **G3b 选项**：
  - 里程碑闸：`["continue", "audit_now", "abort"]`；
  - 普通逐章闸（未达里程碑）：`["continue", "abort"]`，文案提示「继续写作，累计 5 章或进入新卷会提示体检」。
- **卷边界复评用 S4b 阶段**：`audit_now` 或跨卷触发的复检进入 `S4b`（reason=milestone/volume），首检走 `S4`。`S4/S4b` 在 `ResumePipeline` 共用一套处理逻辑；`executeFix` 复评后回到 `ContextJSON.audit_stage` 记录的同阶段。

### 5.2 代码改动清单
| 文件 | 改动 |
|---|---|
| `internal/pipeline/store.go` | `GateContext` 新增 `LastAuditChapter int` / `AuditReason string` / `AuditStage string`（json 字段 `last_audit_chapter` / `audit_reason` / `audit_stage`）。 |
| `internal/pipeline/service.go` | 1) `Service` 新增可替换 `volumeBoundary` 函数（默认 `defaultVolumeBoundary`，查最新两章 `volume_id` 比较）；2) `enterAudit(ctx, run, reason)` 按 reason 选 `S4`/`S4b`，并把 `AuditReason`/`AuditStage` 写回 `ContextJSON`；3) `markAuditedJSON(ctx, chapterCount)` 置 `S4Audited=true` 且 `LastAuditChapter=当前章数`；4) `executeAudit` 以 `run.CurrentStage` 作为 audit 阶段，`isAuditStage` 守卫放宽到 S4/S4b；5) `executeFix` 复评前复位到 `ctxt.AuditStage`；6) `ResumePipeline` S3 分支加入 `audit_now` + 里程碑判定，S4/S4b 合并处理，S5 兜底按 `AuditStage` 回到同阶段。 |
| `frontend/src/types/pipeline.ts` | `GateDecision` 增加 `"audit_now"`。 |
| `frontend/src/components/pipeline/PipelineGateCard.tsx` | `config` 增加 `audit_now` 项（图标 `Stethoscope`，label `pipeline.gate.auditNow`）。 |
| `frontend/src/components/pipeline/PipelineWizard.tsx` | 1) S3 无闸时增加「检查进度 / 继续写作」按钮触发 `resume("continue")`（让里程碑判定可触发）；2) 审计视图条件加入 `S4b`。 |
| `frontend/src/components/pipeline/SignabilityAuditView.tsx` | 阶段判定加入 `S4b`（复检视图与 S4 同款）。 |
| `frontend/src/i18n/locales/zh-CN.json` / `en.json` | 新增 `pipeline.gate.auditNow`；`pipeline.stage.S4b` 改为「卷边界复评 / Volume Re-audit」；`pipeline.hint.s3` 明确里程碑触发条件。 |

### 5.3 测试
- 现有 `service_test.go` 全绿（黄金三章首检 / continue 置位 / fix 闭环复评 三条用例兼容 S4b 改动）。
- 新增 `service_test.go`：
  - `TestServiceMilestoneGateOffersAudit`：注入 `chapterCount=8` 且 `last_audit_chapter=3` → S3 `continue` 应落 G3b 闸且 `options` 含 `audit_now`；
  - `TestServiceVolumeBoundaryOffersAudit`：注入 `volumeBoundary=true` → S3 `continue` 落 G3b 且 `options` 含 `audit_now`；
  - `TestServiceAuditNowEntersS4b`：G3b 选 `audit_now` → `CurrentStage==S4b` 且 `audit_stage==S4b`；
  - `TestServiceNonMilestoneGateNoAudit`：注入 `chapterCount=4` 且 `last_audit_chapter=3` → G3b `options` 仅 `[continue, abort]`；
  - `TestServiceMilestoneResetsLastAuditChapter`：复检后 `continue` → `last_audit_chapter` 更新为当前章数。

---

## 6. PR5 — A 期 Orchestrator 状态机 + S6 完本（未实现，待 PR5）

**目标**：顺序/闸门/复评逻辑下沉后端，确定性；完本有收口；支持「自动跑到下一闸」；导入/续写自动选入口。

### 6.1 设计要点（相对原计划 §6 的修正，贴合当前 C 期代码）

> 当前（PR1–PR4 落地后）阶段流转逻辑是**双源**：`service.go` 的 `ResumePipeline` 里硬编码了 S3 里程碑 / S4 / S5 的特殊分支（约 145–225 行），而 `orchestrator_c.go` 的 `NextStage` 只兜底 S1→G1→S2 与线性收尾。`StageS6` 常量已定义但从未被任何流转引用；`PipelineRun.AutoRun` 字段已存在但无任何接线。PR5 要把这些收敛成**单一可信源**。

- **逻辑下沉到 `Orchestrator`**：新增 `internal/pipeline/orchestrator.go`，`Orchestrator` 持 `run_id / current_stage / store / events / skillRunner`，暴露 `Advance(ctx, run, decision) error` 统一推进。`service.go` 的 `ResumePipeline` 退化为薄壳——仅做参数校验 + `AppendGateDecision` + 调 `orchestrator.Advance`；原 S3/S4/S5 特殊分支整体上移到 `Orchestrator.Advance` 内按 `current_stage` 分派。
- **A 期状态机（DAG）**：流转表覆盖 S0|S1..S6 全阶段 + 回边（S4→S5→复评回 S4/S4b）+ 循环（S3 逐章）。节点动作类型：`RunSkill`（S1/S2/S4/S4b/S5/S6）、里程碑判定（S3）、`create_volume`（S2/S5 补卷）、`edit`（S3 实际写作走前端 chat，后端只判定）。所有推进确定性，不依赖 LLM 记忆步骤。
- **S6 完本终检**：新增 `enterFinalAudit(ctx, run)`（复用 `enterAudit` 范式，`reason="final"`，stage=S6）→ `executeFinalAudit` 跑 `signability-audit` 全本 → 落 **G终闸门**（`options: ["finish", "fix", "abort"]`）。`finish` 置 `Status=finished`；`fix` 进入 S5 修后复评，复评通过再回 S6；`abort` 置 aborted。
- **auto_run 自主连跑**：`StartPipeline` 增加 `autoRun bool` 参数并写入 `AutoRun` 字段。`Orchestrator.Advance` 在 `autoRun=true` 时，每完成一个**非闸门节点**自动以 `continue` 决策继续推进；仅在 `EvGate` 停下。闸门自动裁决策略：G3b 普通闸 auto-`continue`；G4 通过 auto-`continue` / 不通过 auto-`fix`；G终需人工 `finish`（避免误判完本）。同时支持 `ControlPipeline(cmd="set_auto")` 运行中切换。
- **安全检查点**：阶段切换前、S3 各章间、S5 每轮前、auto_run 每章间，轮询 `Status==paused/aborted`，命中即挂起持久化（复用 PR1 `ControlPipeline` 的 pause/resume/stop）。
- **S0 导入/续写自动选入口**：新增 `DetermineEntryStage(novelID)`——无章节→S1；有章节>3 且无台账→S0；已就绪（有章节且有台账）→S3。`StartPipeline` 据结果置初始 `CurrentStage`；S0 对齐子流程（归卷 → 补存量章细纲 → 全量 statesync → 现状体检 → S5 存量修复 → 汇入公共循环）编排进 `Orchestrator`。
- **前端退化**：`PipelineWizard` 移除顺序决策逻辑，仅订阅 `EvStage / EvGate / EvControlAck` 做纯进度展示；阶段条加入 S6 + G终。`PipelineControlBar` 增加「自动运行」开关（调 `StartPipeline(autoRun)` 或 `ControlPipeline("set_auto")`）。

### 6.2 代码改动清单

| 文件 | 改动 |
|---|---|
| `internal/pipeline/orchestrator.go`（**新增**） | `Orchestrator` 结构体 + `Advance(ctx, run, decision)` 统一流转；全阶段节点表与回边；`enterFinalAudit` / `executeFinalAudit`（S6）；`DetermineEntryStage`（S0 选入口）；`autoRun` 连跑与安全检查点轮询。 |
| `internal/pipeline/orchestrator_c.go` | 常量定义（`Stage*`/`Gate*`）保留；`NextStage` 函数迁移到 `orchestrator.go` 后**删除本文件函数**，避免双源。 |
| `internal/pipeline/service.go` | 1) `ResumePipeline` 改为调 `orchestrator.Advance`（原 S3/S4/S5 特殊分支移除，Logic 上移）；2) `StartPipeline` 增加 `autoRun bool` 并写入 `AutoRun`；3) `ControlPipeline` 增加 `set_auto` 命令切换 `AutoRun`；4) S6/G终 的 `finish` 决策置 `Status=finished`。 |
| `internal/pipeline/store.go` | `GateContext` 新增 `EntryStage string` / `ImportAligned bool`（S0 状态追踪，json 字段 `entry_stage` / `import_aligned`）；`PipelineRun` 已有 `AutoRun` / `StartChapter`。 |
| `frontend/src/components/pipeline/PipelineWizard.tsx` | 顺序逻辑移除，纯展示；阶段进度条加入 S6 + G终；S6 视图复用 `SignabilityAuditView` 终检样式。 |
| `frontend/src/components/pipeline/PipelineControlBar.tsx` | 增加「自动运行」开关 → `StartPipeline(autoRun)` / `ControlPipeline("set_auto")`。 |
| `frontend/src/i18n/locales/zh-CN.json` / `en.json` | 新增 `pipeline.stage.S6`（「完本终检 / Final Audit」）、`pipeline.gate.finish`（「确认完本」）、S0 相关文案、`pipeline.autoRun`（「自动运行」）；G终选项 label。 |

### 6.3 测试

- 新增 `internal/pipeline/orchestrator_test.go`（完整 DAG）：
  - `TestOrchestratorFullRun`：从 S1 跑通至 S6 → G终 → `Status==finished`；断言各产物（卷记录 / 体检 JSON / 修复痕迹）落库。
  - `TestOrchestratorGateBlockResume`：S3/G4/G终 闸门点 `Advance` 后 `Status==awaiting_gate`，`ResumePipeline` 恢复后继续。
  - `TestOrchestratorAutoRunStopsAtGate`：`autoRun=true` 时连续推进直到首个 `EvGate`，不在非闸门节点停留。
  - `TestOrchestratorPauseAtCheckpoint`：S5 每轮前 / S3 章间置 `paused`，断言 `Advance` 挂起且不推进。
  - `TestOrchestratorS4S5ReAuditEdge`：S4→fix→S5→复评→S4 回边正确；`audit_stage` 在 S4/S4b 间保持。
  - `TestOrchestratorS0ImportEntry`：注入老书（有章节无台账）→ `DetermineEntryStage` 返回 S0 → 跑对齐子流程至汇入 S3 循环。
  - `TestOrchestratorResumeAfterRestart`：模拟中断后 `GetByRunID` 恢复 `current_stage`+`context_json` 续跑。
- 端到端：起 `wails dev`，A 期 `autoRun` 跑完一本短篇（3 卷 × 数章），断言 `pipeline_runs` 终态 `finished` 且各产物落库。
- CI 门禁同前：prettier / i18n:check / eslint(0 error) / `npm run build` / `go build ./...` / gofmt + golangci-lint。

### 6.4 完成判据（验收）

- [ ] `orchestrator.go` 成为唯一流转源，`ResumePipeline` 无阶段分支硬编码。
- [ ] S6 完本终检 + G终 + `finished` 收口可跑通。
- [ ] `auto_run` 可一键连跑并在闸门/安全检查点停。
- [ ] S0 导入/续写依据小说状态自动选入口并跑通对齐子流程。
- [ ] 前端 `PipelineWizard` 仅展示、无顺序逻辑；`PipelineControlBar` 有自动运行开关。
- [ ] `orchestrator_test.go` 全部用例绿。


---

## 7. 验收总表（对齐设计文档 §11）

| 能力 | 落在 PR | 验收点 |
|---|---|---|
| 线性阶段进度 + 仅点「继续/修改/终止」 | PR1→PR5 | 全流程无需用户记下一步 |
| S4→S5→复评闭环 | PR3 | 不过即修、修完复评、可强制继续 |
| S2 真正建卷/写卷纲落库 | PR2 | 卷记录 + `volumes/id_{id}.md` 同时就位 |
| 章细纲每章强制前置 | PR1(拦) + PR5(校验) | 缺章细纲 `edit` 不可推进 |
| 台账实时维护 | PR1(禁) + PR5(校验) | `statesync` 未落库前禁用下一章/闸门 |
| 续写/导入模式 S0 | PR5 | A 期按状态自动选入口；S0 对齐跑通 |
| 暂停/恢复/停止 + 续跑 | PR1(开关) + PR5(检查点) | 任意安全检查点可中断、从 `current_stage`+`context_json` 续 |
| LLM 自动建卷 | PR2 | story-blueprint 经 `create_volume` 逐卷落库 |
| 确定性编排（不依赖 LLM 记步骤） | PR5 | C：前端强制；A：后端强制 |
| 完本收口 | PR5 | S6 终检 + G终，状态 `finished` |

---

## 8. 依赖与排期

```
[1.0 公共基建: store + events + RunSkill]  ← 必须先做
        │
   ┌────┴─────────────┐
 PR1 闭环+控制          PR2 建卷+蓝图(需 RunSkill)
   │                      │
   │                 PR3 签约+S5(需 RunSkill, 复用 PR2 的 S4 节点)
   │                      │
   └──────┬───────────────┘
       PR4 G3b+卷边界(依赖 PR1 状态机骨架 + PR3 的 S4/S5)
          │
       PR5 Orchestrator 状态机 + S6(汇总 PR1-4 逻辑下沉)
```

- **建议顺序**：1.0 基建 → PR1 → PR2 → PR3 → PR4 → PR5。
- 每 PR 独立可编译、可单独验收、可回滚（各自一个 feature 分支 + PR）。
- `RunSkill` 基建（1.0）是 PR2/PR3 的硬前置，务必先合。

## 9. 风险与回滚

1. **前端状态漂移（§10.4）**：用户游离出向导时以实际产物为准；`GetPipelineState` 提供「从当前面板同步」兜底。
2. **S5 无限循环（§10.7）**：复评上限 + 用户强制继续作为终止安全阀；单测覆盖。
3. **`create_volume` 写通道权限**：复用 `rw_tools.go:36` 既有卷纲 write，不新开文件权限面；若 CI 报权限，回归既有通道。
4. **子 agent 枚举无 blueprint/signability（§5.2）**：坚持走 `RunSkill` 一次性 agent run，**不要**往 `subagent_tools.go:12` 硬塞（保持子 agent 语义纯净）；如需原生子 agent，按 §13 方案甲独立评估。
5. **回滚**：每 PR 独立分支；`pipeline_runs` 表若需回退，`migrate` 的 `AutoMigrate` 仅加列不删，旧表可保留；功能开关（如 `auto_run` 默认 false）保证 PR5 未合时行为不变。
