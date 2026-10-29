package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/novelgo/novelgo/internal/config"
)

// ── 文件路径 ──────────────────────────────────────────────
// 此处都是相对路径，位于小说目录之下的，小说目录由config决定。
// 章节文件以 id 命名（chapters/id_{id}.md）：id_ 前缀将 id 命名空间与
// 历史 num 纯数字命名空间（chapters/001.md）隔离，保证迁移 rename 判断结构性可靠。

// ChapterPath 返回章节正文文件相对路径（按章节 id）。
func ChapterPath(id int64) string {
	return fmt.Sprintf("chapters/id_%d.md", id)
}

func NovelGoPath() string {
	return "novelgo.md"
}

func CoverPath() string {
	return "cover.jpg"
}

func PlanPath(scope string) string {
	return fmt.Sprintf("plans/%s.md", scope)
}

// OutlinePath 返回章节大纲文件相对路径（按章节 id）。
func OutlinePath(id int64) string {
	return fmt.Sprintf("outlines/id_%d.md", id)
}

// VolumePath 返回卷纲文件相对路径（按卷 id）。
func VolumePath(volumeID int64) string {
	return fmt.Sprintf("volumes/id_%d.md", volumeID)
}

// ChapterPathRef 是章节正文或章节大纲虚拟路径的解析结果。
// VolumeID 仅来自两级容错路径，不参与物理文件定位；IsNew 表示 new.md 创建通道。
// ByReadingNumber/ReadingNumber 表示 no_{N} 形式：按实时阅读序号（第N章）寻址，
// 物理 ID 需后续由 chapter.Store 反查，解析阶段只记录序号、不落物理路径。
type ChapterPathRef struct {
	ID              int64
	VolumeID        int64
	IsOutline       bool
	IsNew           bool
	ByReadingNumber bool
	ReadingNumber   int
}

var chapterLikePathRe = regexp.MustCompile(`^(chapters|outlines)/(?:([0-9]+)/)?(?:id_([0-9]+)|no_([0-9]+)|new)\.md$`)
var volumePathRe = regexp.MustCompile(`^volumes/id_([1-9][0-9]*)\.md$`)

// ParseChapterLikePath 解析 rw_tools 支持的章节正文或大纲虚拟路径。
// 支持三种定位形式：id_{id}（按稳定 ID）、no_{N}（按实时阅读序号第N章）、
// new（新建通道）；旧的纯数字章节号路径不被接受。no_{N} 仅记录序号，物理
// ID 由调用方经 chapter.Store.GetChapterIDByReadingNumber 反查后再落盘。
func ParseChapterLikePath(path string) (ChapterPathRef, bool) {
	matches := chapterLikePathRe.FindStringSubmatch(path)
	if matches == nil {
		return ChapterPathRef{}, false
	}

	ref := ChapterPathRef{IsOutline: matches[1] == "outlines"}
	if matches[2] != "" {
		volumeID, err := strconv.ParseInt(matches[2], 10, 64)
		if err != nil {
			return ChapterPathRef{}, false
		}
		ref.VolumeID = volumeID
	}
	switch {
	case matches[3] != "":
		id, err := strconv.ParseInt(matches[3], 10, 64)
		if err != nil {
			return ChapterPathRef{}, false
		}
		ref.ID = id
	case matches[4] != "":
		n, err := strconv.Atoi(matches[4])
		if err != nil || n <= 0 {
			return ChapterPathRef{}, false
		}
		ref.ByReadingNumber = true
		ref.ReadingNumber = n
	default:
		ref.IsNew = true
	}
	return ref, true
}

// ParseVolumePath 解析卷纲虚拟路径。卷 ID 必须是规范的正整数表示。
func ParseVolumePath(path string) (int64, bool) {
	matches := volumePathRe.FindStringSubmatch(path)
	if matches == nil {
		return 0, false
	}
	volumeID, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return volumeID, true
}

// ── 文件读写 ──────────────────────────────────────────────
// path 为相对于小说仓库根目录的路径，如 "chapters/id_1.md"、"novelgo.md"。

var fileWriteMu sync.Mutex

var ErrFileChanged = errors.New("git: file changed since it was read")

func ReadFile(novelID int64, path string) (string, error) {
	fullPath, err := ResolvePath(path, novelID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", os.ErrNotExist, path)
		}
		return "", fmt.Errorf("git: read %s: %w", path, err)
	}
	return string(data), nil
}

func WriteFile(novelID int64, path, content string) error {
	fullPath, err := ResolvePath(path, novelID)
	if err != nil {
		return err
	}
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	return writeFilePath(fullPath, path, content)
}

// WriteFileIfUnchanged 将比较与写入放在同一个进程锁内，避免应用内写入互相覆盖。
func WriteFileIfUnchanged(novelID int64, path, expected, content string) error {
	fullPath, err := ResolvePath(path, novelID)
	if err != nil {
		return err
	}
	return writeFilePathIfUnchanged(fullPath, path, expected, content)
}

// WriteFileAtPathIfUnchanged 供已验证绝对路径的调用方共用同一进程写锁。
func WriteFileAtPathIfUnchanged(fullPath, expected, content string) error {
	return writeFilePathIfUnchanged(fullPath, fullPath, expected, content)
}

func writeFilePathIfUnchanged(fullPath, path, expected, content string) error {
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	current, err := os.ReadFile(fullPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("git: read %s before write: %w", path, err)
	}
	if string(current) != expected {
		return ErrFileChanged
	}
	return writeFilePath(fullPath, path, content)
}

func writeFilePath(fullPath, path, content string) error {
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("git: mkdir for %s: %w", path, err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("git: write %s: %w", path, err)
	}
	return nil
}

// RemoveFile 删除小说仓库中的文件。文件不存在时视为已删除，便于清理可选的大纲文件。
func RemoveFile(novelID int64, path string) error {
	fullPath, err := ResolvePath(path, novelID)
	if err != nil {
		return err
	}
	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()
	if err := os.Remove(fullPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("git: remove %s: %w", path, err)
	}
	return nil
}

// chapterFileRe 匹配章节正文/大纲文件命名：chapters/id_N.md、outlines/id_N.md。
var chapterFileRe = regexp.MustCompile(`^id_\d+\.md$`)

// ListChapterFiles 返回小说目录下 chapters/ 与 outlines/ 子目录中符合
// id_<N>.md 命名的相对路径（不含未分卷等其他文件）。用于孤儿文件回收时
// 枚举磁盘上的章节文件，再与 DB 存活章节比对。
func ListChapterFiles(novelID int64) ([]string, error) {
	dir := novelDir(novelID)
	var out []string
	for _, sub := range []string{"chapters", "outlines"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("git: read %s: %w", sub, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if chapterFileRe.MatchString(e.Name()) {
				out = append(out, filepath.Join(sub, e.Name()))
			}
		}
	}
	return out, nil
}

// ParseChapterFileID 从 chapters/id_N.md 或 outlines/id_N.md 解析章节 ID。
// 返回 (id, true) 表示匹配成功；否则 (0, false)。
func ParseChapterFileID(rel string) (int64, bool) {
	base := filepath.Base(rel)
	if !chapterFileRe.MatchString(base) {
		return 0, false
	}
	id, err := strconv.ParseInt(base[len("id_"):len(base)-len(".md")], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

var ErrPathEscape = errors.New("git: path escapes novel directory")

func novelDir(novelID int64) string {
	return config.NovelDirPath(novelID)
}

// ResolvePath 将用户输入路径解析为真实文件系统路径。
// ~/.novelgo/ 展开到用户目录；其他相对路径基于小说目录。
func ResolvePath(path string, novelID int64) (string, error) {
	if strings.HasPrefix(path, "~/.novelgo/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("git: 获取用户目录失败: %w", err)
		}
		base := filepath.Join(home, ".novelgo")
		rel := strings.TrimPrefix(path, "~/.novelgo/")
		return SafePath(base, rel)
	}

	dir := novelDir(novelID)
	return SafePath(dir, path)
}

// SafePath 对给定的上级目录和相对路径求最终路径，如果路径跳出上级目录则返回 error。
func SafePath(base, rel string) (string, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("git: resolve base: %w", err)
	}
	full := filepath.Clean(filepath.Join(absBase, rel))
	// Windows 文件系统不区分大小写，用 ToLower 做大小写无关前缀比较防止路径穿越绕过
	if runtime.GOOS == "windows" {
		prefix := absBase + string(filepath.Separator)
		if !strings.HasPrefix(strings.ToLower(full), strings.ToLower(prefix)) && !strings.EqualFold(full, absBase) {
			return "", fmt.Errorf("%w: %s", ErrPathEscape, rel)
		}
	} else {
		if !strings.HasPrefix(full, absBase+string(filepath.Separator)) && full != absBase {
			return "", fmt.Errorf("%w: %s", ErrPathEscape, rel)
		}
	}
	return full, nil
}
