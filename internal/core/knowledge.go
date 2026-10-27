package core

import "time"

// KnowledgeDocMeta represents lightweight metadata of an ingested document compiled into the snapshot.
type KnowledgeDocMeta struct {
	ID          string    `json:"id" yaml:"id"`
	AppID       string    `json:"app_id" yaml:"app_id"`
	Title       string    `json:"title" yaml:"title"`
	Description string    `json:"description" yaml:"description"`
	SourceURL   string    `json:"source_url,omitempty" yaml:"source_url,omitempty"`
	Tags        []string  `json:"tags,omitempty" yaml:"tags,omitempty"`
	Revision    int       `json:"revision" yaml:"revision"`
	UpdatedAt   time.Time `json:"updated_at" yaml:"updated_at"`
}

// SearchResult represents a document or passage returned by search engines or hybrid retrieval.
type SearchResult struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Score    float64        `json:"score"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
