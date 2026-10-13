package app

import (
	"fmt"
	"strings"

	"github.com/novelgo/novelgo/internal/git"
)

// GetChapterContentsInput 是读取小说章节正文的入参。
// ChapterIDs 为空表示读取整本书。
type GetChapterContentsInput struct {
	NovelID    int64   `json:"novel_id"`
	ChapterIDs []int64 `json:"chapter_ids"`
}

// GetChapterContentsResult 返回拼接后的章节正文。
type GetChapterContentsResult struct {
	Content      string `json:"content"`
	ChapterCount int    `json:"chapter_count"`
}

// GetChapterContents 读取指定小说（部分章节或全书）的正文，拼接为单一文本。
// 用于风格素材导入等场景，避免用户手动上传章节内容——可像套路/心智提取一样
// 直接选择小说 + 章节范围，由后端从 Git 仓库读取并拼装。
func (a *App) GetChapterContents(input GetChapterContentsInput) (*GetChapterContentsResult, error) {
	if input.NovelID == 0 {
		return nil, fmt.Errorf("请先选择小说")
	}
	if a.chapter == nil {
		return nil, fmt.Errorf("章节服务未初始化")
	}

	chapters, err := a.chapter.ListAllByNovel(a.ctx, nil, input.NovelID)
	if err != nil {
		return nil, fmt.Errorf("读取章节列表失败: %w", err)
	}
	if len(chapters) == 0 {
		return nil, fmt.Errorf("该小说暂无章节")
	}

	// chapter_ids 为空 → 全书；否则仅选中章节。
	want := make(map[int64]bool, len(input.ChapterIDs))
	for _, id := range input.ChapterIDs {
		want[id] = true
	}

	var b strings.Builder
	count := 0
	for _, ch := range chapters {
		if len(want) > 0 && !want[ch.ID] {
			continue
		}
		content, err := git.ReadFile(input.NovelID, git.ChapterPath(ch.ID))
		if err != nil {
			return nil, fmt.Errorf("读取章节《%s》失败: %w", ch.Title, err)
		}
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		if count > 0 {
			b.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&b, "# %s\n\n", ch.Title)
		b.WriteString(content)
		count++
	}

	if count == 0 {
		return nil, fmt.Errorf("所选章节内容为空")
	}
	return &GetChapterContentsResult{
		Content:      b.String(),
		ChapterCount: count,
	}, nil
}
