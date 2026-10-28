package agentcfg

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/skill"
)

// SystemMessages 是注入对话的 system 消息集合。
// chat 和 compress 共用此结构，避免重复构建逻辑（呼应踩坑 16）。
type SystemMessages struct {
	Identity string // AgentIdentity（system1 提示词）
	Always   string // 常驻技能正文
	Catalog  string // auto 模式技能目录
	Profile  string // NovelProfile（设定+偏好，稳定）
	State    string // NovelState（基础信息+novelgo.md，动态）
	Persona  string // 写作人格（创作心智）引导段；为空表示普通模式
}

// BuildSystemMessages 统一构建 main agent 的 system 消息。
// chat 和 compress 共用此函数，避免重复实现。
//
// 全部在事务外构建（compress 模式）：传入的 db 应为带 ctx 的非事务句柄，
// 避免在事务内调用导致 SQLite 单连接池死锁（呼应踩坑 13/15）。
// skillStore 为 nil 时跳过 Always/Catalog。
//
// 容错：Profile/State 构建失败时对应字段为空（写入时跳过），不阻断对话。
// 返回的 error 是聚合错误，调用方可用于 log，但 msg 仍可使用。
//
// 注意：本函数固定使用 MainAgent 的 Identity，不适用于 subagent。
// subagent 路径请直接调 AgentIdentity(at) + NovelProfile + NovelState。
//
// persona 为写作人格引用（可为 nil）：非 nil 时解析并构建引导段写入 msg.Persona，
// 由调用方作为常驻块持久化（新会话）或每轮注入（已有会话）。
func BuildSystemMessages(ctx context.Context, db *gorm.DB, novelID int64, skillStore *skill.Store, persona *PersonaRef) (*SystemMessages, error) {
	msg := &SystemMessages{
		Identity: AgentIdentity(MainAgent),
	}

	if skillStore != nil {
		all := skillStore.ListMeta(novelID)
		// #282：若本书已将该 mental 设为写作人格（kind=skill），从其常驻（always/勾选启用）
		// 与目录候选中排除，避免心智同时经 Always 与 Persona 两条通道重复注入、浪费 token。
		// 人格通道已常驻注入，无需在 Always 块或 auto 目录中再列一次。
		if persona != nil && persona.Kind == "skill" && persona.Ref != "" {
			all = excludeNames(all, []string{persona.Ref})
		}
		msg.Always = BuildAlwaysSkillsContent(all, skillStore, novelID)
		catalogMeta := skillStore.ListMetaForCatalog(all)
		// 已勾选启用的技能已常驻注入，从 auto 目录中排除，避免重复占 token。
		if enabled := skillStore.EnabledNames(novelID); len(enabled) > 0 {
			catalogMeta = excludeNames(catalogMeta, enabled)
		}
		msg.Catalog = BuildSkillCatalog(catalogMeta)
	}

	if persona != nil {
		msg.Persona = BuildPersonaSystemText(persona, skillStore, novelID)
	}

	var errs []string

	if profile, err := NovelProfile(ctx, db, novelID); err != nil {
		errs = append(errs, fmt.Sprintf("novel profile: %v", err))
	} else {
		msg.Profile = profile
	}

	if state, err := NovelState(ctx, db, novelID); err != nil {
		errs = append(errs, fmt.Sprintf("novel state: %v", err))
	} else {
		msg.State = state
	}

	var err error
	if len(errs) > 0 {
		err = fmt.Errorf("build system messages: %s", strings.Join(errs, "; "))
	}
	return msg, err
}
