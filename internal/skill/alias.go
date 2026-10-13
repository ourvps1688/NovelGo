package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// AliasConfig 是用户级「作者别名映射」的持久化结构，落盘于
// config.UserSkillsDir()/author_aliases.json。
//
// 用途：把同一作者的不同署名（如 "Liu Cixin" / "刘慈欣" / "刘慈欣 "）归并到同一
// 规范名，使跨书聚合（MergeMentalSkills / ReconcileAggregates / RebuildAggregated）
// 能把它们当作同一作者处理——否则仅靠 trim+NFKC+大小写折叠无法跨语言/跨写法合并。
//
// 设计为纯文件存储（而非 DB），以保持 internal/skill 的无 DB 依赖、与 skill 文件
// 同目录随迁；结构足够简单，用户可直接手写编辑。
type AliasConfig struct {
	Version int                `json:"version"` // 当前固定为 1，便于未来演进
	Groups  []AuthorAliasGroup `json:"groups"`  // 等价署名分组
}

// AuthorAliasGroup 声明一组等价署名：Canonical 为规范名，Aliases 为其别名。
// 归一后的别名都会解析回 Canonical。
type AuthorAliasGroup struct {
	Canonical string   `json:"canonical"`         // 规范作者名（展示与归并键）
	Aliases   []string `json:"aliases,omitempty"` // 该规范名下的别名（等价署名）
}

// AliasConfigFileName 是别名配置的文件名（位于 UserSkillsDir 下）。
// 与 .md 技能文件区分，不会被 LoadDir/scanDir 误读为 skill。
const AliasConfigFileName = "author_aliases.json"

// DefaultAliasConfig 返回空配置（Version=1，Groups 非空切片）。
func DefaultAliasConfig() *AliasConfig {
	return &AliasConfig{Version: 1, Groups: []AuthorAliasGroup{}}
}

// LoadAliasConfig 从 dir 加载别名配置。
// 文件不存在时返回空配置（不报错）；文件为空或解析失败才返回 error。
func LoadAliasConfig(dir string) (*AliasConfig, error) {
	if dir == "" {
		return DefaultAliasConfig(), nil
	}
	path := filepath.Join(dir, AliasConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultAliasConfig(), nil
		}
		return nil, err
	}
	cfg := DefaultAliasConfig()
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Groups == nil {
		cfg.Groups = []AuthorAliasGroup{}
	}
	return cfg, nil
}

// SaveAliasConfig 原子写入别名配置到 dir（先写 .tmp 再 rename，避免半截文件）。
func SaveAliasConfig(dir string, cfg *AliasConfig) error {
	if dir == "" {
		return os.ErrInvalid
	}
	if cfg == nil {
		cfg = DefaultAliasConfig()
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Groups == nil {
		cfg.Groups = []AuthorAliasGroup{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, AliasConfigFileName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// aliasKey 归一化一个署名，用作别名映射的查找键。
// 与 normalizeAuthor 的归一保持一致（trim + NFKC + 大小写折叠 + 压缩空白），
// 但不含别名替换，避免递归。
func aliasKey(name string) string {
	n := strings.TrimSpace(name)
	if n == "" {
		return ""
	}
	n = norm.NFKC.String(n)
	n = folder.String(n)
	return strings.Join(strings.Fields(n), " ")
}

// ToMap 将分组编译为「归一键 → 规范名」的扁平映射，供 normalizeAuthor 直接使用。
// 规范名自身也作为键映射到自己；别名（与规范名不同者）映射到规范名。
func (c *AliasConfig) ToMap() map[string]string {
	m := make(map[string]string)
	for _, g := range c.Groups {
		canon := aliasKey(g.Canonical)
		if canon == "" {
			continue
		}
		m[canon] = canon
		for _, al := range g.Aliases {
			k := aliasKey(al)
			if k == "" || k == canon {
				continue
			}
			m[k] = canon
		}
	}
	return m
}

// Resolve 把任意署名解析为规范名：命中别名返回规范名，否则返回自身的归一形式。
func (c *AliasConfig) Resolve(raw string) string {
	m := c.ToMap()
	key := aliasKey(raw)
	if canon, ok := m[key]; ok {
		return canon
	}
	return key
}
