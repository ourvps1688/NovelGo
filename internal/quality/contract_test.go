package quality

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const chapterCIMDPath = "../skill/builtin/chapter-ci.md"

// TestL1ReceiptFormatLineInSkillMD 契约单源锁定：skill md 必须原样引用回执格式串
// （与 pipeline verifyReceiptFormatLine ↔ signability-verify.md 同一纪律）。
func TestL1ReceiptFormatLineInSkillMD(t *testing.T) {
	md, err := os.ReadFile(chapterCIMDPath)
	require.NoError(t, err, "读取 chapter-ci.md 失败")
	require.Contains(t, string(md), L1ReceiptFormatLine(),
		"chapter-ci.md 应原样引用契约回执格式串（改契约必须同步 skill md）")
}

// mdJSONKeyRe 从 skill md 的 JSON 示例块提取 `"key":` 形式的键名（示例值不含此类模式）。
var mdJSONKeyRe = regexp.MustCompile(`"([a-z_]+)":`)

// extractMDFirstJSONBlock 提取 skill md 里第一个 ```json 围栏块内容。
func extractMDFirstJSONBlock(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err, "读取 skill md 失败")
	re := regexp.MustCompile("(?s)```json[ \t]*\n(.*?)\n```")
	m := re.FindStringSubmatch(string(raw))
	require.Len(t, m, 2, "skill md 应含 ```json 示例块")
	return m[1]
}

// TestSkillMDL1ExampleKeysMatchContract skill md 输出示例与契约双向同步：
// 示例块的键集合必须与 L1Finding/L1Receipt 的 json tag 集合完全一致。
func TestSkillMDL1ExampleKeysMatchContract(t *testing.T) {
	block := extractMDFirstJSONBlock(t, chapterCIMDPath)
	mdKeys := map[string]bool{}
	for _, k := range mdJSONKeyRe.FindAllStringSubmatch(block, -1) {
		mdKeys[k[1]] = true
	}

	want := map[string]bool{
		"findings": true, "id": true, "check": true, "desc": true,
		"chapter_ids": true, "ledger_refs": true, "summary": true,
	}
	for k := range want {
		assert.True(t, mdKeys[k], "skill md 示例缺契约键 %q", k)
	}
	for k := range mdKeys {
		assert.True(t, want[k], "skill md 示例含契约外键 %q（契约未同步）", k)
	}
}

// TestParseL1Receipt 回执提取：正常/零发现/多块取末/无块与坏 JSON 报错。
func TestParseL1Receipt(t *testing.T) {
	out := "小结文字。\n```json\n" + L1ReceiptFormatLine() + "\n```\n"
	r, err := ParseL1Receipt(out)
	require.NoError(t, err)
	require.Len(t, r.Findings, 1)
	assert.Equal(t, "L1-1", r.Findings[0].ID)
	assert.Equal(t, "continuity|pacing|foreshadow|reader_state|task_card", r.Findings[0].Check)
	assert.Equal(t, []int64{12}, r.Findings[0].ChapterIDs)

	// 零发现回执合法（空清单=已查零发现）。
	r0, err := ParseL1Receipt("```json\n{\"findings\":[],\"summary\":\"未发现增量问题\"}\n```")
	require.NoError(t, err)
	assert.Empty(t, r0.Findings)
	assert.Equal(t, "未发现增量问题", r0.Summary)

	// 多个块取最后一个可解析的（末尾回执为准）。
	multi := "```json\n{\"findings\":[],\"summary\":\"前文提及的示例\"}\n```\n正文。\n```json\n" +
		"{\"findings\":[{\"id\":\"L1-1\",\"check\":\"pacing\",\"desc\":\"中段拖沓\",\"chapter_ids\":[12]}],\"summary\":\"1 项\"}\n```"
	rm, err := ParseL1Receipt(multi)
	require.NoError(t, err)
	require.Len(t, rm.Findings, 1)
	assert.Equal(t, "pacing", rm.Findings[0].Check)

	_, err = ParseL1Receipt("没有任何回执块")
	assert.Error(t, err, "无回执块应报错（调用方降级）")
	_, err = ParseL1Receipt("```json\n{bad json}\n```")
	assert.Error(t, err, "坏 JSON 应报错（调用方降级）")
}

// TestL1ReceiptToFindings 档案发现投影：ID 补号、Level/Status 恒定、作用域回填。
func TestL1ReceiptToFindings(t *testing.T) {
	r := &L1Receipt{Findings: []L1Finding{
		{Check: L1CheckContinuity, Desc: "与前章矛盾", ChapterIDs: []int64{11, 12}, LedgerRefs: []string{"item.hero.sword"}},
		{ID: "L1-9", Check: L1CheckTaskCard, Desc: "钩子未兑现"},
	}}

	got := r.ToFindings(12)
	require.Len(t, got, 2)
	assert.Equal(t, "L1-1", got[0].ID, "空 ID 按序补号")
	assert.Equal(t, "L1-9", got[1].ID, "已有 ID 不动")
	assert.Equal(t, FindingLevelDefault, got[0].Level, "warning 级（P1）恒定")
	assert.Equal(t, FindingStatusPending, got[0].Status, "只报告不改写，落档即待修")
	assert.Equal(t, []int64{11, 12}, got[0].ChapterIDs)
	assert.Equal(t, []string{"item.hero.sword"}, got[0].LedgerRefs)
	assert.Equal(t, []int64{12}, got[1].ChapterIDs, "作用域空缺回填本章阅读序号")

	assert.Nil(t, (*L1Receipt)(nil).ToFindings(1), "nil 回执零发现")
	assert.Empty(t, (&L1Receipt{}).ToFindings(1))
}

// TestL1CheckEnum 检查项枚举单源。
func TestL1CheckEnum(t *testing.T) {
	for _, c := range []string{L1CheckContinuity, L1CheckPacing, L1CheckForeshadow, L1CheckReaderState, L1CheckTaskCard} {
		assert.True(t, L1CheckValid(c), c)
	}
	assert.False(t, L1CheckValid(""))
	assert.False(t, L1CheckValid("word_count"), "L0 检查项不进 L1 回执枚举")
}

// TestL1FormatLineKeysLocked 格式串键集合与契约 struct 对齐（防手抄漂移）。
func TestL1FormatLineKeysLocked(t *testing.T) {
	line := L1ReceiptFormatLine()
	for _, k := range []string{`"findings"`, `"id"`, `"check"`, `"desc"`, `"chapter_ids"`, `"ledger_refs"`, `"summary"`} {
		assert.True(t, strings.Contains(line, k), "格式串缺键 %s", k)
	}
	assert.Equal(t, 1, L1SchemaVersion)
}
