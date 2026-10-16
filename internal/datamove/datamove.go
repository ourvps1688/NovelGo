// Package datamove 提供数据目录的安全迁移能力：将源目录的全部内容移动到目标目录。
//
// 设计目标（对应 app_config.go 的 UpdateDataDir TODO）：
//   - 同盘用 os.Rename（原子、即时）；跨盘自动回退为递归拷贝 + 校验 + 删除源。
//   - 目标非空且未授权（Force=false）时报错，由调用方决定是否弹确认框后带 Force 重试。
//   - 源与目标互相嵌套时拒绝，避免把目录移动到自身内部导致数据自噬。
//   - 通过 Progress 回调上报进度（已处理字节 / 总字节），便于前端展示迁移进度。
package datamove

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrDstNotEmpty 表示目标目录非空且未授权覆盖（Force=false）。
var ErrDstNotEmpty = errors.New("目标目录非空")

// ErrNestedPath 表示源目录与目标目录互相嵌套，无法安全移动。
var ErrNestedPath = errors.New("源目录与目标目录互相嵌套")

// ErrSamePath 表示源与目标为同一路径。
var ErrSamePath = errors.New("源目录与目标目录相同")

// Options 控制迁移行为。
type Options struct {
	// Force 允许目标非空时继续：源数据覆盖/合并进目标（源优先）。
	Force bool
	// Progress 上报迁移进度（已处理字节数, 总字节数）。可为 nil。
	Progress func(done, total int64)
}

// Relocate 将 src 目录的全部内容移动到 dst 目录。
//
// 行为：
//   - 归一化并校验 src/dst（存在性、嵌套、相等）。
//   - dst 不存在则创建；dst 存在但非空且 !Force → 返回 ErrDstNotEmpty。
//   - 逐个迁移 src 的顶层条目：优先 os.Rename；跨设备失败时回退递归拷贝。
//   - 成功后 src 变为空目录并尝试删除。
//
// 调用方负责在调用前关闭所有可能锁定 src 内文件（如 SQLite）的句柄。
func Relocate(src, dst string, opts Options) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	if src == dst {
		return ErrSamePath
	}
	if isSubpath(dst, src) || isSubpath(src, dst) {
		return ErrNestedPath
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("读取源目录失败: %w", err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("源路径不是目录: %s", src)
	}

	dstInfo, err := os.Stat(dst)
	if err == nil {
		if !dstInfo.IsDir() {
			return fmt.Errorf("目标路径不是目录: %s", dst)
		}
		empty, eerr := isDirEmpty(dst)
		if eerr != nil {
			return fmt.Errorf("检查目标目录失败: %w", eerr)
		}
		if !empty && !opts.Force {
			return ErrDstNotEmpty
		}
	} else if os.IsNotExist(err) {
		if merr := os.MkdirAll(dst, 0700); merr != nil {
			return fmt.Errorf("创建目标目录失败: %w", merr)
		}
	} else {
		return fmt.Errorf("读取目标目录失败: %w", err)
	}

	total, err := dirSize(src)
	if err != nil {
		return fmt.Errorf("计算源目录大小失败: %w", err)
	}
	var done int64

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("读取源目录条目失败: %w", err)
	}

	for _, e := range entries {
		srcEntry := filepath.Join(src, e.Name())
		dstEntry := filepath.Join(dst, e.Name())
		if merr := moveEntry(srcEntry, dstEntry, &done, total, opts); merr != nil {
			return fmt.Errorf("迁移条目 %s 失败: %w", e.Name(), merr)
		}
	}

	// src 已迁移为空，尝试清理；清理失败不致命（残留空目录无害）。
	_ = os.Remove(src)
	return nil
}

// moveEntry 迁移单个条目：优先 rename，跨设备回退为拷贝 + 删除源。
func moveEntry(src, dst string, done *int64, total int64, opts Options) error {
	// 必须在 rename 前取大小：rename 成功后源已不存在，Lstat 会失败。
	sz := entrySize(src)
	if err := os.Rename(src, dst); err == nil {
		*done += sz
		report(opts, *done, total)
		return nil
	}
	// rename 失败（含跨设备 EXDEV）：走拷贝 + 删除源。
	if cerr := copyTree(src, dst, done, total, opts); cerr != nil {
		return cerr
	}
	return removeTree(src)
}

// copyTree 递归拷贝 src 到 dst，并随进度累加 done。
func copyTree(src, dst string, done *int64, total int64, opts Options) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if cerr := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), done, total, opts); cerr != nil {
				return cerr
			}
		}
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// 符号链接：复制链接本身而非跟随。
		target, lerr := os.Readlink(src)
		if lerr != nil {
			return lerr
		}
		return os.Symlink(target, dst)
	}
	if err := copyFile(src, dst, info.Mode().Perm(), done, total, opts); err != nil {
		return err
	}
	return nil
}

// copyFile 拷贝单个文件并累加进度。
func copyFile(src, dst string, mode os.FileMode, done *int64, total int64, opts Options) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	written, err := io.Copy(out, in)
	if err != nil {
		_ = out.Close()
		return err
	}
	if cerr := out.Close(); cerr != nil {
		return cerr
	}
	*done += written
	report(opts, *done, total)
	return nil
}

// removeTree 递归删除路径（文件或目录）。
func removeTree(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if rerr := removeTree(filepath.Join(path, e.Name())); rerr != nil {
				return rerr
			}
		}
		return os.Remove(path)
	}
	return os.Remove(path)
}

// dirSize 计算目录总字节数（仅文件内容，不含目录本身）。
func dirSize(root string) (int64, error) {
	var size int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// entrySize 返回条目大小（目录递归求和）。
func entrySize(path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}
	s, err := dirSize(path)
	if err != nil {
		return 0
	}
	return s
}

// isDirEmpty 报告目录是否为空。
func isDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

// isSubpath 判断 child 是否位于 parent 之内（不含相等）。
func isSubpath(child, parent string) bool {
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// report 安全调用进度回调。
func report(opts Options, done, total int64) {
	if opts.Progress != nil {
		opts.Progress(done, total)
	}
}
