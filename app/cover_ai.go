package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/novelgo/novelgo/internal/llm"
	"github.com/novelgo/novelgo/internal/novel"
)

// ── AI 封面生成 ───────────────────────────────────────────
//
// 复用用户已配置的 LLM Provider（ChatURL + APIKey），不引入新的密钥。
// 流程：LLM 依据小说标题/题材/描述生成「绘画提示词」→ 调用该 Provider 的图像端点生成封面 → 复用 SaveCover 落库。

// GenerateCoverPromptInput 是生成封面绘画提示词的入参（预留，目前 prompt 由后端依据小说元信息生成）。
type GenerateCoverInput struct {
	NovelID      int64  `json:"novel_id"`
	Prompt       string `json:"prompt"`        // 用户可编辑的绘画提示词；空则后端自动生成
	ProviderName string `json:"provider_name"` // 复用哪个已配置 Provider 的图像端点
	Model        string `json:"model"`         // 图像模型，留空走默认 image_model/gpt-image-1
	Size         string `json:"size"`          // 如 1024x1024，留空默认 1024x1024
}

// GenerateCoverPrompt 调用 LLM 依据小说元信息生成一份封面绘画提示词，供用户预览/编辑。
func (a *App) GenerateCoverPrompt(novelID int64) (string, error) {
	var n novel.Novel
	if err := a.novel.DB.WithContext(a.ctx).First(&n, novelID).Error; err != nil {
		return "", fmt.Errorf("generate cover prompt: %w", err)
	}

	providerName, modelID, err := a.defaultProviderModel()
	if err != nil {
		return "", err
	}

	messages := []map[string]any{
		{
			"role": "system",
			"content": "你是一名专业的图书封面插画师。请基于用户提供的小说信息，输出一段用于 AI 图像生成的英文绘画提示词（prompt）。" +
				"要求：1) 突出书名气质与题材核心；2) 给出具体的视觉构图、主体、色调、光影与氛围；3) 避免任何文字/标题出现在画面中；" +
				"4) 只输出提示词本身，不要解释、不要引号、不要 Markdown。控制在 200 词以内。",
		},
		{
			"role":    "user",
			"content": fmt.Sprintf("书名：%s\n题材：%s\n简介：%s", n.Title, n.Genre, n.Description),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompt, err := a.llmClient.GenerateText(ctx, providerName, messages, modelID)
	if err != nil {
		return "", fmt.Errorf("generate cover prompt: %w", err)
	}
	return strings.TrimSpace(prompt), nil
}

// GenerateCover 调用 Provider 的图像端点生成封面，并复用 SaveCover 落库提交到 Git。
func (a *App) GenerateCover(input GenerateCoverInput) error {
	if strings.TrimSpace(input.Prompt) == "" {
		return fmt.Errorf("封面提示词不能为空")
	}

	// 1) 选定 Provider（优先前端指定，否则回退默认可用 Provider）
	providerName := input.ProviderName
	var p llm.Provider
	if providerName != "" {
		pp, ok := a.llmClient.Providers()[providerName]
		if !ok {
			return fmt.Errorf("未找到指定的 LLM Provider: %s", providerName)
		}
		p = pp
	} else {
		name, _, err := a.defaultProviderModel()
		if err != nil {
			return err
		}
		p = a.llmClient.Providers()[name]
	}
	if p.APIKey == "" {
		return fmt.Errorf("该 Provider 未配置 API Key，无法生成封面")
	}

	// 2) 图像端点：显式配置优先，否则按 ChatURL 推导
	imageURL := a.settingsSnapshot().ImageURL
	if imageURL == "" {
		imageURL = deriveImageURL(p.ChatURL)
	}
	if imageURL == "" {
		return fmt.Errorf("无法确定图像生成端点（ChatURL 为空且未配置 image_url）")
	}

	// 3) 图像模型与尺寸
	model := input.Model
	if model == "" {
		model = a.settingsSnapshot().ImageModel
	}
	if model == "" {
		model = "gpt-image-1"
	}
	size := input.Size
	if size == "" {
		size = "1024x1024"
	}

	payload := map[string]any{
		"model":           model,
		"prompt":          input.Prompt,
		"n":               1,
		"size":            size,
		"response_format": "b64_json",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("封面请求序列化失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, imageURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("封面请求创建失败: %w", err)
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + p.APIKey,
	}
	if p.BuildHeaders != nil {
		headers = p.BuildHeaders(nil, headers)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := a.llmClient.HTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("封面请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取封面响应失败: %w", err)
	}

	if resp.StatusCode >= 400 {
		msg := parseImageError(respBody, p)
		return fmt.Errorf("封面生成失败 [%d]: %s", resp.StatusCode, msg)
	}

	imgBytes, err := decodeImageResponse(respBody)
	if err != nil {
		return err
	}

	// 4) 复用现有封面落库流程
	if err := a.SaveCover(input.NovelID, imgBytes); err != nil {
		return fmt.Errorf("保存封面失败: %w", err)
	}
	return nil
}

// defaultProviderModel 返回第一个配置了 APIKey 且有模型的 Provider，用于提示词生成与缺省图像 Provider。
func (a *App) defaultProviderModel() (string, string, error) {
	for name, p := range a.llmClient.Providers() {
		if p.APIKey == "" || len(p.Models) == 0 {
			continue
		}
		return name, p.Models[0].ID, nil
	}
	return "", "", fmt.Errorf("没有配置可用的 LLM Provider（缺少 API Key）")
}

// deriveImageURL 从 ChatURL 推导 OpenAI 兼容的图像生成端点。
// 常见形态：.../chat/completions → .../images/generations；.../v1/chat/completions → .../v1/images/generations。
func deriveImageURL(chatURL string) string {
	if chatURL == "" {
		return ""
	}
	u, err := url.Parse(chatURL)
	if err != nil || u.Host == "" {
		return ""
	}
	switch {
	case strings.HasSuffix(u.Path, "/chat/completions"):
		u.Path = strings.TrimSuffix(u.Path, "chat/completions") + "images/generations"
	case strings.HasSuffix(u.Path, "/completions"):
		u.Path = strings.TrimSuffix(u.Path, "completions") + "images/generations"
	default:
		u.Path = strings.TrimRight(u.Path, "/") + "/images/generations"
	}
	return u.String()
}

// decodeImageResponse 从图像端点响应中提取 PNG/JPG 字节，兼容 b64_json 与 url 两种返回。
func decodeImageResponse(respBody []byte) ([]byte, error) {
	var parsed struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("解析图像响应失败: %w", err)
	}
	if len(parsed.Data) == 0 {
		if parsed.Error.Message != "" {
			return nil, fmt.Errorf("图像服务返回错误: %s", parsed.Error.Message)
		}
		return nil, fmt.Errorf("图像响应中没有可用的图片数据")
	}
	if parsed.Data[0].B64JSON != "" {
		decoded, err := base64.StdEncoding.DecodeString(parsed.Data[0].B64JSON)
		if err != nil {
			return nil, fmt.Errorf("封面 base64 解码失败: %w", err)
		}
		return decoded, nil
	}
	if parsed.Data[0].URL != "" {
		// 部分兼容端点只返回临时 url，需要再拉取一次
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.Data[0].URL, nil)
		if err != nil {
			return nil, fmt.Errorf("拉取图像 url 失败: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("拉取图像 url 失败: %w", err)
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}
	return nil, fmt.Errorf("图像响应既无 b64_json 也无 url")
}

// parseImageError 解析非 2xx 图像响应，优先使用 Provider 自定义错误解析。
func parseImageError(body []byte, p llm.Provider) string {
	if p.ParseError != nil {
		if err := p.ParseError(body); err != nil {
			return err.Error()
		}
	}
	// 兜底：尝试 OpenAI 标准 error.message
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		return parsed.Error.Message
	}
	return strings.TrimSpace(string(body))
}
