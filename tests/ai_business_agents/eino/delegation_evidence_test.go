//go:build ai_business_agents

package eino

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	messages "github.com/leventsg/e-commerce-AI-system/dal/model/ai/messages"
	calls "github.com/leventsg/e-commerce-AI-system/dal/model/ai/tool_calls"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
)

func TestDelegationEvidenceUnavailableIsUniform(t *testing.T) {
	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{UserID: 42, ConversationID: "conv"})
	for _, storeErr := range []error{sql.ErrNoRows, messages.ErrNotFound} {
		resolver := &DelegationEvidenceResolver{FindMessages: func(context.Context, uint64, string, []string) ([]*messages.AiMessages, error) { return nil, storeErr }}
		if _, err := resolver.Resolve(ctx, "message:missing"); err != ErrDelegationEvidenceUnavailable {
			t.Fatalf("not-found leaked storage details: %v", err)
		}
	}
	resolver := &DelegationEvidenceResolver{}
	for _, ref := range []string{"message:missing", "tool_call:missing", "unknown:missing", "message:", "message: msg"} {
		if _, err := resolver.Resolve(ctx, ref); err != ErrDelegationEvidenceUnavailable {
			t.Fatalf("reference %q gave %v", ref, err)
		}
	}
}

func TestDelegationEvidenceRequiresTrustedOwnership(t *testing.T) {
	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{UserID: 42, ConversationID: "conv"})
	row := &calls.AiToolCalls{UserId: 42, ConversationId: "conv", ToolCallId: "call", ToolName: "order_cancel", Arguments: `{"order_id":"o"}`, Result: `{"cancelled":false}`, Status: "failed", ErrorMessage: "already shipped"}
	queried := 0
	resolver := &DelegationEvidenceResolver{FindToolCall: func(_ context.Context, uid uint64, cid, id string) (*calls.AiToolCalls, error) {
		queried++
		if uid != 42 || cid != "conv" || id != "call" {
			t.Fatalf("wrong trusted query scope: %d %s %s", uid, cid, id)
		}
		return row, nil
	}}
	got, err := resolver.Resolve(ctx, "tool_call:call")
	if err != nil || got.Status != "failed" || got.ToolName != "order_cancel" || string(got.Result) != `{"cancelled":false}` || got.ErrorMessage != "already shipped" {
		t.Fatalf("lost real failure: %#v %v", got, err)
	}
	for _, mutate := range []func(){func() { row.UserId = 7 }, func() { row.UserId = 42; row.ConversationId = "other" }, func() { row.ConversationId = "conv"; row.ToolCallId = "other" }} {
		mutate()
		if _, err := resolver.Resolve(ctx, "tool_call:call"); !errors.Is(err, ErrDelegationEvidenceUnavailable) {
			t.Fatalf("cross-owner/ref evidence accepted: %v", err)
		}
	}
	before := queried
	for _, ref := range []string{"tool_call:call", "unknown:call", "message:", "tool_call: call"} {
		if _, err := resolver.Resolve(context.Background(), ref); !errors.Is(err, ErrDelegationEvidenceUnavailable) {
			t.Fatalf("untrusted lookup: %v", err)
		}
	}
	if queried != before {
		t.Fatal("untrusted request queried storage")
	}
}

func TestDelegationEvidenceMessagesAndBounds(t *testing.T) {
	ctx := helper.WithToolExecutionContext(context.Background(), helper.ToolExecutionContext{UserID: 42, ConversationID: "conv"})
	row := &messages.AiMessages{UserId: 42, ConversationId: "conv", MsgId: "msg", Role: "user", Content: "Do not cancel"}
	resolver := &DelegationEvidenceResolver{FindMessages: func(_ context.Context, uid uint64, cid string, ids []string) ([]*messages.AiMessages, error) {
		if uid != 42 || cid != "conv" || len(ids) != 1 || ids[0] != "msg" {
			t.Fatal("message query omitted ownership")
		}
		return []*messages.AiMessages{row}, nil
	}}
	got, err := resolver.Resolve(ctx, "message:msg")
	if err != nil || got.Content != "Do not cancel" || got.Role != "user" {
		t.Fatalf("message unavailable: %#v %v", got, err)
	}
	row.UserId = 9
	if _, err := resolver.Resolve(ctx, "message:msg"); !errors.Is(err, ErrDelegationEvidenceUnavailable) {
		t.Fatal("cross-owner message accepted")
	}
	row.UserId = 42
	row.Content = strings.Repeat("x", 33*1024)
	if _, err := resolver.Resolve(ctx, "message:msg"); !errors.Is(err, ErrDelegationEvidenceUnavailable) {
		t.Fatal("oversized evidence accepted")
	}
	boom := errors.New("storage offline")
	resolver.FindMessages = func(context.Context, uint64, string, []string) ([]*messages.AiMessages, error) { return nil, boom }
	if _, err := resolver.Resolve(ctx, "message:msg"); !errors.Is(err, boom) {
		t.Fatalf("infrastructure failure hidden: %v", err)
	}
}
