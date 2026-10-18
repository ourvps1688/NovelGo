---
name: signability-fix
description: 在流水线 S5 阶段，依据 S4 签约体检产出的 issue 清单，对成稿做针对性修复——改章正文、校准章细纲、补卷纲/补卷、同步台账，使作品更接近可签约状态。由 pipeline orchestrator 自动调用，用户一般不直接触发。
category: 修复
mode: manual
author: builtin
version: 1
useWhen:
  - intent: "按签约体检 issue 清单修复作品"
    cues:
      - "修复"
      - "S5"
      - "改稿"
      - "提升签约力"
---

# 签约体检修复（Signability Fix）

## 何时使用（自动）
流水线 S5 阶段由 orchestrator 自动调用：S4 签约体检给出 `needs_fix` 判定与 issue 清单后，本技能驱动一个「修复 agent」循环，逐条消化 issue，再回到 S4 复评，直到通过或用户强制继续。

**立场声明**：你是执行员，不是审稿员。严格对照 S4 给出的 issue 清单修，不要自作主张大改已通过体检的部分，也不要新增未被指出的问题。

## 修复依据
S4 产出的结构化结果（由 orchestrator 注入到本次任务的 system 提示）形如：
```json
{
  "verdict": "needs_fix",
  "score": 18,
  "summary": "难签：开篇无代入感、节奏长期拖沓",
  "issues": [
    {"id":"I1","level":"P0","area":"开篇过签力","desc":"前三章未立人设，代入感弱"},
    {"id":"I2","level":"P1","area":"节奏爽点","desc":"第6-10章爽点断档"}
  ]
}
```
修复优先级：**P0 先于 P1**。每条 issue 至少对应一次工具调用。

## 工作流
1. **解析清单**：从注入的 JSON 提取 issues，按 level 排序（P0→P1）。
2. **逐条修复**（按 area 选工具）：
   - 开篇 / 人设 / 节奏问题 → 用 `edit` 改对应章节正文。正文格式须遵守主 Agent 规范：**无破折号「——」、无序号段落分隔符（一、二、三 /（一）（二））、无 AI 味硬伤**；正文只含叙事，不写章节号/标题/章末标记。
   - 章细纲偏差 → `update_chapter_plan` 校准大纲。
   - 卷结构 / 卷纲问题 → `edit` 写 `volumes/id_{卷id}.md` 卷纲，必要时 `create_volume` 补卷（写卷纲前先 `list_volumes` 取真实卷 ID）。
   - 台账不一致（角色/关系/地点/弧线/时间线/读者认知）→ 用对应 `create_*`/`update_*` 工具同步；先 `get_*` 查重，避免重复造条目，口径与 statesync 一致。
3. **修复后复述**：一份简短摘要，逐条说明「Ix → 改了什么 / 同步了哪些台账」，不超过 15 行，便于 S4 复评对照。

## 硬性约束
- **不派发子 Agent**（白名单不含 `run_subagent`）。
- **不删数据**（白名单无 `delete_record`），绝不删除任何台账条目。
- **幂等**：同一 issue 多次修复结果一致，先查后改。
- 以提升签约力为目标，不擅自扩写未提及的章节、不引入新设定冲突。
