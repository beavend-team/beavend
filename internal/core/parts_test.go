package core_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/beavend/beavend/internal/core"
)

func TestChatMessage_EnsureParts(t *testing.T) {
	// Case 1: Legacy text message
	msg := core.ChatMessage{
		Role:    "user",
		Content: "Hello world",
	}
	msg.EnsureParts()
	require.Len(t, msg.Parts, 1)
	assert.Equal(t, core.PartTypeText, msg.Parts[0].Type)
	assert.Equal(t, "Hello world", msg.Parts[0].Text)
	assert.Equal(t, "Hello world", msg.TextContent())

	// Case 2: Legacy tool call message
	msgWithTools := core.ChatMessage{
		Role:    "assistant",
		Content: "",
		ToolCalls: []core.ToolCall{
			{ID: "call_1", Name: "search", Arguments: `{"q":"beavend"}`},
		},
	}
	msgWithTools.EnsureParts()
	require.Len(t, msgWithTools.Parts, 1)
	assert.Equal(t, core.PartTypeToolCall, msgWithTools.Parts[0].Type)
	require.NotNil(t, msgWithTools.Parts[0].ToolCall)
	assert.Equal(t, "call_1", msgWithTools.Parts[0].ToolCall.ID)

	// Case 3: Legacy tool result message
	toolResultMsg := core.ChatMessage{
		Role:       "tool",
		ToolCallID: "call_1",
		Content:    `{"status":"success"}`,
	}
	toolResultMsg.EnsureParts()
	require.Len(t, toolResultMsg.Parts, 2) // text part + tool_result part
	assert.Equal(t, core.PartTypeText, toolResultMsg.Parts[0].Type)
	assert.Equal(t, core.PartTypeToolResult, toolResultMsg.Parts[1].Type)
	assert.Equal(t, "call_1", toolResultMsg.Parts[1].ToolResult.CallID)

	// Case 4: Message constructed with Parts syncs Content
	partsMsg := core.ChatMessage{
		Role: "user",
		Parts: []core.Part{
			core.TextPart("Part 1"),
			core.MediaPart(&core.MediaRef{
				MediaID:  "med_123",
				MimeType: "image/png",
				Digest:   "sha256:abc",
				Caption:  "A diagram",
			}),
			core.TextPart("Part 2"),
		},
	}
	partsMsg.EnsureParts()
	assert.Equal(t, "Part 1\nPart 2", partsMsg.Content)
	assert.Equal(t, "Part 1\nPart 2", partsMsg.TextContent())
}

func TestChatMessage_JSONRoundTripWithParts(t *testing.T) {
	msg := core.ChatMessage{
		Role:    "user",
		Content: "Look at this picture",
		Parts: []core.Part{
			core.TextPart("Look at this picture"),
			core.MediaPart(&core.MediaRef{
				MediaID:  "med_test",
				MimeType: "image/jpeg",
				Digest:   "sha256:fedcba",
				URL:      "https://example.com/image.jpg",
				Caption:  "Sunset",
			}),
		},
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var unmarshaled core.ChatMessage
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, msg.Role, unmarshaled.Role)
	assert.Equal(t, msg.Content, unmarshaled.Content)
	require.Len(t, unmarshaled.Parts, 2)
	assert.Equal(t, core.PartTypeText, unmarshaled.Parts[0].Type)
	assert.Equal(t, core.PartTypeMedia, unmarshaled.Parts[1].Type)
	require.NotNil(t, unmarshaled.Parts[1].Media)
	assert.Equal(t, "med_test", unmarshaled.Parts[1].Media.MediaID)
	assert.Equal(t, "image/jpeg", unmarshaled.Parts[1].Media.MimeType)
	assert.Equal(t, "Sunset", unmarshaled.Parts[1].Media.Caption)
}
