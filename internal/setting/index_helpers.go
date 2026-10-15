package setting

import (
	"fmt"
	"strings"
)

// SettingChunkType 是设定在向量表中的 chunk 类型标识，与章节块
// (summary/content/chapter_brief) 区分，检索时用 ChunkTypes 过滤。
const SettingChunkType = "setting"

// settingChunkID 返回设定在向量表中的稳定 chunk id，用于幂等 upsert 与删除。
func settingChunkID(id int64) string { return fmt.Sprintf("setting:%d", id) }

// parseSettingID 从 chunk id（"setting:<id>"）解析设定主键。
func parseSettingID(chunkID string) int64 {
	const prefix = "setting:"
	if !strings.HasPrefix(chunkID, prefix) {
		return 0
	}
	var id int64
	if _, err := fmt.Sscanf(chunkID[len(prefix):], "%d", &id); err != nil {
		return 0
	}
	return id
}

// splitCatContent 将 "分类：内容" 形式的块文本拆回分类与内容。
func splitCatContent(s string) (string, string) {
	const sep = "："
	if i := strings.Index(s, sep); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(sep):])
	}
	return "", s
}
