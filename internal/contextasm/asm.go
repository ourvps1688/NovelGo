// Package contextasm 是 P0「接线」的统一上下文装配器（ContextAssembler v1）：
// 把 Facts（对不对）/ Voice（像不像）/ Intent（该不该）三类上下文按统一契约，
// 装配进流水线各提示词尾部与 chat 写作块。
//
// 注入纪律四条（与卷级 WBS 2.3 已验证契约同源，勿改）：
//  1. 有则注入：块内容非空 → 按块格式渲染注入；
//  2. 无则降级为空串：provider 为 nil / 返回空 / 返回 error → 该块不注入，
//     最终提示词与未接该块时逐字一致（error 只聚合给调用方记日志，不阻断、不改文案）；
//  3. 预算截断：Voice/Intent 超预算时头部保留、尾部截断并标注「（已截断）」；
//  4. 坏数据宁缺勿错：截断保证 rune 边界，绝不注入半截/乱码内容。
//
// 分层契约：Assembler.Build 按 purpose×块矩阵（WBS §0.5）决定取哪些块；
// ContextBlocks.Render / RenderVoice 只做格式化，purpose 仅决定 Voice 引导语（WBS §0.6）。
//
// 本包是纯文本包，只依赖标准库；不得反向 import internal/pipeline 等重依赖。
package contextasm

import (
	"context"
	"errors"
	"unicode/utf8"
)

// Purpose 标识提示词用途（WBS §0.5/§0.6）。
type Purpose string

const (
	// PurposeChat 对话写作（chat 平面）：只装配 Voice；Facts 由 Profile/State 通道提供。
	PurposeChat Purpose = "chat"
	// PurposeRewrite S3 章节重写：Facts+Voice+Intent 三块全注（文风锚定第一现场）。
	PurposeRewrite Purpose = "rewrite"
	// PurposeAudit S4 签约体检 / 完本审：Facts+Voice（Voice 仅作参照不扣分）。
	PurposeAudit Purpose = "audit"
	// PurposeFix S5 台账修复：Facts+Voice（防修复把文风写歪）。
	PurposeFix Purpose = "fix"
	// PurposePatrol 一致性巡检：Facts+Voice（张力参照）。
	PurposePatrol Purpose = "patrol"
)

// TextProvider 提供一块上下文正文（Facts=既定事实文本 / Voice=创作心智正文 / Intent=创作意图正文）。
// 返回空串或 error 都降级为「该块不注入」；error 由 Build 聚合返回，仅用于调用方记日志。
type TextProvider func(ctx context.Context, novelID int64) (string, error)

// ContextBlocks 是装配完成的三块正文（不含块头/引导语/尾注，渲染见 blocks.go）。
// 块顺序固定 Facts → Voice → Intent（硬事实在前、软约束在后）。
type ContextBlocks struct {
	Facts  string
	Voice  string
	Intent string
}

// Assembler 按 purpose 矩阵从 TextProvider 取块，并对 Voice/Intent 做预算截断。
type Assembler struct {
	facts  TextProvider
	voice  TextProvider
	intent TextProvider
}

// NewAssembler 构建装配器；三个 provider 均可为 nil（对应块恒降级为空）。
func NewAssembler(facts, voice, intent TextProvider) *Assembler {
	return &Assembler{facts: facts, voice: voice, intent: intent}
}

// Build 按 purpose×块矩阵（WBS §0.5）取块：
// rewrite 三块全取；audit/fix/patrol 取 Facts+Voice；chat 只取 Voice；未知 purpose 同 audit（不取 Intent）。
// 块级失败（error/空）置空该块并继续；返回 error 仅聚合各 provider 错误供调用方记日志，
// 返回的 blocks 始终可用（空块即降级）。Voice/Intent 超预算按 budget.go 截断。
func (a *Assembler) Build(ctx context.Context, novelID int64, purpose Purpose) (ContextBlocks, error) {
	if a == nil {
		return ContextBlocks{}, nil
	}
	var (
		blocks ContextBlocks
		errs   []error
	)
	get := func(p TextProvider) string {
		if p == nil {
			return ""
		}
		text, err := p(ctx, novelID)
		if err != nil {
			errs = append(errs, err)
			return ""
		}
		if text == "" {
			return ""
		}
		if !utf8.ValidString(text) {
			// 坏数据宁缺勿错：非法 UTF-8 整块放弃，绝不注入半截/乱码内容（§0.2 纪律 4）。
			errs = append(errs, errors.New("contextasm: provider 返回内容非法 UTF-8，整块放弃"))
			return ""
		}
		return text
	}

	if purpose != PurposeChat {
		// Facts 的 24KB 预算由 snapshot.BibleText 既有表粒度截断负责，装配器不动（现状不变）。
		blocks.Facts = get(a.facts)
	}
	blocks.Voice = fitBudget(get(a.voice), VoiceBudgetBytes)
	if purpose == PurposeRewrite {
		blocks.Intent = fitBudget(get(a.intent), IntentBudgetBytes)
	}
	return blocks, errors.Join(errs...)
}
