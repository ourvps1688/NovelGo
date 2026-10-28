package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/activity"
	"github.com/novelgo/novelgo/internal/llm"
)

// ── 供应商熔断共享判定（P0 重设计）────────────────────────────────
//
// 重设计（docs/future/llm-provider-breaker-idle-timeout-redesign.md）把「空闲超时」从熔断
// 触发信号剥离：熔断只认硬失败（连接拒绝 / DNS 不可达 / 首字节超时 / >=5xx），三态（Closed/
// Open/Half-Open）自愈、按 provider 隔离、chat/pipeline 共用（P1 补齐 chat 接线）。
//
// 本文件的 providerTripped 改为基于 llm.ProviderBreaker.AnyOpen 的取数接缝——熔断状态机
// （阈值/冷却/自愈/按 provider 隔离）全部收敛在 breaker.go，本文件只做「取数 + 派发拦截」。
//
// 空闲超时（idle watchdog）仅作观测（沿用 llm.idleFires），绝不再驱动熔断，避免 thinking
// 模型长静默 / 代理缓冲被误杀。
//
// 降级纪律（最高验收）：判定自身异常 → 照常派发（绝不误熔断）；无任何 provider 处于 Open
// → 行为与判定引入前逐字一致（零 agent 派发拦截）。

// breakerOpenProviders 是「当前处于 Open 的 provider 列表」的取数接缝（生产=llm.ProviderBreaker.
// AnyOpen，测试可替换以确定性驱动熔断判定）。判定逻辑单源在 providerTripped。
var breakerOpenProviders = llm.ProviderBreaker.AnyOpen

// errProviderTripped 是熔断跳派发的哨兵错误（提示统一「疑似供应商挂起」口径），
// 供调用方（如 S3 逐章循环）用 errors.Is 识别「失败=熔断」并做整循环中断等差异化落地。
var errProviderTripped = errors.New("疑似供应商挂起")

// providerTripped 是全部 skillRunner 派发点共用的供应商熔断判定（单源）：
// 返回触发与否与处于 Open 的 provider 列表（列表供「疑似供应商挂起」提示口径使用）。
func providerTripped() (tripped bool, openProviders []string) {
	// 降级纪律：判定自身异常 → 照常派发（返回 false），绝不误熔断。
	defer func() {
		if r := recover(); r != nil {
			tripped, openProviders = false, nil
		}
	}()
	open := breakerOpenProviders()
	return len(open) > 0, open
}

// providerTripErr 构造熔断哨兵错误（统一「疑似供应商挂起」口径，附 Open 的 provider 列表）。
// errors.Is(err, errProviderTripped) 可识别。
func providerTripErr(openProviders []string) error {
	who := "供应商"
	if len(openProviders) > 0 {
		who = "供应商（" + strings.Join(openProviders, "、") + "）"
	}
	return fmt.Errorf("%w（%s疑似挂起，已跳过派发，请稍后重试）", errProviderTripped, who)
}

// dispatchSkill 是 skillRunner 的唯一派发口：派发前做供应商熔断判定（P0 重设计），
// 熔断 → 返回 errProviderTripped 哨兵错误、零 agent 派发，由各派发点按自身原有失败
// 通道落地（各接各的，勿统一成一种）；未触发 → 照常派发（行为与判定引入前逐字一致）。
//
// R0 观测（pipeline-core-redesign-proposal.md §5）：每次派发在收口后记一条
// activity.ExecStat 样本（skill/耗时/turn 数/状态/归属 run），纯本地、零行为变化——
// 观测自身失败绝不影响派发结果（recordExecStat 内部兜底）。
//
// 源码纪律：internal/pipeline 生产代码里 s.skillRunner 的直接调用只允许出现在本方法内
// （测试 TestSkillRunnerDispatchOnlyViaTripGuard 源码锁定）——任何新增派发点必须走
// dispatchSkill，天然过熔断判定与 R0 观测。
func (s *Service) dispatchSkill(ctx context.Context, name string, input SkillInput) (SkillOutput, error) {
	if tripped, open := providerTripped(); tripped {
		s.recordExecStat(ctx, activity.ExecStat{
			NovelID: input.NovelID, RunID: input.RunID, Skill: name,
			Turns: 0, MaxTurns: input.MaxTurns, Status: activity.ExecStatusTripped,
		})
		return SkillOutput{}, providerTripErr(open)
	}
	start := time.Now()
	out, err := s.skillRunner(ctx, name, input)
	status := activity.ExecStatusOK
	if err != nil {
		status = activity.ExecStatusError
	}
	s.recordExecStat(ctx, activity.ExecStat{
		NovelID: input.NovelID, RunID: input.RunID, Skill: name,
		DurationMS: time.Since(start).Milliseconds(),
		Turns:      out.Turns, MaxTurns: input.MaxTurns, Status: status,
	})
	return out, err
}
