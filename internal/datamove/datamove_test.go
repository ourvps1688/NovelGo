package datamove

import (
	"os"
	"path/filepath"
	"testing"
)

// makeTree 在 root 下创建固定结构：a.txt(内容"a")、sub/b.txt(内容"b")。返回 root。
func makeTree(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatal(err)
	}
}

// assertMoved 校验 src 内容已完整迁移到 dst，且 src 已清空。
func assertMoved(t *testing.T, src, dst string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Fatalf("dst 缺少 a.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "b.txt")); err != nil {
		t.Fatalf("dst 缺少 sub/b.txt: %v", err)
	}
	// src 应被清空/删除
	if entries, err := os.ReadDir(src); err == nil && len(entries) != 0 {
		t.Fatalf("src 未被清空，仍有 %d 个条目", len(entries))
	}
}

func TestRelocate_SameDisk(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, "dst")
	makeTree(t, src)

	var gotDone, gotTotal int64
	err := Relocate(src, dst, Options{
		Progress: func(done, total int64) { gotDone, gotTotal = done, total },
	})
	if err != nil {
		t.Fatalf("Relocate 失败: %v", err)
	}
	assertMoved(t, src, dst)
	if gotTotal <= 0 {
		t.Fatalf("进度 total 应 > 0，实际 %d", gotTotal)
	}
	if gotDone != gotTotal {
		t.Fatalf("进度 done(%d) != total(%d)", gotDone, gotTotal)
	}
}

func TestRelocate_SamePath(t *testing.T) {
	base := t.TempDir()
	if err := Relocate(base, base, Options{}); err != ErrSamePath {
		t.Fatalf("期望 ErrSamePath，实际 %v", err)
	}
}

func TestRelocate_NestedRejected(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	inner := filepath.Join(src, "inner")
	makeTree(t, src)
	if err := Relocate(src, inner, Options{}); err != ErrNestedPath {
		t.Fatalf("期望 ErrNestedPath，实际 %v", err)
	}
	// 反向嵌套也应拒绝
	if err := Relocate(inner, src, Options{}); err != ErrNestedPath {
		t.Fatalf("反向嵌套期望 ErrNestedPath，实际 %v", err)
	}
}

func TestRelocate_DstNotEmptyNoForce(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, "dst")
	makeTree(t, src)
	// dst 已非空
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "existing.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Relocate(src, dst, Options{}); err != ErrDstNotEmpty {
		t.Fatalf("期望 ErrDstNotEmpty，实际 %v", err)
	}
	// 未授权时不应改动 src
	if _, err := os.Stat(filepath.Join(src, "a.txt")); err != nil {
		t.Fatalf("未授权迁移不应改动 src: %v", err)
	}
}

func TestRelocate_DstNotEmptyForce(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	dst := filepath.Join(base, "dst")
	makeTree(t, src)
	// dst 已有不冲突的文件 + 与 src 冲突的文件（dst/sub/b.txt 内容不同）
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "existing.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dst, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "sub", "b.txt"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Relocate(src, dst, Options{Force: true}); err != nil {
		t.Fatalf("Force 迁移失败: %v", err)
	}
	// src 内容已并入 dst
	if _, err := os.Stat(filepath.Join(dst, "a.txt")); err != nil {
		t.Fatalf("dst 缺少并入的 a.txt: %v", err)
	}
	// 冲突项以源（src）为准
	got, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt"))
	if string(got) != "b" {
		t.Fatalf("冲突项应以源为准，实际 %q", string(got))
	}
	// 不冲突的原 dst 文件保留
	if _, err := os.Stat(filepath.Join(dst, "existing.txt")); err != nil {
		t.Fatalf("原 dst 不冲突文件应保留: %v", err)
	}
}

func TestCopyTreeAndRemoveTree(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	makeTree(t, src)
	dst := filepath.Join(base, "dst")

	var done int64
	if err := copyTree(src, dst, &done, 100, Options{}); err != nil {
		t.Fatalf("copyTree 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "sub", "b.txt")); err != nil {
		t.Fatalf("copyTree 结果缺失: %v", err)
	}
	if err := removeTree(dst); err != nil {
		t.Fatalf("removeTree 失败: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("removeTree 后 dst 应不存在: %v", err)
	}
}

func TestIsSubpath(t *testing.T) {
	cases := []struct {
		child, parent string
		want          bool
	}{
		{"/a/b", "/a", true},
		{"/a", "/a/b", false},
		{"/a", "/a", false},
		{"/x", "/y", false},
	}
	for _, c := range cases {
		if got := isSubpath(c.child, c.parent); got != c.want {
			t.Fatalf("isSubpath(%q,%q)=%v, want %v", c.child, c.parent, got, c.want)
		}
	}
}
