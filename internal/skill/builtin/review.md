---
name: review
description: 调度审核编辑 agent，根据用户指令执行审阅任务
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
---
用户要求你调用 review agent。请根据用户指令执行审阅任务。
