package stages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/prompts"
)

var (
	// ErrPromptNotFound indicates the configured prompt was not found in the snapshot.
	ErrPromptNotFound = errors.New("prompt not found in snapshot")
	// ErrSkillNotFound indicates a configured skill was not found in the snapshot.
	ErrSkillNotFound = errors.New("skill not found in snapshot")

	promptMeter             = otel.Meter("beavend/stages/prompt")
	promptRendersCounter, _ = promptMeter.Int64Counter("beavend_prompt_renders_total", metric.WithDescription("Total count of prompt template renders"))
)

// PromptStage renders versioned prompt templates and injects system instructions and skills.
type PromptStage struct {
	engine *prompts.Engine
	tracer trace.Tracer
}

// NewPromptStage creates a new PromptStage.
func NewPromptStage(engine *prompts.Engine) (*PromptStage, error) {
	if engine == nil {
		engine = prompts.NewEngine()
	}

	return &PromptStage{
		engine: engine,
		tracer: otel.Tracer("beavend/stages/prompt"),
	}, nil
}

// Handle resolves the prompt and attached skills from the snapshot, interpolates variables, and sets the system message.
func (s *PromptStage) Handle(ctx context.Context, state *core.ReqState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if state == nil {
		return errors.New("nil req state")
	}

	if state.ChatResponse != nil {
		return nil
	}

	ctx, span := s.tracer.Start(ctx, "stage.prompt",
		trace.WithAttributes(
			attribute.String("project_id", state.ProjectID),
			attribute.String("environment", state.Environment),
			attribute.String("ai_app_id", state.AIAppID),
			attribute.String("request_id", state.RequestID),
		),
	)
	defer span.End()

	if state.Snapshot == nil {
		state.Error = &core.ErrorResponse{
			Code:      "unavailable",
			Message:   "missing configuration snapshot in request state",
			RequestID: state.RequestID,
		}
		return ErrMissingSnapshot
	}

	if state.AIApp == nil {
		if state.AIAppID == "" {
			state.Error = &core.ErrorResponse{
				Code:      "invalid_argument",
				Message:   "missing ai app id in request state",
				RequestID: state.RequestID,
			}
			return ErrMissingAIApp
		}
		app, exists := state.Snapshot.App(state.AIAppID)
		if !exists {
			state.Error = &core.ErrorResponse{
				Code:      "not_found",
				Message:   fmt.Sprintf("ai app %q not found", state.AIAppID),
				RequestID: state.RequestID,
			}
			return fmt.Errorf("%w: %s", ErrMissingAIApp, state.AIAppID)
		}
		state.AIApp = app
	}

	hasPrompt := state.AIApp.PromptID != ""
	hasSkills := len(state.AIApp.SkillIDs) > 0

	// If neither prompt nor skills are configured on this AI App, skip rendering
	if !hasPrompt && !hasSkills {
		span.SetAttributes(
			attribute.Bool("prompt.configured", false),
			attribute.Int("skills.count", 0),
		)
		return nil
	}

	// Merge request-level metadata if present
	mergedMeta := make(map[string]any)
	for k, v := range state.Metadata {
		mergedMeta[k] = v
	}
	if state.ChatRequest != nil && state.ChatRequest.Metadata != nil {
		for k, v := range state.ChatRequest.Metadata {
			mergedMeta[k] = v
		}
	}

	varCtx := prompts.BuildVariableContext(state.Snapshot, state.AIApp, state.Identity, mergedMeta)

	var (
		basePrompt    string
		promptVersion int
	)

	if hasPrompt {
		span.SetAttributes(
			attribute.Bool("prompt.configured", true),
			attribute.String("prompt.id", state.AIApp.PromptID),
		)

		promptVer, exists := state.Snapshot.Prompt(state.AIApp.PromptID)
		if !exists {
			state.Error = &core.ErrorResponse{
				Code:      "not_found",
				Message:   fmt.Sprintf("prompt %q not found in snapshot", state.AIApp.PromptID),
				RequestID: state.RequestID,
			}
			return fmt.Errorf("%w: %s", ErrPromptNotFound, state.AIApp.PromptID)
		}

		promptVersion = promptVer.Version
		span.SetAttributes(attribute.Int("prompt.version", promptVersion))

		rendered, err := s.engine.Render(promptVer.Template, varCtx)
		if err != nil {
			span.RecordError(err)
			state.Error = &core.ErrorResponse{
				Code:      "invalid_argument",
				Message:   fmt.Sprintf("prompt render failed: %v", err),
				RequestID: state.RequestID,
			}
			return fmt.Errorf("render prompt template: %w", err)
		}
		basePrompt = rendered

		if promptRendersCounter != nil {
			promptRendersCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("prompt_id", state.AIApp.PromptID),
				attribute.Int("version", promptVersion),
			))
		}
	} else {
		span.SetAttributes(attribute.Bool("prompt.configured", false))
	}

	var skillsBlock string
	if hasSkills {
		span.SetAttributes(attribute.Int("skills.count", len(state.AIApp.SkillIDs)))
		var (
			alwaysSkills   []*core.SkillVersion
			onDemandSkills []*core.SkillVersion
		)
		for _, skillID := range state.AIApp.SkillIDs {
			sk, exists := state.Snapshot.Skill(skillID)
			if !exists || sk == nil {
				state.Error = &core.ErrorResponse{
					Code:      "not_found",
					Message:   fmt.Sprintf("skill %q not found in snapshot", skillID),
					RequestID: state.RequestID,
				}
				return fmt.Errorf("%w: %s", ErrSkillNotFound, skillID)
			}
			if sk.InjectionMode == core.SkillInjectionOnDemand {
				onDemandSkills = append(onDemandSkills, sk)
			} else {
				alwaysSkills = append(alwaysSkills, sk)
			}
		}

		renderedSkills, err := prompts.RenderSkillsBlock(alwaysSkills, onDemandSkills, varCtx, s.engine)
		if err != nil {
			span.RecordError(err)
			state.Error = &core.ErrorResponse{
				Code:      "invalid_argument",
				Message:   fmt.Sprintf("skill render failed: %v", err),
				RequestID: state.RequestID,
			}
			return fmt.Errorf("render skills block: %w", err)
		}
		skillsBlock = renderedSkills
	}

	// Inject rendered prompt & skills into ChatRequest while preserving any memory recall or pass-through system prompt
	if state.ChatRequest == nil {
		state.ChatRequest = &core.ChatRequest{}
	}

	var existingClientSys, memoryBlock string
	if len(state.ChatRequest.Messages) > 0 && state.ChatRequest.Messages[0].Role == "system" {
		existingClientSys, memoryBlock = splitExistingSystemMessage(state.ChatRequest.Messages[0].Content)
	}

	primaryPrompt := basePrompt
	if !hasPrompt {
		primaryPrompt = existingClientSys
	}

	var parts []string
	if primaryPrompt != "" {
		parts = append(parts, primaryPrompt)
	}
	if memoryBlock != "" {
		parts = append(parts, memoryBlock)
	}
	if skillsBlock != "" {
		parts = append(parts, skillsBlock)
	}
	finalSystemContent := strings.Join(parts, "\n\n")

	span.SetAttributes(attribute.Int("prompt.char_count", len(finalSystemContent)))

	switch {
	case len(state.ChatRequest.Messages) == 0:
		state.ChatRequest.Messages = []core.ChatMessage{{Role: "system", Content: finalSystemContent}}
	case state.ChatRequest.Messages[0].Role == "system":
		state.ChatRequest.Messages[0].Content = finalSystemContent
	default:
		systemMsg := core.ChatMessage{
			Role:    "system",
			Content: finalSystemContent,
		}
		state.ChatRequest.Messages = append([]core.ChatMessage{systemMsg}, state.ChatRequest.Messages...)
	}

	slog.InfoContext(ctx, "prompt rendered and injected",
		"request_id", state.RequestID,
		"project_id", state.ProjectID,
		"env", state.Environment,
		"ai_app_id", state.AIAppID,
		"prompt_id", state.AIApp.PromptID,
		"version", promptVersion,
		"skills_count", len(state.AIApp.SkillIDs),
		"length", len(finalSystemContent),
	)

	return nil
}

// splitExistingSystemMessage strips any previously rendered skills blocks and separates
// a client system message from a ContextStage "Relevant user memories:" block.
func splitExistingSystemMessage(content string) (clientPrompt string, memoryBlock string) {
	cleaned := stripTagBlock(content, "<active_skills>", "</active_skills>")
	cleaned = stripTagBlock(cleaned, "<available_skills>", "</available_skills>")
	cleaned = strings.TrimSpace(cleaned)

	const memPrefix = "Relevant user memories:\n"
	if idx := strings.Index(cleaned, memPrefix); idx != -1 {
		before := strings.TrimSpace(cleaned[:idx])
		mem := strings.TrimSpace(cleaned[idx:])
		return before, mem
	}
	return cleaned, ""
}

func stripTagBlock(s, openTag, closeTag string) string {
	for {
		start := strings.Index(s, openTag)
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], closeTag)
		if end == -1 {
			s = strings.TrimSpace(s[:start])
			break
		}
		endIdx := start + end + len(closeTag)
		s = strings.TrimSpace(s[:start]) + "\n\n" + strings.TrimSpace(s[endIdx:])
		s = strings.TrimSpace(s)
	}
	return s
}
