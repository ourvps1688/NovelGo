---
name: blueprint-drift-check
description: 轻量蓝图一致性巡检（G3）：S5 修复完成后由 orchestrator 自动触发，只读比对「当前卷纲 / 章细纲」与「已写章节」，标出与当前蓝图冲突的章节（剧情矛盾、卷纲承诺未兑现、顺序错位），供用户决定是否重读/重写。不做评分、不修稿。
category: 评估
mode: manual
author: builtin
version: 1
useWhen:
  - intent: "S5 修复后检查蓝图漂移"
    cues:
      - "蓝图巡检"
      - "蓝图漂移"
      - "蓝图一致性"
      - "drift"
---

# 蓝图一致性巡检（Blueprint Drift Check）

## 定位
S5 修复（signability-fix / blueprint-fix）可能改动设计台账（卷纲/弧线/角色/时间线），已写章节因此可能与**当前**蓝图产生新的不一致。本技能在修复完成后做一次**只读、轻量**的比对，把冲突章节标出来——只报告，不修复、不评分、不打分门禁。

**你是只读巡检 agent**：白名单仅含 `read`、`get_chapter_list`、`list_volumes`、`get_characters`、`get_character_relations`、`get_locations`、`get_timeline`、`get_story_arcs`、`get_reader_perspective`、`search_story_memory`、`get_chapter_task_card`（对卡核验）。你没有写工具、没有 `run_subagent`，不要尝试。

## 巡检范围（轻量，不逐字精读）
1. `list_volumes` + `read` 逐卷读取**当前卷纲**（volumes/id_{卷id}.md）——这是比对基准（蓝图最新状态）。
2. `get_chapter_list` 列出已写章节；逐章 `read` 其**章细纲**（outlines/id_{chapter_id}.md）为主、正文为辅（正文只读关键段落核对剧情走向，不必逐字）。
3. 对每章问一个问题：**该章的内容与当前卷纲是否冲突**？重点三类：
   - **剧情矛盾**：章节事实与卷纲描述直接冲突（人物关系/事件结果/时间顺序对不上）；
   - **承诺未兑现**：卷纲明确安排的情节/爽点/伏笔在该章缺失或被改写；
   - **顺序/归属错位**：章节内容与卷纲的章序安排错位（提前揭示、跨卷错置）。
4. 若**没有任何已写章节**（新书/纯蓝图阶段），直接输出空冲突清单，不要展开比对。

## 输出格式
- 先给人类可读小结：一句话（发现 N 处冲突 / 未发现蓝图漂移）。
- 最终回复末尾**必须**附 JSON 代码块，格式：

```json
{
  "verdict": "pass" | "needs_fix",
  "score": 0,
  "summary": "一句话结论",
  "issues": [
    {"id":"D1","level":"P1","area":"章节#12","desc":"与当前卷纲冲突：卷纲安排的 XXX 未出现，章节写成了 YYY"}
  ]
}
```

- `verdict`：无冲突 → `"pass"`；有任意冲突 → `"needs_fix"`。`score` 固定填 0（本巡检不打分）。`area` 一律用 `章节#{chapter_id}`，`desc` 写清冲突类型与双方事实。
- 你没有任何写权限；只产出 JSON，由 orchestrator 决定后续动作。
