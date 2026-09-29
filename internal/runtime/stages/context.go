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

			// Prepend previous thread history
			if len(th.Messages) > 0 {
				state.ChatRequest.Messages = append(th.Messages, state.ChatRequest.Messages...)
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

	return nil
}
