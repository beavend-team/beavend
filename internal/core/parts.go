package core

import "strings"

// PartType denotes the canonical message part category.
type PartType string

const (
	PartTypeText       PartType = "text"
	PartTypeMedia      PartType = "media"
	PartTypeToolCall   PartType = "tool_call"
	PartTypeToolResult PartType = "tool_result"
)

// MediaRef represents an object storage reference to media (image, audio, video, document).
type MediaRef struct {
	MediaID  string `json:"media_id,omitempty" yaml:"media_id,omitempty"`
	MimeType string `json:"mime_type" yaml:"mime_type"`
	Digest   string `json:"digest,omitempty" yaml:"digest,omitempty"`
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
	Caption  string `json:"caption,omitempty" yaml:"caption,omitempty"`
	Filename string `json:"filename,omitempty" yaml:"filename,omitempty"`
	Size     int64  `json:"size,omitempty" yaml:"size,omitempty"`
}

// ToolResultPart represents structured tool output in a canonical message turn.
type ToolResultPart struct {
	CallID   string         `json:"call_id" yaml:"call_id"`
	ToolID   string         `json:"tool_id,omitempty" yaml:"tool_id,omitempty"`
	Content  string         `json:"content" yaml:"content"`
	IsError  bool           `json:"is_error,omitempty" yaml:"is_error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

// Part represents a canonical message chunk in a model-agnostic thread turn.
type Part struct {
	Type       PartType        `json:"type" yaml:"type"`
	Text       string          `json:"text,omitempty" yaml:"text,omitempty"`
	Media      *MediaRef       `json:"media,omitempty" yaml:"media,omitempty"`
	ToolCall   *ToolCall       `json:"tool_call,omitempty" yaml:"tool_call,omitempty"`
	ToolResult *ToolResultPart `json:"tool_result,omitempty" yaml:"tool_result,omitempty"`
}

// TextPart creates a new text canonical part.
func TextPart(text string) Part {
	return Part{
		Type: PartTypeText,
		Text: text,
	}
}

// MediaPart creates a new media canonical part.
func MediaPart(media *MediaRef) Part {
	return Part{
		Type:  PartTypeMedia,
		Media: media,
	}
}

// EnsureParts populates Parts from legacy Content and ToolCalls if Parts is empty,
// or synchronizes Content from Parts if Content is empty.
func (m *ChatMessage) EnsureParts() {
	if len(m.Parts) == 0 {
		var parts []Part
		if m.Content != "" {
			parts = append(parts, TextPart(m.Content))
		}
		for _, tc := range m.ToolCalls {
			tcCopy := tc
			parts = append(parts, Part{
				Type:     PartTypeToolCall,
				ToolCall: &tcCopy,
			})
		}
		if m.Role == "tool" && m.ToolCallID != "" {
			parts = append(parts, Part{
				Type: PartTypeToolResult,
				ToolResult: &ToolResultPart{
					CallID:  m.ToolCallID,
					Content: m.Content,
				},
			})
		}
		m.Parts = parts
		return
	}

	// If Parts exists but Content is empty, sync Content for backward compatibility
	if m.Content == "" {
		m.Content = m.TextContent()
	}
}

// TextContent extracts and concatenates all text fragments from the message parts.
func (m *ChatMessage) TextContent() string {
	if len(m.Parts) == 0 {
		return m.Content
	}
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type == PartTypeText && p.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(p.Text)
		}
	}
	if b.Len() == 0 {
		return m.Content
	}
	return b.String()
}
