package agentcfg

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 回归锁定：review agent 是纯只读审读（工具白名单只有 get_*/read），
// 提示词不得承诺白名单之外的写工具（历史失实句「也可以调用部分 update 工具来修正发现的问题」）。
func TestReviewAgentPromptReadOnlyContract(t *testing.T) {
	require.NotContains(t, reviewAgentSystem1, "update",
		"reviewAgentSystem1 不得声称 update 写工具（白名单纯只读）")
	require.Contains(t, reviewAgentSystem1, "只读",
		"reviewAgentSystem1 应声明只读职责")

	for _, tool := range reviewAgentTools {
		ok := strings.HasPrefix(tool, "get_") ||
			strings.HasPrefix(tool, "list_") ||
			strings.HasPrefix(tool, "search_") ||
			tool == "read"
		require.True(t, ok, "reviewAgentTools 只允许只读工具（get_*/list_*/search_/read），发现越权工具 %q", tool)
	}
}
