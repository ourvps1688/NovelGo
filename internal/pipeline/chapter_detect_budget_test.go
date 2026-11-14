package pipeline

// #125 方案 3 契约：章节检测专属硬超时必须长于思考模型的 idle 空闲预算。
// 旧值 8min < thinkingIdleTimeout(9min)，慢供应商下硬墙先到期，来不及走完
// 「一次 idle → 停止重试 → 收口」，15:28 / 20:02 两次手动重检被腰斩。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChapterDetectTimeoutCoversIdleWatchdog(t *testing.T) {
	assert.Equal(t, 12*time.Minute, chapterDetectTimeout)
	assert.Greater(t, chapterDetectTimeout, 9*time.Minute, "须大于 internal/llm.thinkingIdleTimeout")
	// beginDetectCtx 取两者较小值，仍须由章节专属上限决定（其余三类走 60min 通用兜底）。
	assert.Less(t, chapterDetectTimeout, detectTaskTimeout)
}
