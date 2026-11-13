package text

import (
	"regexp"
	"strings"
	"unicode"
)

var enWordRe = regexp.MustCompile(`[a-zA-Z]+(?:'[a-zA-Z]+)?`)

// Stats 是文本的详细统计信息。
type Stats struct {
	ChineseChars     int `json:"chinese_chars"`
	EnglishWords     int `json:"english_words"`
	WordCount        int `json:"word_count"` // 中文字符 + 英文单词
	LineCount        int `json:"line_count"`
	CharCountSpace   int `json:"char_count_space"`   // 计空格
	CharCountNoSpace int `json:"char_count_nospace"` // 不计空格
	ParagraphCount   int `json:"paragraph_count"`
}

// ComputeStats 返回文本的详细统计。单次遍历处理字符分类。
func ComputeStats(s string) Stats {
	st := Stats{}
	if s == "" {
		return st
	}

	runes := []rune(s)
	st.LineCount = strings.Count(s, "\n") + 1
	st.CharCountSpace = len(runes)

	cjk := 0
	spaces := 0
	inPara := false

	for _, r := range runes {
		if unicode.Is(unicode.Han, r) {
			cjk++
		} else if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			spaces++
		}

		if r == '\n' {
			if inPara {
				st.ParagraphCount++
				inPara = false
			}
		} else if !unicode.IsSpace(r) {
			inPara = true
		}
	}
	if inPara {
		st.ParagraphCount++
	}

	st.ChineseChars = cjk
	st.EnglishWords = len(enWordRe.FindAllString(s, -1))
	st.WordCount = cjk + st.EnglishWords
	st.CharCountNoSpace = st.CharCountSpace - spaces

	return st
}

// TruncateRunes 按 rune 边界把 s 截到至多 n 个 rune，超出时追加省略号。
// #117 P0：此前 retcon / snapshot / storage / structure 各持一份逐字相同的
// 私有实现，统一收到纯文本层单源；app/chapter_precontext.go 的变体追加
// 「（已截断）」标注、语义不同，刻意不并入。
func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ContainsAny 判断 text 是否含 signals 中任一非空信号词。
// #117 P0：此前 quality 与 structure 各持一份逐字相同的私有实现。
func ContainsAny(text string, signals []string) bool {
	for _, s := range signals {
		if s != "" && strings.Contains(text, s) {
			return true
		}
	}
	return false
}
