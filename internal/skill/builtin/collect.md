---
name: collect
description: 全面收集当前章节相关的角色、情节、伏笔、设定信息，整理成创作参考卡片。当用户说"汇总一下当前状态""整理伏笔/角色关系/时间线""动笔前让我看全局""把设定理一理"时手动调用。
category: 系统
mode: manual
author: builtin
version: 1

useWhen:
  - intent: "汇总当前状态 / 整理全局设定与伏笔"
    cues:
      - "整理全局"
      - "理一理"
      - "梳理"
      - "全局状态"
      - "整理伏笔"
      - "动笔前看全局"
      - "汇总"
---

用户要求你全面收集信息，整理成创作参考。请根据用户指令执行。

## 何时使用（手动）
用户说"汇总一下当前状态""整理伏笔/角色关系/时间线""动笔前让我看全局""把设定/世界观理一理"时调用。本技能是**全局快照**工具——不创作、只聚合，产出一份供用户或后续创作技能消费的参考卡片。

## 收集清单（按用户指令裁剪）
- **角色与关系**：`get_characters` / `get_character_relations`
- **主线与伏笔**：`get_story_arcs` / `get_timeline`（或 `search_story_memory` 检索散落设定）
- **设定 / 世界观**：`get_locations` 及设定库检索（与 `worldbuilding-design` 衔接）
- **读者认知态**：`get_reader_perspective`（当前读者已知 / 误知）
- **章节进度**：`get_chapter_list`

## 输出
按「角色 / 主线伏笔 / 设定 / 读者已知」四栏整理成一份创作参考卡片，供 `story-blueprint` / `scene-beats` / `opening-chapters` 等创作技能消费。

**对齐声明**：聚合结果须以本书题材卡与设定库为基准，标记任何与「题材禁止漂移」或「设定一致性」冲突的点，便于后续创作前修正。
