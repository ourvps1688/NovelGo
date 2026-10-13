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
