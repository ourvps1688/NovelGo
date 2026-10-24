package pipeline

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/novelgo/novelgo/internal/llm"
)

// ── 供应商熔断共享判定（WBS p4 §0.2）────────────────────────────────
//
// 全部 skillRunner 派发点共用一个熔断判定：近 providerIdleWindow 内 LLM 流式空闲超时
// 达到 fixProviderIdleTrip 次，说明 Provider 基本不可用（正常任务期间接近 0），再派发
// agent 只会白烧整轮墙钟（每轮白等 3 分钟被看门狗取消）。此时跳过派发、走各派发点
// 原有失败通道（S5 批次计数/闸门、S3 逐章进度 error、S4/S6 落 paused、巡检跳过），
// 提示统一「疑似供应商挂起」口径（附近期空闲超时次数）。
//
// 降级纪律（最高验收）：判定自身异常 → 照常派发（绝不误熔断）；窗口内超时为 0 →
// 行为与判定引入前逐字一致。

const (
	// providerIdleWindow 是供应商熔断判定的观察窗口，与 fixProviderIdleTrip 阈值构成
	// 「熔断判定」唯一常量对（单源纪律：窗口/阈值只在此一处定义）。
	providerIdleWindow = 10 * time.Minute
	// fixProviderIdleTrip 是供应商熔断阈值：近 providerIdleWindow 内 LLM 流式空闲超时
	// 达到该次数说明 Provider 基本不可用，全部 skillRunner 派发点派发前据此跳过派发、
	// 快速走各点原有失败通道（沿用 S5 修复既有阈值语义，值不变）。
	fixProviderIdleTrip = 10
)

// idleTimeoutCount 是「近 window 内流式空闲超时次数」的取数接缝（生产=llm.RecentIdleTimeouts，
// 测试可替换以确定性驱动熔断判定）。判定逻辑本身单源在 providerTripped。
var idleTimeoutCount = llm.RecentIdleTimeouts

// errProviderTripped 是熔断跳派发的哨兵错误（提示统一「疑似供应商挂起」口径），
// 供调用方（如 S3 逐章循环）用 errors.Is 识别「失败=熔断」并做整循环中断等差异化落地。
var errProviderTripped = errors.New("疑似供应商挂起")

// providerTripped 是全部 skillRunner 派发点共用的供应商熔断判定（单源）：
// 返回触发与否与窗口内空闲超时次数（次数供「疑似供应商挂起」提示口径使用）。
// 语义与既有 `llm.RecentIdleTimeouts(10*time.Minute) >= fixProviderIdleTrip` 逐字一致。
func providerTripped() (tripped bool, idleCount int) {
	// 降级纪律：判定自身异常 → 照常派发（返回 false），绝不误熔断。
	defer func() {
		if r := recover(); r != nil {
			tripped, idleCount = false, 0
		}
	}()
	n := idleTimeoutCount(providerIdleWindow)
	return n >= fixProviderIdleTrip, n
}

// providerTripErr 构造熔断哨兵错误（统一「疑似供应商挂起」口径，附近期空闲超时次数）。
// errors.Is(err, errProviderTripped) 可识别。
func providerTripErr(idleCount int) error {
	return fmt.Errorf("%w（近 %d 分钟 LLM 流式空闲超时 %d 次），已跳过派发，请稍后重试",
		errProviderTripped, int(providerIdleWindow.Minutes()), idleCount)
}

// dispatchSkill 是 skillRunner 的唯一派发口：派发前做供应商熔断判定（WBS p4 §0.2），
// 熔断 → 返回 errProviderTripped 哨兵错误、零 agent 派发，由各派发点按自身原有失败
// 通道落地（各接各的，勿统一成一种）；未触发 → 照常派发（行为与判定引入前逐字一致）。
//
// 源码纪律：internal/pipeline 生产代码里 s.skillRunner 的直接调用只允许出现在本方法内
// （测试 TestSkillRunnerDispatchOnlyViaTripGuard 源码锁定）——任何新增派发点必须走
// dispatchSkill，天然过熔断判定。
func (s *Service) dispatchSkill(ctx context.Context, name string, input SkillInput) (SkillOutput, error) {
	if tripped, n := providerTripped(); tripped {
		return SkillOutput{}, providerTripErr(n)
	}
	return s.skillRunner(ctx, name, input)
}
