package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/beavend/beavend/internal/core"
)

const (
	// CompactionSummaryMetadataType identifies system messages containing compaction summaries.
	CompactionSummaryMetadataType = "compaction_summary"
	// DefaultCapacityThresholdRatio is the default context budget utilization (75%) triggering compaction.
	DefaultCapacityThresholdRatio = 0.75
	// DefaultContextBudgetTokens is the fallback context token budget when unconfigured.
	DefaultContextBudgetTokens = 8192
)

// LLMAdapter abstracts the model chat completion call needed for compaction summaries.
type LLMAdapter interface {
	Chat(ctx context.Context, req core.ChatRequest) (*core.ChatResponse, error)
}

// CompactorConfig configures the asynchronous out-of-band Compactor.
type CompactorConfig struct {
	ThreadStore core.ThreadStore
	UsageStore  core.UsageStore
	Adapter     LLMAdapter
	Model       string
	Threshold   float64
	Logger      *slog.Logger
}

// Compactor manages out-of-band asynchronous conversation summarization and checkpoint persistence.
type Compactor struct {
	threadStore core.ThreadStore
	usageStore  core.UsageStore
	adapter     LLMAdapter
	model       string
	threshold   float64
	logger      *slog.Logger
	inFlight    sync.Map // threadKey -> struct{} to deduplicate concurrent triggers
}

// NewCompactor creates a new Compactor.
func NewCompactor(cfg CompactorConfig) *Compactor {
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultCapacityThresholdRatio
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Compactor{
		threadStore: cfg.ThreadStore,
		usageStore:  cfg.UsageStore,
		adapter:     cfg.Adapter,
		model:       cfg.Model,
		threshold:   cfg.Threshold,
		logger:      cfg.Logger,
	}
}

// Threshold returns the capacity threshold ratio (e.g. 0.75).
func (c *Compactor) Threshold() float64 {
	return c.threshold
}

// TriggerCompaction launches an out-of-band compaction job for a thread if not already running.
func (c *Compactor) TriggerCompaction(ctx context.Context, projectID, env, appID, threadID string) {
	if c == nil || c.threadStore == nil || c.adapter == nil || threadID == "" {
		return
	}

	key := fmt.Sprintf("%s:%s:%s", projectID, env, threadID)
	if _, loaded := c.inFlight.LoadOrStore(key, struct{}{}); loaded {
		return // Compaction already in-flight for this thread
	}

	go func() {
		defer c.inFlight.Delete(key)

		compactCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if err := c.CompactThread(compactCtx, projectID, env, appID, threadID); err != nil {
			c.logger.WarnContext(compactCtx, "asynchronous thread compaction failed",
				"thread_id", threadID,
				"project_id", projectID,
				"env", env,
				"error", err,
			)
		}
	}()
}

// CompactThread performs the out-of-band summarization for older conversation turns in a thread.
func (c *Compactor) CompactThread(ctx context.Context, projectID, env, appID, threadID string) error {
	th, err := c.threadStore.GetThread(ctx, projectID, env, threadID)
	if err != nil {
		return fmt.Errorf("load thread %s: %w", threadID, err)
	}
	if th == nil || len(th.Messages) < 4 {
		return nil // Not enough messages to summarize
	}

	// Determine split point: keep the last 2 turns intact, summarize the older messages.
	// Find the message index to split at.
	splitIdx := len(th.Messages) - 4
	if splitIdx <= 0 {
		splitIdx = 1
	}

	// Ensure we do not split an assistant message from its subsequent tool results!
	for splitIdx < len(th.Messages)-1 {
		if th.Messages[splitIdx].Role == "tool" {
			splitIdx++
		} else {
			break
		}
	}

	olderMsgs := th.Messages[:splitIdx]
	recentMsgs := th.Messages[splitIdx:]

	if len(olderMsgs) == 0 {
		return nil
	}

	// Format older messages into a readable conversation transcript for the summarizer LLM
	var transcript strings.Builder
	transcript.WriteString("Please summarize the following conversation history into a concise, detailed recap preserving all key facts, user preferences, and decisions:\n\n")

	for _, m := range olderMsgs {
		role := strings.ToUpper(m.Role)
		content := m.Content
		if len(m.Parts) > 0 {
			content = m.TextContent()
		}
		if len(content) > 500 {
			content = content[:500] + "...[truncated]"
		}
		transcript.WriteString(fmt.Sprintf("[%s]: %s\n", role, content))
	}

	chatReq := core.ChatRequest{
		Model: c.model,
		Messages: []core.ChatMessage{
			{
				Role:    "system",
				Content: "You are a concise conversation summarizer. Provide a coherent, factual summary recap of earlier dialogue.",
			},
			{
				Role:    "user",
				Content: transcript.String(),
			},
		},
	}

	start := time.Now().UTC()
	resp, err := c.adapter.Chat(ctx, chatReq)
	if err != nil {
		return fmt.Errorf("compact summary chat: %w", err)
	}
	if resp == nil || resp.Message.Content == "" {
		return fmt.Errorf("empty summary response from adapter")
	}

	summaryContent := strings.TrimSpace(resp.Message.Content)

	// Build the compaction summary system checkpoint message
	summaryMsg := core.ChatMessage{
		Role:    "system",
		Content: fmt.Sprintf("Summary of previous conversation:\n%s", summaryContent),
		Metadata: map[string]any{
			"type":            CompactionSummaryMetadataType,
			"compacted_turns": len(olderMsgs),
			"compacted_at":    time.Now().UTC().Format(time.RFC3339),
		},
	}

	newMessages := make([]core.ChatMessage, 0, len(recentMsgs)+1)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, recentMsgs...)

	// Update thread in store
	th.Messages = newMessages
	th.UpdatedAt = time.Now().UTC()
	if err := c.threadStore.CreateThread(ctx, th); err != nil {
		return fmt.Errorf("update thread checkpoint: %w", err)
	}

	// Record compaction usage event
	if c.usageStore != nil {
		usageState := &core.ReqState{
			RequestID:   fmt.Sprintf("comp_%s_%d", threadID, time.Now().Unix()),
			ProjectID:   projectID,
			Environment: env,
			AIAppID:     appID,
			Usage:       resp.Usage,
			Metadata: map[string]any{
				"source":    "compaction",
				"thread_id": threadID,
			},
			StartTime: start,
		}
		_ = c.usageStore.RecordUsage(context.Background(), usageState)
	}

	c.logger.InfoContext(ctx, "thread compaction completed",
		"thread_id", threadID,
		"compacted_messages", len(olderMsgs),
		"remaining_messages", len(newMessages),
	)

	return nil
}

// ApproximateTokens estimates token count from text using 4 characters per token.
func ApproximateTokens(text string) int {
	chars := len(text)
	tokens := (chars + 3) / 4
	if tokens < 1 && chars > 0 {
		return 1
	}
	return tokens
}

// ApproximateMessageTokens calculates the approximate tokens in a list of chat messages.
func ApproximateMessageTokens(messages []core.ChatMessage) int {
	total := 0
	for _, m := range messages {
		total += ApproximateTokens(m.Content)
		for _, p := range m.Parts {
			total += ApproximateTokens(p.Text)
		}
		for _, tc := range m.ToolCalls {
			total += ApproximateTokens(tc.Name) + ApproximateTokens(tc.Arguments)
		}
	}
	return total
}
