package skillbundle

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Files is the complete installable skill payload.
//
//go:embed dt-task/SKILL.md dt-task/agents/openai.yaml dt-task-worktree/SKILL.md dt-task-worktree/agents/openai.yaml
var Files embed.FS

var names = []string{"dt-task", "dt-task-worktree"}

func SkillNames() []string { return append([]string(nil), names...) }

func SkillFiles() []string { return []string{"SKILL.md", path.Join("agents", "openai.yaml")} }

func EmbeddedPath(skillName, fileName string) string { return path.Join(skillName, fileName) }

func Validate() error {
	for _, skillName := range names {
		if err := validateSkill(skillName); err != nil {
			return err
		}
	}
	return nil
}

func validateSkill(skillName string) error {
	for _, fileName := range SkillFiles() {
		name := EmbeddedPath(skillName, fileName)
		info, err := fs.Stat(Files, name)
		if err != nil {
			return fmt.Errorf("embedded skill missing %s: %w", name, err)
		}
		if info.IsDir() {
			return fmt.Errorf("embedded skill entry %s is a directory", name)
		}
	}
	markdown, err := fs.ReadFile(Files, EmbeddedPath(skillName, "SKILL.md"))
	if err != nil {
		return err
	}
	text := string(markdown)
	if !strings.HasPrefix(text, "---\n") {
		return fmt.Errorf("embedded %s/SKILL.md is missing frontmatter", skillName)
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return fmt.Errorf("embedded %s/SKILL.md frontmatter is unterminated", skillName)
	}
	var frontmatter map[string]string
	if err := yaml.Unmarshal([]byte(text[4:4+end]), &frontmatter); err != nil {
		return fmt.Errorf("parse embedded %s/SKILL.md frontmatter: %w", skillName, err)
	}
	if strings.TrimSpace(frontmatter["name"]) == "" || strings.TrimSpace(frontmatter["description"]) == "" {
		return fmt.Errorf("embedded %s/SKILL.md requires name and description", skillName)
	}
	metadata, err := fs.ReadFile(Files, EmbeddedPath(skillName, path.Join("agents", "openai.yaml")))
	if err != nil {
		return err
	}
	var interfaceData map[string]any
	if err := yaml.Unmarshal(metadata, &interfaceData); err != nil {
		return fmt.Errorf("parse embedded %s/agents/openai.yaml: %w", skillName, err)
	}
	return nil
}
