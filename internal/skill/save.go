package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Serialize 将 Skill 序列化为完整的 markdown 文本（YAML frontmatter + 正文）。
//
// frontmatter 来自 Skill 的全部导出字段（Content/RawContent 已用 yaml:"-" 排除）。
// 派生聚合字段（aggregated/aggregated_from/merged_books/...）带 omitempty，
// 普通 skill 序列化时不会产出这些键，保持与既有文件格式兼容。
func Serialize(sk *Skill) (string, error) {
	if sk == nil {
		return "", fmt.Errorf("skill: 序列化失败，skill 为 nil")
	}
	if sk.Name == "" {
		return "", fmt.Errorf("skill: 序列化失败，name 字段为空")
	}

	fmBytes, err := yaml.Marshal(sk)
	if err != nil {
		return "", fmt.Errorf("skill: 序列化 frontmatter 失败: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.Write(fmBytes)
	sb.WriteString("---\n")
	body := strings.TrimRight(sk.Content, "\n")
	sb.WriteString(body)
	if body != "" {
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// WriteSkill 将 Skill 序列化后落盘到指定目录（文件名取 {Name}.md）。
// 目录不存在时自动创建。返回落盘后的完整文件路径。
func WriteSkill(dir string, sk *Skill) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("skill: 写入失败，目录为空")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("skill: 创建目录失败 %s: %w", dir, err)
	}

	content, err := Serialize(sk)
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, SanitizeFileName(sk.Name)+".md")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("skill: 写入 skill 文件失败 %s: %w", path, err)
	}
	return path, nil
}
