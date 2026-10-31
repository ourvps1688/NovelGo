//go:build cgo

package rag

import (
	"testing"
)

// TestSplitTokenWindows 锁定长文分段聚合的切窗纯逻辑（不依赖 ONNX / tokenizer）。
// 这是 P1 长文分段聚合正确性的最小不变量：非重叠、覆盖全部 token、单窗 ≤ winSize。
func TestSplitTokenWindows(t *testing.T) {
	win := bertMaxLen - 2 // 510

	cases := []struct {
		name    string
		tokens  []int
		winSize int
		wantN   int
	}{
		{"空序列", nil, win, 0},
		{"短于窗口", []int{1, 2, 3}, win, 1},
		{"恰好窗口", seq(1, win), win, 1},
		{"略超窗口", seq(1, win+5), win, 2},
		{"整两倍+余数", seq(1, win*2+3), win, 3},
		{"窗口小于序列", seq(1, 10), 4, 3},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitTokenWindows(c.tokens, c.winSize)
			if len(got) != c.wantN {
				t.Fatalf("窗口数 = %d, want %d", len(got), c.wantN)
			}
			// 非重叠且完整覆盖。
			var rebuilt []int
			for _, w := range got {
				if len(w) > c.winSize {
					t.Fatalf("单窗长度 %d 超过窗口上限 %d", len(w), c.winSize)
				}
				rebuilt = append(rebuilt, w...)
			}
			if len(rebuilt) != len(c.tokens) {
				t.Fatalf("覆盖 token 数 = %d, want %d", len(rebuilt), len(c.tokens))
			}
			for i := range c.tokens {
				if rebuilt[i] != c.tokens[i] {
					t.Fatalf("位置 %d 覆盖不一致: got %d, want %d", i, rebuilt[i], c.tokens[i])
				}
			}
		})
	}
}

func seq(start, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = start + i
	}
	return out
}
