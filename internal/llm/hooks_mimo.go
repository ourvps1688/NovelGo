package llm

import "strings"

// mimoBuildHeaders 替换标准 Authorization: Bearer <key> 为 Mimo 专用的 api-key 鉴权头。
// 不消费 opts（Mimo 无需会话感知），签名统一以便 Provider.BuildHeaders 传参。
func mimoBuildHeaders(_ *CallOptions, base map[string]string) map[string]string {
	key := strings.TrimPrefix(base["Authorization"], "Bearer ")
	base["api-key"] = key
	delete(base, "Authorization")
	return base
}

// mimoBuildRequest 适配小米 MiMo 与标准 OpenAI 格式的差异：
// MiMo 推理模型以 max_completion_tokens 控制「思考 + 回答」的总长度，而
// buildPayload 默认只发 max_tokens（整数）。两者并存时 MiMo 以
// max_completion_tokens 为准，故在此复用同一个值补上，避免长生成（整本蓝图、
// 整章覆写）被截断到 MiMo 的默认长度。
func mimoBuildRequest(payload map[string]any) map[string]any {
	if mt, ok := payload["max_tokens"].(int); ok {
		payload["max_completion_tokens"] = mt
	}
	return payload
}
