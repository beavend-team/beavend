package prompts_test

import (
	"strings"
	"testing"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/prompts"
)

func TestParseSkillMarkdown(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantName     string
		wantDesc     string
		wantMode     string
		wantBody     string
		wantVarCount int
	}{
		{
			name: "standard yaml frontmatter",
			raw: `---
name: Code Reviewer
description: Reviews pull requests and code diffs
injection_mode: on_demand
---
# Instructions
Please review the following code carefully.
Target: {{repo}}
`,
			wantName:     "Code Reviewer",
			wantDesc:     "Reviews pull requests and code diffs",
			wantMode:     core.SkillInjectionOnDemand,
			wantBody:     "# Instructions\nPlease review the following code carefully.\nTarget: {{repo}}",
			wantVarCount: 1,
		},
		{
			name: "frontmatter with trailing spaces on delimiters and title fallback",
			raw: "---  \ntitle: \"Security Auditor\"\ndescription: Scans for vulnerabilities\nmode: always\n---   \nYou are an auditor: {{auditor_name}}",
			wantName:     "Security Auditor",
			wantDesc:     "Scans for vulnerabilities",
			wantMode:     core.SkillInjectionAlways,
			wantBody:     "You are an auditor: {{auditor_name}}",
			wantVarCount: 1,
		},
		{
			name: "no frontmatter but h1 title",
			raw: `# Database Query Planner
Plans optimal PostgreSQL query execution.
Context: {{db_schema}}`,
			wantName:     "Database Query Planner",
			wantDesc:     "",
			wantMode:     core.SkillInjectionAlways,
			wantBody:     "# Database Query Planner\nPlans optimal PostgreSQL query execution.\nContext: {{db_schema}}",
			wantVarCount: 1,
		},
		{
			name:         "empty document",
			raw:          "",
			wantName:     "",
			wantDesc:     "",
			wantMode:     core.SkillInjectionAlways,
			wantBody:     "",
			wantVarCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := prompts.ParseSkillMarkdown(tc.raw)
			if got.Name != tc.wantName {
				t.Errorf("got Name = %q, want %q", got.Name, tc.wantName)
			}
			if got.Description != tc.wantDesc {
				t.Errorf("got Description = %q, want %q", got.Description, tc.wantDesc)
			}
			if got.InjectionMode != tc.wantMode {
				t.Errorf("got InjectionMode = %q, want %q", got.InjectionMode, tc.wantMode)
			}
			if strings.TrimSpace(got.Body) != strings.TrimSpace(tc.wantBody) {
				t.Errorf("got Body = %q, want %q", got.Body, tc.wantBody)
			}
			if len(got.Variables) != tc.wantVarCount {
				t.Errorf("got %d variables, want %d", len(got.Variables), tc.wantVarCount)
			}
		})
	}
}

func TestRenderSkillsBlock(t *testing.T) {
	engine := prompts.NewEngine()
	alwaysSkills := []*core.SkillVersion{
		{
			SkillID: "brand-voice",
			Name:    "Brand Voice",
			Content: "Always speak warmly and professionally. User plan is {{claims.plan}}.",
		},
	}
	onDemandSkills := []*core.SkillVersion{
		{
			SkillID:     "sql-expert",
			Name:        "SQL Expert",
			Description: "Assists with complex queries",
		},
	}

	varCtx := map[string]any{
		"claims": map[string]any{
			"plan": "enterprise",
		},
	}

	rendered, err := prompts.RenderSkillsBlock(alwaysSkills, onDemandSkills, varCtx, engine)
	if err != nil {
		t.Fatalf("unexpected error rendering skills block: %v", err)
	}

	if !strings.Contains(rendered, "<active_skills>") {
		t.Errorf("expected <active_skills> tag in rendered block, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "User plan is enterprise.") {
		t.Errorf("expected interpolated variable in active skill, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "<available_skills>") {
		t.Errorf("expected <available_skills> tag in rendered block, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, `id="sql-expert"`) {
		t.Errorf("expected sql-expert in available skills catalog, got:\n%s", rendered)
	}
}
