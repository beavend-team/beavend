package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/prompts"
)

const (
	// MaxSkillBytes is the maximum size of a rendered skill returned by load_skill (128KB).
	MaxSkillBytes = 128 * 1024
)

type skillLoaderExecTemplate struct {
	AppID    string            `json:"app_id"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// BuiltinLoadSkillToolDefinition constructs the built-in load_skill ToolDefinition for an AI App's on-demand skills.
func BuiltinLoadSkillToolDefinition(appID string, onDemandSkills []*core.SkillVersion, metadata map[string]string) core.ToolDefinition {
	enumIDs := make([]string, 0, len(onDemandSkills))
	descParts := make([]string, 0, len(onDemandSkills))
	for _, sk := range onDemandSkills {
		if sk == nil {
			continue
		}
		enumIDs = append(enumIDs, sk.SkillID)
		if sk.Description != "" {
			descParts = append(descParts, fmt.Sprintf("%s (%s)", sk.SkillID, sk.Description))
		} else {
			descParts = append(descParts, sk.SkillID)
		}
	}

	propSchema := map[string]any{
		"type":        "string",
		"description": "Identifier of the on-demand skill to load.",
	}
	if len(enumIDs) > 0 {
		propSchema["enum"] = enumIDs
	}

	tmplBytes, _ := json.Marshal(skillLoaderExecTemplate{
		AppID:    appID,
		Metadata: metadata,
	})

	desc := "Load the full specialized instructions for an available on-demand skill by its skill_id."
	if len(descParts) > 0 {
		desc += " Available skills: " + strings.Join(descParts, "; ")
	}

	return core.ToolDefinition{
		ID:          "load_skill",
		Name:        "load_skill",
		Description: desc,
		SideEffect:  "read",
		Confirmation: "none",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"skill_id"},
			"properties": map[string]any{
				"skill_id": propSchema,
			},
		},
		Executor: core.ExecutorConfig{
			Type:     "skill_loader",
			Template: string(tmplBytes),
		},
	}
}

// SkillLoaderExecutor executes the built-in load_skill tool against the in-memory Snapshot.
type SkillLoaderExecutor struct {
	def    core.ToolDefinition
	engine *prompts.Engine
}

// NewSkillLoaderExecutor creates a new SkillLoaderExecutor.
func NewSkillLoaderExecutor(def core.ToolDefinition) *SkillLoaderExecutor {
	return &SkillLoaderExecutor{
		def:    def,
		engine: prompts.NewEngine(),
	}
}

// Execute loads and renders the requested skill from the active Snapshot in context.
func (e *SkillLoaderExecutor) Execute(ctx context.Context, call core.ToolCall, identity core.Identity) (core.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}

	snap, ok := core.SnapshotFromContext(ctx)
	if !ok || snap == nil {
		return core.ToolResult{}, errors.New("missing configuration snapshot in context")
	}

	var args struct {
		SkillID string `json:"skill_id"`
	}
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return core.ToolResult{}, fmt.Errorf("invalid load_skill arguments: %w", err)
		}
	}

	skillID := strings.TrimSpace(args.SkillID)
	if skillID == "" {
		return core.ToolResult{}, errors.New("skill_id is required")
	}

	var tmpl skillLoaderExecTemplate
	if e.def.Executor.Template != "" {
		_ = json.Unmarshal([]byte(e.def.Executor.Template), &tmpl)
	}

	var app *core.AIApp
	if tmpl.AppID != "" {
		app, _ = snap.App(tmpl.AppID)
	}

	skill, exists := snap.Skill(skillID)
	if !exists || skill == nil {
		return core.ToolResult{}, fmt.Errorf("skill %q not found in snapshot", skillID)
	}

	// Verify the requested skill is attached to the AI App
	if app != nil {
		allowed := false
		for _, id := range app.SkillIDs {
			if id == skill.SkillID || id == skill.Name || id == skillID {
				allowed = true
				break
			}
		}
		if !allowed {
			return core.ToolResult{}, fmt.Errorf("skill %q is not attached to app %q", skillID, app.ID)
		}
	}

	metaMap := make(map[string]any, len(tmpl.Metadata))
	for k, v := range tmpl.Metadata {
		metaMap[k] = v
	}

	varCtx := prompts.BuildVariableContext(snap, app, &identity, metaMap)
	rendered, err := e.engine.Render(skill.Content, varCtx)
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("render skill %q: %w", skill.SkillID, err)
	}

	if len(rendered) > MaxSkillBytes {
		rendered = rendered[:MaxSkillBytes] + "\n...[truncated]"
	}

	name := skill.Name
	if name == "" {
		name = skill.SkillID
	}

	wrapped := fmt.Sprintf("<skill id=%q name=%q>\n%s\n</skill>", skill.SkillID, name, strings.TrimSpace(rendered))
	return core.ToolResult{
		CallID:  call.ID,
		ToolID:  "load_skill",
		Content: wrapped,
		IsError: false,
	}, nil
}
