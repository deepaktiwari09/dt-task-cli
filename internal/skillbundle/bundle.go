package skillbundle

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"
)

// Files is the complete installable skill payload.
//
//go:embed dt-task/SKILL.md dt-task/agents/openai.yaml
var Files embed.FS

func Validate() error {
	for _, name := range []string{"dt-task/SKILL.md", "dt-task/agents/openai.yaml"} {
		info, err := fs.Stat(Files, name)
		if err != nil {
			return fmt.Errorf("embedded skill missing %s: %w", name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("embedded skill entry %s is a directory", name)
		}
	}
	markdown, err := fs.ReadFile(Files, "dt-task/SKILL.md")
	if err != nil {
		return err
	}
	text := string(markdown)
	if !strings.HasPrefix(text, "---\n") {
		return fmt.Errorf("embedded SKILL.md is missing frontmatter")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return fmt.Errorf("embedded SKILL.md frontmatter is unterminated")
	}
	var frontmatter map[string]string
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &frontmatter); err != nil {
		return fmt.Errorf("parse embedded SKILL.md frontmatter: %w", err)
	}
	if strings.TrimSpace(frontmatter["name"]) == "" || strings.TrimSpace(frontmatter["description"]) == "" {
		return fmt.Errorf("embedded SKILL.md requires name and description")
	}
	metadata, err := fs.ReadFile(Files, "dt-task/agents/openai.yaml")
	if err != nil {
		return err
	}
	var interfaceData map[string]any
	if err := yaml.Unmarshal(metadata, &interfaceData); err != nil {
		return fmt.Errorf("parse embedded agents/openai.yaml: %w", err)
	}
	return nil
}
