package tools

import "github.com/beavend/beavend/internal/core"

// BuiltinAskUserToolDefinition constructs the tool definition for ask_user.
func BuiltinAskUserToolDefinition() core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "ask_user",
		Name:        "ask_user",
		Description: "Ask the user a clarification question or request additional information.",
		Version:     1,
		SideEffect:  "read",
		Confirmation: "none",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "The clarification question to ask the user",
				},
				"options": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"description": "Optional list of choices for the user to select from",
				},
			},
			"required": []any{"question"},
		},
		Executor: core.ExecutorConfig{
			Type: "ask_user",
		},
	}
}

// BuiltinSubAgentToolDefinition constructs the tool definition for sub_agent.
func BuiltinSubAgentToolDefinition() core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "sub_agent",
		Name:        "sub_agent",
		Description: "Delegate a scoped subtask to an isolated child agent.",
		Version:     1,
		SideEffect:  "read",
		Confirmation: "none",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "The task or objective for the sub-agent to accomplish",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Optional context or background data for the sub-agent",
				},
			},
			"required": []any{"task"},
		},
		Executor: core.ExecutorConfig{
			Type: "sub_agent",
		},
	}
}

// BuiltinProposeMemoryToolDefinition constructs the tool definition for propose_memory.
func BuiltinProposeMemoryToolDefinition() core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "propose_memory",
		Name:        "propose_memory",
		Description: "Propose an addition or update to project, app, or user document memory for admin review.",
		Version:     1,
		SideEffect:  "write",
		Confirmation: "none",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"target_scope": map[string]any{
					"type":        "string",
					"enum":        []any{"project", "app", "user"},
					"description": "Scope of memory to update ('project', 'app', or 'user')",
				},
				"key": map[string]any{
					"type":        "string",
					"description": "Unique key or title for the memory entry",
				},
				"value": map[string]any{
					"type":        "string",
					"description": "Content of the memory snippet (max 2048 bytes)",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Justification explaining why this memory should be remembered",
				},
			},
			"required": []any{"target_scope", "key", "value", "reason"},
		},
		Executor: core.ExecutorConfig{
			Type: "propose_memory",
		},
	}
}

// BuiltinKnowledgeSearchToolDefinition constructs the tool definition for knowledge_search.
func BuiltinKnowledgeSearchToolDefinition() core.ToolDefinition {
	return core.ToolDefinition{
		ID:          "knowledge_search",
		Name:        "knowledge_search",
		Description: "Search the application knowledge base using hybrid semantic vector and keyword retrieval.",
		Version:     1,
		SideEffect:  "read",
		Confirmation: "none",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query or factual topic to retrieve documentation for",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of passages to return (default 5)",
				},
			},
			"required": []any{"query"},
		},
		Executor: core.ExecutorConfig{
			Type: "knowledge_search",
		},
	}
}
