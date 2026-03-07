// Package agent implements the core agent loop and related utilities.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SkillMetadata represents the parsed YAML frontmatter of a skill.
type SkillMetadata struct {
	Description string `json:"description"`
	Always      bool   `json:"always"`
	Metadata    string `json:"metadata"`
	Requires    struct {
		Bins []string `json:"bins"`
		Env  []string `json:"env"`
	} `json:"requires"`
}

// SkillInfo represents a discovered skill.
type SkillInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"` // "workspace" or "builtin"
}

// SkillsLoader loads and manages agent skills.
// Skills are markdown files (SKILL.md) that teach the agent how to use
// specific tools or perform certain tasks.
type SkillsLoader struct {
	workspace      string
	workspaceSkills string
	builtinSkills  string
}

// NewSkillsLoader creates a SkillsLoader.
func NewSkillsLoader(workspace string, builtinSkillsDir string) *SkillsLoader {
	s := &SkillsLoader{
		workspace:       workspace,
		workspaceSkills: filepath.Join(workspace, "skills"),
		builtinSkills:   builtinSkillsDir,
	}
	return s
}

// ListSkills returns all available skills.
// If filterUnavailable is true, skills with unmet requirements are excluded.
func (s *SkillsLoader) ListSkills(filterUnavailable bool) []SkillInfo {
	skills := make(map[string]SkillInfo) // use map to dedupe by name

	// Workspace skills (highest priority)
	if info, err := os.Stat(s.workspaceSkills); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(s.workspaceSkills)
		for _, entry := range entries {
			if entry.IsDir() {
				skillPath := filepath.Join(s.workspaceSkills, entry.Name(), "SKILL.md")
				if _, err := os.Stat(skillPath); err == nil {
					skills[entry.Name()] = SkillInfo{
						Name:   entry.Name(),
						Path:   skillPath,
						Source: "workspace",
					}
				}
			}
		}
	}

	// Built-in skills
	if s.builtinSkills != "" {
		if info, err := os.Stat(s.builtinSkills); err == nil && info.IsDir() {
			entries, _ := os.ReadDir(s.builtinSkills)
			for _, entry := range entries {
				if entry.IsDir() {
					name := entry.Name()
					if _, exists := skills[name]; !exists {
						skillPath := filepath.Join(s.builtinSkills, name, "SKILL.md")
						if _, err := os.Stat(skillPath); err == nil {
							skills[name] = SkillInfo{
								Name:   name,
								Path:   skillPath,
								Source: "builtin",
							}
						}
					}
				}
			}
		}
	}

	// Convert to slice
	result := make([]SkillInfo, 0, len(skills))
	for _, skill := range skills {
		if filterUnavailable {
			meta := s.getSkillMeta(skill.Name)
			if !s.checkRequirements(meta) {
				continue
			}
		}
		result = append(result, skill)
	}

	// Sort by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// LoadSkill loads a skill by name. Returns the skill content without frontmatter.
func (s *SkillsLoader) LoadSkill(name string) string {
	// Check workspace first
	content, err := os.ReadFile(filepath.Join(s.workspaceSkills, name, "SKILL.md"))
	if err == nil {
		return s.stripFrontmatter(string(content))
	}

	// Check built-in
	if s.builtinSkills != "" {
		content, err := os.ReadFile(filepath.Join(s.builtinSkills, name, "SKILL.md"))
		if err == nil {
			return s.stripFrontmatter(string(content))
		}
	}

	return ""
}

// LoadSkillsForContext loads specific skills for inclusion in agent context.
func (s *SkillsLoader) LoadSkillsForContext(skillNames []string) string {
	parts := make([]string, 0, len(skillNames))
	for _, name := range skillNames {
		content := s.LoadSkill(name)
		if content != "" {
			parts = append(parts, fmt.Sprintf("### Skill: %s\n\n%s", name, content))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// BuildSkillsSummary builds an XML summary of all skills for progressive loading.
func (s *SkillsLoader) BuildSkillsSummary() string {
	allSkills := s.ListSkills(false)
	if len(allSkills) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, "<skills>")
	for _, skill := range allSkills {
		name := escapeXML(skill.Name)
		path := skill.Path
		desc := escapeXML(s.getSkillDescription(skill.Name))
		meta := s.getSkillMeta(skill.Name)
		available := s.checkRequirements(meta)

		lines = append(lines, fmt.Sprintf(`  <skill available="%t">`, available))
		lines = append(lines, fmt.Sprintf("    <name>%s</name>", name))
		lines = append(lines, fmt.Sprintf("    <description>%s</description>", desc))
		lines = append(lines, fmt.Sprintf("    <location>%s</location>", path))

		if !available {
			missing := s.getMissingRequirements(meta)
			if missing != "" {
				lines = append(lines, fmt.Sprintf("    <requires>%s</requires>", escapeXML(missing)))
			}
		}

		lines = append(lines, "  </skill>")
	}
	lines = append(lines, "</skills>")

	return strings.Join(lines, "\n")
}

// GetAlwaysSkills returns skills marked as always=true that meet requirements.
func (s *SkillsLoader) GetAlwaysSkills() []string {
	result := []string{}
	for _, skill := range s.ListSkills(true) {
		meta := s.GetSkillMetadata(skill.Name)
		if meta != nil && (meta.Always || s.getAlwaysFromMetadata(meta)) {
			result = append(result, skill.Name)
		}
	}
	return result
}

// GetSkillMetadata returns the parsed metadata from a skill's frontmatter.
func (s *SkillsLoader) GetSkillMetadata(name string) *SkillMetadata {
	content := s.loadRawSkill(name)
	if content == "" {
		return nil
	}

	meta := parseFrontmatter(content)
	if meta == nil {
		return nil
	}

	return meta
}

// loadRawSkill loads raw skill content including frontmatter.
func (s *SkillsLoader) loadRawSkill(name string) string {
	// Check workspace first
	content, err := os.ReadFile(filepath.Join(s.workspaceSkills, name, "SKILL.md"))
	if err == nil {
		return string(content)
	}

	// Check built-in
	if s.builtinSkills != "" {
		content, err := os.ReadFile(filepath.Join(s.builtinSkills, name, "SKILL.md"))
		if err == nil {
			return string(content)
		}
	}

	return ""
}

// getSkillDescription returns the description from frontmatter or the skill name.
func (s *SkillsLoader) getSkillDescription(name string) string {
	meta := s.GetSkillMetadata(name)
	if meta != nil && meta.Description != "" {
		return meta.Description
	}
	return name
}

// stripFrontmatter removes YAML frontmatter from markdown content.
func (s *SkillsLoader) stripFrontmatter(content string) string {
	if strings.HasPrefix(content, "---") {
		re := regexp.MustCompile(`(?s)^---\n.*?\n---\n`)
		return strings.TrimSpace(re.ReplaceAllString(content, ""))
	}
	return content
}

// parseFrontmatter parses YAML frontmatter from skill content.
func parseFrontmatter(content string) *SkillMetadata {
	if !strings.HasPrefix(content, "---") {
		return nil
	}

	re := regexp.MustCompile(`(?s)^---\n(.*?)\n---`)
	match := re.FindStringSubmatch(content)
	if len(match) < 2 {
		return nil
	}

	meta := &SkillMetadata{}
	frontmatter := match[1]

	// Simple YAML parsing
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		value = strings.Trim(value, `"'`)

		switch key {
		case "description":
			meta.Description = value
		case "always":
			meta.Always = value == "true"
		case "metadata":
			meta.Metadata = value
		}
	}

	return meta
}

// getSkillMeta returns the nanobot metadata from a skill.
func (s *SkillsLoader) getSkillMeta(name string) map[string]any {
	meta := s.GetSkillMetadata(name)
	if meta == nil {
		return nil
	}

	if meta.Metadata == "" {
		return nil
	}

	// Parse the metadata JSON string
	var data map[string]any
	if err := json.Unmarshal([]byte(meta.Metadata), &data); err != nil {
		return nil
	}

	// Support both "nanobot" and "openclaw" keys for compatibility
	if v, ok := data["nanobot"]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	if v, ok := data["openclaw"]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}

	return data
}

// getAlwaysFromMetadata checks if skill is marked as always in metadata JSON.
func (s *SkillsLoader) getAlwaysFromMetadata(meta *SkillMetadata) bool {
	m := s.getSkillMetaFromMetadata(meta)
	if m == nil {
		return false
	}
	if v, ok := m["always"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func (s *SkillsLoader) getSkillMetaFromMetadata(meta *SkillMetadata) map[string]any {
	if meta.Metadata == "" {
		return nil
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(meta.Metadata), &data); err != nil {
		return nil
	}

	if v, ok := data["nanobot"]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	if v, ok := data["openclaw"]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}

	return nil
}

// checkRequirements checks if skill requirements (bins, env vars) are met.
func (s *SkillsLoader) checkRequirements(meta map[string]any) bool {
	if meta == nil {
		return true
	}

	requires, ok := meta["requires"]
	if !ok {
		return true
	}

	req, ok := requires.(map[string]any)
	if !ok {
		return true
	}

	// Check binary requirements
	if bins, ok := req["bins"]; ok {
		if binList, ok := bins.([]any); ok {
			for _, b := range binList {
				if binName, ok := b.(string); ok {
					if _, err := exec.LookPath(binName); err != nil {
						return false
					}
				}
			}
		}
	}

	// Check environment variable requirements
	if envs, ok := req["env"]; ok {
		if envList, ok := envs.([]any); ok {
			for _, e := range envList {
				if envName, ok := e.(string); ok {
					if os.Getenv(envName) == "" {
						return false
					}
				}
			}
		}
	}

	return true
}

// getMissingRequirements returns a description of missing requirements.
func (s *SkillsLoader) getMissingRequirements(meta map[string]any) string {
	if meta == nil {
		return ""
	}

	requires, ok := meta["requires"]
	if !ok {
		return ""
	}

	req, ok := requires.(map[string]any)
	if !ok {
		return ""
	}

	var missing []string

	if bins, ok := req["bins"]; ok {
		if binList, ok := bins.([]any); ok {
			for _, b := range binList {
				if binName, ok := b.(string); ok {
					if _, err := exec.LookPath(binName); err != nil {
						missing = append(missing, fmt.Sprintf("CLI: %s", binName))
					}
				}
			}
		}
	}

	if envs, ok := req["env"]; ok {
		if envList, ok := envs.([]any); ok {
			for _, e := range envList {
				if envName, ok := e.(string); ok {
					if os.Getenv(envName) == "" {
						missing = append(missing, fmt.Sprintf("ENV: %s", envName))
					}
				}
			}
		}
	}

	return strings.Join(missing, ", ")
}

// escapeXML escapes special characters for XML output.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
