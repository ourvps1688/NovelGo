package quality

// feedback_contract_test.go —— 编辑反馈回流契约锁（P1-1）：
// 回执格式串与 editor-feedback.md 原样同步、示例键集合与契约 struct 双向同步、
// 解析/归一化纪律（F 补号、坏分类剔除、宁缺勿错）回归。

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const editorFeedbackMDPath = "../skill/builtin/editor-feedback.md"

// TestFeedbackReceiptFormatLineInSkillMD 契约单源锁定：skill md 必须原样引用回执格式串
// （与 L1ReceiptFormatLine ↔ chapter-ci.md 同一纪律）。
func TestFeedbackReceiptFormatLineInSkillMD(t *testing.T) {
	md, err := os.ReadFile(editorFeedbackMDPath)
	require.NoError(t, err, "读取 editor-feedback.md 失败")
	require.Contains(t, string(md), FeedbackReceiptFormatLine(),
		"editor-feedback.md 应原样引用契约回执格式串（改契约必须同步 skill md）")
}

// TestSkillMDFeedbackExampleKeysMatchContract skill md 输出示例与契约双向同步：
// 示例块的键集合必须与 FeedbackReceipt/FeedbackIssue/FeedbackConstraint 的 json tag
// 集合完全一致（防手抄漂移）。
func TestSkillMDFeedbackExampleKeysMatchContract(t *testing.T) {
	block := extractMDFirstJSONBlock(t, editorFeedbackMDPath)
	mdKeys := map[string]bool{}
	for _, k := range mdJSONKeyRe.FindAllStringSubmatch(block, -1) {
		mdKeys[k[1]] = true
	}

	want := map[string]bool{
		"dims": true, "issues": true, "id": true, "dim": true, "level": true, "area": true,
		"desc": true, "fix_type": true, "chapter_ids": true,
		"constraints": true, "target": true, "key": true, "value": true, "reason": true,
		"summary": true,
	}
	for k := range want {
		assert.True(t, mdKeys[k], "skill md 示例缺契约键 %q", k)
	}
	for k := range mdKeys {
		assert.True(t, want[k], "skill md 示例含契约外键 %q（契约未同步）", k)
	}
}

// TestFeedbackContractVersionLocked 契约版本与枚举全集锁。
func TestFeedbackContractVersionLocked(t *testing.T) {
	assert.Equal(t, 1, FeedbackSchemaVersion)
	for _, d := range []string{
		FeedbackDimOpening, FeedbackDimGenre, FeedbackDimCharacter, FeedbackDimPacing,
		FeedbackDimSetting, FeedbackDimLongline, FeedbackDimCompliance, FeedbackDimReader,
	} {
		assert.True(t, FeedbackDimValid(d), "八维枚举应合法: %s", d)
	}
	assert.False(t, FeedbackDimValid("opening"))
	assert.False(t, FeedbackDimValid(""))

	assert.True(t, FeedbackConstraintValid(FeedbackConstraint{Target: FeedbackTargetChecker, Key: ConstraintKeyPremiseKeywords, Value: `["灾星"]`}))
	assert.True(t, FeedbackConstraintValid(FeedbackConstraint{Target: FeedbackTargetPreference, Key: "编辑口径", Value: "看点开篇兑现"}))
	assert.False(t, FeedbackConstraintValid(FeedbackConstraint{Target: FeedbackTargetChecker, Key: "unknown_key", Value: "1"}))
	assert.False(t, FeedbackConstraintValid(FeedbackConstraint{Target: FeedbackTargetChecker, Key: ConstraintKeyWindowN, Value: " "}))
	assert.False(t, FeedbackConstraintValid(FeedbackConstraint{Target: "other", Key: "k", Value: "v"}))
}

// TestParseFeedbackReceipt 回执提取：格式串往返 / 归一化纪律 / 零发现 / 多块取末 /
// 无块与坏 JSON 报错。
func TestParseFeedbackReceipt(t *testing.T) {
	// 格式串往返（skill md 同款用法）。
	out := "小结文字。\n```json\n" + FeedbackReceiptFormatLine() + "\n```\n"
	r, err := ParseFeedbackReceipt(out)
	require.NoError(t, err)
	require.Len(t, r.Issues, 1)
	assert.Equal(t, "F1", r.Issues[0].ID)
	assert.Equal(t, FeedbackDimOpening, r.Issues[0].Dim)
	assert.Equal(t, []int64{1, 2, 3}, r.Issues[0].ChapterIDs)
	assert.Equal(t, []string{FeedbackDimOpening}, r.Dims)

	// 归一化纪律：ID 空缺补 F{n}（按剔除前序号）、坏 dim 剔除、空 desc 剔除、
	// fix_type 空归 ledger、坏约束剔除。
	raw := "```json\n" + `{"dims":["opening_hook","bogus","opening_hook"],"issues":[` +
		`{"id":"","dim":"opening_hook","level":"P0","area":"开篇过签力","desc":"代入感弱"},` +
		`{"id":"F9","dim":"bogus","level":"P0","area":"x","desc":"坏维度"},` +
		`{"id":"F3","dim":"genre_fit","level":"P1","area":"题材契合","desc":""}],` +
		`"constraints":[` +
		`{"target":"checker_config","key":"premise_keywords","value":"[\"灾星\"]","reason":"r"},` +
		`{"target":"checker_config","key":"bad_key","value":"1","reason":"r"},` +
		`{"target":"preference","key":"","value":"v","reason":"r"}],` +
		`"summary":"归一化"}` + "\n```"
	r2, err := ParseFeedbackReceipt(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{FeedbackDimOpening}, r2.Dims, "坏维度剔除+去重")
	require.Len(t, r2.Issues, 1, "坏 dim / 空 desc 剔除后只剩一条")
	assert.Equal(t, "F1", r2.Issues[0].ID, "ID 空缺按原序补 F{n}")
	assert.Equal(t, "ledger", r2.Issues[0].FixType, "fix_type 空归 ledger")
	require.Len(t, r2.Constraints, 1, "坏 target/key 剔除")
	assert.Equal(t, ConstraintKeyPremiseKeywords, r2.Constraints[0].Key)

	// 零发现回执合法（原话无具体指向）。
	r0, err := ParseFeedbackReceipt("```json\n{\"dims\":[],\"issues\":[],\"constraints\":[],\"summary\":\"原话无具体指向，仅沉淀样本\"}\n```")
	require.NoError(t, err)
	assert.Empty(t, r0.Issues)
	assert.Empty(t, r0.Dims)

	// 多个块取最后一个可解析的（末尾回执为准）。
	multi := "```json\n{\"dims\":[],\"issues\":[],\"constraints\":[],\"summary\":\"前文示例\"}\n```" +
		"\n正文。\n```json\n{\"dims\":[\"pacing_beats\"],\"issues\":[{\"id\":\"F1\",\"dim\":\"pacing_beats\",\"level\":\"P1\",\"area\":\"节奏爽点\",\"desc\":\"拖沓\"}],\"constraints\":[],\"summary\":\"末尾\"}\n```"
	rm, err := ParseFeedbackReceipt(multi)
	require.NoError(t, err)
	require.Len(t, rm.Issues, 1)
	assert.Equal(t, FeedbackDimPacing, rm.Issues[0].Dim)

	_, err = ParseFeedbackReceipt("没有任何回执块")
	assert.Error(t, err, "无回执块应报错（调用方降级保留 raw）")
	_, err = ParseFeedbackReceipt("```json\n{bad json}\n```")
	assert.Error(t, err, "坏 JSON 应报错（调用方降级保留 raw）")
}
