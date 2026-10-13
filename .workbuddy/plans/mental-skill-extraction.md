# 心智 Skill 提取 — 开发计划

> 状态：设计已定，待实现（V1）
> 关联决策：① 心智 Skill 独立成文件（不并入套路 Skill）；② 前端「提取」页新增并列 tab「心智提取」；③ 复用 `internal/pattern` 编排 + `flavor` 参数；④ 混合法——合成用 LLM，V2 证据收集走 ONNX 向量检索、跨弧验证走确定性计数；⑤ 生成后写入本书 `.enabled.json` 自动并入写作流（心智模型常驻、启发式按需）。
> **已知约束（2026-10-13 补充）**：用户导入的参考素材以 **TXT/MD 为主、不是 EPUB**。TXT 无内嵌作者元数据（`parseTxt` 只做章节切分），故"同一作者"**无法自动识别**。但心智 Skill 落盘本就是一份 `skills/{name}.md`，其 frontmatter 已支持 `author` 字段，因此**作者在"保存心智 Skill"时手填即可**——身份归属落在技能文件层而非小说模型层，**无需改动 `Novel` 模型 / 导入管线 / txt 解析**（详见 §二·续）。跨书聚合（V3）的前置即"保存时填好来源作者"，随 V1 保存框即可交付，不做成独立模型迁移。

## 一、目标

从单本书（或同一作者多本书）的已写章节中，蒸馏**作者/叙事的"创作心智"**，产出结构化的、可跨书复用的 Skill 文件：

- **叙事心智模型**（3–7 个）：作者如何构造冲突、何时收放信息、对 agency/命运/节奏的信念。
- **创作决策启发式**（5–10 条）：面临抉择时的快速规则，附书中案例章节锚点。
- **价值观与反模式**：追求 / 拒绝 / 内在张力。
- **表达 DNA**：句式指纹 + 风格标签 + 禁忌词（复用 `internal/style` 的 6 维度画像）。
- **诚实边界**：明确声明"这是从虚构文本推断的创作人格，非作者真人三观"，并附推断时间。

## 一·A nuwa 借鉴与小说领域适配原则（借鉴而非抄写）

本设计的**五层心智结构来自 nuwa-skill**（表达DNA/心智模型/决策启发式/反模式/诚实边界），但**全部按"小说写作"领域重适配**——目标是产出"可指导 AI 写新章节的创作心智指南"，而不是"扮演一个真人回答问题的 persona"。因此对 nuwa 的方法论做如下**有原则的裁剪**，既不照抄、也不凭空：

| nuwa 原设计 | 小说领域处置 | 落点 |
|---|---|---|
| 五层心智结构 | **保留**：直译为"叙事心智模型 / 创作决策启发式 / 价值观与反模式 / 表达DNA / 诚实边界"（加"叙事/创作"前缀适配写书语境） | §一 目标 |
| 诚实边界 | **保留+强化**：因对象是**虚构文本**，明确"推断自小说，非作者真人三观；可能混有角色声音" | §一 目标 |
| 心智模型三重验证（跨域复现 / 有生成力 / 有排他性） | **适配**：① 跨域 → **跨独立章节/情节弧复现**（同一技法在 ≥2 条不相关情节线出现才算稳定）；② 有生成力 → **能指导写出新章节**（而非预测真人立场）；③ 排他性 → **区别于通用写作建议**（过滤"show don't tell"这类谁都适用者） | V1 final prompt（见 §三）；确定性计数见 V2 |
| 表达DNA量化（句式指纹统计） | **适配**：复用 `internal/style` 现有 6 维度画像（句式/节奏/词汇/确定性…），并补**小说专属维度**：POV与视角处理、时态、意象密度、对话-叙述比 | V1 表达DNA层 |
| 证据锚点（nuwa：引用出处） | **适配**：改为 **`ChapterAnchor` 章节/段落锚点**（书在磁盘可回溯），比纯文本引用更可核验 | §二 架构图 |
| 矛盾处理（时间性/领域性/本质性矛盾） | **适配**：小说里关键是区分 **作者稳定技法 vs 角色个体声音 vs 作者有意变化（多 narrator / 不可靠叙事）**，避免把角色世界观误归为作者心智 | V1 价值观与反模式层 |
| 六路并行采集（全网调研真人） | **弃用**：参考素材是已落盘的**单本书章节**，无需 web 调研 agent；采集由 `internal/pattern` 的 boundary→summary→chunk 承担 | §二 架构图 |
| 角色扮演规则（第一人称"我是X"回应） | **弃用**：心智 Skill 是**写作时注入的指南**（常驻 + catalog），不是对话 persona；但保留其两条诚实子规则——"未表态主题标推断""关键引用可分辨"——分别由诚实边界与 `ChapterAnchor` 承担 | §五 影响 |
| 保真度评分卡 / 产出后测试（已知立场一致 + 未知问题适度不确定） | **适配**：V1 末轮加"质量自检清单"；V2 加"产出后保真度测试"——用本书作者已明确处理过的冲突测方向一致，用作者从未涉及的题材测"适度不确定而非斩钉截铁" | §四 V1/V2 |

> 一句话原则：**结构借 nuwa，验证借 nuwa 的"防臆造"精神，但所有机制都重写成"对小说写作有用、对落盘章节可核验"的形态**。凡 nuwa 为"扮演真人"服务的部分（并行调研、第一人称角色扮演）一律弃用或下沉为写作指南。

## 二、架构

```
前端 ExtractWorkspaceView
  tab: 风格 | 套路 | 心智(新增)
                    │
                    └─ MentalExtractView（复用 PatternExtractView 的 scope/模型选择/进度，flavor="mental"）
                                 │
后端 internal/pattern（同一编排，加 flavor 分流）
  preAnalyzeBoundaries → 章节摘要 → initialChunks → compressChunks → finalSkill(flavor)
                                          │
                              flavor="struct"  → PatternSkillOutput（现有）
                              flavor="mental"  → MentalSkillOutput（新增 5 层 schema）
                                                          │
                                          落盘 skills/{name}.md（frontmatter mode:auto + kind:mental）
                                          生成后 skill.Enable(novelID, name) → 写 .enabled.json
                                                                  │
                                          写作流 BuildSystemMessages 已支持 always/auto/enabled → 自动并入
```

**关键复用点**：编排（boundary→summary→chunk→final）只写一遍；只有 final round 的提示词与输出 schema 按 flavor 不同。写作流注入机制（`skill_catalog.go` 的 always/auto/enabled）零改动即可消费。

## 二·续 作者身份：写在心智 Skill 文件层（而非小说模型层）

**核心简化（2026-10-13 用户建议）**：与其给 `Novel` 模型加 `Author` 字段、改导入管线、改 txt 解析，不如**让用户在"保存心智 Skill"时手填来源作者**——因为心智 Skill 落盘就是一份 `skills/{name}.md`，其 frontmatter 本就支持作者字段。身份归属落在技能文件层，正是它语义所属的位置，改动面最小。

> 关键结论：**跨书聚合（V3）的基础不是自动提取，而是用户保存时手填的来源作者**。该字段随 V1 的"保存心智 Skill"对话框即可交付，**无需** `Novel.Author`、`internal/import/parse.go`、导入对话框等任何模型/管线改动。EPUB 的 `<dc:creator>` 不再需要（TXT 工作流下作者只能手填，EPUB 自动提取反而是例外路径，省去）。

### 字段设计：`source_author`（仅 `kind:mental` 有意义）

为避免与既有 `author` 字段（语义 = "本技能文件的创建者：builtin/user/me"，`parse.go:57` 自动补全）混淆，新增**独立** frontmatter 字段：

- `author: user` — 技能文件创建者（既有语义，保持不变）
- `source_author: 刘慈欣` — **被蒸馏的那位作者的心智来源**（新增，仅 `kind:mental` 使用）

| 文件 | 改动 |
|---|---|
| `internal/skill/types.go` | `Skill` 增加 `SourceAuthor string`（`json:"source_author" yaml:"source_author"`） |
| `internal/skill/parse.go` | `parseFrontmatter` 已支持任意 YAML 字段透传，无需改解析；仅 `ParseBytes` 的 `defaultAuthor` 逻辑保持只补全 `author`、不动 `source_author`（留空即用户未填） |
| 前端「保存心智 Skill」对话框 | 新增**必填「来源作者」输入框**（V1 即提供），绑定 `source_author`；留空则该心智 Skill 不参与跨书聚合，但单书提取（V1）仍可用 |

### 跨书聚合的归并键（V3）

按 `skill.SourceAuthor` 分组，`kind:mental` 且同 `source_author` 的多个心智 Skill → 合并/加权为"作者心智总 Skill"。作者名的大小写/空白/别名归一在技能层做轻量 canonicalize（trim + 大小写折叠 + 可选别名映射），**不再涉及小说模型**。

**鲁棒性兜底**：除"按来源作者名隐式分组"外，另保留显式「合并这些心智 Skill 为一个作者档案」操作——用户手动勾选多个心智 Skill 参与聚合，彻底避开作者名拼写/别名导致的漏聚合。两条路径并存。

> 若用户保存心智 Skill 时不填来源作者，该书仍可做**单书心智提取（V1）**，只是无法参与**跨书聚合（V3）**——两者解耦，互不阻塞。此解耦在前述 §一·续 旧方案（改 Novel 模型）下同样成立，但本方案省去了整张模型/管线改动表。

## 三、文件级改动清单（V1）

### 后端
| 文件 | 改动 |
|---|---|
| `internal/pattern/types.go` | 新增 `Flavor` 类型（`struct`/`mental`）；新增 `MentalSkillOutput`（5 层 + 证据锚点 `[]ChapterAnchor` + `Confidence`）；`ExtractPatternResult` 加 `Flavor` 字段 |
| `internal/pattern/prompts.go` | 新增 `mentalFinalPrompt(input, chunks)`：5 层 schema + **小说适配版三重验证指令**（① 跨独立章节/情节弧复现：同一技法须 ≥2 条不相关情节线出现才算心智模型，否则降为启发式；② 有生成力：该模型须能指导写出新章节而非仅描述；③ 排他性：区别于"show don't tell"等通用写作建议）+ **质量自检清单**（nuwa §六：每模型≥2 章证据、数量 3–7、表达有辨识度、诚实边界含来源与时间）+ 诚实边界强制（"推断自虚构文本，非作者真人三观，可能混有角色声音"）+ 证据 `chapter_id` 锚点要求；现有 `structFinalPrompt` 不动 |
| `internal/pattern/extract.go` | `ExtractPatternInput` 加 `Flavor`；`Run`/`finalSkill` 按 flavor 分流；mental 成熟度门禁：对应章节 < 20 → 标 `low_confidence` 并缩减层数 |
| `internal/skill/types.go` | `Skill` 加 `Kind` 字段（`struct`/`mental`，便于 UI 分组与按需过滤）；加 `SourceAuthor` 字段（`source_author`，仅 `kind:mental` 有意义，作为 V3 跨书聚合归并键，由"保存心智 Skill"对话框手填） |
| `internal/skill/store.go` 或 extract 完成回调 | 生成后调用 `Enable(novelID, safeName)` 写入本书 `.enabled.json`（心智模型常驻、启发式 catalog） |
| `internal/skill/ParseBytes` | 解析 `kind` frontmatter 字段 |

### 前端
| 文件 | 改动 |
|---|---|
| `components/extract/ExtractWorkspaceView.tsx` | `Tab` 类型加 `"mental"`；tabs 数组加「心智提取」项（图标 `Brain`） |
| `components/pattern/MentalExtractView.tsx`（新增） | 复制 `PatternExtractView` 并参数化 `flavor="mental"`；最低章数提示改为 ~20；文案区分 |
| `components/pattern/PatternSessionView.tsx` | 进度文案按 flavor 切换（"正在提取作者心智"） |
| `i18n/locales/zh-CN.json` + `en.json` | 新增 `extract.mentalExtract`、`extract.mentalChapterNote`（"心智提取建议 ≥20 章"）等 |
| `lib/wailsjs/go/models.ts` + `App.d.ts` + `App.js` | `ExtractPatternInput` 已有字段；如需新增 `MentalSkillOutput` 类型则同步（Go 结构体字段 wails 自动序列化，前端传 `flavor` 即可） |

### 验证
- 后端 `go test`：加 `mental_final_prompt_test`（schema 解析合规）、`extract_flavor_test`（struct/mental 分流正确）。
- 前端 `tsc -b && vite build`、`i18n:check`、`theme:check`、`prettier --check`。
- 手动：选一本 ≥20 章的书跑「心智提取」→ 确认生成 `skills/{name}.md` 且自动出现在 Skill 列表并勾选。

## 四、分期

> **与早期 V1/V2/V3 方案的对照说明**：最早评估时把"注入写作上下文（用 X 的创作心智写新章）"列为 V3。后续确认 `internal/skill` 的 `.enabled.json` + `BuildSystemMessages` 注入机制已原生支持该能力——因此该能力已在 **V1 的"生成后自动写 `.enabled.json`"** 中交付，不再作为独立阶段。原 V2 中的"多镜头精提 / 同一作者多本跨书聚合 / 置信度标注"部分被吸收进 V1（置信度→成熟度门禁），部分遗漏，现将"跨书聚合 + 多镜头 + 写作闭环增强"明确列为 **V3**，使 roadmap 与前期讨论一致。

- **V1（本计划）**：flavor 分流 + `MentalSkillOutput` + mental final prompt（含**小说适配三重验证** + **质量自检清单**）+ 前端 tab + 自动写 `.enabled.json`（即"注入写作上下文"的原始 V3 能力在此交付）。合成全 LLM（复用现有递归压缩的 token 预算）。置信度标注已含于成熟度门禁（`low_confidence`）。
- **V2（优化）**：ONNX 向量检索做证据收集前置（减少 LLM 重读全本）；跨弧复现验证改为确定性计数门禁（统计独立弧数 ≥2）；token 上限护栏（心智模型层 ~1.5–2.5k）；**产出后保真度测试**：用本书作者已明确处理过的冲突测方向一致性，用作者从未涉及的题材测"适度不确定而非斩钉截铁"（nuwa 产出后测试的fiction 适配版）。
- **V3（增强，应对多书多作者场景）**：
  - **前置（来源作者，见 §二·续）**：V3 启动前无需任何模型/管线改动——V1 的"保存心智 Skill"对话框已提供必填「来源作者」输入，写入 `source_author` frontmatter 字段。保存时填好来源作者，V3 即可按该字段聚合。
  - **跨书聚合**：按 `skill.SourceAuthor` 分组，`kind:mental` 且同来源作者的多个心智 Skill 合并/加权为"作者心智总 Skill"——**跨书复现才算稳定习惯（高权重），仅单书出现的习惯降级标记**。作者名在技能层做轻量归一（trim + 大小写折叠 + 可选别名映射）；并提供"手动勾选多个心智 Skill 合并"作为备选（避开作者名拼写问题）。
  - **多镜头精提**：把单轮 final 拆为表达/心智/决策三个独立镜头的 final round，各自更深、互不稀释（对应早期 V2 的"多镜头"）。
  - **写作闭环增强**：在 V1 "启用 skill 即常驻" 之外，提供"以 X 的创作心智写新章"的专用写作模式/模板（不止于被动注入，而是主动调用心智 Skill 作为写章 persona）。

## 五、对当前流程的影响

- **不影响**现有套路/风格提取：纯新增 flavor 分支，旧路径零改动。
- 心智 Skill 默认 `mode: auto`，**不自动常驻**；仅生成后写入 `.enabled.json` 才常驻——用户随时可在 Skill 列表取消勾选恢复 `auto`。
- 写作流 `BuildSystemMessages` 已原生支持 always/auto/enabled，`kind:mental` 的 Skill 零改动即可被消费。

## 六、增量 vs 臃肿判定

**增量设计**。证据：
1. 复用 80% 编排（仅 flavor 分支 + 一个输出 schema）。
2. 复用既有技能注入机制与 `.enabled.json` 开关，不新建存储/写入通道。
3. 复用前端 tab 范式（风格/套路/心智 三并列），不重构提取页。
4. 唯一新增常驻成本是"心智模型层"约 2k token，且 V2 有护栏约束。
不另起炉灶、不重复造轮。
