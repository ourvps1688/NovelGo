# Phase 4 QA 报告：signability-workflow 技能内核

> 评审角色：skill-qa（QA 专员，独立评审，非 developer 自测）
> 评审对象：`03-源码/signability-workflow.md`（169 行 / 12943 字节正文）
> 评审依据：需求 §5 AC1–AC9、设计蓝图 §3/§5/§7、NovelGo `internal/skill` 解析器源码
> 结论：**PASS**（全部 AC 满足，0 缺陷，0 阻塞项）

---

## 1. 评审方法与证据链

| 检查项 | 方法 | 证据 |
|--------|------|------|
| frontmatter 合法性 | 用 NovelGo 真实解析器 `internal/skill.ParseFile` 实跑 | `PARSE_OK`，字段全解析正确（见 §2） |
| 字段形态合规 | 比对 `internal/skill/types.go` 的 `Skill`/`SkillMeta` 结构体 | `name/description/category/mode/useWhen[].intent+cues/author/version` 全部命中 |
| 与现有内置技能格式一致 | 比对 `story-blueprint.md` frontmatter | 7 字段、缩进、useWhen 形态完全一致 |
| AC8 不写库 | 正则扫描 9 个落库指令关键字 | 0 命中 |
| cues 数量 | `grep -cE '^\s*- "'` | 10 |
| 被编排技能不复制 | 全量通读 169 行正文 + 关键字引用计数 | 4 个技能名被引用（story-blueprint×10 / worldbuilding-design×10 / signability-audit×13 / next×7），正文为编排指令、未复制方法论 |
| 章节结构 | 比对设计蓝图 §3.1 骨架 | 13 节齐全，顺序一致 |
| AC1–AC9 落地 | 逐条比对设计 §5 映射表 + 正文 | 9/9 均可指认落点（见 §3） |

---

## 2. frontmatter 实跑解析结果（gold-standard 证据）

```
PARSE_OK
name="signability-workflow"
category="系统"
mode="manual"
author="builtin"
version=1
useWhen_len=1
intent="规划签约潜力 / 通盘编排签约工作流（规划→设定→写作→体检→回修）"
cues_len=10   → 签约潜力 / 签约规划 / 签约工作流 / 一书通盘规划 / 蓝图到体检 /
               过签规划 / 帮我规划签约 / 这本书能不能签 / 编辑愿意签吗 / 签约潜力工作流
content_len=12943
```

- `mode=manual` 经 `normalizeMode` 合法归一化为 `ModeManual`，符合 §12.5 门禁锁定与"长流程主动进入"的设计意图。
- `category=系统` 为自由字符串，按首次出现分组进"内置技能（只读）"目录的「系统」子组，与 `next.md` 等调度/元技能同桶，零新增分组。
- 文件名 `signability-workflow.md` == `name`，满足 Phase 5 落点一致性（§1.3 / R5）。

---

## 3. AC1–AC9 逐条核验

| AC | 要求 | 正文落点 | 判定 |
|----|------|----------|------|
| **AC1** 入口与阶段清单 | 触发后输出 6 阶段，每阶段含目标/完成判据/技能名/题材卡提示 | 「阶段总览」表（正文 L48–57）列阶段0+①–⑤，四要素齐全 | ✅ |
| **AC2** 状态感知不重复触发 | 对已存在 story_arc 的书再触发，经读取确认规划完成、从写作/体检开始 | 阶段0 真实读 `get_story_arcs`（L66）；「阶段间分支逻辑」L135–140 与「防重复机制」L142 | ✅ |
| **AC3** 规划阶段产物 | 完成后本书有 ≥1 主线 arc + ≥1 node 或卷纲更新 | 阶段① 引导 `story-blueprint`，完成判据 L80 | ✅ |
| **AC4** 设定阶段产物（可选） | 进入设定后设定库 ≥1 条 | 阶段② 引导 `worldbuilding-design`，完成判据 L91（注明可选） | ✅ |
| **AC5** 体检阶段产物 | 完成后有 signability-audit 写入的"本书签约体检"偏好 | 阶段④ 引导 `signability-audit`，完成判据 L110 | ✅ |
| **AC6** 闭环修复 | 含 P0/P1 时输出回修指引，逐条映射"回哪阶段+调哪个技能" | 阶段⑤ L116–127 含映射表 + 修复→复检回路 | ✅ |
| **AC7** 题材卡约束遵守 | 全程引用 genreTemplates 字段；无卡提示"按通用标准"且不报错 | 对齐声明 L43（C5 + 无卡降级不中断）、各阶段题材卡提示 | ✅ |
| **AC8** 不越权/不重写 | 正文不含落库写指令复制；3 文件未被修改 | 工具协同清单 L159–168 声明不含写指令；9 关键字扫描 0 命中；3 个被编排文件本次未触碰（Phase 5 哈希校验） | ✅ |
| **AC9** 空数据兜底 | 无可达数据时提示"无法读取本书数据"并停止，不臆造 | 阶段0 首步 `get_chapter_list`，空/读不到即 STOP（L63–64） | ✅ |

---

## 4. 边界与约束符合性（需求 §4 B1–B8）

| 边界 | 判定 | 说明 |
|------|------|------|
| B1 不重写被编排技能 | ✅ | 仅引用技能名，未复制方法论（正文 L27/L29 明述） |
| B2 不直接落库 | ✅ | AC8 扫描 0 命中；工具协同清单声明不含写指令 |
| B3 不替代评分逻辑 | ✅ | 阶段④ 仅引导触发 signability-audit，不重做七维打分 |
| B4 不做 UI | ✅ | 内核为纯 `.md`，无 UI 代码 |
| B5 不修改现有 3 技能 | ✅ | 3 文件未被本流程修改（Phase 5 落地时哈希校验） |
| B6 不臆造数据 | ✅ | 对齐声明 L45「不臆造」+ 阶段0 真实读取铁律 + AC9 |
| B7 不做越权判定 | ✅ | 对齐声明 L46、阶段⑤ L129「不替编辑/平台做一票否决」 |
| B8 不覆盖无关能力 | ✅ | 不介入清单 L35–39 明确单点/非签约/非 NovelGo 不抢夺 |

---

## 5. 设计风险披露复核（设计 §9 R1–R5）

- **R1（preferences 无回读工具）**：已作为设计约束在阶段⑤ L128 显式落地（"不设计读旧体检报告步骤，靠重新跑 audit 闭环"），未静默带过。✔ 非阻塞。
- **R2（跨体检 diff）**：不在内核范围，不依赖。✔
- **R3（category=系统）**：已锁定，与现有 taxonomy 一致。✔
- **R4（mode=manual 可发现性）**：按门禁结论实现，不阻塞。✔
- **R5（落点文件名=name）**：已满足，Phase 5 仅复制不改名。✔

---

## 6. 缺陷清单（Defects）

**无。** 未发现任何 blocking / major / minor 缺陷。

---

## 7. 唤起测试结论（developer 自测回溯）

developer 自测 5 项（见 `03-源码/源码清单.md`）PASS；QA 独立复验其中关键两项：
- 「AC8 写指令扫描 0 命中」→ 独立复验一致。
- 「cues=10」→ 独立复验一致。
- 追加「真实解析器加载」一项作为 gold-standard 补充，PASS。

---

## 8. Phase 4 出口结论

**PASS。** 技能满足需求 AC1–AC9、设计全部落点、frontmatter 经 NovelGo 真实解析器验证可加载、边界 B1–B8 全符合、设计风险已透明披露且均非阻塞。

**建议**：直接进入 Phase 5 发布——将 `03-源码/signability-workflow.md` 复制至 `internal/skill/builtin/signability-workflow.md`，并对 `story-blueprint.md` / `worldbuilding-design.md` / `signability-audit.md` 做哈希校验确认三文件未变。
