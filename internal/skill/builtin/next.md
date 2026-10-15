\---  
name: next  
description: 每章创作的"总调度"——编排写前对齐→写中代入→写后质检→周期体检四阶段，串联 story-blueprint / character-design / worldbuilding-design / opening-chapters / reader-immersion / scene-beats / dialogue-subtext / pacing-control / plot-hook / deai-flavor / review / signability-audit 等专项技能，确保每章走完签约级全链路。当用户说"写下一章""继续""next""开始创作下一章""动笔"时手动触发。  
category: 系统  
mode: manual  
author: builtin  
version: 2  
\---

# 下一章创作总调度（Chapter Orchestrator）

## 何时使用（手动）

用户说"写下一章""继续""next""开始创作下一章""动笔"时触发。本技能是每章创作的**总调度**——不替代各专项技能，而是按四阶段编排它们，确保每章都走完签约级全链路。

**定位声明**：你是总导演，各专项 skill 是部门。依次调用，不跳步、不漏步。

## 阶段 0：定位本章

- 用 `get_chapter_list` 确认进度与上一章结尾，**继承其章尾钩子**作为本章起始张力。
- 读取本章计划（`update_chapter_plan`）：本章推进哪条主线、埋/收哪个伏笔、给哪个爽点。
- 若全书尚无蓝图：先触发 `story-blueprint` 补宏观结构，避免散写。

## 阶段 1：写前对齐（Before Writing）

1. **蓝图对齐** — 调 `story-blueprint`：确认本章在卷纲/蓝图中的功能位置（推进/转折/收束），不偏离宏观结构。
2. **人设加载** — 调 `character-design`：本章出场角色的教义档案（升级驱动/经营/情感/解谜/心理）+ 卖点化节点；用 `get_characters` 取已建档案，确保言行一致、不漂移。
3. **设定校验** — 调 `worldbuilding-design`：本章涉及的设定/力量体系/金手指，用 `search_story_memory` / `get_locations` / 设定库检索核对一致性，防吃书。
4. **读者认知态** — 用 `get_reader_perspective` 取当前读者已知/误知态，据此决定本 reveal 还是 withhold（与 `reader-immersion` 的"好奇心缺口"配合）。
5. **前三章专项** — 若为新书第 1~3 章，调 `opening-chapters` 执行冷开场/前千字法则/首个甜头。
6. **产出"本章功能卡"**：一句话写清——推进什么 + 埋什么钩子 + 给什么甜头；可 `upsert_preference` 固化，供写后核对。

## 阶段 2：写中代入（While Writing）

依次应用以下技法（均为 auto，写时直接调用）：

- `reader-immersion`：POV 黏着、感官锚定、心理距离走近端、情绪镜像——默认基调。
- `scene-beats`：每场戏有功能+情感推力；章尾用题材卡「章尾钩子」收束。
- `dialogue-subtext`：对白承载潜台词，密度/风格服本书题材调性。
- `pacing-control`：节奏落在题材卡「情绪曲线/爽点密度」节点（爽文每章 ≥1~2 爽点）。
- `plot-hook`：伏笔/钩子对齐题材卡章尾钩子，长线钩子纳回 `story-blueprint`。
- `deai-flavor`：全程规避 AI 指纹（破折号/命运套话/模板化身体/上帝视角）。
- 卡住时用 `brainstorm-composer` 发散，但发散须落题材约束内（见其对齐声明）。

## 阶段 3：写后质检（After Writing，强制）

不通过不准进下一章：

1. 执行系统提示词 `identity` 第 5 步「写后质检」清单（结构 > 场景 > 句子；字数门槛：低于目标 70% 不达标，缺口一次性补足+15% 缓冲）。
2. 跑 `deai-flavor` 隐蔽 AI 指纹复核（tell-not-show、弱化副词成串、比喻密度等）。
3. `run_subagent` 调 `review` 做内部一致性复核（角色/时间线/设定/读者认知）。
4. 三方任一不通过 → 退回重写对应项，再走一遍本阶段。

**状态同步说明**：系统已在每章正文落盘后**自动派发 state-sync 子 Agent** 回填台账（角色/地点/弧线/时间线/读者认知），无需你手动逐条维护；写后可用 get_* 快速核对关键条目是否到位。若怀疑某章台账漂移，可手动 `run_subagent`（agent_type=statesync）触发一次补同步。

## 阶段 4：周期体检（推荐）

- 每 5~10 章（或一个情节单元结束），手动触发 `signability-audit` 做签约级体检，暴露长线致命伤（开篇过签力/题材契合/人设卖点/设定自洽/长线牵引/合规），及时纠偏。
- 用 `create_reader_perspective_entry` 持续维护读者认知态。

## 对齐声明

- 全程服从本书题材卡「禁止漂移」与 `identity` 规则（大纲先行、写后质检、去 AI 味）。
- **题材卡优先级 > 任何专项 skill 的通用教义**；冲突时以题材卡为准。

## 工具协同

`get_chapter_list` · `update_chapter_plan` · `get_characters` · `get_story_arcs` · `get_timeline` · `search_story_memory` · `get_locations` · `get_reader_perspective` · `create_reader_perspective_entry` · `upsert_preference` · `run_subagent` · `read`/`edit`
