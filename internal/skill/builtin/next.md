---
name: next

description: 每章创作的"总调度"——编排写前对齐→写中技法，串联 story-blueprint / character-design / worldbuilding-design / opening-chapters / reader-immersion / scene-beats / dialogue-subtext / pacing-control / plot-hook / deai-flavor 等专项写作技能；写后质检与周期体检由机器流水线（章节 CI + 里程碑体检）承担。当用户说"写下一章""继续""next""开始创作下一章""动笔"时手动触发。

category: 系统

mode: manual

author: builtin

version: 3
useWhen:
  - intent: "写下一章 / 继续创作 / 开始动笔"
    cues:
      - "写下一章"
      - "继续"
      - "next"
      - "动笔写"
      - "开始创作"
      - "开始写"
      - "写新章"
      - "下一章"
      - "继续写"
      - "写章"
      - "创作下一章"
      - "开工"
      - "落笔"
---

# 下一章创作总调度（Chapter Orchestrator）

## 何时使用（手动）

用户说"写下一章""继续""next""开始创作下一章""动笔"时触发。本技能是每章创作的**总调度**——不替代各专项技能，而是编排写前对齐与写中技法两阶段。

**定位声明**：你是总导演，各专项 skill 是部门，按本章实际需要调用。写后质检与周期体检由机器流水线承担：每章正文落盘后自动跑章节 CI（L0 确定性检查 + L1 增量检查，发现落章节质量档案），黄金三章/卷收口/完本等节点由流水线触发签约级体检——这两类节奏不在本技能内自调度。

## 阶段 0：定位本章

- 用 `get_chapter_list` 确认进度与上一章结尾，**继承其章尾钩子**作为本章起始张力。
- 读取本章计划（`update_chapter_plan`）：本章推进哪条主线、埋/收哪个伏笔、给哪个爽点。
- 若全书尚无蓝图：先触发 `story-blueprint` 补宏观结构，避免散写。

## 阶段 1：写前对齐（Before Writing）

1. **蓝图对齐** — 调 `story-blueprint`：确认本章在卷纲/蓝图中的功能位置（推进/转折/收束），不偏离宏观结构。
2. **人设加载** — 调 `character-design`：本章出场角色的教义档案（升级驱动/经营/情感/解谜/心理）+ 卖点化节点；用 `get_characters` 取已建档案，确保言行一致、不漂移。
3. **设定校验** — 调 `worldbuilding-design`：本章涉及的设定/力量体系/金手指，写前用 `search_settings`（语义优先，按本章将涉及的实体/矛盾/地点检索最相关设定，返回带 `setting_id` 的条目）核对一致性、防吃书；也可配合 `get_locations` / `search_story_memory`。需要修正时拿返回的 `setting_id` 调 `upsert_setting` 合并更新，勿新建重复条目。
4. **读者认知态** — 用 `get_reader_perspective` 取当前读者已知/误知态，据此决定本 reveal 还是 withhold（与 `reader-immersion` 的"好奇心缺口"配合）；关键 reveal/withhold 节点可即时用 `create_reader_perspective_entry` 落记录，章级增量亦由后台 statesync 自动同步。
5. **前三章专项** — 若为新书第 1~3 章，调 `opening-chapters` 执行冷开场/前千字法则/首个甜头。
6. **立章纲任务卡** — 调 `upsert_chapter_task_card` 立结构化四件套（机器校验缺一拒收）：**钩子任务**（本章埋/收哪个坑 + 章尾断章钩设计）、**爽点任务**（爽点类型与情绪落点）、**人设时刻**（谁的高光/反差）、**推进任务**（主线/卷纲功能位）。这是"本章为什么写"的机器可验契约；写后由章节 CI 对卡核验兑现（发现落章节质量档案），方向调整先改卡再改文。

## 阶段 2：写中技法（While Writing）

依次应用以下技法（均为 auto，写时直接调用）：

- `reader-immersion`：POV 黏着、感官锚定、心理距离走近端、情绪镜像——默认基调。
- `scene-beats`：每场戏有功能+情感推力；章尾用题材卡「章尾钩子」收束。
- `dialogue-subtext`：对白承载潜台词，密度/风格服本书题材调性。
- `pacing-control`：节奏落在题材卡「情绪曲线/爽点密度」节点（爽文每章 ≥1~2 爽点）。
- `plot-hook`：伏笔/钩子对齐题材卡章尾钩子，长线钩子纳回 `story-blueprint`。
- `deai-flavor`：全程规避 AI 指纹（破折号/命运套话/模板化身体/上帝视角）。
- 卡住时用 `brainstorm-composer` 发散，但发散须落题材约束内（见其对齐声明）。

## 协作约定

- **状态同步**：正文落盘后系统自动派发 statesync 子 Agent 回填台账（角色/地点/弧线/时间线/读者认知），无需手动逐条维护；若怀疑某章台账漂移，可手动 `run_subagent`（agent_type=statesync）触发一次补同步。
- **输出约束**：不要在回复中重复输出「现在台账的状态」这类完整台账盘点；当用户未主动询问时，只给出与当前写作直接相关的结论和下一步动作。

## 对齐声明

- 全程服从本书题材卡「禁止漂移」与 `identity` 规则（大纲先行、去 AI 味）。
- **题材卡优先级 > 任何专项 skill 的通用教义**；冲突时以题材卡为准。

## 工具协同

`get_chapter_list` · `update_chapter_plan` · `upsert_chapter_task_card`/`get_chapter_task_card` · `get_characters` · `get_story_arcs` · `get_timeline` · `search_story_memory` · `get_locations` · `get_reader_perspective` · `create_reader_perspective_entry` · `upsert_preference` · `search_settings` · `run_subagent` · `read`/`edit`
