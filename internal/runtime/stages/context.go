package stages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/beavend/beavend/internal/core"
	"github.com/beavend/beavend/internal/memory"
)

// ContextStage hydrates conversation threads, document memory, and user-scoped semantic memory.
type ContextStage struct {
	memoryEngine *memory.Engine
	threadStore  core.ThreadStore
	compactor    *memory.Compactor
	redisClient  redis.UniversalClient
}

// NewContextStage creates a new initialized ContextStage.
func NewContextStage(engine *memory.Engine, threadStore core.ThreadStore) *ContextStage {
	return &ContextStage{
		memoryEngine: engine,
		threadStore:  threadStore,
	}
}

// SetCompactor configures the asynchronous out-of-band compactor.
func (s *ContextStage) SetCompactor(c *memory.Compactor) {
	s.compactor = c
}

// SetRedisClient configures the Redis client for user-scoped document memory caching.
func (s *ContextStage) SetRedisClient(r redis.UniversalClient) {
	s.redisClient = r
}

// Handle executes the Context stage:
// 1. Thread retrieval and history prepending if thread_id is specified.
// 2. User-scoped semantic memory recall if AIApp memory is enabled.
func (s *ContextStage) Handle(ctx context.Context, state *core.ReqState) error {
	if state == nil || state.ChatRequest == nil {
		return nil
	}

	if isResumed, _ := state.Metadata["is_resumed_turn"].(bool); isResumed {
		// On resumed turns, thread history and semantic memories are already preserved in ChatRequest.Messages
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

			// Fast-path: find latest compaction summary checkpoint
			messagesToPrepend := th.Messages
			for i := len(th.Messages) - 1; i >= 0; i-- {
				m := th.Messages[i]
				if m.Role == "system" && m.Metadata != nil && m.Metadata["type"] == memory.CompactionSummaryMetadataType {
					messagesToPrepend = th.Messages[i:]
					break
				}
			}

			// Prepend previous thread history (deep copy to avoid mutating stored thread canonical parts)
			if len(messagesToPrepend) > 0 {
				copiedHistory := make([]core.ChatMessage, len(messagesToPrepend))
				for idx, orig := range messagesToPrepend {
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

	// 4. Snapshot-Compiled & Proposal-Gated Document Memory Hydration (0 hot-path database reads)
	hydrateDocumentMemories(ctx, state, s.redisClient)

	// 5. Two-Tier Context Compaction (0ms Hot-Path TTFT Impact)
	enforceTwoTierCompaction(state, s.compactor)

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

	// Adapt unsupported attachments in state.ChatRequest.Attachments
	if len(supportedModalities) > 0 && len(state.ChatRequest.Attachments) > 0 {
		var remainingAttachments []core.Attachment
		for _, att := range state.ChatRequest.Attachments {
			mod := detectAttachmentModality(&att)
			if !hasModality(mod) {
				// Convert unsupported attachment to placeholder in the last user message
				name := att.Filename
				if name == "" {
					name = mod
				}
				modelName := state.SelectedModel
				if modelName == "" && state.ResolvedModel != nil {
					modelName = state.ResolvedModel.LogicalModel
				}
				placeholder := fmt.Sprintf("[%s: %s (input modality not supported by target model %s)]", mod, name, modelName)
				if len(state.ChatRequest.Messages) > 0 {
					lastIdx := len(state.ChatRequest.Messages) - 1
					lastMsg := &state.ChatRequest.Messages[lastIdx]
					if lastMsg.Content != "" {
						lastMsg.Content += "\n" + placeholder
					} else {
						lastMsg.Content = placeholder
					}
					if len(lastMsg.Parts) > 0 {
						lastMsg.Parts = append(lastMsg.Parts, core.TextPart(placeholder))
					}
				}
			} else {
				remainingAttachments = append(remainingAttachments, att)
			}
		}
		state.ChatRequest.Attachments = remainingAttachments
	}
}

func detectAttachmentModality(att *core.Attachment) string {
	if att == nil {
		return "document"
	}
	if t := strings.ToLower(strings.TrimSpace(att.Type)); t != "" {
		return t
	}
	mime := strings.ToLower(strings.TrimSpace(att.MimeType))
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
		return "document"
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

// hydrateDocumentMemories injects snapshot-compiled project/app memories and Redis-cached user memories.
func hydrateDocumentMemories(ctx context.Context, state *core.ReqState, redisClient redis.UniversalClient) {
	if state == nil || state.Snapshot == nil {
		return
	}

	// 1. Snapshot-compiled project- and app-level document memories (0 DB reads!)
	compiledMems := state.Snapshot.DocumentMemoriesForApp(state.AIAppID)

	// 2. User-scoped document memory via Redis read-through cache
	var userMems []*core.DocumentMemory
	if state.Identity != nil && state.Identity.Subject != "" && redisClient != nil {
		userCacheKey := fmt.Sprintf("mem:user:%s:%s:%s", state.ProjectID, state.Environment, state.Identity.Subject)
		if cached, err := redisClient.Get(ctx, userCacheKey).Bytes(); err == nil && len(cached) > 0 {
			var decoded []*core.DocumentMemory
			if err := json.Unmarshal(cached, &decoded); err == nil {
				userMems = decoded
			}
		}
	}

	allMems := append(compiledMems, userMems...)
	if len(allMems) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("Project Guidelines & Document Memory:\n")
	for _, m := range allMems {
		sb.WriteString(fmt.Sprintf("- [%s]: %s\n", m.Key, m.Value))
	}

	docMemMsg := core.ChatMessage{
		Role:    "system",
		Content: strings.TrimSpace(sb.String()),
		Metadata: map[string]any{
			"type": "document_memory",
		},
	}

	if state.ChatRequest == nil {
		state.ChatRequest = &core.ChatRequest{}
	}
	state.ChatRequest.Messages = append([]core.ChatMessage{docMemMsg}, state.ChatRequest.Messages...)
}

// enforceTwoTierCompaction executes the fast-path synchronous context compaction:
// 1. Never makes synchronous LLM calls (0ms hot-path TTFT overhead).
// 2. When exceeding capacity threshold (75%), triggers asynchronous out-of-band summarization.
// 3. Truncates role: "tool" outputs older than the last 2 turns to 200 bytes.
// 4. Drops oldest turn groups if still over budget while strictly keeping assistant tool_calls paired with tool results.
func enforceTwoTierCompaction(state *core.ReqState, compactor *memory.Compactor) {
	if state == nil || state.ChatRequest == nil || len(state.ChatRequest.Messages) == 0 {
		return
	}

	budget := memory.DefaultContextBudgetTokens
	if state.AIApp != nil && state.AIApp.Memory.MaxTokens > 0 {
		budget = state.AIApp.Memory.MaxTokens
	}

	thresholdTokens := int(float64(budget) * memory.DefaultCapacityThresholdRatio)
	totalTokens := memory.ApproximateMessageTokens(state.ChatRequest.Messages)

	// If over 75% capacity, trigger asynchronous out-of-band summarization
	if totalTokens > thresholdTokens && compactor != nil && state.ChatRequest.ThreadID != "" {
		compactor.TriggerCompaction(context.Background(), state.ProjectID, state.Environment, state.AIAppID, state.ChatRequest.ThreadID)
	}

	maxHistory := 0
	if state.AIApp != nil && state.AIApp.Memory.MaxHistory > 0 {
		maxHistory = state.AIApp.Memory.MaxHistory
	}

	// Fast Trim Step 1: Truncate role: "tool" outputs older than the last 2 user dialogue turns to 200 bytes
	msgs := state.ChatRequest.Messages
	userTurns := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		m := &msgs[i]
		if m.Role == "user" {
			userTurns++
		}
		if userTurns > 2 && m.Role == "tool" {
			if len(m.Content) > 200 {
				m.Content = m.Content[:200] + "...[truncated]"
			}
			for j := range m.Parts {
				if m.Parts[j].Type == core.PartTypeText && len(m.Parts[j].Text) > 200 {
					m.Parts[j].Text = m.Parts[j].Text[:200] + "...[truncated]"
				}
			}
		}
	}

	totalTokens = memory.ApproximateMessageTokens(msgs)

	needsPruning := totalTokens > budget
	if maxHistory > 0 {
		nonSys := 0
		for _, m := range msgs {
			if m.Role != "system" {
				nonSys++
			}
		}
		if nonSys > maxHistory {
			needsPruning = true
		}
	}

	if !needsPruning {
		state.ChatRequest.Messages = msgs
		return
	}

	// Fast Trim Step 2: Drop oldest turn groups (strictly keeping assistant tool_calls paired with tool results)
	var leadingSystem []core.ChatMessage
	var rest []core.ChatMessage
	for i, m := range msgs {
		if m.Role == "system" && len(rest) == 0 {
			leadingSystem = append(leadingSystem, m)
		} else {
			rest = msgs[i:]
			break
		}
	}

	var groups [][]core.ChatMessage
	i := 0
	for i < len(rest) {
		m := rest[i]
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			grp := []core.ChatMessage{m}
			i++
			for i < len(rest) && rest[i].Role == "tool" {
				grp = append(grp, rest[i])
				i++
			}
			groups = append(groups, grp)
		} else {
			groups = append(groups, []core.ChatMessage{m})
			i++
		}
	}

	for len(groups) > 1 {
		current := memory.ApproximateMessageTokens(leadingSystem)
		for _, g := range groups {
			current += memory.ApproximateMessageTokens(g)
		}

		overBudget := current > budget
		overHistory := maxHistory > 0 && len(groups) > maxHistory

		if !overBudget && !overHistory {
			break
		}

		groups = groups[1:]
	}

	finalMsgs := make([]core.ChatMessage, 0, len(leadingSystem)+len(groups)*2)
	finalMsgs = append(finalMsgs, leadingSystem...)
	for _, g := range groups {
		finalMsgs = append(finalMsgs, g...)
	}

	state.ChatRequest.Messages = finalMsgs
}
