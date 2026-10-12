# 竞品拆解分析：AI-Novel-Writing-Assistant 对本项目（NovelGO）写作质量提升的借鉴

> 分析对象：<https://github.com/ExplosiveCoderflome/AI-Novel-Writing-Assistant>（Biz Novel Studio，TypeScript + LangChain/LangGraph + Electron，3147★，AGPL-3.0）
> 分析方法：拉取 README、写法引擎（`writingFormulaGraph.ts` / `formulaTools.ts`）、写作工具（`writeTools.ts`）、拆书/连续性诊断（`bookAnalysisTools.ts`）、以及被误读为"质检"的 `ai-novel-vibe-doctor` 技能源码精读。
> 结论先行：本项目（Go + Wails，本地桌面优先）在**读者视角追踪、图可视化、编辑审批管线**上已领先；对方在**「从参考文本自动萃取可复用写法」+「确定性重复/一致性检测」+「反 AI 化规则」**三块最值得借鉴，且正好对应我们最近反复出现的"——滥用、序号段落、质量自检不稳定"痛点。

---

## 1. 对方能力全景（与写作质量强相关）

| 能力 | 实现位置 | 对写作质量的作用 |
| --- | --- | --- |
| **写法引擎 / Writing Formula** | `writingFormulaGraph.ts`（LangGraph 三步：风格分析→技巧萃取→公式文档→结构化） + `formulaTools.ts`（DB 存储 genre/style/toneVoice/themes/motifs/formulaDescription/formulaSteps/applicationTips） | 从一段参考文本自动提炼"可复现写法公式"，写作时按信号匹配注入 |
| **拆书 / Book Analysis** | `bookAnalysisTools.ts`（`bookAnalysis` 任务表，拆结构/人物/写法） | 把整本参考书拆成可复用资产，喂给写法库+知识库，可"照着一本书写"开新书 |
| **确定性章节连续性诊断** | `audit_chapter_continuity`（无 LLM，关键词簇+开头片段查重） | 检测"时间+地点+动作"重复场景与开头重复，**零成本、可复用** |
| **质量债务根因归因** | `analyze_quality_debt_attribution`（基于 riskFlags 确定性聚合） | 统计反复修复失败的章节根因分布，指导优化方向 |
| **反 AI 规则 / 去 AI 化** | README 提及 `anti-ai-rules` 模块（结构化、按书生效，参与生成/检查/修文） | 抑制典型 LLM 痕迹（破折号滥用、套路化表达） |
| **RAG 知识库** | `knowledgeTools.ts` + Qdrant | 写作时按需召回世界/角色/参考资料 |
| **自动导演（Director）** | LangGraph 编排 plan→draft→review→save→sync | 端到端流水线，含阶段审批门 |
| 写法链空壳 | `writingFormulaChain.ts` 仅 `return input`（占位） | 注意：真实逻辑在 graph，不是这个文件 |

### ⚠️ 重要澄清：被名字误导的 `ai-novel-vibe-doctor`
它的 `description` 明确写的是"Diagnose and repair **repository problems**（启动/构建失败、模型报错、UI 状态不一致、脏数据）"。**它是给本项目代码做排障的"仓库医生"，不是小说正文的文气质检工具**。不要把它当作"质量自检"参考——本项目已有的 `self-check` 才是正文质检，二者目标不同。

---

## 2. 与 NovelGO 现状对照

| 维度 | NovelGO 现状 | 对方 | 谁更优 |
| --- | --- | --- | --- |
| 读者视角追踪 | ✅ `reader_perspective_tools.go`（known/suspense/misconception + 章节归属校验） | 无明显对应工具 | **NovelGO 领先** |
| 图可视化 | ✅ G6 角色/地点关系图、虚拟滚动 | 世界地图/角色关系图（实现未知） | 各有侧重 |
| 编辑审批管线 | ✅ `rw_tools.go` 审批前硬校验（刚加"——"拦截） | `apply_chapter_patch` 带 `approvalRequired` | 打平 |
| 写法/文风约束 | ✅ **已有"提取→技能"**（StyleView `ExtractStyle` 导出用户技能）+ 预置技法技能（deai-flavor/pacing-control/scene-beats/dialogue-subtext）+ `genre-constraints.md` | ✅ 从参考文本自动萃取公式 + `explain_formula_match` 写时信号匹配注入 | **打平**（NovelGO 萃取更完整；对方强在写时自动匹配注入） |
| 重复/一致性检测 | ⚠️ 仅依赖 LLM self-check（易被绕过、字数不稳） | ✅ 确定性查重（零 LLM 成本） | **对方领先** |
| 反 AI 化 | ⚠️ 提示词 + 单点"——"硬校验 | ✅ 结构化、按书生效、贯穿生成/检查/修文 | **对方领先** |
| 参考作品学习 | ❌ 无 | ✅ 拆书→写法库→照书开新书的闭环 | **对方领先** |

---

## 3. 建议借鉴项（按价值与落地成本排序）

### 🥇 A. 写法公式萃取 → **NovelGO 已有，本项改为"补写时自动注入"**（原建议"新增萃取"已不成立）
> ⚠️ 修正（2026-10-11）：经核实 NovelGO 的"提取"工作区**已具备**等价能力，无需从零新建。
> - `frontend/src/components/extract/ExtractWorkspaceView.tsx` 下有两个子功能：
>   - **StyleView（文风样例）**：收集文风样例文本（可打标签、可绑书/全局）→ 选中→「提取」(`ExtractStyle`)→ LLM 生成带 frontmatter 的**技能文件**→「保存到用户技能」。即"把文本/书提炼成技能"，与目标 `writingFormulaGraph` 同一类能力。
>   - **PatternExtractView（套路萃取）**：选整本/指定章节（≥5 章）→ 抽取可复用叙事套路（目标项目无此"从自己书提炼"能力）。
> - `internal/skill/builtin/` 已有预置写作技巧技能：`deai-flavor`、`pacing-control`、`scene-beats`、`dialogue-subtext` 等。
>
> **真正的差距不在"萃取"，而在"写时自动应用"**：目标项目用 `explain_formula_match` 在写作时按风格信号匹配当前章节/大纲并注入公式；NovelGO 萃取出的用户技能若默认是 `manual`/`auto` 模式，未必会在每章生成时被自动加载。
**NovelGO 可落地（增强而非新建）**：
1. **根因已定位**（见 `internal/skill/types.go` + `internal/agentcfg/skill_catalog.go`）：技能分 `auto`(默认，仅出现在 catalog，需 LLM 主动 `read` 才加载) / `manual`(仅 `/` 触发) / `always`(每轮通过 `BuildAlwaysSkillsContent` 常驻注入 system 消息)。`ExtractStyle` 导出的用户技能默认是 `auto`/`manual`，**不会自动约束章节生成**——这正是"萃取结果未影响写作"的根因。
2. **修复方向**：为"当前小说文风/写法技能"提供**常驻注入开关**（设为 `ModeAlways`），让 `BuildAlwaysSkillsContent` 把其正文拼入每轮系统消息；或在 NovelProfile/System1 前缀显式 `read` 该技能。这样等价于目标项目 `explain_formula_match` 的"写法公式始终参与生成"意图。
3. （可选进阶）借鉴 `explain_formula_match` 做轻量信号匹配：写作前用萃取技能的关键词与本章大纲比对，命中才强调对应技法，比全量常驻更省 token。
4. 价值：把已有的萃取能力真正转化为"正文质量上限提升"，而非仅存档。

### 🥇 B. 确定性连续性/重复检测【高价值·低成本·强烈推荐】
**对方做法**：`audit_chapter_continuity` **完全不调 LLM**，用关键词簇（如 `["凌晨","旅馆","蹲"]`）扫描章节，统计重复出现的"时间+地点+动作"组合与开头相同片段（前 30 字相同≥3 章即告警）。
**NovelGO 可落地**：
1. 在 `rw_tools.go` 保存章节后，或一个独立巡检工具里，加一段确定性逻辑：扫描本书近期章节正文，检测 (a) 开头 30 字相同片段≥2 处；(b) 高频场景关键词簇重复≥2 次。
2. 命中则写入章节 `riskFlags` 或在 self-check 报告里单列"重复风险"。
3. 价值：**零 LLM 成本、稳定可靠**，正好补我们在 self-check 上的痛点（LLM 自检不触发/不稳定）。这是"用确定性手段兜底 LLM 质检"的最佳实践，强烈建议先做。

### 🥈 C. 反 AI 规则系统化【高价值·中成本】
**对方做法**：把"反 AI 化"做成**结构化、按书生效**的规则集，贯穿"生成遵循→专项检测→修文遵循"三处。
**NovelGO 可落地**：
1. 把当前散落在 `identity.go` 的格式禁令 + `rw_tools.go` 的"——"硬校验，抽象为一个**可配置的反 AI 规则列表**（如：禁用破折号连段 / 禁用一/二/三序号 / 禁用"仿佛""宛如"堆砌 / 禁用特定套路句式），每条带"生成期注入 + 保存期硬校验 + 自检期卡点"三段行为。
2. 用户可在创作偏好里增删规则。
3. 价值：我们刚手动加的"——"拦截是其中一条，系统化后扩展性与可控性都更好。

### 🥈 D. 拆书 / 参考作品学习闭环【高价值·中成本】
**对方做法**：`bookAnalysis` 任务把整本参考书拆成结构/人物/写法，导入写法库、知识库，并支持"照着一本书写"开新书。
**NovelGO 可落地**：与 A 组合——新增"以某参考风格为标杆"入口：用户粘贴某作者若干段落→萃取写法公式（复用 A 的工具）→绑定到当前小说。这是提升"文风辨识度/质量上限"的最直接杠杆。

### 🥉 E. 质量债务确定性归因【中价值·低成本】
对方 `analyze_quality_debt_attribution` 把反复修复失败的章节做根因聚合。NovelGO 可在 `self-check` 失败/被绕过时记录确定性 `riskFlags`（如"破折号反复出现""字数反复不达标""重复场景"），便于长期统计哪类问题最多、指导提示词/硬校验优化。属于"可观测性"增强。

### 低优先级（架构/工程，非直接写作质量）
- **F. RAG 知识库**（Qdrant）：NovelGO 已有结构化世界/角色/读者视角数据，外部参考资料召回优先级不高。
- **G. 自动导演端到端编排**：NovelGO 用 Go agent + identity 步骤 + 审批管线已实现等价能力，无需照搬 LangGraph。
- **H. Token 前缀缓存**：对方把稳定资产前置以复用前缀缓存。NovelGO 可确保 NovelProfile/写法公式作为稳定前缀输出（工程优化，间接提升一致性）。

---

## 4. 不建议照搬 / 需澄清

1. **`ai-novel-vibe-doctor` 不是正文质检**——它是代码排障技能。不要把它当"质量自检"参考。
2. **许可证风险**：对方为 **AGPL-3.0**。本项目若未来有 SaaS/托管分发意图，直接复制其源码会触发 AGPL 传染。**本分析仅借鉴"设计思路与提示词策略"，具体实现请用 NovelGO 自有代码（Go 侧工具 + 内置技能）重写**，避免代码层直接搬运。
3. **技术栈差异**：对方 LangChain/LangGraph/Electron，NovelGO 是 Go+Wails+G6。编排范式不可直接套，只取"能力点"。

---

## 5. 建议落地优先级（结合当前痛点）

| 优先级 | 项 | 对应痛点 | 工作量 |
| --- | --- | --- | --- |
| P0 | **B 确定性重复/开头检测** | self-check 不稳定、章节套路化重复 | 小（Go 侧确定性逻辑） |
| P0 | **C 反 AI 规则系统化** | "——"滥用、序号段落（已在手动修，系统化更稳） | 中（抽象配置 + 三段行为） |
| P1 | **A 写法萃取"写时自动注入"**（萃取能力 NovelGO 已有，本项只补"如何让萃取出的文风技能稳定影响每章生成"） | 萃取结果可能未被写作流程加载 | 小（技能 mode/注入开关） |
| P1 | **D 拆书/标杆风格** | 想模仿某作者味道（可用现有 StyleView 样例+抽取实现，无需新建） | 小（复用 StyleView） |
| P2 | **E 质量债务归因** | 长期质量可观测性 | 小 |
| P2 | F/G/H | 工程与编排 | 视情况 |

> 注：P0 两项正好把我们最近手动修的"——"与"序号"从"一次性补丁"升级为"可持续、可扩展、带确定性兜底"的机制，建议作为下一轮审计/优化的首选。
