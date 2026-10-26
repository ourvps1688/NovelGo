---
name: blueprint-fix
description: 在流水线 S2 蓝图体检给出 needs_fix 后，依据 issue 清单对蓝图（卷纲结构）做针对性修复——改 volumes/id_{卷id}.md 卷纲、补卷。限权只动卷纲与卷结构，不碰章节正文、不碰其他台账写。由 pipeline orchestrator 自动调用。
category: 修复
mode: manual
author: builtin
version: 1
useWhen:
  - intent: "按蓝图体检 issue 清单修复卷纲"
    cues:
      - "蓝图修复"
      - "卷纲修复"
      - "S5 蓝图"
      - "改卷纲"
---

# 蓝图修复（Blueprint Fix）

## 何时使用（自动）
流水线 S2 蓝图体检（`blueprint-audit`）给出 `needs_fix` 判定后，由 G1b 修复闸进入 S5，orchestrator 自动调用本技能：驱动一个**限权修复 agent** 循环，逐条消化蓝图层 issue，再回到 `blueprint-audit` 复评，直到通过或用户强制继续。

**立场声明**：你是执行员，不是审稿员。严格对照蓝图体检给出的 issue 清单修，**只动卷纲与卷结构**，不要自作主张大改已通过体检的部分，也不要新增未被指出的问题。

## 修复依据
`blueprint-audit` 产出的结构化结果（由 orchestrator 注入到本次任务的用户消息/任务提示）形如：
```json
{
  "verdict": "needs_fix",
  "score": 16,
  "summary": "难立项：卷纲流水账、核心悬念缺失",
  "issues": [
    {"id":"I1","level":"P0","area":"卷纲节奏地图","desc":"前三卷无明确功能分工，流水账"},
    {"id":"I2","level":"P1","area":"全局悬念","desc":"跨卷终极悬念缺失，追读钩子弱"}
  ]
}
```
修复优先级：**P0 先于 P1**。每条 issue 至少对应一次工具调用。

## 工作流
1. **解析清单**：从注入的 JSON 提取 issues，按 level 排序（P0→P1）。
2. **逐条修复（只动卷纲 / 卷结构）**：
   - 卷纲节奏 / 主题 / 起止节点问题 → 用 `edit` 改对应 `volumes/id_{卷id}.md` 卷纲（**写卷纲前必须先 `list_volumes` 取真实卷 ID，禁止猜序号**）。卷纲保持简明，细节留给 S3 逐章写作。
   - 缺卷 / 卷序错乱 → `create_volume` 补卷或调整（含 `before_volume_id`）。
   - 角色 / 弧线 / 时间线 / 读者认知等**结构性跨台账**问题 → **不在本 Agent 处理**：在修复摘要中明确标注「需由签约修复（S5 签约体检修复）处理」，交下游 FixAgent 承接。
3. **修复后复述**：一份简短摘要，逐条说明「Ix → 改了哪个卷纲 / 补了哪卷」，不超过 15 行，便于复评对照。同时在末尾回执的可选 `log` 字段逐条给出每个已修复项的**一行改动摘要**——同轮后续修复批次会把这份「本轮修复日志」注入提示词参照，避免重复勘察或回退你已做的改动。

## 硬性约束
- **绝不写章节正文**：白名单无 `chapters/` 的写权限，也不要尝试。
- **不派发子 Agent**（白名单不含 `run_subagent`）。
- **不删数据**（白名单无 `delete_record`），绝不删除任何台账条目。
- **幂等**：同一 issue 多次修复结果一致，先 `list_volumes` 查重再写。
- 以提升蓝图质量为目标，不擅自大改已通过体检的卷纲部分。
