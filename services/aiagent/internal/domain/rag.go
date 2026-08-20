package domain

// AgentSource 是注入 assistant_message 的来源信息，按文档去重分组。
type AgentSource struct {
	DocumentID  string             `json:"document_id"`
	Title       string             `json:"title"`
	DocumentURL string             `json:"document_url,omitempty"`
	Chunks      []AgentSourceChunk `json:"chunks"`
}

// AgentSourceChunk 是单条检索片段，Content 为前端展示用的截断内容。
type AgentSourceChunk struct {
	ChunkID string  `json:"chunk_id"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}
