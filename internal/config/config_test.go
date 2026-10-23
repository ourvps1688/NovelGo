package config

import (
	"path/filepath"
	"testing"

	"github.com/novelgo/novelgo/internal/testsupport"
)

func TestExpandTilde_WithHome(t *testing.T) {
	home := testsupport.RequireEnv(t, "HOME")
	result := expandTilde("~/data")
	// 期望值用 filepath.Join（expandTilde 内部经 filepath.Clean/Join），
	// Windows 反斜杠分隔符下硬拼 "/" 会误报。
	want := filepath.Join(home, "data")
	if result != want {
		t.Errorf("expected %s, got %s", want, result)
	}
}

func TestExpandTilde_NoTilde(t *testing.T) {
	result := expandTilde("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("expected /absolute/path, got %s", result)
	}
}

func TestExpandTilde_Empty(t *testing.T) {
	home := testsupport.RequireEnv(t, "HOME")
	result := expandTilde("")
	// 空字符串等价于 "~"，直接返回 home 目录
	if result != home {
		t.Errorf("expected %s, got %s", home, result)
	}
}

func TestExpandTilde_OnlyTilde(t *testing.T) {
	home := testsupport.RequireEnv(t, "HOME")
	result := expandTilde("~")
	if result != home {
		t.Errorf("expected %s, got %s", home, result)
	}
}
