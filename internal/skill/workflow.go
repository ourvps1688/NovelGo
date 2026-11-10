package skill

// WorkflowAutoArm 描述一个「工作流自动武装点」（#111 P3）：系统在该工作流触发时
// 由 Go 确定性注入对应挂载点的技能正文，用户无需知道技能名、无需手动启用或 read。
//
// 这是说明性单源，只用于两处：生成技能目录的自动武装说明、开发期健康检查静态断言。
// 它**不参与运行时装配决策**——运行时仍由各挂载点自己的确定性装配器负责，
// 避免再造出第二套与 cue 路由竞争的调度器，也避免把「目录说明」误当成注入回执。
type WorkflowAutoArm struct {
	Workflow string // 用户可见的工作流名称
	Mount    string // mounts 枚举，必须与 health_test.go 的 mountOwners 一致
}

// WorkflowAutoArms 是技能目录降级后，回答「我不勾选、不 slash、不 read，技能怎么生效」
// 的唯一清单。顺序按用户可感知的写作流程排列，便于目录直接渲染。
//
// 收口边界：
//   - slash_only 是高级入口，不作为可用性指标，故不列入；
//   - always / catalog_read 是 .enabled.json 与目录自读的兼容层，目录降级后
//     不承担正文通道职责，故不列入；
//   - 新增工作流须同步 internal/skill/health_test.go 的 mountOwners 与
//     TestWorkflowAutoArmContract，并由对应装配器真实消费。
var WorkflowAutoArms = []WorkflowAutoArm{
	{"开新书 / 卖点引导", "pitch_flow"},
	{"对话意图装配", "chat_intent"},
	{"起草章节（含条件化技法）", "writing_bundle"},
	{"章节写后自检", "chapter_ci"},
	{"章节状态同步", "state_sync"},
	{"章节重写 / 低分维修复", "rewrite_method"},
	{"签约体检 / 一致性巡检", "pipeline_dispatch"},
	{"全书蓝图体检", "blueprint_audit"},
	{"编辑反馈回流", "feedback"},
}
