---
name: review
description: 编辑视角章后评审：调度 review 子 Agent 做主观审读，并为章 CI 提供只读评审清单
category: 系统
mode: manual
author: builtin
version: 1
useWhen:
  - intent: "编辑视角深读 / 主观审读意见"
    cues:
      - "检阅"
      - "评审"
      - "评章"
      - "审一遍"
      - "审读"
      - "编辑意见"
mounts: [chapter_ci, slash_only]
internalOnly: false
budget: 4000
---
## 章后评审清单（chapter_ci 写后自检通道）
本清单在章 CI / L1 增量检查里只作为「只报告不改写」的只读条目使用：
不调用任何写工具、不评分、不给 verdict、不派发子 Agent，命中问题只写进发现清单。
1. 开篇三行是否给出具体人物处境与当下张力，而非背景说明或天气描写。
2. 场景目标是否在本章内兑现；未兑现处标明是刻意留钩子还是漏写。
3. 对白是否各自带潜台词与立场差，是否存在全员同一语气、借对白直接讲解设定。
4. 代入感锚点：是否有可感知的动作或感官细节，避免连续内心独白、用总结句推进剧情。
5. 章末钩子是否来自本章已建立的因果，而非凭空新增人物或悬念。
6. 编辑视角主观项：这一章最想划掉的一段、最想保留的一句，各给一处并说明原因。

## /review 调度契约（slash_only 通道）
用户显式输入 /review 时调用 run_subagent，AgentType 固定为 "review"，Instruction 取用户消息。
review 子 Agent 输出「主观审读意见」，与章 CI 的客观质检清单分开，互不覆盖；
不得把主观意见写成客观质检结论，也不得据此直接改写正文。
