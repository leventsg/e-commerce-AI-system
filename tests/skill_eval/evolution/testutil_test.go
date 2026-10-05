//go:build ai_skill_evolution

package evolution

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/eino"
)

// repoRoot walks up from this file until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// scriptedModel is a deterministic, in-memory model for the tested-agent path.
// It never performs a business RPC and only returns pre-scripted messages.
type scriptedModel struct {
	script []*schema.Message
	next   int
}

func (m *scriptedModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) { return m, nil }
func (m *scriptedModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if m.next >= len(m.script) {
		return &schema.Message{Role: schema.Assistant, Content: "unused"}, nil
	}
	msg := m.script[m.next]
	m.next++
	return msg, nil
}
func (m *scriptedModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := m.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func toolCall(name, args string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "call_" + name, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}}
}

func reply(content string) *schema.Message {
	return &schema.Message{Role: schema.Assistant, Content: content}
}

// scriptedFactory returns a ModelFactory whose agents follow the given scripts.
// Agent models are built in spec order (sub-agents first, supervisor last), which
// matches NewSupervisorAgent's construction order. Because a single factory is
// reused across all A/B trials, the agent index wraps modulo 6 per trial.
func scriptedFactory(supervisor []*schema.Message) eino.ModelFactory {
	return scriptedFactoryWithAgents(map[string][]*schema.Message{"supervisor_agent": supervisor})
}

// scriptedFactoryWithAgents lets each agent (by name) follow its own script.
func scriptedFactoryWithAgents(scripts map[string][]*schema.Message) eino.ModelFactory {
	names := []string{"product_discovery_agent", "shopping_decision_agent", "order_fulfillment_agent", "after_sales_agent", "general_agent", "supervisor_agent"}
	built := 0
	return eino.NewModelFactory(eino.WithChatModelBuilder(func(context.Context, string, config.EinoConfig) (model.BaseChatModel, error) {
		name := names[built%len(names)]
		built++
		script := scripts[name]
		if len(script) == 0 {
			script = []*schema.Message{reply("unused")}
		}
		return &scriptedModel{script: script}, nil
	}))
}
