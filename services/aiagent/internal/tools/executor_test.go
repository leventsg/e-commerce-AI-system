package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
)

func TestExecutorInjectsAuthenticatedUserID(t *testing.T) {
	registry := NewRegistry(config.ToolTimeoutConfig{})
	executor := NewExecutor(registry)

	var got HandlerRequest
	event := executor.Execute(context.Background(), ExecuteRequest{
		UserID:         42,
		ConversationID: "conv_1",
		MessageID:      "msg_1",
		ToolName:       domain.ToolCartAdd,
		Arguments: map[string]any{
			"user_id":    999,
			"product_id": 12,
			"quantity":   2,
			"profile": map[string]any{
				"token": "secret",
				"keep":  "value",
			},
			"items": []any{
				map[string]any{
					"auth":   "bearer",
					"sku_id": 7,
				},
			},
		},
	}, func(_ context.Context, req HandlerRequest) (HandlerResult, error) {
		got = req
		return HandlerResult{
			Data:    map[string]any{"ok": true},
			Summary: "购物车已更新。",
		}, nil
	})

	if event.Status != "success" {
		t.Fatalf("event.Status = %q, want success", event.Status)
	}
	if got.UserID != 42 {
		t.Fatalf("handler UserID = %d, want authenticated user 42", got.UserID)
	}
	assertNoSensitiveKey(t, got.Arguments)
	if _, ok := got.Arguments["product_id"]; !ok {
		t.Fatalf("sanitized arguments lost product_id: %#v", got.Arguments)
	}
}

func TestExecutorUsesRegistryTimeouts(t *testing.T) {
	registry := NewRegistry(config.ToolTimeoutConfig{})
	executor := NewExecutor(registry)

	assertToolDeadline(t, executor, domain.ToolProductSearch, 3*time.Second)
	assertToolDeadline(t, executor, domain.ToolCartAdd, 5*time.Second)
}

func TestExecutorFailureEventDoesNotClaimSuccess(t *testing.T) {
	registry := NewRegistry(config.ToolTimeoutConfig{})
	executor := NewExecutor(registry)

	event := executor.Execute(context.Background(), ExecuteRequest{
		UserID:         42,
		ConversationID: "conv_1",
		MessageID:      "msg_1",
		ToolName:       domain.ToolOrderGet,
		Arguments:      map[string]any{"order_id": "202406300001"},
	}, func(context.Context, HandlerRequest) (HandlerResult, error) {
		return HandlerResult{}, errors.New("rpc unavailable")
	})

	if event.Type != domain.EventToolResult {
		t.Fatalf("event.Type = %q, want %q", event.Type, domain.EventToolResult)
	}
	if event.Status != "failed" {
		t.Fatalf("event.Status = %q, want failed", event.Status)
	}
	if strings.Contains(event.Content, "已成功") {
		t.Fatalf("failure content must not claim success: %q", event.Content)
	}
	if event.DataJSON == "" {
		t.Fatal("failed event should include compact error payload")
	}
}

func TestExecutorUnknownToolFailsBeforeHandler(t *testing.T) {
	registry := NewRegistry(config.ToolTimeoutConfig{})
	executor := NewExecutor(registry)

	called := false
	event := executor.Execute(context.Background(), ExecuteRequest{
		UserID:         42,
		ConversationID: "conv_1",
		MessageID:      "msg_1",
		ToolName:       "missing.tool",
		Arguments:      map[string]any{},
	}, func(context.Context, HandlerRequest) (HandlerResult, error) {
		called = true
		return HandlerResult{}, nil
	})

	if called {
		t.Fatal("handler was called for unknown tool")
	}
	if event.Type != domain.EventToolResult {
		t.Fatalf("event.Type = %q, want %q", event.Type, domain.EventToolResult)
	}
	if event.Status != "failed" {
		t.Fatalf("event.Status = %q, want failed", event.Status)
	}
}

func assertToolDeadline(t *testing.T, executor *Executor, toolName string, want time.Duration) {
	t.Helper()

	event := executor.Execute(context.Background(), ExecuteRequest{
		UserID:         42,
		ConversationID: "conv_1",
		MessageID:      "msg_1",
		ToolName:       toolName,
		Arguments:      map[string]any{},
	}, func(ctx context.Context, _ HandlerRequest) (HandlerResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("%s handler context has no deadline", toolName)
		}
		remaining := time.Until(deadline)
		if remaining < want-500*time.Millisecond || remaining > want+500*time.Millisecond {
			t.Fatalf("%s deadline remaining = %s, want about %s", toolName, remaining, want)
		}
		return HandlerResult{Data: map[string]any{"ok": true}}, nil
	})
	if event.Status != "success" {
		t.Fatalf("%s event.Status = %q, want success", toolName, event.Status)
	}
}

func assertNoSensitiveKey(t *testing.T, value any) {
	t.Helper()

	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "user_id", "token", "session_id", "auth":
				t.Fatalf("sensitive key %q leaked in %#v", key, typed)
			}
			assertNoSensitiveKey(t, nested)
		}
	case []any:
		for _, item := range typed {
			assertNoSensitiveKey(t, item)
		}
	}
}
