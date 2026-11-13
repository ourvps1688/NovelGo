package quality

// writing_constraints.go —— #116「写前硬约束」唯一真相源（检测与写作同源）。
//
// 旧链路把检测器判定式只塞进改稿消息的【本章验收条件】，写章时模型完全不知道
// L0 会按什么常量判：技能与平台规范要求「第一句出事 / 开篇最炸」，检测器却
// 要求「首段低、后段更高 ≥5.0」，方向相反；真实 31 章曲线 25 章（80.6%）不达标，
// 单条 opening_slope 连续 21 轮未消除。于是「按最佳实践写 → 检测报 → 改 → 再报」。
//
// 本文件把检测侧常量直接投影成一段常驻 system 文本，在写章之前交给模型：
// 事前知道判定式、按同一数字写，不再靠事后反复改稿补救。所有数值引用同一份
// 常量，禁止在技能 markdown、身份提示词或其它文件另写一份阈值数字。
//
// 纯函数、零 DB、零 LLM、零写操作；不读发现项、不改阈值、不派发任何修复。
// 「所有修复都走 chat」纪律不变。

import "fmt"

// WritingConstraints 返回注入写章常驻 system 的「写前硬约束」块。
// 与 NovelProfile 的 ChapterWordsTarget.SystemLine 并列注入，放在偏好自由文本
// 之前，避免被旧卡里的口径盖过。
func WritingConstraints() string {
	return fmt.Sprintf(`【写前硬约束（与检测 L0 同源，落盘前逐条自检）】
- 开篇 %d 字窗口（按 %d 字切段，真实章节通常 2 段）：窗口内至少一段 tension ≥ %.0f（不平坦），或后段 tension 比首段高 ≥ %.1f，两者任一达标即不报。「第一句出事 / 冷开场 / 开篇爆点」已满足峰值口径，不要为了坡度再往首段堆冲突词；首段之后保持加压，别让张力塌下去。
- 张力只认确定性信号：具体动作细节（动作密度 ≥ %.2f 次/百字）、≤%d 字短句占比 ≥ %.2f、对话比与句长变化；结构峰阈值 %.2f、情绪峰阈值 %.0f。禁止靠堆直白情绪词或感叹号抬张力（会同时命中去 AI 味）；非武侠题材不要硬凑武侠冲突词。
- 全章至少一处爽点；连续平坦段（tension < %.0f）不得超过 %d 段。
- 章末 %d 字内须有悬念/转折信号词，或以问句/省略号收束。
- 单章字数以上方「单章字数口径」权威行为准；避开 AI 味套话（internal/text/deslop.go 词表）。
- 这些是本地确定性判定结果，不是主观评分：写完按同一口径自查，勿按其它数字自行估算。`,
		openingSlopeWindowRunes, tensionSegTargetRunes,
		openingPressureMin, openingSlopeMinRise,
		tensionActionCap, tensionShortSentenceMax, tensionShortRatioCap,
		structuralPeakAt, tensionEmotionClimaxAt,
		tensionFlatBelow, pacingDragMinRun,
		tailHookWindow)
}
