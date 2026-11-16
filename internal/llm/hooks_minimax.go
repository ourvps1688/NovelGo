package llm

// minimaxBuildRequest 适配 MiniMax 与标准 OpenAI 格式的差异：
// - reasoning_split 将思考内容从 <think> 标签分离到 reasoning_content 字段
// - thinking.type 从 "enabled" 映射为 MiniMax 用的 "adaptive"
func minimaxBuildRequest(payload map[string]any) map[string]any {
	payload["reasoning_split"] = true

	if thinking, ok := payload["thinking"].(map[string]string); ok {
		switch thinking["type"] {
		case "enabled":
			thinking["type"] = "adaptive"
		case "disabled":
			// MiniMax 无 disabled 语义：显式关闭思考时退化为「不传 thinking」，
			// 避免把不被识别的 type 发给服务端；长思维由调用方的推理护栏兜底。
			delete(payload, "thinking")
		}
	}

	return payload
}
