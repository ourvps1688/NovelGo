---
name: memory
description: 记忆检索与写后归档：调度 memory 子 Agent 检索既有设定，并为状态同步提供归档核对维度
category: 系统
mode: manual
author: builtin
version: 1
useWhen:
  - intent: "检索既有设定 / 回忆前文细节 / 写后归档核对"
    cues:
      - "检索记忆"
      - "之前提到"
      - "前文写过"
      - "查一下设定"
      - "回忆一下"
mounts: [state_sync, slash_only]
internalOnly: true
budget: 1200
---
## 写后归档核对（state_sync 通道）
状态同步完成后，按「角色 / 主线伏笔 / 设定 / 读者已知」四栏复核本次增量是否齐全：
只核对已落库的台账条目，不推测、不补写正文、不重复登记已存在的条目。
四栏各自回答「本次是否新增或更新」；无变化写「无」，避免把旧信息当成新发现。
归档口径只作核对维度，不替代状态同步指令里的工具白名单与「只改台账」纪律；
本技能在 state_sync 通道里是节选注入，不得据此派发任何子 Agent。

## /memory 调度契约（slash_only 通道）
用户显式输入 /memory 时，这是一次子 Agent 调度，不是普通问答：
必须调用 run_subagent 工具，AgentType 固定为 "memory"，Instruction 取用户消息里的检索目标。
主 Agent 不得自行编造、也不得用零星上下文代替检索结果；memory 子 Agent 负责检索本书既有设定与前文。
检索范围：角色与关系、地点与世界观、伏笔与时间线、章节计划卡、读者认知态、历史对话要点。

## 输出
按「命中条目 / 来源（章节或台账）/ 与当前问题的关系」三栏整理，末尾标注未命中的部分。
检索不到就明确写「未找到」，禁止用常识补全；结论只用于本次回答，不自动改写任何台账。
