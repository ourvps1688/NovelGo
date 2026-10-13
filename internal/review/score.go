package review

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// LLMClient 是评分判官所需的 LLM 能力抽象，便于测试时注入桩实现。
// *llm.Client 的方法签名与之兼容（GenerateText(ctx, providerName, messages, model)）。
type LLMClient interface {
	GenerateText(ctx context.Context, providerName string, messages []map[string]any, model string) (string, error)
}

// ScoreResult 是评分判官的输出。
type ScoreResult struct {
	Overall    int               `json:"overall"`              // 总分 0-100
	Dimensions map[string]int    `json:"dimensions"`           // 分维度得分（0-100）
	Verdict    string            `json:"verdict"`              // "pass" | "fail"（判官自评，调用方以总分+阈值准）
	Critique   string            `json:"critique"`             // 具体、可执行的修改建议（中文）
	Raw        string            `json:"-"`                    // 原始模型输出，便于排查
}

// scoringDimensions 定义评分维度与权重说明，注入给判官。
var scoringDimensions = []struct {
	Key   string
	Label string
	Desc  string
}{
	{"word_count", "字数达标度", "是否达到大纲/题材要求的字数，有无注水凑数"},
	{"plot_advance", "情节推进", "本章是否推进了至少一条主线，是否有明确的状态改变"},
	{"emotion_hook", "情绪流与爽点", "情绪曲线是否有起伏，爽文/题材是否有满足感节点，章末是否有钩子"},
	{"consistency", "人设与一致性", "角色性格/称呼/关系、时间线、已确立设定是否前后一致"},
	{"deai", "去AI味", "是否规避破折号等硬伤，及模板化比喻/微表情/套词密度是否过高"},
	{"taboo", "禁忌合规", "有无破墙表达、现实人名/品牌/地名、吃书等硬禁忌"},
}

// buildSystemPrompt 构造评分判官的系统提示词。
func buildSystemPrompt() string {
	var b strings.Builder
	b.WriteString("你是一位严谨的小说章节评审专家，对刚写完的一章进行客观评分。\n")
	b.WriteString("请基于以下维度逐一打分（每项 0-100，越高越好），并给出总分与可执行修改意见：\n")
	for _, d := range scoringDimensions {
		fmt.Fprintf(&b, "- %s（%s）：%s\n", d.Label, d.Key, d.Desc)
	}
	b.WriteString("\n评分原则：\n")
	b.WriteString("1. 总分 overall 是各维度的综合，不要无脑给满分；明显质量问题必须扣分。\n")
	b.WriteString("2. critique 必须具体、可执行：指出问题出现在哪类内容，建议如何改，不要空泛夸奖。\n")
	b.WriteString("3. 字数达标度：若章节明显过短或大量注水凑字数，该项应明显偏低。\n")
	b.WriteString("4. 只评正文本身的写作质量，不评价是否契合用户个人口味。\n\n")
	b.WriteString("严格只输出一个 JSON 对象（不要包含 ```json 代码围栏或任何额外文字），结构如下：\n")
	b.WriteString(`{
  "overall": 0,
  "dimensions": {"word_count": 0, "plot_advance": 0, "emotion_hook": 0, "consistency": 0, "deai": 0, "taboo": 0},
  "verdict": "pass",
  "critique": "具体修改建议……"
}`)
	return b.String()
}

// buildUserPrompt 构造用户消息：章节大纲（如有）+ 正文 + 字数信息。
func buildUserPrompt(content, outline string, wordCount int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("本章正文（中文字数约 %d）：\n\n", wordCount))
	b.WriteString("===== 正文开始 =====\n")
	b.WriteString(content)
	b.WriteString("\n===== 正文结束 =====\n")
	if outline != "" {
		b.WriteString("\n本章大纲（供对照目标与字数预期）：\n")
		b.WriteString(outline)
	}
	return b.String()
}

// ScoreChapter 调用 LLM 对章节正文评分。
// outline 可为空（新建章节尚无 id 时）；wordCount 由调用方用 text.ComputeStats 计算。
// 评分失败时返回 error，调用方应放行（不阻断写作），由门禁决定降级行为。
func ScoreChapter(ctx context.Context, client LLMClient, provider, model string, content, outline string, wordCount int) (ScoreResult, error) {
	if client == nil {
		return ScoreResult{}, fmt.Errorf("review: nil LLM client")
	}
	messages := []map[string]any{
		{"role": "system", "content": buildSystemPrompt()},
		{"role": "user", "content": buildUserPrompt(content, outline, wordCount)},
	}
	raw, err := client.GenerateText(ctx, provider, messages, model)
	if err != nil {
		return ScoreResult{}, fmt.Errorf("review: LLM 评分失败: %w", err)
	}
	res, err := parseScore(raw)
	if err != nil {
		return ScoreResult{}, err
	}
	res.Raw = raw
	return res, nil
}

// jsonBlockRE 匹配 ```json ... ``` 或 ``` ... ``` 代码围栏。
var jsonBlockRE = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// parseScore 从模型输出中稳健提取 JSON 并解析为 ScoreResult。
// 兼容：纯 JSON、```json 围栏、以及夹杂在解释文字中的 JSON。
func parseScore(raw string) (ScoreResult, error) {
	clean := strings.TrimSpace(raw)

	// 1) 直接整体解析
	if strings.HasPrefix(clean, "{") {
		if idx := strings.LastIndex(clean, "}"); idx >= 0 {
			if r, err := decodeScore(clean[:idx+1]); err == nil {
				return r, nil
			}
		}
	}

	// 2) 代码围栏
	if m := jsonBlockRE.FindStringSubmatch(clean); m != nil {
		if r, err := decodeScore(m[1]); err == nil {
			return r, nil
		}
	}

	// 3) 从任意位置截取首个 { 到末尾最后一个 }
	if start := strings.Index(clean, "{"); start >= 0 {
		if end := strings.LastIndex(clean, "}"); end > start {
			if r, err := decodeScore(clean[start : end+1]); err == nil {
				return r, nil
			}
		}
	}

	return ScoreResult{}, fmt.Errorf("review: 无法从模型输出解析评分 JSON")
}

// decodeScore 解析并做范围裁剪与兜底。
func decodeScore(s string) (ScoreResult, error) {
	var r ScoreResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return ScoreResult{}, err
	}
	r.Overall = clampScore(r.Overall)
	if r.Dimensions == nil {
		r.Dimensions = map[string]int{}
	}
	for k, v := range r.Dimensions {
		r.Dimensions[k] = clampScore(v)
	}
	r.Verdict = strings.ToLower(strings.TrimSpace(r.Verdict))
	return r, nil
}

// clampScore 将分数裁剪到 0-100。
func clampScore(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
