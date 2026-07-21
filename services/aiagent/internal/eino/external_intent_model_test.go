package eino

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
)

func TestExternalIntentModelGenerate(t *testing.T) {
	if os.Getenv("RUN_AI_MODEL_INTEGRATION") != "1" {
		t.Skip("set RUN_AI_MODEL_INTEGRATION=1 to call the external intent model")
	}

	cfg := config.EinoConfig{
		Provider:    requiredEnv(t, "AI_INTENT_MODEL_PROVIDER"),
		APIKey:      requiredEnv(t, "AI_INTENT_MODEL_API_KEY"),
		BaseURL:     requiredEnv(t, "AI_INTENT_MODEL_BASE_URL"),
		Model:       requiredEnv(t, "AI_INTENT_MODEL_NAME"),
		Timeout:     5,
		MaxTokens:   128,
		Temperature: 0,
	}

	totalStarted := time.Now()
	createStarted := time.Now()
	chatModel, err := NewModelFactory().NewChatModel(context.Background(), cfg)
	createElapsed := time.Since(createStarted)
	if err != nil {
		t.Fatalf("create external intent model failed after %s: reason=%s err=%v", createElapsed, ErrorReason(err), err)
	}
	t.Logf("external intent model created in %s", createElapsed)

	generateStarted := time.Now()
	response, err := chatModel.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage(`你是一名客服人员，请根据用户的输入，判断用户的意图，并返回一个简短的意图描述。`),
		schema.UserMessage("西红柿炒蛋怎么做"),
	})
	generateElapsed := time.Since(generateStarted)
	if err != nil {
		t.Fatalf("external intent model generate failed: generate_elapsed=%s total_elapsed=%s reason=%s err=%v", generateElapsed, time.Since(totalStarted), ErrorReason(err), err)
	}
	if response == nil {
		t.Fatalf("external intent model returned nil response: generate_elapsed=%s total_elapsed=%s", generateElapsed, time.Since(totalStarted))
	}
	if strings.TrimSpace(response.Content) == "" {
		t.Fatalf("external intent model returned empty content: generate_elapsed=%s total_elapsed=%s", generateElapsed, time.Since(totalStarted))
	}

	t.Logf("external intent model succeeded: generate_elapsed=%s total_elapsed=%s response=%q", generateElapsed, time.Since(totalStarted), response.Content)
}

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("required environment variable %s is empty", name)
	}
	return value
}
