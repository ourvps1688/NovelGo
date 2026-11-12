package activity

import "context"

// #114 P2：技能账本的「本轮身份」通过 ctx 一次性下沉，避免给分散在
// app / slash_command / agentcfg 的 13 个埋点逐个改函数签名。
//
// turnID 复用 chat 链路既有的 session.NextTurn 原子自增 int（不是新 ID）；
// findingKeys 是 #114 P1 单源 quality.FindingKeys 生成的 JSON 快照，只在
// 带验收条件的改稿轮非空。pipeline / mcp_tools 侧埋点用 context.Background()
// 或独立 ctx，天然为零值——它们本就不属于 chat turn，归属继续用 RunID/ChapterID。
type turnFindingKey struct{}

type turnFindingValue struct {
	turnID      int
	findingKeys string
}

// WithTurnFindingKeys 把本轮标识绑到 ctx。传 nil ctx 时退化为 Background。
func WithTurnFindingKeys(ctx context.Context, turnID int, findingKeys string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, turnFindingKey{}, turnFindingValue{
		turnID:      turnID,
		findingKeys: findingKeys,
	})
}

// turnFindingFromContext 读取本轮标识；未绑定时返回零值（旧链路/子任务天然兼容）。
func turnFindingFromContext(ctx context.Context) (int, string) {
	if ctx == nil {
		return 0, ""
	}
	v, ok := ctx.Value(turnFindingKey{}).(turnFindingValue)
	if !ok {
		return 0, ""
	}
	return v.turnID, v.findingKeys
}
