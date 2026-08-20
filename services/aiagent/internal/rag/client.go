package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
)

const maxRAGAPIResponse = 4 << 20

type retrievedChunk struct {
	ChunkID       string
	Content       string
	Score         float64
	DocumentID    string
	DocumentTitle string
	SourcePath    string
	SourceURL     string
}

type ragAPIEnvelope struct {
	Code      string     `json:"code"`
	Message   any        `json:"message"`
	Data      ragAPIData `json:"data"`
	RequestID any        `json:"requestId"`
	Success   bool       `json:"success"`
}

type ragAPIData struct {
	QueryEmbedding []float32     `json:"queryEmbedding"`
	Chunks         []ragAPIChunk `json:"chunks"`
}

type ragAPIChunk struct {
	ChunkID  string         `json:"chunkId"`
	Content  string         `json:"content"`
	Score    float64        `json:"score"`
	Document ragAPIDocument `json:"document"`
}

type ragAPIDocument struct {
	DocumentID string `json:"documentId"`
	Title      string `json:"title"`
	Type       string `json:"type"`
	UpdatedAt  string `json:"updatedAt"`
	SourceURL  string `json:"sourceUrl"`
	SourcePath string `json:"sourcePath"`
}

type retrieveRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"topK"`
}

type retrieveOutcome struct {
	QueryEmbedding []float32
	Chunks         []retrievedChunk
}

type embeddingEnvelope struct {
	Code    string        `json:"code"`
	Success bool          `json:"success"`
	Data    embeddingData `json:"data"`
}

type embeddingData struct {
	Embedding []float32 `json:"embedding"`
}

type embeddingRequest struct {
	Query          string `json:"query"`
	EmbeddingModel string `json:"embeddingModel"`
}

type RagentClient struct {
	baseURL       string
	apiKey        string
	retrievePath  string
	embeddingPath string
	httpClient    *http.Client
}

func NewRagentClient(cfg config.RAGConfig) *RagentClient {
	timeout := time.Duration(cfg.Timeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &RagentClient{
		baseURL:       strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:        strings.TrimSpace(cfg.APIKey),
		retrievePath:  firstNonEmpty(cfg.RetrievePath, "/api/ragent/open-api/v1/retrieve"),
		embeddingPath: firstNonEmpty(cfg.EmbeddingPath, "/api/ragent/open-api/v1/embedding"),
		httpClient:    &http.Client{Timeout: timeout},
	}
}

func (c *RagentClient) Retrieve(ctx context.Context, query string, topK int) (*retrieveOutcome, error) {
	if c == nil || c.baseURL == "" || c.apiKey == "" {
		return nil, fmt.Errorf("ragent client not configured")
	}
	body, err := json.Marshal(retrieveRequest{Query: query, TopK: topK})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.retrievePath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRAGAPIResponse))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ragent retrieve status %d: %s", resp.StatusCode, truncateRAGBody(raw))
	}

	var envelope ragAPIEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode ragent retrieve response: %w", err)
	}
	if !envelope.Success || envelope.Code != "0" {
		msg, _ := envelope.Message.(string)
		return nil, fmt.Errorf("ragent retrieve code=%s msg=%s", envelope.Code, msg)
	}

	chunks := make([]retrievedChunk, 0, len(envelope.Data.Chunks))
	for _, chunk := range envelope.Data.Chunks {
		chunks = append(chunks, retrievedChunk{
			ChunkID:       chunk.ChunkID,
			Content:       strings.TrimSpace(chunk.Content),
			Score:         chunk.Score,
			DocumentID:    chunk.Document.DocumentID,
			DocumentTitle: strings.TrimSpace(chunk.Document.Title),
			SourcePath:    chunk.Document.SourcePath,
			SourceURL:     chunk.Document.SourceURL,
		})
	}
	return &retrieveOutcome{
		QueryEmbedding: envelope.Data.QueryEmbedding,
		Chunks:         chunks,
	}, nil
}

func (c *RagentClient) Embed(ctx context.Context, query string, embeddingModel string) ([]float32, error) {
	if c == nil || c.baseURL == "" || c.apiKey == "" {
		return nil, fmt.Errorf("ragent client not configured")
	}
	body, err := json.Marshal(embeddingRequest{Query: query, EmbeddingModel: embeddingModel})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+c.embeddingPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRAGAPIResponse))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ragent embed status %d: %s", resp.StatusCode, truncateRAGBody(raw))
	}

	var envelope embeddingEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode ragent embed response: %w", err)
	}
	if !envelope.Success || envelope.Code != "0" {
		return nil, fmt.Errorf("ragent embed code=%s", envelope.Code)
	}
	vector := envelope.Data.Embedding
	if len(vector) == 0 {
		return nil, fmt.Errorf("ragent embed returned empty vector")
	}
	return vector, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func truncateRAGBody(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}
