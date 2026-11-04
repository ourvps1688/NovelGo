package mcp_tools

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// coerceJSONTextArgs 修正「JSON 文本字段」的参数形态不匹配。
//
// 背景（真实线上报错）：create_character 的 abilities/personality 等字段在 Go 侧
// 声明为 string（存储 JSON 文本），但工具描述教 LLM 传 JSON 数组/对象——LLM 照做后
// 反序列化直接炸：「json cannot unmarshal array into Go struct field
// CreateCharacterArgs.characters.0.abilities of type string」。同类字段遍布
// character/location/timeline/invariant 四组工具，且 update 工具的 PATCH 二次
// Unmarshal（tc.RawArgs → DB entity，见 rules.md §9）同样会炸。
//
// 在唯一反序列化入口（Registry.Execute）按 Args 结构把 rawArgs 中「声明为 string 的
// 字段」收到的 JSON 数组/对象值就地改写为紧凑 JSON 文本，两条解码路径同时修复：
//   - 字符串原文（含字符串形式 JSON）与 null 原样保留；
//   - 其它字段类型与嵌套结构原样保留；数值经 json.Number 透传不做 float64 转换，
//     避免大整数精度损失；
//   - 数值/布尔不强制转字符串（那是真正的类型错误，保留报错让 LLM 自纠）；
//   - 无可改写时返回原始字节，避免重排 key 顺序等无谓扰动。
func coerceJSONTextArgs(t reflect.Type, raw json.RawMessage) json.RawMessage {
	if t == nil || len(raw) == 0 {
		return raw
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		// JSON 语法错误原样透传，交给正常 Decode 报错（保持错误文案稳定）。
		return raw
	}
	tree, changed := coerceJSONTextNode(tree, t)
	if !changed {
		return raw
	}
	out, err := json.Marshal(tree)
	if err != nil {
		return raw
	}
	return out
}

// coerceJSONTextNode 递归对齐（JSON 值, Go 类型），仅改写 string 类型字段上的
// 数组/对象值；返回值与是否发生改写。
func coerceJSONTextNode(v any, t reflect.Type) (any, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return v, false
		}
		changed := false
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := jsonFieldName(f)
			if name == "" {
				continue
			}
			fv, ok := m[name]
			if !ok {
				continue
			}
			nv, c := coerceJSONTextNode(fv, f.Type)
			if c {
				m[name] = nv
				changed = true
			}
		}
		return m, changed

	case reflect.Slice, reflect.Array:
		arr, ok := v.([]any)
		if !ok {
			return v, false
		}
		changed := false
		for i := range arr {
			nv, c := coerceJSONTextNode(arr[i], t.Elem())
			if c {
				arr[i] = nv
				changed = true
			}
		}
		return arr, changed

	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return v, false
		}
		changed := false
		for k, fv := range m {
			nv, c := coerceJSONTextNode(fv, t.Elem())
			if c {
				m[k] = nv
				changed = true
			}
		}
		return m, changed

	case reflect.String:
		switch v.(type) {
		case []any, map[string]any:
			b, err := json.Marshal(v)
			if err != nil {
				return v, false
			}
			return string(b), true
		}
		return v, false
	}
	return v, false
}

// jsonFieldName 取字段的 JSON 名（encoding/json 规则的子集：tag 首段；"-" 跳过；
// 无 tag 或首段为空用字段名）。Args 结构都是平铺 tag 显式命名，子集足够。
func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return f.Name
	}
	return name
}
