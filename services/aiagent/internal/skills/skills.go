// Package skills 加载随服务发布的客服 Skill 策略文档。
// 本包只负责解析 SKILL.md（YAML frontmatter + Markdown 正文），不依赖 Eino；
// 与 Eino Skill 中间件的适配在 internal/eino 中完成。
package skills

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// IssueTriage 是主 Agent（Supervisor）绑定的电商问题诊断 Skill。
	IssueTriage = "issue-triage"
	// PurchaseAdvisor 是商品 Agent 绑定的购物决策建议 Skill。
	PurchaseAdvisor = "purchase-advisor"
)

//go:embed issue-triage/SKILL.md
var issueTriageRaw string

//go:embed purchase-advisor/SKILL.md
var purchaseAdvisorRaw string

var embeddedSkills = map[string]string{
	IssueTriage:     issueTriageRaw,
	PurchaseAdvisor: purchaseAdvisorRaw,
}

// Document 是去除 YAML frontmatter 后的 Skill 定义。
type Document struct {
	Name        string
	Description string
	Content     string
}

// ByName 返回指定名称的内嵌 Skill；不存在时第二个返回值为 false。
func ByName(name string) (Document, bool) {
	raw, ok := embeddedSkills[name]
	if !ok {
		return Document{}, false
	}
	document, err := Parse(raw)
	if err != nil {
		return Document{}, false
	}
	return document, true
}

// Names 返回全部内嵌 Skill 名称，便于测试与启动期校验。
func Names() []string {
	names := make([]string, 0, len(embeddedSkills))
	for name := range embeddedSkills {
		names = append(names, name)
	}
	return names
}

// Hash returns a stable content hash for a parsed Document. It covers the name,
// description and body so that a same-name document with different content
// produces a different hash, which is what A/B loading evidence relies on.
func Hash(document Document) string {
	sum := sha256.Sum256([]byte(document.Name + "\n" + document.Description + "\n" + document.Content))
	return hex.EncodeToString(sum[:])
}

// LoadDir 扫描目录下每个子目录的 SKILL.md，返回 name→Document 映射。
// 用于配置化加载：外置目录可覆盖同名内嵌 Skill、追加新 Skill。子目录名必须与
// frontmatter 的 name 一致，否则报错；不含 SKILL.md 的子目录被跳过。
func LoadDir(dir string) (map[string]Document, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]Document{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name(), "SKILL.md"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		document, err := Parse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("skill dir %q: %w", entry.Name(), err)
		}
		if document.Name != entry.Name() {
			return nil, fmt.Errorf("skill dir %q has frontmatter name %q", entry.Name(), document.Name)
		}
		out[document.Name] = document
	}
	return out, nil
}

// Parse 解析 SKILL.md：frontmatter 仅支持 name/description 单行字段。
func Parse(raw string) (Document, error) {
	const delimiter = "---"
	data := strings.TrimSpace(raw)
	if !strings.HasPrefix(data, delimiter) {
		return Document{}, errors.New("skill file must start with frontmatter delimiter")
	}
	rest := data[len(delimiter):]
	end := strings.Index(rest, "\n"+delimiter)
	if end < 0 {
		return Document{}, errors.New("skill frontmatter closing delimiter not found")
	}
	document := Document{Content: strings.TrimSpace(rest[end+len("\n"+delimiter):])}
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "name:"); ok {
			document.Name = strings.TrimSpace(value)
			continue
		}
		if value, ok := strings.CutPrefix(line, "description:"); ok {
			document.Description = strings.TrimSpace(value)
		}
	}
	if document.Name == "" || document.Description == "" || document.Content == "" {
		return Document{}, fmt.Errorf("skill %q requires name, description and content", document.Name)
	}
	return document, nil
}
