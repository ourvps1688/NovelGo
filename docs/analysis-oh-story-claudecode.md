# zenstory-ai/oh-story-claudecode 深度拆解与可蒸馏清单

> 研究对象：https://github.com/zenstory-ai/oh-story-claudecode（7.4k★，MIT，Python/Markdown skill 包）
> 研究目的：找出能为 NovelGO 添砖加瓦的方法论、质量规则与架构思想。
> 研究日期：2026-10-12

## 0. 一句话定位与关键差异

oh-story 是一套**装进编程 Agent（Claude Code / Codex / OpenCode / ZCode / OpenClaw 等）的网文写作 skill 包**，覆盖全流程：扫榜 → 拆文 → 搭大纲写正文 → 去 AI 味 → 封面图。它本质上用**文件系统当记忆**、用**确定性脚本当质检门禁**、用**100+ 份方法论文档按需加载**来约束 LLM。

**与 NovelGO 的根本差异（决定蒸馏方式）**：

| 维度 | oh-story | NovelGO |
|---|---|---|
| 形态 | 编程 Agent 的 skill 包（文件即数据库） | 桌面应用（Go + Wails + React，结构化 DB） |
| 记忆 | `大纲/正文/追踪/` 目录树 + `_tracking-state.json` | DB 中的章节/角色/设定/技能记录 + git 文件 |
| 门禁 | bash/JS hook（PreToolUse 拦截） | `rw_tools.go` 里的 Go 硬校验 + LLM self-check skill |
| 去 AI 味 | `check-ai-patterns.js` 等确定性 lint | `countForbiddenDashes` 破折号硬校验 + self-check |
| 题材 | 27 张题材文风卡 + 拆文库 | 4 套题材约束模板（偏好面板） |

**结论**：不要照搬它的 agent/hook 部署胶水。要蒸馏的是三层东西——**① 确定性质量规则（可落地为 Go 硬校验）② 题材/写法内容资产（可落地为技能/模板）③ 连续性追踪与门禁的架构思想（可映射到 NovelGO 既有结构）**。

---

## 1. oh-story 的核心架构（值得借鉴的思想）

1. **文件系统即记忆，权威层 + 派生层分离**
   `_tracking-state.json` 是唯一权威（schema、状态修订号、伏笔、时间线、角色快照）；`上下文.md / 伏笔.md / 角色状态/ / 时间线/` 全部由它**确定性重渲染**，手改派生文件会被 `check` 拒绝。→ NovelGO 是 DB 后端，天然有"权威模型 + 视图"结构，可直接映射。

2. **确定性检查与门禁分离**
   - 硬门禁（写前/写后脚本，exit code 决定放行）：缺细纲不准写正文、去味句式欠账不准写下一章、破折号/工程词零容忍。
   - 语义自检（LLM 读清单）：情绪流、爽点密度、一致性。
   → 这条"机械归机械、语义归语义"的分工正是 NovelGO 已经在走的路（`countForbiddenDashes` 是机械层，self-check 是语义层），应**系统化扩写**。

3. **"宁可漏拦不可误伤"原则**
   所有 hook 不确定时 `exit 0` 放行。→ NovelGO 的硬校验也应保持"只拦确定性错误"，把模糊项留给 self-check。

4. **固定格式 + 硬上限的状态卡**
   `上下文.md` 固定 7 栏、硬上限 12KB，每章只读这一份，不进正文 prompt。→ 上下文预算纪律，对应 NovelGO 已做的 `clampMaxTokens` + 压缩守卫。

5. **评分要给得出理由**
   拆文报告每一项评分都附"节选 + 理由"，不靠分数代替解释。→ NovelGO 自检/质检应坚持"带证据"。

---

## 2. 可蒸馏清单（按 ROI 排序）

### 🔴 P0 — 最高 ROI、低成本，且 NovelGO 已有地基

#### A. 去 AI 味确定性 lint 从"破折号"升级为完整门禁
**现状**：`internal/mcp_tools/rw_tools.go` 的 `countForbiddenDashes` 只拦破折号；`self-check.md` 第 5 项只列了破折号和序号。
**oh-story 给了什么**：`check-ai-patterns.js` + `banned-words.md` 是一套分级规则，分两类——

- **Blocking（机械，直接拒收，与破折号同级）**：
  - `not-is-comparison`：「不是 A，（而）是 B」「不是 A，也不是 B，只是 C」
  - `voice-contrast`：「声音不大，却带着……」「语气平静无波」「声音平直/听不出情绪」
  - `negation-parade`：「没有 X，没有 Y……」「没 X，只是 Y」
  - `reverse-not-is`：「是 A，不是 B」
  - `trailer-ending` / `trailer-summary`：章尾「这一夜注定……」「命运的齿轮」「才刚刚开始」「他不知道的是……」
  - `em-dash`（已有）
- **Advisory（提示，需读语境判断，进 self-check 清单而非硬拒）**：碎句号、长段落、微动作复读（了下/了一下）、套式反应（指尖/指节/喉结成片）、抽象总结复读、套词密度（仿佛/一丝/深吸一口气）、比喻密度、解释链密度、监控摄像头式动作清单、引号强调滥用、工整并列。

外加**一级禁用词表**（情态/动作/表情/心理/判断/形容/过渡七类，出现即替换）和**二级密度控制**（突然/缓缓/微微每千字≤3）。

**落地建议**：
1. 在 `rw_tools.go` 扩展一个 `checkForbiddenPatterns(proposed)`（或复用现有 `searchReplace` 同款思路），把上述 **blocking** 类做成正则/状态机硬校验，与破折号同位置（editChapterLike 第 349 行附近）拦截并提示改写。
2. 把 **advisory** 类整理成 `self-check.md` 的新增清单项（如「去 AI 味复查」），让 LLM 写后自检时逐项过。
3. 新增 `internal/text/deslop.go` 集中放规则，便于单测（类比现有 `rw_tools_test.go` 的 `TestEditChapterBodyRejectsEmDash`）。
4. **按书白名单**：参考 oh-story 的 book-local whitelist——在小说偏好里存"本书豁免片段"，扫描时跳过。低优先，可后做。

> 价值：这是单点投入产出比最高的一项。它把 NovelGO 已有的"破折号硬校验"思路推广成一套完整的"去 AI 味确定性护栏"，且全部可单测、不会误伤（blocking 只拦确定性句式）。

#### B. 题材文风卡：从 4 套 → 27 套
**现状**：`frontend/src/components/preference/genreTemplates.ts` + `docs/genre-constraints.md` 只有「爽文/悬疑/言情/传统文学」4 套约束模板。
**oh-story 给了什么**：`references/genre-prose-cards/` 下 27 张题材卡（都市高武、仙侠、悬疑灵异、女频种田、宫斗宅斗、抗战谍战、都市脑洞、玄幻言情……），每张含：**开场抓手 / 冲突发动机 / 爽点与情绪释放 / 对话与声线 / 章尾钩子 / 场景颗粒 / 前中后期打法 / 节奏密度 / 禁止漂移 / 证据摘要（样本量）**。这是经语料校准的写作资产。

**落地建议**：
- 把 27 张卡的中文内容直接搬运进 NovelGO 的题材体系：可扩展 `genreTemplates.ts` 的模板数据，或在 `internal/skill/builtin/` 下为高频题材各建一个 `题材-XXX` 技能（走既有常驻注入管线，复用已做的"按书勾选技能"能力）。
- 每张卡自带"证据摘要（可用 N 本、抽样 M 本）"，比 NovelGO 现有 4 套纯手写约束更可信，也呼应了用户之前要的"题材约束可选项"。
- 优先级：先搬用户最常用题材（都市/仙侠/悬疑/言情/种田/抗战），其余作长尾。

> 价值：纯内容资产，零架构改动，直接放大 NovelGO 题材适配能力，且与已落地的"题材约束模板"无缝衔接。

---

### 🟠 P1 — 中成本、高价值，补齐结构性能力

#### C. 写正文前的"大纲门禁"
**oh-story**：`guard-outline-before-prose.sh` 在 PreToolUse 拦截——正文首建且缺细纲（不计 # 号空白不足 30 字）直接 `exit 2` 阻止；短篇缺小节大纲同理。
**NovelGO 现状**：章节靠大纲+正文共用 `chapter_id`，self-check 第 2 项要求"有推进"，但没有**写前硬门禁**——LLM 仍可能跳过细纲直接写正文。
**落地建议**：在 `rw_tools.go` 的 `editChapterLike` 新建通道（`ref.IsNew && !ref.IsOutline`）加一道检查：若本书/本章不存在对应大纲文件（`outlines/id_X.md`）或大纲实质为空，则拒绝写正文并返回提示"先补细纲"。同样遵循"宁可漏拦"——只拦确定性缺失。可复用 oh-story 的"非空白字符 < 30"判空口径。

#### D. 伏笔 + 作者真相/读者已知 的结构化追踪
**oh-story**：`_tracking-state.json` 里 `foreshadow_changes`（planted_chapter / planned_resolution_chapter / status / importance）+ `时间线` 严格区分 **作者真相**（角色已知、未揭示）与 **读者已知**（已向读者披露）。这正是避免"角色提前知道答案、伏笔写飞"的核心。
**NovelGO 现状**：`reader_perspective_tools.go` 已有 `revealed_chapter_id`（读者视角揭示章）；`editChapterLike` 写后注入提醒"更新伏笔登记/读者认知"，但**伏笔只是提醒，没有结构化实体**。
**落地建议**：
- 在 DB 增 `foreshadow` 实体（id、summary、planted_chapter、planned_resolution_chapter、status[已埋/已回收]、importance[高/中/低]），并给一个"埋/收伏笔"的工具或技能动作。
- 在读者视角/时间线里区分"作者真相"与"读者已知"两栏（复用 `revealed_chapter_id` 语义）。
- 自检清单第 6 项"伏笔处理"改为对照这个实体做确定性核对。

#### E. 拆文库 / 拆文：带评分理由的可复用模块提取
**oh-story**：`story-long-analyze` 把一部书拆成黄金三章深度拆解、逐章摘要、剧情单元、节奏、情绪模块（情绪按 −9~+9 标强度）、角色、设定、文风；`story-short-analyze` 产出 54 个情节节点 + 11 项写作手法。评分**带理由**。
**NovelGO 现状**：已有"提取 → 技能"能力（用户之前指正过 NovelGO 已有类似功能），但偏向"整书/整段萃取成技能"，缺"**拆爆款 → 评分 → 可复用剧情/手法模块库**"这一层。
**落地建议**：新增一个 `拆文` 技能/流程：输入一本书（或 NovelGO 内已有书），输出结构化拆解（黄金三章、逐章摘要、情节节点含情绪强度、写作手法清单），并生成"可复用模块库"供后续同题材写作调用。复用现有 LLM 调用与文件落盘能力，重点是**提示词模板 + 输出 schema**，不必另起炉灶。

---

### 🟡 P2 — 高成本或依赖外部环境，按需

#### F. 扫榜 / 题材趋势（rank scraping）
`story-long-scan` 用 CDP 浏览器爬起点/番茄/晋江/七猫/起点女生等榜单。NovelGO 是桌面应用，要落地需浏览器自动化 + 联网。可作为"题材趋势研究"工具后置，优先级低于 A~E。

#### G. 连续性状态机（权威 state + 派生视图）
oh-story 的 `_tracking-state.json` + 派生视图重渲染 + `state_revision` 乐观锁。NovelGO 已是 DB 后端，等价于"DB 表是权威、前端/导出是视图"；若要引入"逐章记录 + 章节回写事务"可参考其 `tracking_commit.py` 的事务语义，但**不必复制文件型实现**。D 项的伏笔追踪已覆盖其最有价值的部分。

---

## 3. 不建议照搬的部分

- **Agent/hook 部署胶水**（`guard-outline-before-prose.sh`、`story_hook_core.js`、各编程 Agent 的 agents/toml）：这些是"让 skill 在 Claude Code 等里跑起来"的适配层，对 NovelGO 桌面应用无直接价值。
- **文件树型项目结构**（`大纲/正文/追踪/` 目录约定）：NovelGO 用 DB + git 文件，结构不同，只取其"权威/派生分离"思想。
- **多 Agent Spawn 编排**（narrative-writer / consistency-checker / story-architect 等 7 个 sub-agent）：NovelGO 当前是单 Agent 对话 + 技能常驻注入，强行拆多 Agent 收益低、复杂度高。

---

## 4. 推荐落地顺序（roadmap）

| 顺序 | 项 | 改动面 | 预期收益 |
|---|---|---|---|
| 1 | **A. 去 AI 味确定性 lint 升级** | `rw_tools.go` + `text/deslop.go` + `self-check.md` + 单测 | 立竿见影，把现有破折号校验扩成完整护栏 |
| 2 | **B. 题材文风卡 4→27** | `genreTemplates.ts` / builtin 技能 | 纯内容，直接放大题材适配 |
| 3 | **C. 大纲写前门禁** | `rw_tools.go` editChapterLike | 防止跳纲直写，结构更稳 |
| 4 | **D. 伏笔 + 真相/已知追踪** | DB 实体 + 读者视角扩展 + self-check | 解决"伏笔写飞"，复用已有 reader_perspective |
| 5 | **E. 拆文库/拆文流程** | 新技能 + 提示词 + 输出 schema | 补齐"爆款→模块库"能力 |
| 6 | F / G | 浏览器/DB 事务 | 长尾，按需 |

其中 **A 和 B 是"今天就能做、做了立刻更好"的两项**，且与 NovelGO 已完成的破折号硬校验、题材约束模板、按书技能勾选一脉相承，无架构风险。

---

## 5. 关键文件对照（方便直接动手）

| oh-story 资产 | NovelGO 落点 |
|---|---|
| `scripts/check-ai-patterns.js` + `references/banned-words.md` | 新增 `internal/text/deslop.go`，在 `rw_tools.go` editChapterLike 调用 |
| `references/genre-prose-cards/*.md`（27 张） | 扩展 `frontend/src/components/preference/genreTemplates.ts` 或 `internal/skill/builtin/题材-*` |
| `guard-outline-before-prose.sh` | `rw_tools.go` editChapterLike 新建正文通道的细纲存在性校验 |
| `_tracking-state.json` 伏笔/真相字段 | 新增 `foreshadow` DB 实体 + 扩展 `reader_perspective_tools.go` |
| `story-long-analyze` 拆文流水线 | 新增 `拆文` 技能（提示词 + 输出 schema） |
| "宁可漏拦不可误伤" 原则 | 所有硬校验的默认失败策略 |
