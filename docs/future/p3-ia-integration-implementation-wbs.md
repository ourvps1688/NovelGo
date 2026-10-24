# P3「IA 整合（闭 W3）」—— 三工厂旅程化 · 空态漏斗 · 设置瘦身 · 叙案统一 —— 分阶段实施计划细化（WBS）

> 承接 `ultimate-form-integration-plan.md` P3 阶段（W3「双平面平行摆放/功能堆」闭合；§3.3 三工厂 IA 终态、§5-P3 四条承诺、§7「IA 重排引发老用户迷失」风险对策）。
> 顺序铁律：先定死工厂映射与导航骨架 → 叙案与空态漏斗 → 设置瘦身与质检旅程 → 吸收/创作旅程 → 验收收尾。
> 每个任务标注「文件触点 / 交付物 / 验收 / 依赖 / 风险」，未过验收不得进入下一任务，一任务一提交一销账。
> **范围铁律**：P3 只做 UI/叙事层重组（旅程化导航、空态漏斗、设置瘦身、叙案统一）；**后端零改动**（仅允许新增只读空态判定 API，零写路径）；**不合并双速语义**（plan 非目标 #1，ChatPanel 常驻与流水线闸门语义不动）；**功能型入口不硬移除**（plan 风险对策：旅程式默认视图 + 功能型次级导航）；P0–P2 工件（装配器/voice/漂移检测/快照/retcon）零触碰；**desktop-first**（用户偏好：布局按桌面设计，不主动做移动端 parity）。

---

## 0. 契约定案（冻结 · 实现唯一依据）

### 0.1 三工厂旅程契约（factory map 单源 + 旅程式默认 + 功能型次级导航）

- **工厂归属映射表单源**：新 `frontend/src/lib/factory.ts` 导出 `FACTORY_MAP: Record<SidebarPanelId, FactoryId>` 与「喂给谁」文案 key（i18n）。`FactoryId = "absorb" | "create" | "quality" | "base"`。**全量覆盖断言**：`SidebarPanelId` 每一项必须有归属（TS `Record` 全键 + 组件测试双重锁定），这是验收①「任意功能入口能回答喂给谁」的机械保证。
- **归属定案**（喂给谁=该功能的消费方，plan §0 判断准则）：

| 工厂 | 面板（现有 PanelId，零删零改） | 喂给谁（叙案短语） |
|---|---|---|
| absorb 吸收工厂 | `style-samples`（提取工作区）、`skills`、`skill-matrix` | 喂给文风上限（心智/套路/表达 DNA 入库） |
| create 创作工厂 | `chapters`、`chapter-management`、`preferences`、`novel-settings`、`profile`、`bookpitch`、`characters`、`locations`、`storyarcs`、`timeline` | 喂给正文与真理层（意图→章节→台账） |
| quality 质检工厂 | `signability`、`pipeline`、`reader` | 喂给签约下限（体检→修复→冻结→巡检） |
| base 基础设施 | `novels`、`search`、`git` | 喂给全部（书架/检索/版本） |

- **导航形态**：ActivityBar 重组为**四个工厂分组**（组标题=工厂名+叙案副标题），组内条目即现有面板——**功能条目全保留为次级导航，不做硬移除**（plan §7 风险对策原文）。分组顺序=价值链顺序（吸收→创作→质检→基础）。
- **旅程视图**：新增三个轻量旅程视图面板（PanelId 扩展：`journey-absorb` / `journey-create` / `journey-quality`），按旅程顺序排功能卡片（点击直达既有功能面板，一跳可达原工作面）：
  - journey-absorb：导入参考书 → 提取（套路/心智/风格）→ 入库 Voice/技能库；
  - journey-create：意图（pitch/蓝图）→ 写作（**自由区 chat + 质检区流水线双速卡**）→ 重写；
  - journey-quality：体检（8 维+文风漂移）→ 修复/重写 → 卷末冻结（事实+风格双快照）→ 巡检张力报告。
- **默认落地**：进入书后默认 `journey-create`（创作工厂=旅程中枢，含「继续写作」主卡一跳回 chapters 原工作面）；ChatPanel 常驻不受影响（双速共存，plan §3.3/§6.1）。
- **面板归属面包屑**：主区功能面板顶部显示归属条（「质检工厂 · 喂给签约下限」），回答「喂给谁」不靠记忆靠可视。

### 0.2 叙案统一契约（上限/下限一组词）

- **叙案单源**（i18n key `narrative.tagline`，双语）：「**流水线保证签约下限，心智提取抬高文风上限**」（plan 北极星原文）；配套 `narrative.upper`/`narrative.lower` 短语供分组副标题引用。**禁止硬编码变体**——组件测试 grep 断言 UI 代码无散装「上限/下限」句子（`genreTemplates.ts` 等业务文案除外）。
- **落点**（plan「贯穿 onboarding 与入口文案」）：①onboarding（`InitView`/`StartupOverlay` 首启叙事）；②三工厂分组副标题；③三旅程视图头部；④空态引导文案；⑤About 页一句话定位。全部走 i18n（zh-CN/en 同步，`i18n:check` 闸门）。

### 0.3 空态引导漏斗契约（未提取 → 重写提示）

- **判定源**：`GetVoiceLedger(novelID)`（P1 T3.1 既有 API，后端零新增）十维全部 `Present=false` 或返回空 → 「未提取心智」。判定调用失败/异常 → **静默不提示**（降级纪律：宁缺勿错，绝不弹错误）。
- **触发点**（plan「未提取心智的书**在重写时**提示」）：①创作工厂自由区重写动作入口（写作页/ChatPanel 重写相关动作处）；②质检工厂重写交接口（rewrite_handoff「请在写作页重写相关章节」提示处）。
- **提示形态**：非阻断 inline hint：「可提取参考书心智以保持文风」+ 主按钮直达吸收工厂提取工作区（`style-samples`）。**消隐条件**：已提取（台账非空）或用户当次会话关闭；绝不阻断重写动作本身。
- 空态判定缓存按 novelID 切换失效；提取完成后回到重写入口即不再提示（台账已非空）。

### 0.4 设置页瘦身契约（写作评分 → 质检工厂面板）

- `WritingConfigTab`（写作评分配置：自动评分开关/及格阈值/最大重试）**迁入质检工厂**：挂 `journey-quality` 的「写作评分配置」卡 + quality 面包屑入口；组件与 `useWritingConfig` 逻辑**零改动**，只动挂载点与入口（plan 原文「写作评分配置并入质检工厂面板」）。
- `SettingsDialog` 瘦身为三 Tab：general（**数据目录等基础设施留设置**，plan 原文）/ model / about；`writing` Tab 移除，旧入口不留双轨（避免两处配置同一数据的困惑）。

### 0.5 兼容与勿回退（P0–P2 成果 + A1–A7）

- **PanelId 路由零删除**：18 个既有 PanelId 渲染分支一字不动；仅**新增** 3 个 journey PanelId 与导航层重组（`types/panel.ts` 扩展、`WorkspaceView` 追加分支）。
- **双速语义勿合并**（plan §6.1）：ChatPanel 常驻、流水线闸门/PipelineWizard 语义零改动；旅程卡片只是**入口**，不重排既有工作面内部。
- **P0–P2 零触碰**：`internal/contextasm`/`internal/voice`/`internal/pipeline` 漂移检测/`internal/snapshot`/`internal/retcon` 不进本 WBS 文件触点；后端至多新增只读空态 API。
- **主题纪律**（CI theme-check 教训 #39）：样式一律 `index.css` 皮肤 token/主题变量（`bg-muted`/`text-foreground` 等），禁硬编码色值。
- **desktop-first**（用户偏好）：布局按桌面设计；移动端抽屉化/parity 不主动提案。

### 0.6 降级纪律（P0 四条平移）

1. 有则注入（叙案/归属/空态提示有数据才显示）；2. 无则降级且界面与旧行为逐字一致（空态判定失败=零提示；factory map 缺键兜底 `base` 不崩溃）；3. 文案预算照旧（i18n 短语）；4. 坏数据宁缺勿错（GetVoiceLedger 报错静默）。

---

## 1. 阶段 1：IA 骨架（工厂映射 + 旅程导航）

### T1.1 工厂映射表与导航分组

- **内容**：新 `frontend/src/lib/factory.ts`（FACTORY_MAP 单源，§0.1 表）+ `ActivityBar` 四工厂分组重组（组标题=工厂名+叙案副标题 key；组内条目=现有 16 项全保留；分组分隔线替代现有硬编码 `i===0/i===4` 分隔）。i18n 双语补 `shell.factory.*` 组标题与「喂给谁」短语。
- **文件触点**：新 `frontend/src/lib/factory.ts` + `factory.test.ts`；`components/shell/ActivityBar.tsx`；`i18n/locales/{zh-CN,en}.json`。
- **验收**：①FACTORY_MAP 全键覆盖断言（SidebarPanelId 每项有归属，缺键类型不过编译）；②组件测试：四分组渲染、16 条目无一丢失；③i18n:check 双语齐。
- **依赖**：§0.1。
- **风险**：分组顺序即价值链顺序，勿按字母/原序；折叠态（collapsed）下只显示图标不显示分组——保持现有折叠行为。

### T1.2 三工厂旅程视图骨架与默认落地

- **内容**：`types/panel.ts` 扩展 `journey-absorb`/`journey-create`/`journey-quality`；`WorkspaceView` 追加三分支（旅程视图组件骨架：头部叙案+功能卡片占位）；默认落地改 `journey-create`（「继续写作」主卡一跳 `chapters`）；主区面板归属面包屑条（FACTORY_MAP 驱动）。
- **文件触点**：`types/panel.ts`、`views/WorkspaceView.tsx`、新 `components/journey/{JourneyAbsorb,JourneyCreate,JourneyQuality}.tsx`（骨架）+ `JourneyHeader.tsx`（叙案+归属面包屑）；i18n 双语。
- **验收**：①三个旅程面板可从 ActivityBar 进入并渲染骨架；②默认落地=journey-create，「继续写作」跳 chapters 成功；③功能面板顶部归属面包屑显示正确工厂；④既有 WorkspaceView.test 全绿（18 面板渲染零回退）。
- **依赖**：T1.1。
- **风险**：老用户迷失（plan §7）——默认落地内「继续写作」主卡一跳回原工作面；全部功能条目仍在 ActivityBar 次级导航可达。

---

## 2. 阶段 2：叙案统一与空态漏斗

### T2.1 叙案统一（上限/下限一组词）

- **内容**：i18n 单源 key（`narrative.tagline`/`narrative.upper`/`narrative.lower`，§0.2）落五处：InitView/StartupOverlay onboarding、三工厂组副标题（T1.1 挂点）、三旅程视图头（T1.2 挂点）、空态提示（T2.2 挂点）、About 页。组件测试 grep 断言无硬编码变体。
- **文件触点**：`views/InitView.tsx`、`components/startup/StartupOverlay.tsx`、`components/settings/AboutTab.tsx`、`components/journey/JourneyHeader.tsx`、`components/shell/ActivityBar.tsx`（副标题）；i18n 双语；`narrative.test.ts`（grep 断言）。
- **验收**：①五落点渲染同一组词（快照/断言）；②grep 断言无散装变体；③双语齐。
- **依赖**：T1.1/T1.2。
- **风险**：文案演进只改 i18n 单源；勿在组件内拼接变体句子。

### T2.2 空态引导漏斗（未提取 → 重写提示）

- **内容**：`useVoiceExtractionState(novelID)` hook（GetVoiceLedger 判定 Present 全 false=未提取，失败静默 false）；重写触发点两处挂非阻断 hint（创作自由区重写动作处 + 质检 rewrite_handoff 提示处）：「可提取参考书心智以保持文风」+ 按钮跳 `style-samples`；会话级消隐（关闭本次不再弹）。
- **文件触点**：新 `hooks/useVoiceExtractionState.ts` + 测试；`components/chat/`（重写动作 hint 挂点）；`components/pipeline/SignabilityAuditView.tsx`（rewrite_handoff hint 挂点）；i18n 双语。
- **验收**：①未提取+重写入口 → hint 出现且一键跳吸收工厂；②已提取 → 零提示（逐字回到旧行为）；③API 失败 → 静默零提示（降级最高纪律）；④关闭后当次会话不再提示。
- **依赖**：T2.1（文案）、T1.2（跳转目标）。
- **风险**：误提示骚扰——判定失败/异常一律静默；hint 非阻断，绝不拦截重写动作。

---

## 3. 阶段 3：设置瘦身与质检旅程

### T3.1 写作评分配置迁入质检工厂（设置页瘦身）

- **内容**：`WritingConfigTab` 挂载点迁入 `journey-quality`（「写作评分配置」卡展开或 quality 面板设置区）；`SettingsDialog` 移除 `writing` Tab 瘦身为 general/model/about（数据目录等基础设施留 general）；组件与 `useWritingConfig` 逻辑零改动。
- **文件触点**：`components/settings/SettingsDialog.tsx`（移除 Tab）、`components/journey/JourneyQuality.tsx`（挂载 WritingConfigTab）、i18n 双语（入口文案迁移）。
- **验收**：①质检工厂内可完成写作评分配置读写（组件测试/手测路径）；②设置页仅剩三 Tab，数据目录仍在 general；③WritingConfigTab 内部行为逐字不变（仅挂载点变）。
- **依赖**：T1.2。
- **风险**：两处配置同一数据的双轨困惑——settings 旧入口不留跳转双轨（一次迁移干净）。

### T3.2 质检工厂旅程内容（体检→修复→冻结→巡检）

- **内容**：`JourneyQuality` 填充旅程卡：体检（8 维+文风漂移，进 `signability`）→ 修复/重写（进 `pipeline`）→ 卷末冻结（事实+风格双快照，进 `pipeline` 卷管理）→ 巡检张力报告（进 `pipeline`）+ 写作评分配置卡（T3.1）+ 每卡「喂给签约下限」短语。
- **文件触点**：`components/journey/JourneyQuality.tsx`、i18n 双语。
- **验收**：旅程卡按序渲染、点击直达正确面板；与 `SignabilityWorkflowView`/`PipelineWizard` 既有入口并存不冲突。
- **依赖**：T3.1。
- **风险**：卡片只做入口聚合，绝不内嵌/重排既有工作面（双速与闸门语义勿动）。

---

## 4. 阶段 4：吸收 / 创作旅程内容

### T4.1 吸收工厂旅程内容（导入→提取→入库 + 空态承接）

- **内容**：`JourneyAbsorb` 填充旅程卡：导入参考书（书架 reference 入口）→ 提取（套路/心智/风格，进 `style-samples` 提取工作区）→ 入库（Voice 台账/技能库，进 `skills`/`skill-matrix`）；空态文案与 T2.2 漏斗承接（「未提取」状态在旅程卡上同语提示）。
- **文件触点**：`components/journey/JourneyAbsorb.tsx`、i18n 双语。
- **验收**：旅程卡按序渲染、直达正确面板；「喂给文风上限」短语落卡。
- **依赖**：T1.2。
- **风险**：提取是长跑任务（ExtractWorkspaceView 常驻挂载纪律），旅程卡不得引入会卸载提取视图的新路由。

### T4.2 创作工厂旅程内容（意图→双速写作→重写）

- **内容**：`JourneyCreate` 填充旅程卡：意图（`bookpitch`/蓝图）→ 写作（**自由区 chat 卡 + 质检区 pipeline 卡并列**，双速语义刻意保留的可视化）→ 重写（含 T2.2 空态 hint 挂点复用）；「继续写作」主卡（T1.2）保持首位。
- **文件触点**：`components/journey/JourneyCreate.tsx`、i18n 双语。
- **验收**：双速卡并列呈现且各直达原工作面；「继续写作」一跳 chapters；叙案副标题一致。
- **依赖**：T2.2/T3.2/T4.1（旅程叙事一致性）。
- **风险**：自由区/质检区不可合并为一张卡（plan §6.1 反模式）。

---

## 5. 阶段 5：验收与收尾

### T5.1 双验收（P3 验收标准本体）

- **内容**：端到端验收测试：①**任意功能入口能回答"喂给谁"**（FACTORY_MAP 全键覆盖 + 面包屑渲染断言 + ActivityBar 全条目有分组归属）；②**新用户按旅程走通吸收→创作→质检**（旅程视图卡片链路测试：absorb 提取卡 → create 写作卡 → quality 体检卡逐跳可达）。结论记入本节（3 行内）。
- **验收**：双联断言全绿 + 结论写入本文件。
- **依赖**：T4.2。
- **结论**：双联断言全绿——①FACTORY_MAP 22 键全归属 + 面包屑逐入口渲染 + ActivityBar 16 条目四分组无一丢失；②吸收提取卡→创作双速写作卡→质检体检卡逐跳可达。PanelId 零删除、18 面板渲染分支全保留（`p3-acceptance.test.tsx` 5 例 + WorkspaceView.test 31 例回归）。

### T5.2 全量门禁

- **内容**：前端四道闸（prettier/i18n:check/eslint 限 0 error/build）；`go test ./...`（后端零改动预期，瞬态判定纪律照旧）；`git diff` 确认 `internal/`（P0–P2 工件）零触碰。
- **验收**：全绿；P0–P2 工件零 diff。
- **依赖**：T5.1。

### T5.3 提交推送与收尾

- **内容**：按「一任务一提交」提交（含测试）；`GIT_SSH_COMMAND="ssh -F /c/Users/Administrator/.ssh/config" git push origin master` + `git status -sb` 核验；`.workbuddy/memory` 日志 + `MEMORY.md` 摘要。
- **验收**：远端同步；任务账销清。
- **依赖**：T5.2。

---

## 6. 横切关注点（贯穿）

- **「喂给谁」是本 WBS 最高验收标准**：任何功能入口答不出喂给谁=验收不过；factory map 是唯一事实源，组件不得自造归属。
- **双速语义勿合并**（plan §6.1）：chat 低摩擦与流水线高保证是刻意分工；旅程卡只做入口聚合，不改交互语义。
- **功能型入口不硬移除**（plan §7）：16 个既有功能条目全保留为次级导航；PanelId 渲染分支零删除。
- **降级逐字一致**（P0/P1/P2 纪律平移）：空态判定失败=零提示；factory map 缺键兜底 base；UI 与旧行为逐字一致（无提示时界面零多余字）。
- **叙案单源禁变体**：上限/下限一组词只在 i18n，组件 grep 断言锁定。
- **desktop-first + theme token**（用户偏好 + CI #39 教训）：桌面布局优先；样式走 index.css 皮肤 token，禁硬编码色值。
- **枚举单源**：FactoryId、PanelId 扩展、i18n key 命名空间（`shell.factory.*`/`narrative.*`/`journey.*`/`emptyHint.*`）各只允许一处定义。

## 7. 需求覆盖映射（plan §5-P3 → 任务）

| P3 需求（源自整合规划 §5/§3.3/§7） | 覆盖任务 |
|---|---|
| 三工厂旅程化 UI 重排 | T1.1 / T1.2 / T3.2 / T4.1 / T4.2 |
| 空态引导漏斗（未提取 → 重写提示） | T2.2（判定 T2.2、文案 T2.1） |
| 设置页瘦身（写作评分并入质检工厂，基础设施留设置） | T3.1 |
| 叙案统一（上限/下限贯穿 onboarding 与入口文案） | T2.1 |
| 验收①任意功能入口能回答"喂给谁" | T1.1（映射单源）+ T5.1 |
| 验收②新用户按旅程走通吸收→创作→质检 | T1.2 / T4.1 / T4.2 + T5.1 |
| 风险对策：旅程式默认 + 功能型次级导航不硬移除 | T1.1 / T1.2（默认落地含一跳回原工作面） |

## 8. 风险与对策（plan §7 平移 + 本 WBS 增补）

| 风险 | 对策 |
|---|---|
| IA 重排引发老用户迷失（plan §7） | 旅程式默认视图 + 功能型入口保留次级导航；默认落地「继续写作」一跳回原工作面 |
| 空态误提示骚扰 | 判定失败/异常静默零提示；已提取自动消隐；hint 非阻断 |
| 双速被误合并 | 旅程卡只做入口；ChatPanel 常驻与闸门语义零触碰（测试回归） |
| theme-check CI 违规（#39 教训） | 样式一律主题 token；新增组件沿用 ui/ 既有组件类 |
| i18n 漏译/键漂移 | i18n:check 闸门 + 双语同步提交；key 命名空间单源 |
| 移动端 parity 诉求蔓延 | desktop-first 明示于范围铁律，移动端不主动提案 |
