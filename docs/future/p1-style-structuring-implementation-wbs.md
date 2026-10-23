# P1「风格结构化」—— Voice 台账 · 分阶段实施计划细化（WBS）

> 承接 `ultimate-form-integration-plan.md` P1 阶段（闭 W4「风格资产非结构化」）。
> 顺序铁律：先定死契约 → 台账表 → 提取双写 → 查询与消费 → 快照/diff → 验收收尾。
> 每个任务标注「文件触点 / 交付物 / 验收 / 依赖 / 风险」，未过验收不得进入下一任务。
> **范围铁律**：P1 只做结构化落库 + 可查询 + 进快照；**不动 S4/闸门**（P2 范围）、**前端零改动**（UI 属 P3）；**无台账数据时提示词与 P0 行为逐字一致**（降级纪律原样延续）。

---

## 0. 契约定案（冻结 · 实现唯一依据）

### 0.1 Voice 画像 schema（10 维，本节为维度清单唯一事实源）

源自主规划 §5 P1 口径，与 `internal/pattern/prompts.go` 表达 DNA 提示语对齐：

| # | 枚举值 | 维度 | 类层 | 主要素材源 |
|---|--------|------|------|-----------|
| 1 | `sentence_fingerprint` | 句式指纹（长短句偏好、疑问/陈述比例） | 通用六维 | style_samples 确定性 Stats + LLM |
| 2 | `high_freq_words` | 高频词 | 通用六维 | LLM（素材/正文） |
| 3 | `taboo_words` | 禁忌词 | 通用六维 | LLM（素材/正文） |
| 4 | `rhythm` | 节奏（先结论还是先铺垫、转折方式） | 通用六维 | LLM + Stats |
| 5 | `humor` | 幽默 | 通用六维 | LLM |
| 6 | `certainty_tone` | 确定性语气 | 通用六维 | LLM |
| 7 | `pov` | POV 与视角处理 | 小说维度 | LLM |
| 8 | `tense` | 时态 | 小说维度 | LLM |
| 9 | `imagery_density` | 意象密度 | 小说维度 | LLM |
| 10 | `dialogue_ratio` | 对话-叙述比 | 小说维度 | style_samples `Stats.QuoteDensity` 映射 + LLM |

- 维度枚举值一经定案**禁改**（diff/快照以枚举值为行键语义）；缺失维度留空行（宁缺勿错），不造默认值。
- 旧「style 六维」（句式/用词/修辞/节奏/叙事视角/氛围语调）为提示词口径遗产，**不作为枚举**；其语义由上表覆盖（句式→1、用词→2/3、修辞→并入 1/9、节奏→4、叙事视角→7、氛围语调→5/6）。

### 0.2 台账存储契约

```go
// internal/voice（新包，Voice 层真理源）
type Entry struct {
    ID        int64     // 主键
    NovelID   int64     // 归属书（Voice 为书级，不挂 volume_id——风格非卷级事实）
    Dimension string    // §0.1 枚举值；(novel_id, dimension) 唯一
    Content   string    // 维度正文（描述性结论，LLM/人工产出）
    Metrics   string    // JSON：确定性数值（如 avg_sent_len/quote_density），可空
    Evidence  string    // JSON：证据锚点（chapter_id 列表 / 素材 sample_id 列表），可空
    Source    string    // 枚举：style_sample | pattern_mental | manual
    CreatedAt / UpdatedAt time.Time
}
```

- 表名 `voice_dimensions`；`(novel_id, dimension)` 唯一索引，写入 Upsert 幂等。
- **不入 temporal/history**（不加 `changed_at`，风格修改不走台账履历——快照 diff 已覆盖变更可见性）；**不入 2.4 台账审计**（无 ref.dangling 类结构约束）。
- 数据源禁止假设存在：三源全空 → 台账为空 → 一切消费端走降级（§0.4）。

### 0.3 双写契约（提取产物结构化落库）

- **技能文件保留为人类可读视图，双写**：`kind: mental` 技能 md、style_profiles 的产出路径**一字不动**；结构化维度是**新增并行落库**，不做视图迁移、不删旧产物。
- 结构化字段进提取 toolcall 输出契约（`pattern.MentalSkillOutput` / `LensOutput` 增可选 `voice_profile` 字段；`style.StyleSkillOutput` 同理），LLM 未产出该字段 → **降级不落库**（宁缺勿错，不解析正文兜底猜测）。
- 提取侧已有「独立证据 <2 自动降级」纪律（B1）平移适用：`evidence` 锚点不足时维度照常落库但 `evidence` 置空并标注，不阻断。

### 0.4 消费端契约（voice provider 升级）

- P0 的 `SetVoiceProvider` 链路不变，**渲染顺序**：`心智正文（P0 路径，persona 未配置则空）` + `台账渲染（voice_dimensions 非空时追加）`。
- **降级逐字一致（本 WBS 最高验收标准）**：台账为空 → provider 输出与 P0 行为**逐字一致**（即仅心智正文/空串）；台账非空 → 台账块**追加在心智正文之后**（P0 输出成为前缀，一字不动）。
- 台账渲染格式单源于 `internal/contextasm` 或 `internal/voice`（二选一，倾向 voice 包出纯文本、contextasm 不感知台账结构）；块内按 §0.1 固定维度顺序渲染。

### 0.5 快照/diff 契约

- `voice_dimensions` 加入 `snapshot.registry`（采集口径=冻结时刻全量，同现有 10 表）；**capture/diff/restore 机制零改动**（registry 驱动，横向复用 A2）。
- diff 可读性走既有 `snapshot.FormatDelta`（retcon 留痕同款）；Restore 回滚含 voice 行（为 P2「retcon 支持风格回改」铺路，P1 不改 A3 状态机）。
- 不进 `AuditLedger` 范围（§0.2）；不进 G_freeze 双快照语义变更（P2）。

### 0.6 风格素材裁决（W2 二选一，定案）

**采纳「并入 Voice 成为素材源」**（主规划推荐项，闭悬空）：
- `style_samples` 从「仅 CRUD + 管理页」升级为 Voice 画像素材源：`style.Stats`（确定性统计）→ `metrics` 映射落库；LLM 风格分析产出维度 `content`。
- 消费链闭合：style_samples → voice_dimensions → voice provider → 提示词（W2「无任何 prompt 消费点」就此闭合）。
- **不做**「删除入口」方案；StyleView/StyleSampleView 前端零改动（P3 整合）。

### 0.7 降级纪律（四条，P0 §0.2 原样平移，勿改）

1. 有则注入；2. 无则降级空串且输出与旧行为逐字一致；3. 预算截断（Voice 8KB，`contextasm.fitBudget` 统一负责）；4. 坏数据宁缺勿错（渲染失败整块放弃）。

---

## 1. 阶段 1：schema 与台账（internal/voice）

### T1.1 voice_dimensions 表与维度枚举

- **内容**：新包 `internal/voice/types.go`——`Dimension` 常量（§0.1 十值）+ `Entry` 模型（§0.2）；migrate 注册建表（`(novel_id, dimension)` 唯一索引；**不加 changed_at**，避开 temporal 回调）。
- **文件触点**：新 `internal/voice/types.go`；`internal/migrate/`（建表清单注册，参照 v170 既有表迁移体例）。
- **验收**：建表成功；枚举值与 §0.1 逐字一致单测；schema 测试（唯一索引生效）。
- **依赖**：§0 契约。
- **风险**：表名勿与 `characters.voice_traits` 列混淆（语义不同：那是角色声纹特征）。

### T1.2 voice.Store 与单测

- **内容**：`internal/voice/store.go`——`Upsert`（novel_id+dimension 幂等覆盖）/`List`/`Get`/`DeleteByNovel`；纯 CRUD，无业务逻辑。
- **文件触点**：新 `internal/voice/store.go` + `store_test.go`。
- **验收**：CRUD 单测全绿；同维度重复 Upsert 只留最新（幂等断言）。
- **依赖**：T1.1。

---

## 2. 阶段 2：提取双写（结构化落库）

### T2.1 pattern mental 提取双写

- **内容**：`MentalSkillOutput`/`LensOutput` 增可选 `voice_profile`（`[{dimension, content, metrics?, evidence?}]`）toolcall 字段；提取完成落盘 skill md 处（`app/pattern_api.go` 产物持久化链）同步 Upsert voice_dimensions（`source=pattern_mental`）。**skill md 产出路径一字不动**（双写）。
- **文件触点**：`internal/pattern/types.go`（toolcall 契约）、`internal/pattern/extract.go`（透传）、`app/pattern_api.go`（落库接线）。
- **验收**：带 voice_profile 的提取 → 台账落库 + skill md 与改造前逐字一致；LLM 未产出 voice_profile → 台账为空、md 正常（降级）。
- **依赖**：T1.2。
- **风险**：toolcall 新增字段不得破坏既有解析（可选字段、omitempty；`extract_test.go` 既有断言全绿）。

### T2.2 style 链路双写 + 风格素材并入（闭 W2）

- **内容**：`StyleSkillOutput` 增同款可选 `voice_profile`；`style.Extract` 产出 → Upsert（`source=style_sample`）；`style.Stats` 确定性映射 metrics（`AvgSentLen/SentLenStdDev/ShortSentPct`→句式指纹、`QuoteDensity`→对话-叙述比、标点密度→确定性语气辅助）。
- **文件触点**：`internal/style/types.go`、`internal/style/extract.go`、`app/style.go`（落库接线）。
- **验收**：素材提取 → 台账落库（含 metrics 数值）；无素材/提取失败 → 空台账降级；W2 消费链闭合（素材→台账→provider→提示词）。
- **依赖**：T1.2。
- **风险**：Stats 映射是确定性代码（非 LLM），指标字段名与 §0.2 metrics JSON 键**单源**，禁止两处各写一份。

---

## 3. 阶段 3：查询与消费

### T3.1 Voice 查询 API

- **内容**：app 层 `GetVoiceLedger(novelID)`（列全部维度行，含空维度占位视图）+ `UpsertVoiceDimension`（人工校正入口，`source=manual`）。
- **文件触点**：新 `app/voice.go`（参照 `app/intent.go` 体例）+ `app/handler.go` Bind 注册。
- **验收**：API 单测（有行/空台账/人工覆盖三路）。
- **依赖**：T1.2。

### T3.2 voice provider 升级（台账渲染，降级逐字一致）

- **内容**：`app/handler.go` 的 `SetVoiceProvider` 实现升级为 §0.4 顺序：心智正文（P0 路径不动）+ 台账渲染追加；渲染函数放 `internal/voice`（纯文本，按 §0.1 固定维度顺序）。
- **文件触点**：`app/handler.go:447` 一带、`internal/voice/render.go`（新）。
- **验收**：golden 测试——①台账空 → provider 输出与 P0 **逐字一致**；②台账非空 → P0 输出为前缀一字不动 + 台账块按序追加；③维度乱序落库 → 渲染按 §0.1 固定顺序。
- **依赖**：T2.1/T2.2/T3.1。
- **风险**：P0 的 `rewrite_voice_ab_e2e_test.go` 与 chat golden 全部不得变化（台账空时）——CI 三线是回归防线。

---

## 4. 阶段 4：快照与 diff

### T4.1 voice_dimensions 进快照 registry

- **内容**：`snapshot.registry` 增 `voice_dimensions` 表项（`internal/voice` import 方向：snapshot→voice 单向）；capture/diff/restore 零改动覆盖。
- **文件触点**：`internal/snapshot/registry.go`、`internal/snapshot/service_test.go`（覆盖断言）。
- **验收**：freeze→diff→restore 链路测试含 voice 行（增/改/回滚三态断言）；既有快照测试全绿。
- **依赖**：T1.2。
- **风险**：存量卷首快照后新增表 → 首个含 voice 的快照 diff 相对基准呈现「全量新增行」，属预期行为（记录于测试注释，勿当 bug 修）。

### T4.2 diff 可读性与 retcon 兼容验证

- **内容**：`snapshot.FormatDelta` 渲染 voice 行变更可读；retcon 回滚链路（Restore）覆盖 voice 行且 A3 状态机零改动验证。
- **文件触点**：`internal/snapshot/format_test.go`、`internal/retcon/`（只加测试不动实现）。
- **验收**：voice 变更在 delta 中可读；retcon Rollback 后 voice 行回滚；retcon 既有测试全绿。
- **依赖**：T4.1。

---

## 5. 阶段 5：验收与收尾

### T5.1 三联验收（P1 验收标准本体）

- **内容**：端到端验收测试：**可查询**（GetVoiceLedger 返回落库行）→ **可 diff**（两版本快照 diff 出 voice 行变更）→ **进快照**（freeze payload 含 voice_dimensions 行）。结论记入本节（3 行内）。
- **验收**：三联断言全绿 + 结论写入本文件。
- **依赖**：T4.2。
- **结论**（T5.1 已毕）：`app/voice_acceptance_test.go` 三联断言全绿——①可查询：GetVoiceLedger 十维视图落库行 Present=true、未落库占位；②可 diff：v2 相对 v1 diff 出节奏内容改 + 禁忌词增（dimension 行键语义可见）；③进快照：freeze payload 含 voice_dimensions 2 行。

### T5.2 全量门禁

- **内容**：`go test ./...`；`golangci-lint run`；前端四道闸（prettier/i18n:check/eslint/build）——**前端零改动，四道闸应原样通过**（防误伤）。
- **验收**：全绿；`git diff` 确认前端源码零触碰。
- **依赖**：T5.1。

### T5.3 提交推送与收尾

- **内容**：按「一任务一提交」提交（含测试）；`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master` + `git status -sb` 核验；`.workbuddy/memory` 日志 + `MEMORY.md` 摘要。
- **验收**：远端同步；任务账销清。
- **依赖**：T5.2。

---

## 6. 横切关注点（贯穿）

- **降级逐字一致是本 WBS 的最高验收标准**：台账空时任何消费端输出多出一个字，视为 P1 bug——它会破坏「无数据=旧行为」的兼容承诺。
- **双写不迁移**：技能 md / style_profiles 是人类可读视图，**不删、不改产出路径**；台账是并行真理源，两者不互相覆盖。
- **枚举值单源**：维度枚举（§0.1）、metrics JSON 键、source 枚举各只允许一处定义，跨包引用常量。
- **AGPL/NOTICE 义务不涉**（无新依赖）；模块路径 `github.com/novelgo/novelgo` 不动。

## 7. 与既有代码的衔接（避免打架）

| 既有资产 | 本 WBS 的关系 |
|----------|----------------|
| P0 `contextasm` 三块契约/降级纪律 | **零改动**；voice provider 返回值变长（追加台账块），装配器预算截断照旧生效 |
| P0 golden（`p0_baseline_*`）/ A/B E2E | 台账空时**必须**继续全绿（=逐字一致的机械证明） |
| `kind: mental` 技能产出（B1） | 双写保留，md 路径一字不动；V2 证据门禁纪律平移到 evidence 字段 |
| `style_samples`/`style_profiles`/`Stats` | 并入素材源（§0.6 定案）；CRUD API 与前端零改动 |
| 快照/retcon 机制（A2/A3） | registry 加一表，机制零改动；A3 状态机零触碰（P2 才扩风格回改语义） |
| 2.4 台账审计 / temporal 履历 | voice_dimensions **不纳入**（无结构引用约束、不加 changed_at） |
| S4/S5/闸门（A1/A6/A7） | 零触碰（文风进闸门是 P2） |

## 8. 完备性核对

### 8.1 需求 → 任务覆盖矩阵

| P1 需求（源自整合规划 §5） | 覆盖任务 |
|---------------------------|----------|
| Voice 画像 schema（六维+小说维度） | T1.1（§0.1 十维枚举） |
| 风格素材（W2）二选一裁决 | T2.2（§0.6 并入素材源） |
| 提取产物结构化落库（双写） | T2.1（mental）/ T2.2（style） |
| 验收·可查询 | T3.1 + T5.1 |
| 验收·可 diff | T4.1/T4.2 + T5.1 |
| 验收·进快照 | T4.1 + T5.1 |
| （判则「它喂给谁」）台账须有消费端 | T3.2（voice provider） |

### 8.2 疏漏清单（立项时已识别，执行中发现新项追加于此）

| # | 疏漏/风险 | 处置 |
|---|-----------|------|
| G1 | 提取 LLM 可能不产出 voice_profile 或维度残缺 | 可选字段降级不落库；缺失维度留空不造默认值（宁缺勿错） |
| G2 | metrics 阈值无使用数据支撑 | P1 只存数值不设阈值；阈值属 P2 漂移检测/P4 度量 |
| G3 | P0 疏漏 G3（provider 解析多书并发热点） | 维持现状；如成热点再缓存（顺带项，非 P1 必做） |
| G4 | voice_dimensions 无 volume_id，跨卷快照逐次全量携带 | 体量小（≤10 行/书），全量携带即正确口径（同 §0.2 设计），非缺陷 |
