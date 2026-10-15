---
name: review
description: 调度审核编辑 agent，根据用户指令执行审阅任务
category: 系统
mode: manual
author: builtin
version: 1
useWhen:
  - intent: "章节质检与评分"
    cues:
      - "评分"
      - "质量检查"
      - "检阅"
      - "打分"
      - "评审"
      - "质检"
      - "评章"
      - "审一遍"
      - "章节评分"
---
用户要求你调用 review agent。请根据用户指令执行审阅任务。
