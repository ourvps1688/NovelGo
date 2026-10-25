// Package schemaof 是 JSON Schema 生成的叶子工具包（OpenAI function calling 兼容）。
//
// 从带 jsonschema tag 的 struct 生成参数 schema，供 MCP 工具定义与技能输出契约共用。
// 原居 mcp_tools；因 internal/style（技能输出 schema）与 internal/quality（章节 CI）
// 都需要它、而 mcp_tools 会被二者经写后钩子间接引用，故下放叶子包破除 import 环。
// 行为与原 mcp_tools.SchemaOf 逐字一致；mcp_tools.SchemaOf 保留为薄委托，调用方零改动。
package schemaof

import (
	"encoding/json"
	"strings"

	"github.com/invopop/jsonschema"
)

// schemaReflector 是全局配置，RequiredFromJSONSchemaTags=true 配合 jsonschema tag 的 required 选项。
// ExpandedStruct 让输出内联不含 $ref/$defs。
var schemaReflector = &jsonschema.Reflector{
	RequiredFromJSONSchemaTags: true,
	ExpandedStruct:             true,
}

// Of 从带 jsonschema tag 的 struct 生成 OpenAI function calling 兼容的 JSON Schema。
// 内部用 github.com/invopop/jsonschema 生成，取 type/properties/required 并将 $defs 内联到 $ref 位置。
func Of(v any) json.RawMessage {
	s := schemaReflector.Reflect(v)

	b, _ := json.Marshal(s)
	var full map[string]any
	_ = json.Unmarshal(b, &full)

	clean := map[string]any{"type": full["type"]}
	if props, ok := full["properties"]; ok {
		clean["properties"] = props
	}
	if req, ok := full["required"]; ok {
		clean["required"] = req
	}
	if defs, ok := full["$defs"]; ok {
		clean["$defs"] = defs
	}

	resolved := inlineDefs(clean)
	raw, _ := json.Marshal(resolved)
	return raw
}

// inlineDefs 将 $defs 中的定义递归替换到 $ref 引用位置，消除跨节点跳转。
func inlineDefs(schema map[string]any) map[string]any {
	defs, _ := schema["$defs"].(map[string]any)
	delete(schema, "$defs")
	return resolveRefs(schema, defs).(map[string]any)
}

func resolveRefs(node any, defs map[string]any) any {
	switch v := node.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok && defs != nil {
			name := strings.TrimPrefix(ref, "#/$defs/")
			if def, ok := defs[name].(map[string]any); ok {
				return resolveRefs(def, defs)
			}
		}
		for k, val := range v {
			v[k] = resolveRefs(val, defs)
		}
		return v
	case []any:
		for i, val := range v {
			v[i] = resolveRefs(val, defs)
		}
		return v
	}
	return node
}
