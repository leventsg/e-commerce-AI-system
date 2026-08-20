package rag

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
)

func TestRagentClientRetrieve(t *testing.T) {
	var authHeader string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, `{
			"code":"0","success":true,"data":{"chunks":[{
				"chunkId":"chunk-1",
				"content":"知识内容",
				"score":0.9,
				"document":{"documentId":"doc-1","title":"规范.md","sourceUrl":"","sourcePath":"a/b.md"}
			}],"queryEmbedding":[0.1,0.2]}}`)
	}))
	defer server.Close()

	client := NewRagentClient(config.RAGConfig{
		BaseURL: server.URL, APIKey: "key-1",
		Timeout: 1000,
	})
	outcome, err := client.Retrieve(context.Background(), "query", 5)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	chunks := outcome.Chunks
	if len(chunks) != 1 || chunks[0].ChunkID != "chunk-1" || chunks[0].DocumentID != "doc-1" {
		t.Fatalf("chunks=%+v", chunks)
	}
	if len(outcome.QueryEmbedding) != 2 || outcome.QueryEmbedding[0] != 0.1 {
		t.Fatalf("query embedding=%+v", outcome.QueryEmbedding)
	}
	if authHeader != "Bearer key-1" {
		t.Fatalf("auth header=%q", authHeader)
	}
	if gotBody == nil || gotBody["topK"] != float64(5) || gotBody["query"] != "query" {
		t.Fatalf("body=%+v", gotBody)
	}
}

func TestParseDecision(t *testing.T) {
	decision, err := parseDecision("```json\n{\"need_rag\":true,\"confidence\":0.8,\"reason\":\"policy\"}\n```")
	if err != nil {
		t.Fatalf("parseDecision: %v", err)
	}
	if !decision.NeedRAG || decision.Confidence != 0.8 {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestBuildSourcesDedupesAndTruncates(t *testing.T) {
	sources := buildSources([]retrievedChunk{
		{ChunkID: "c1", Content: strings.Repeat("知", 500), Score: 0.9, DocumentID: "d1", DocumentTitle: "doc"},
		{ChunkID: "c2", Content: "second", Score: 0.8, DocumentID: "d1", DocumentTitle: "doc"},
	}, config.RAGConfig{SourcePreviewChars: 200, PreviewBaseURL: "http://kb.example.com"})
	if len(sources) != 1 {
		t.Fatalf("sources len=%d", len(sources))
	}
	if len(sources[0].Chunks) != 2 || len([]rune(sources[0].Chunks[0].Content)) != 200 {
		t.Fatalf("source=%+v", sources[0])
	}
	if sources[0].DocumentURL != "http://kb.example.com/preview/doc/d1" {
		t.Fatalf("document_url=%q", sources[0].DocumentURL)
	}
}

func TestRagentClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer server.Close()
	client := NewRagentClient(config.RAGConfig{BaseURL: server.URL, APIKey: "k", Timeout: 20})
	_, err := client.Retrieve(context.Background(), "q", 5)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
