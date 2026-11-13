package pipeline

// #116 退役链路契约：后台自动重写整链已下线，rewriteMethodBlock 只保留
// 历史映射口径供契约测试使用，不得重新接入生产调用，也不得再写技能账本
// （旧埋点会给「从未注入任何提示词」的死链留下 rewrite_method 假记录）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readPipelineSource(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	require.NoError(t, err, "读取 %s", rel)
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// retiredCallOutsideDecl 检测「声明以外的调用点」：函数/方法声明行与注释行
// 里的名字只是定义或说明，不算生产接线。这样定义文件本身也能被扫描覆盖，
// 一旦有人在 rewrite_skills.go 里把死链自调用接回去会立刻失败。
func retiredCallOutsideDecl(src, name string) bool {
	call := name + "("
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.HasPrefix(trimmed, "func ") && strings.Contains(trimmed, call) {
			continue
		}
		if strings.Contains(line, call) {
			return true
		}
	}
	return false
}

func TestRewriteMethodNeverWiredToProduction(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src := readPipelineSource(t, f)
		assert.False(t, retiredCallOutsideDecl(src, "rewriteMethodBlock"),
			"%s 不得把退役的自动重写方法论块接回生产链路", f)
		assert.False(t, retiredCallOutsideDecl(src, "joinRewriteBlocks"),
			"%s 不得把退役的重写提示词拼接块接回生产链路", f)
	}
}

func TestRewriteSkillsCarryNoFakeInvocation(t *testing.T) {
	src := readPipelineSource(t, "rewrite_skills.go")
	assert.Contains(t, src, "func rewriteMethodBlock(", "历史映射口径保留供契约测试")
	assert.NotContains(t, src, "RecordSkillInvocation", "死链不得再写技能账本")
	assert.NotContains(t, src, `Channel: "rewrite_method"`, "死链不得伪造 rewrite_method 用量")
	assert.Contains(t, src, "#116 退役状态")
}
