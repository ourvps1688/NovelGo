package pipeline

// A 方案契约：章节专属硬墙（原 12min）已撤销，四类检测统一只走通用安全兜底。
// 流级 9min idle watchdog 是 chat/体检/巡检/复验共用的死连接保护，必须保留。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChapterHasNoDedicatedHardTimeout(t *testing.T) {
	assert.Equal(t, 60*time.Minute, detectTaskTimeout)
	detect := readChapterDetectSource(t, "detect.go")
	status := readChapterDetectSource(t, "detect_status.go")
	for _, src := range []string{detect, status} {
		assert.NotContains(t, src, "chapterDetectTimeout", "章节专属墙钟不得回潮")
	}
	assert.Contains(t, status, "context.WithTimeout(s.appCtx, detectTaskTimeout)")
	// 超时后档案诚实停在 pending，不再「保留上一轮 L1」冒充完成。
	assert.Contains(t, detect, "本版 L1 保持待复核（旧版结论已作废）")
	// 流级死连接保护不得随章节墙钟一起被删。
	llm := readChapterDetectSource(t, "../llm/stream.go")
	assert.Contains(t, llm, "const thinkingIdleTimeout = 9 * time.Minute")
}
