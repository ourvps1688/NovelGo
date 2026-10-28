package agentcfg

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/novelgo/novelgo/internal/skill"
)

// skillSpec 描述一个写入磁盘的测试技能。
type skillSpec struct {
	name         string
	kind         string
	sourceAuthor string
	body         string
	aggregated   bool
}

// personaTestStore 在临时 HOME 下创建 skill 仓库（先写技能再建 store，保证 Get 命中内存），
// 使 Get 与 ListMeta（后者从 config.UserSkillsDir() 重载）都指向同一目录。
func personaTestStore(t *testing.T, skills ...skillSpec) (*skill.Store, int64) {
	t.Helper()
	home := t.TempDir()
	// Windows 上 os.UserHomeDir() 优先 USERPROFILE，二者都设以保证一致。
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	skillsDir := filepath.Join(home, ".novelgo", "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0o755))
	for _, s := range skills {
		writePersonaSkill(t, skillsDir, s.name, s.kind, s.sourceAuthor, s.body, s.aggregated)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := skill.NewStore(logger, skillsDir)
	require.NoError(t, err)
	return store, int64(7)
}

func writePersonaSkill(t *testing.T, skillsDir, name, kind, sourceAuthor, body string, aggregated bool) {
	t.Helper()
	agg := ""
	if aggregated {
		agg = "\naggregated: true"
	}
	fm := "---\nname: " + name + "\nmode: always\ndescription: 测试心智 " + name +
		"\nkind: " + kind + "\nsource_author: " + sourceAuthor + agg + "\n---\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, name+".md"), []byte(fm), 0o644))
}

func TestBuildPersonaSystemText_Skill(t *testing.T) {
	store, novelID := personaTestStore(t, skillSpec{
		name: "X 的创作心智", kind: "mental", sourceAuthor: "刘慈欣", body: "X_MENTAL_BODY",
	})

	got := BuildPersonaSystemText(&PersonaRef{Kind: "skill", Ref: "X 的创作心智"}, store, novelID)
	require.Contains(t, got, PersonaMarker, "应包含 persona 标识头")
	require.Contains(t, got, "X_MENTAL_BODY", "应包含技能正文")
	require.Contains(t, got, PersonaHonestyNote, "应包含诚实边界声明")
}

// TestBuildPersonaSystemText_Golden 锁 chat 常驻块全文逐字（T3.1 渲染单源化前后零变化）：
// PersonaMarker + "\n\n" + chat 引导语 + "\n\n" + 心智正文（技能正文 + "\n\n" + 诚实声明）。
// 引导语文案与 agentcfg.BuildPersonaSystemText 现行文案逐字一致（WBS §0.6 chat 行 golden，禁改）。
func TestBuildPersonaSystemText_Golden(t *testing.T) {
	store, novelID := personaTestStore(t, skillSpec{
		name: "X 的创作心智", kind: "mental", sourceAuthor: "刘慈欣", body: "X_MENTAL_BODY",
	})

	got := BuildPersonaSystemText(&PersonaRef{Kind: "skill", Ref: "X 的创作心智"}, store, novelID)
	want := "【写作人格 · Persona】\n\n" +
		"以下创作心智将指导你起草本轮新章节。请在保持故事世界一致性的前提下，以该作者的叙事习惯、表达指纹与创作决策启发式来写作，而不是简单模仿或套用公式。" +
		"\n\nX_MENTAL_BODY\n\n" +
		"注意：以下创作心智推断自虚构文本，仅供参考，不得作为真实作者意图的断言。"
	require.Equal(t, want, got, "chat 常驻块全文须逐字不变（golden）")
}

func TestBuildPersonaSystemText_Author_PrefersAggregated(t *testing.T) {
	store, novelID := personaTestStore(t,
		skillSpec{name: "刘慈欣-心智-聚合", kind: "mental", sourceAuthor: "刘慈欣", body: "AGGREGATED_BODY", aggregated: true},
		skillSpec{name: "刘慈欣-心智-单书", kind: "mental", sourceAuthor: "刘慈欣", body: "SINGLE_BOOK_BODY"},
	)

	got := BuildPersonaSystemText(&PersonaRef{Kind: "author", Ref: "刘慈欣"}, store, novelID)
	require.Contains(t, got, "AGGREGATED_BODY", "author 解析应优先聚合档")
	require.NotContains(t, got, "SINGLE_BOOK_BODY", "不应选中单书档")
}

func TestBuildPersonaSystemText_Unresolved(t *testing.T) {
	store, novelID := personaTestStore(t)
	require.Empty(t, BuildPersonaSystemText(&PersonaRef{Kind: "skill", Ref: "不存在"}, store, novelID),
		"引用不存在的技能应返回空串")
	require.Empty(t, BuildPersonaSystemText(&PersonaRef{Kind: "author", Ref: "无人"}, store, novelID),
		"引用不存在的作者应返回空串")
	require.Empty(t, BuildPersonaSystemText(nil, store, novelID), "nil persona 应返回空串")
}

// TestBuildPersonaMindText 锁 P0 provider 口径（WBS §0.3）：心智正文 = 技能正文 +
// 诚实边界声明，逐字断言；**不含** PersonaMarker 块头与 chat 引导语（那两段由
// 统一装配器渲染，provider 不得自带，否则 Voice 块会嵌套两个块头）。
func TestBuildPersonaMindText(t *testing.T) {
	store, novelID := personaTestStore(t, skillSpec{
		name: "X 的创作心智", kind: "mental", sourceAuthor: "刘慈欣", body: "X_MENTAL_BODY",
	})

	got := BuildPersonaMindText(&PersonaRef{Kind: "skill", Ref: "X 的创作心智"}, store, novelID)
	require.Equal(t, "X_MENTAL_BODY"+"\n\n"+PersonaHonestyNote, got, "心智正文=正文+诚实声明（逐字）")
	require.NotContains(t, got, PersonaMarker, "provider 不得自带块头")
	require.NotContains(t, got, "以下创作心智将指导你起草本轮新章节", "provider 不得自带引导语")

	// 解析失败降级为空串（对应块不注入）。
	require.Empty(t, BuildPersonaMindText(&PersonaRef{Kind: "skill", Ref: "不存在"}, store, novelID))
	require.Empty(t, BuildPersonaMindText(nil, store, novelID))
}

func TestBuildSystemMessages_PersonaDoesNotBreakAlways(t *testing.T) {
	store, novelID := personaTestStore(t,
		skillSpec{name: "X 的创作心智", kind: "mental", sourceAuthor: "刘慈欣", body: "X_MENTAL_BODY"},
		skillSpec{name: "常驻技能", kind: "", sourceAuthor: "", body: "ALWAYS_BODY"},
	)

	// 最小 in-memory DB（Profile/State 会因缺表报错，但 BuildSystemMessages 容错继续，
	// Always/Persona 仍会被填充，足以验证 persona 注入不破坏常驻 Always）。
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	msg, _ := BuildSystemMessages(t.Context(), db, novelID, store, &PersonaRef{Kind: "skill", Ref: "X 的创作心智"})
	require.Contains(t, msg.Always, "ALWAYS_BODY", "常驻 Always 不应被 persona 破坏")
	require.Contains(t, msg.Persona, "X_MENTAL_BODY", "persona 应被注入")
	require.Contains(t, msg.Persona, PersonaHonestyNote, "persona 应含诚实边界声明")

	// 无 persona 时 msg.Persona 为空，Always 仍在。
	msg2, _ := BuildSystemMessages(t.Context(), db, novelID, store, nil)
	require.Empty(t, msg2.Persona, "无 persona 时 Persona 应为空")
	require.Contains(t, msg2.Always, "ALWAYS_BODY", "无 persona 时 Always 仍在")
}

// TestBuildSystemMessages_PersonaExcludedFromAlways 验证 #282：本书已将某 mental 设为写作人格
// （kind=skill）时，该技能从常驻 Always 块中排除，避免与 Persona 通道重复注入、浪费 token。
// 同时验证无 persona 时该技能仍正常常驻注入（排除逻辑仅由 persona 触发）。
func TestBuildSystemMessages_PersonaExcludedFromAlways(t *testing.T) {
	store, novelID := personaTestStore(t,
		skillSpec{name: "X 的创作心智", kind: "mental", sourceAuthor: "刘慈欣", body: "X_MENTAL_BODY"},
		skillSpec{name: "常驻技能", kind: "", sourceAuthor: "", body: "ALWAYS_BODY"},
	)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 人格指向 X 的创作心智：它应只经 Persona 注入，不出现在 Always。
	msg, _ := BuildSystemMessages(t.Context(), db, novelID, store, &PersonaRef{Kind: "skill", Ref: "X 的创作心智"})
	require.Contains(t, msg.Persona, "X_MENTAL_BODY", "persona 应注入心智正文")
	require.NotContains(t, msg.Always, "X_MENTAL_BODY", "人格技能不应重复出现在 Always 块")
	require.Contains(t, msg.Always, "ALWAYS_BODY", "其他常驻技能仍应注入")

	// 无 persona 时，X 的创作心智（mode=always）应正常出现在 Always。
	msg2, _ := BuildSystemMessages(t.Context(), db, novelID, store, nil)
	require.Contains(t, msg2.Always, "X_MENTAL_BODY", "无 persona 时常驻技能应正常注入")
}
