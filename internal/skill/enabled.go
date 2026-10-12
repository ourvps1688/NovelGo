package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/novelgo/novelgo/internal/config"
)

// enabledFileName 是小说级"启用技能"清单文件，存于小说技能目录下。
// scanDir 只扫描 .md 文件，本文件不会被误认为技能。
// 语义：勾选清单与技能文件本身解耦——勾选只影响注入（等效临时 always），
// 不改写技能 frontmatter，取消勾选即恢复原 mode 行为。
const enabledFileName = ".enabled.json"

// EnabledFilePath 返回指定小说的启用技能清单文件路径。
func EnabledFilePath(novelID int64) string {
	return filepath.Join(config.NovelSkillsDir(novelID), enabledFileName)
}

// LoadEnabled 读取小说勾选启用的技能名清单。
// 文件不存在返回空切片（非错误）。
func LoadEnabled(novelID int64) ([]string, error) {
	data, err := os.ReadFile(EnabledFilePath(novelID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skill: 读取启用清单失败: %w", err)
	}
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, fmt.Errorf("skill: 启用清单格式非法: %w", err)
	}
	return names, nil
}

// SaveEnabled 全量覆盖写入小说的启用技能名清单（去重、去空白）。
func SaveEnabled(novelID int64, names []string) error {
	cleaned := make([]string, 0, len(names))
	seen := make(map[string]bool)
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		cleaned = append(cleaned, n)
	}

	dir := config.NovelSkillsDir(novelID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("skill: 创建技能目录失败: %w", err)
	}
	data, err := json.MarshalIndent(cleaned, "", "  ")
	if err != nil {
		return fmt.Errorf("skill: 序列化启用清单失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, enabledFileName), data, 0o644); err != nil {
		return fmt.Errorf("skill: 写入启用清单失败: %w", err)
	}
	return nil
}

// EnabledNames 返回小说勾选启用的技能名清单；读取失败按空处理并 Warn。
func (s *Store) EnabledNames(novelID int64) []string {
	names, err := LoadEnabled(novelID)
	if err != nil {
		s.logger.Warn("skill: 读取启用清单失败，按空处理", "novel_id", novelID, "err", err)
		return nil
	}
	return names
}
