package mcp_tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type coerceSampleItem struct {
	Name      string            `json:"name"`
	Abilities string            `json:"abilities"`
	Person    string            `json:"personality"`
	Count     int               `json:"count"`
	Ptr       *string           `json:"ptr"`
	Meta      map[string]string `json:"meta"`
}

type coerceSampleArgs struct {
	Characters []coerceSampleItem `json:"characters"`
	Single     coerceSampleItem   `json:"single"`
	Params     string             `json:"params"`
	Extra      string             `json:"-"` // 不参与 JSON
	unexported string             //nolint:unused // 覆盖未导出字段跳过分支
}

// TestCoerceJSONTextArgsArrayObjectIntoString 核心修复：string 字段收到
// 数组/对象 → 紧凑 JSON 文本（即线上报错 "cannot unmarshal array into …
// of type string" 的场景）。
func TestCoerceJSONTextArgsArrayObjectIntoString(t *testing.T) {
	typ := reflect.TypeOf(coerceSampleArgs{})
	raw := json.RawMessage(`{
		"characters": [
			{"name": "林川", "abilities": ["剑术", "隐身"], "personality": {"brief": "热血青年", "traits": ["勇敢"]}},
			{"name": "苏晚", "abilities": "字符串形式JSON", "personality": null}
		],
		"params": {"character": "主角", "before_reading_number": 50},
		"single": {"name": "旁白", "meta": {"a": ["x"]}}
	}`)

	out := coerceJSONTextArgs(typ, raw)
	var args coerceSampleArgs
	if err := json.Unmarshal(out, &args); err != nil {
		t.Fatalf("coerced args should decode: %v\n%s", err, out)
	}
	if args.Characters[0].Abilities != `["剑术","隐身"]` {
		t.Errorf("abilities: got %s", args.Characters[0].Abilities)
	}
	if args.Characters[0].Person != `{"brief":"热血青年","traits":["勇敢"]}` {
		t.Errorf("personality: got %s", args.Characters[0].Person)
	}
	if args.Characters[1].Abilities != "字符串形式JSON" {
		t.Errorf("string form must be preserved: got %s", args.Characters[1].Abilities)
	}
	if args.Characters[1].Person != "" {
		t.Errorf("null should decode to empty: got %s", args.Characters[1].Person)
	}
	if args.Params != `{"before_reading_number":50,"character":"主角"}` {
		t.Errorf("params object: got %s", args.Params)
	}
	if args.Single.Meta["a"] != `["x"]` {
		t.Errorf("map[string]string values coerced too: got %s", args.Single.Meta["a"])
	}
}

// TestCoerceJSONTextArgsNoop 原样保留：未触发改写时返回原始字节（不重排 key、
// 不动数值），字符串/数值/布尔/未知字段/语法错误全部透传。
func TestCoerceJSONTextArgsNoop(t *testing.T) {
	typ := reflect.TypeOf(coerceSampleArgs{})
	cases := []string{
		`{"single":{"name":"a","count":3}}`,
		`{"single":{"name":123}}`,   // 数值进 string 是真类型错误，保留报错
		`{"single":{"name":true}}`,  // 布尔同理
		`{"unknown_key":{"a":[1]}}`, // 未知字段不归一化（由 DisallowUnknownFields 报错）
		`{"params":"{\"a\":1}"}`,    // 字符串形式 JSON 原样保留
		`{not json`,                 // 语法错误透传
		``,                          // 空输入透传
		`{"single":{"name":"x","count":12345678901234567890}}`, // 大整数不得被 float64 化
	}
	for _, raw := range cases {
		out := coerceJSONTextArgs(typ, json.RawMessage(raw))
		if string(out) != raw {
			t.Errorf("expected passthrough for %s\ngot  %s", raw, out)
		}
	}
}

// TestCoerceJSONTextArgsNumberPrecision 改写触发时数值仍须走 json.Number
// 透传，避免 2^53 以上整数精度损失。
func TestCoerceJSONTextArgsNumberPrecision(t *testing.T) {
	typ := reflect.TypeOf(coerceSampleArgs{})
	raw := json.RawMessage(`{"params":{"big":1234567890123456789},"single":{"abilities":[1]}}`)
	out := coerceJSONTextArgs(typ, raw)
	// params 值转文本后引号会被转义，故只断言数位原样（float64 化会变成 1.2345678901234568e+18）。
	if got := string(out); !strings.Contains(got, `1234567890123456789`) {
		t.Errorf("big integer must round-trip verbatim, got %s", got)
	}
}
