package stages

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/memory"
)

// ContextStage hydrates conversation threads and recalls user-scoped semantic memory.
type ContextStage struct {
	memoryEngine *memory.Engine
	threadStore  core.ThreadStore
}

// NewContextStage creates a new initialized ContextStage.
func NewContextStage(engine *memory.Engine, threadStore core.ThreadStore) *ContextStage {
	return &ContextStage{
		memoryEngine: engine,
		threadStore:  threadStore,
	}
}

// Handle executes the Context stage:
// 1. Thread retrieval and history prepending if thread_id is specified.
// 2. User-scoped semantic memory recall if AIApp memory is enabled.
func (s *ContextStage) Handle(ctx context.Context, state *core.ReqState) error {
	if state == nil || state.ChatRequest == nil {
		return nil
	}

	// 1. Conversation Threads
	if state.ChatRequest.ThreadID != "" && s.threadStore != nil {
		th, err := s.threadStore.GetThread(ctx, state.ProjectID, state.Environment, state.ChatRequest.ThreadID)
		if err == nil && th != nil {
			// Validate subject isolation: if thread is owned by a subject, caller must match
			if th.Subject != "" && state.Identity != nil && th.Subject != state.Identity.Subject {
				state.Error = &core.ErrorResponse{
					Code:      "forbidden",
					Message:   "conversation thread belongs to another user",
					RequestID: state.RequestID,
				}
				return fmt.Errorf("thread %s subject mismatch", state.ChatRequest.ThreadID)
			}

			if state.Metadata == nil {
				state.Metadata = make(map[string]any)
			}
			state.Metadata["thread_history_count"] = len(th.Messages)

			// Prepend previous thread history (deep copy to avoid mutating stored thread canonical parts)
			if len(th.Messages) > 0 {
				copiedHistory := make([]core.ChatMessage, len(th.Messages))
				for idx, orig := range th.Messages {
					msgCopy := orig
					if len(orig.Parts) > 0 {
						msgCopy.Parts = make([]core.Part, len(orig.Parts))
						for pIdx, part := range orig.Parts {
							pCopy := part
							if part.Media != nil {
								mCopy := *part.Media
								pCopy.Media = &mCopy
							}
							if part.ToolCall != nil {
								tcCopy := *part.ToolCall
								pCopy.ToolCall = &tcCopy
							}
							if part.ToolResult != nil {
								trCopy := *part.ToolResult
								pCopy.ToolResult = &trCopy
							}
							msgCopy.Parts[pIdx] = pCopy
						}
					}
					copiedHistory[idx] = msgCopy
				}
				state.ChatRequest.Messages = append(copiedHistory, state.ChatRequest.Messages...)
			}
		} else if errors.Is(err, memory.ErrThreadNotFound) {
			if state.Metadata == nil {
				state.Metadata = make(map[string]any)
			}
			state.Metadata["thread_history_count"] = 0

			// Thread does not exist yet; initialize new thread record
			ttl := 86400
			if state.AIApp != nil && state.AIApp.Memory.ThreadTTLSec > 0 {
				ttl = state.AIApp.Memory.ThreadTTLSec
			}

			newTh := &core.Thread{
				ID:          state.ChatRequest.ThreadID,
				ProjectID:   state.ProjectID,
				Environment: state.Environment,
				AIAppID:     state.AIAppID,
				Messages:    nil,
				TTLSeconds:  ttl,
			}
			if state.Identity != nil {
				newTh.Issuer = state.Identity.Issuer
				newTh.Subject = state.Identity.Subject
			}
			_ = s.threadStore.CreateThread(ctx, newTh)
		} else if err != nil {
			return fmt.Errorf("load thread %s: %w", state.ChatRequest.ThreadID, err)
		}
	}

	// 2. Semantic Memory Recall
	if state.AIApp != nil && state.AIApp.Memory.Enabled && state.AIApp.Memory.VectorRecall && s.memoryEngine != nil {
		if state.Identity != nil && state.Identity.Subject != "" {
			provider, err := s.memoryEngine.ResolveProvider(state.AIApp)
			if err == nil && provider != nil {
				limit := state.AIApp.Memory.RecallLimit
				if limit <= 0 {
					limit = 3
				}

				records, err := provider.Recall(ctx, core.MemoryFilter{
					ProjectID:   state.ProjectID,
					Environment: state.Environment,
					Issuer:      state.Identity.Issuer,
					Subject:     state.Identity.Subject,
					AIAppID:     state.AIAppID,
					Limit:       limit,
				})
				if err == nil && len(records) > 0 {
					var sb strings.Builder
					sb.WriteString("Relevant user memories:\n")
					for _, rec := range records {
						sb.WriteString("- ")
						sb.WriteString(rec.Content)
						sb.WriteString("\n")
					}

					sysMsg := core.ChatMessage{
						Role:    "system",
						Content: strings.TrimSpace(sb.String()),
					}
					// Prepend memory context
					state.ChatRequest.Messages = append([]core.ChatMessage{sysMsg}, state.ChatRequest.Messages...)
				}
			}
		}
	}

	// 3. Modality Adaptation & Placeholder Substitution (ADR-017 §7)
	// When the routed model lacks a modality found in message history,
	// substitute a text placeholder/caption so text-only models do not fail.
	adaptModalitiesForTargetModel(state)

	// 4. Enforce Context Window / Max History Turns per App & Model
	enforceContextWindow(state)

	return nil
}

// adaptModalitiesForTargetModel checks the target resolved/selected model's input modalities.
// If the target model lacks a modality found in history, substitute text placeholders.
func adaptModalitiesForTargetModel(state *core.ReqState) {
	if state == nil || state.ChatRequest == nil || len(state.ChatRequest.Messages) == 0 {
		return
	}

	// Determine target model modalities
	var supportedModalities []string
	if state.Snapshot != nil && state.SelectedModel != "" {
		if routing, ok := state.Snapshot.Model(state.SelectedModel); ok && routing != nil {
			supportedModalities = routing.InputModalities
		}
	} else if state.ResolvedModel != nil {
		supportedModalities = state.ResolvedModel.InputModalities
	}

	// If the model does not specify input_modalities, default assumed support is "text" only.
	// If it specifies input_modalities, check against that list.
	hasModality := func(mod string) bool {
		if len(supportedModalities) == 0 {
			// No explicit modalities configured: if target model is explicitly vision-oriented or similar,
			// or if unconfigured, default is text.
			return strings.EqualFold(mod, "text")
		}
		for _, sm := range supportedModalities {
			if strings.EqualFold(strings.TrimSpace(sm), mod) {
				return true
			}
		}
		return false
	}

	for i := range state.ChatRequest.Messages {
		msg := &state.ChatRequest.Messages[i]
		msg.EnsureParts()

		adapted := false
		for j := range msg.Parts {
			part := &msg.Parts[j]
			if part.Type == core.PartTypeMedia && part.Media != nil {
				mediaType := detectMediaModality(part.Media)
				if !hasModality(mediaType) {
					// Substitute placeholder
					caption := part.Media.Caption
					if caption == "" {
						caption = part.Media.Filename
					}
					desc := caption
					if desc != "" {
						desc = " (" + desc + ")"
					}
					refInfo := part.Media.Digest
					if refInfo == "" {
						refInfo = part.Media.MediaID
					}
					placeholder := fmt.Sprintf("[%s: %s%s]", mediaType, refInfo, desc)
					*part = core.TextPart(placeholder)
					adapted = true
				}
			}
		}
		if adapted {
			msg.Content = msg.TextContent()
		}
	}
}

func detectMediaModality(media *core.MediaRef) string {
	if media == nil {
		return "media"
	}
	mime := strings.ToLower(strings.TrimSpace(media.MimeType))
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "application/pdf") || strings.HasPrefix(mime, "text/"):
		return "document"
	default:
		return "image"
	}
}

// enforceContextWindow trims older message turns from state.ChatRequest.Messages
// if total turns exceed state.AIApp.Memory.MaxHistory while preserving system messages.
func enforceContextWindow(state *core.ReqState) {
	if state == nil || state.ChatRequest == nil || state.AIApp == nil {
		return
	}
	maxHistory := state.AIApp.Memory.MaxHistory
	if maxHistory <= 0 {
		return
	}

	msgs := state.ChatRequest.Messages
	// Count non-system messages
	var nonSysCount int
	for _, m := range msgs {
		if m.Role != "system" {
			nonSysCount++
		}
	}

	if nonSysCount <= maxHistory {
		return
	}

	toDrop := nonSysCount - maxHistory
	var pruned []core.ChatMessage
	dropped := 0
	for _, m := range msgs {
		if m.Role != "system" && dropped < toDrop {
			dropped++
			continue
		}
		pruned = append(pruned, m)
	}

	state.ChatRequest.Messages = pruned
}
