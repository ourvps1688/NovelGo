package pipeline

// 阶段与闸门标识。PR5 起，所有阶段流转逻辑集中在 internal/pipeline/orchestrator.go 的
// Orchestrator.Advance，本文件只保留常量定义，不再承载 NextStage 等状态机函数（消除双源）。
const (
	StageS0  = "S0"   // 导入/续写对齐入口（PR5：存量书归卷/补章细纲/台账后汇入公共循环）
	StageS1  = "S1"   // 孵化（book-pitch）
	GateG1   = "G1"   // 蓝图确认闸（蓝图体检通过后停留，用户决定 continue/modify/abort）
	GateG1b  = "G1b"  // 蓝图体检修复闸（蓝图体检不通过时停留，用户决定 fix/continue/abort）
	StageS2  = "S2"   // 蓝图（PR2 落库；A 方案复用为「生成 → 蓝图体检 → 蓝图修复」宿主阶段）
	StageS3  = "S3"   // 逐章写作（含评分门禁 + statesync）
	GateG3b  = "G3b"  // 里程碑闸（PR4 启用，每 5 章 / 卷边界）
	StageS4  = "S4"   // 签约体检（signability-audit，只读体检 + issue 清单）
	StageS4b = "S4b"  // 卷边界复评（PR4：每 5 章 / 跨卷时触发，复用 S4 节点）
	StageS5  = "S5"   // 修复闭环（按 issue 清单改稿 / 同步台账，修复后回到 S4/S4b/S6 复评）
	GateG4   = "G4"   // 签约体检确认闸（出 S4/S4b 时停留，用户决定 continue / fix / abort）
	StageS6  = "S6"   // 完本终检
	GateGEnd = "GEnd" // 完本终检确认闸（出 S6 时停留，用户决定 finish / fix / abort）
)
