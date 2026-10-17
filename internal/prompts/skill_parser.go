package prompts

import (
	"fmt"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"gopkg.in/yaml.v3"
)

// ParsedSkill represents the result of parsing a SKILL.md document with optional YAML frontmatter.
type ParsedSkill struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	InjectionMode string   `json:"injection_mode"`
	Body          string   `json:"body"`
	Variables     []string `json:"variables"`
}

type skillFrontmatter struct {
	Name          string `yaml:"name"`
	Title         string `yaml:"title"`
	Description   string `yaml:"description"`
	InjectionMode string `yaml:"injection_mode"`
	Mode          string `yaml:"mode"`
}

// ParseSkillMarkdown parses a SKILL.md document, extracting optional YAML frontmatter
// (name, description, injection_mode) and {{variables}} from the Markdown body.
func ParseSkillMarkdown(raw string) ParsedSkill {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	trimmed := strings.TrimSpace(normalized)

	var fm skillFrontmatter
	body := trimmed

	if strings.HasPrefix(trimmed, "---") {
		if firstNL := strings.Index(trimmed, "\n"); firstNL != -1 && strings.TrimSpace(trimmed[:firstNL]) == "---" {
			rest := trimmed[firstNL+1:]
			if endIdx := strings.Index(rest, "\n---"); endIdx != -1 {
				fmBlock := rest[:endIdx]
				after := rest[endIdx+4:]
				if nextNL := strings.Index(after, "\n"); nextNL != -1 {
					body = strings.TrimSpace(after[nextNL+1:])
				} else {
					body = ""
				}

				if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
					// Fallback line-by-line parser for frontmatter with unquoted colons
					for _, line := range strings.Split(fmBlock, "\n") {
						colon := strings.Index(line, ":")
						if colon <= 0 {
							continue
						}
						key := strings.ToLower(strings.TrimSpace(line[:colon]))
						val := strings.Trim(strings.TrimSpace(line[colon+1:]), `"'`)
						switch key {
						case "name", "title":
							if fm.Name == "" {
								fm.Name = val
							}
						case "description":
							fm.Description = val
						case "injection_mode", "mode":
							fm.InjectionMode = val
						}
					}
				}
			}
		}
	}

	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = strings.TrimSpace(fm.Title)
	}
	if name == "" {
		// Fallback: extract from first level-1 markdown heading if present
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "# ") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "# "))
				break
			}
		}
	}

	mode := strings.ToLower(strings.TrimSpace(fm.InjectionMode))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(fm.Mode))
	}
	if mode != core.SkillInjectionOnDemand {
		mode = core.SkillInjectionAlways
	}

	vars := ExtractVariables(body)

	return ParsedSkill{
		Name:          name,
		Description:   strings.TrimSpace(fm.Description),
		InjectionMode: mode,
		Body:          body,
		Variables:     vars,
	}
}

// RenderSkillsBlock renders attached always-injected skills and on-demand skill catalog metadata
// into a structured block for the system prompt.
func RenderSkillsBlock(
	alwaysSkills []*core.SkillVersion,
	onDemandSkills []*core.SkillVersion,
	varCtx map[string]any,
	engine *Engine,
) (string, error) {
	if len(alwaysSkills) == 0 && len(onDemandSkills) == 0 {
		return "", nil
	}
	if engine == nil {
		engine = NewEngine()
	}

	var sections []string

	if len(alwaysSkills) > 0 {
		var sb strings.Builder
		sb.WriteString("<active_skills>\n")
		for i, sk := range alwaysSkills {
			if sk == nil {
				continue
			}
			renderedContent, err := engine.Render(sk.Content, varCtx)
			if err != nil {
				return "", fmt.Errorf("render skill %q: %w", sk.SkillID, err)
			}
			name := sk.Name
			if name == "" {
				name = sk.SkillID
			}
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("<skill id=%q name=%q>\n%s\n</skill>\n", sk.SkillID, name, strings.TrimSpace(renderedContent)))
		}
		sb.WriteString("</active_skills>")
		sections = append(sections, sb.String())
	}

	if len(onDemandSkills) > 0 {
		var sb strings.Builder
		sb.WriteString("<available_skills>\n")
		sb.WriteString("The following specialized skills are available on demand. Call the `load_skill` tool with the corresponding `skill_id` when a request requires that skill's specialized instructions:\n")
		for _, sk := range onDemandSkills {
			if sk == nil {
				continue
			}
			name := sk.Name
			if name == "" {
				name = sk.SkillID
			}
			sb.WriteString(fmt.Sprintf("<skill id=%q name=%q description=%q />\n", sk.SkillID, name, sk.Description))
		}
		sb.WriteString("</available_skills>")
		sections = append(sections, sb.String())
	}

	return strings.Join(sections, "\n\n"), nil
}
