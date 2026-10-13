package review

import (
	"context"
	"testing"
)

// fakeLLM 是 LLMClient 的桩实现，用于测试评分引擎解析路径。
type fakeLLM struct {
	out string
	err error
}

func (f fakeLLM) GenerateText(ctx context.Context, providerName string, messages []map[string]any, model string) (string, error) {
	return f.out, f.err
}

func TestParseScore_PureJSON(t *testing.T) {
	raw := `{"overall":72,"dimensions":{"word_count":80,"plot_advance":70},"verdict":"pass","critique":"不错"}`
	r, err := parseScore(raw)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if r.Overall != 72 {
		t.Fatalf("overall=%d", r.Overall)
	}
	if r.Dimensions["word_count"] != 80 {
		t.Fatalf("dim word_count=%d", r.Dimensions["word_count"])
	}
}

func TestParseScore_Fenced(t *testing.T) {
	raw := "```json\n{\"overall\":50,\"dimensions\":{},\"verdict\":\"fail\",\"critique\":\"X\"}\n```"
	r, err := parseScore(raw)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if r.Overall != 50 {
		t.Fatalf("overall=%d", r.Overall)
	}
}

func TestParseScore_Embedded(t *testing.T) {
	raw := "以下是评分：{\"overall\":90,\"dimensions\":{},\"verdict\":\"pass\",\"critique\":\"好\"} 完毕"
	r, err := parseScore(raw)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if r.Overall != 90 {
		t.Fatalf("overall=%d", r.Overall)
	}
}

func TestParseScore_Invalid(t *testing.T) {
	if _, err := parseScore("no json here"); err == nil {
		t.Fatal("expected err for non-JSON input")
	}
}

func TestClampScore(t *testing.T) {
	if clampScore(-5) != 0 {
		t.Fatal("negative should clamp to 0")
	}
	if clampScore(150) != 100 {
		t.Fatal("overflow should clamp to 100")
	}
	if clampScore(42) != 42 {
		t.Fatal("in-range should pass through")
	}
}

func TestScoreChapter_OK(t *testing.T) {
	f := fakeLLM{out: `{"overall":40,"dimensions":{"word_count":30},"verdict":"fail","critique":"太短"}`}
	r, err := ScoreChapter(context.Background(), f, "p", "m", "正文内容", "", 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if r.Overall != 40 {
		t.Fatalf("overall=%d", r.Overall)
	}
	if r.Critique != "太短" {
		t.Fatalf("critique=%q", r.Critique)
	}
}

func TestScoreChapter_LLMError(t *testing.T) {
	f := fakeLLM{err: context.Canceled}
	if _, err := ScoreChapter(context.Background(), f, "p", "m", "x", "", 10); err == nil {
		t.Fatal("expected err when LLM fails")
	}
}
