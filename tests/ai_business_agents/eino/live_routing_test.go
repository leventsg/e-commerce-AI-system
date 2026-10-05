//go:build ai_business_agents

package eino

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/config"
	agentprompt "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/agent"
	aitools "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools"
)

// This opt-in check calls a real model with synthetic requests and production
// prompts/schemas. It never executes a business tool or contacts business RPCs.
// It measures first-turn routing, not full conversation success or accuracy.
func TestLiveBusinessAgentRouting(t *testing.T) {
	if os.Getenv("AI_AGENT_LIVE_ROUTING") != "1" {
		t.Skip("set AI_AGENT_LIVE_ROUTING=1 and AI_AGENT_LIVE_API_KEY/MODEL/BASE_URL to run real-model routing")
	}
	cfg := config.EinoConfig{
		Provider: "openai-compatible", APIKey: os.Getenv("AI_AGENT_LIVE_API_KEY"),
		Model: os.Getenv("AI_AGENT_LIVE_MODEL"), BaseURL: os.Getenv("AI_AGENT_LIVE_BASE_URL"),
		Timeout: 45, MaxTokens: 2048, Temperature: 0,
	}
	if cfg.APIKey == "" || cfg.Model == "" || cfg.BaseURL == "" {
		t.Fatal("live routing requires AI_AGENT_LIVE_API_KEY, AI_AGENT_LIVE_MODEL and AI_AGENT_LIVE_BASE_URL")
	}
	ctx := context.Background()
	factory := NewModelFactory()
	registry := aitools.NewRegistry(aitools.DefaultBusinessTools(aitools.DefaultToolClients{}, config.ToolTimeoutConfig{}))
	var infos []*schema.ToolInfo
	for _, spec := range supervisorSubAgentSpecs {
		child, err := newBusinessAgent(ctx, factory, cfg, registry, nil, nil, nil, spec)
		if err != nil {
			t.Fatal("failed to construct live child model")
		}
		info, err := adk.NewAgentTool(ctx, child).Info(ctx)
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, info)
	}
	chatModel, err := newAgentChatModel(ctx, factory, cfg, infos)
	if err != nil {
		t.Fatal("failed to construct live supervisor model")
	}
	cases := []struct{ name, request, want string }{
		{"discovery", "搜索500元以内的耳机，先给我候选商品。", "product_discovery_agent"},
		{"decision", "比较商品101和102哪个适合出差，帮我分析取舍。", "shopping_decision_agent"},
		{"coupon", "查一下我有哪些可用优惠券。", "shopping_decision_agent"},
		{"cart", "把商品101加入购物车，数量1。", "shopping_decision_agent"},
		{"order", "查订单O123的当前状态。", "order_fulfillment_agent"},
		{"checkout", "我要结算商品101一件，使用地址2，请准备下单。", "order_fulfillment_agent"},
		{"address", "查询我的收货地址列表。", "order_fulfillment_agent"},
		{"cancel", "我要取消订单O123。", "after_sales_agent"},
		{"refund", "订单O123我要退款，不要取消订单。", "after_sales_agent"},
		{"profile", "请把我的用户名改为小林。", "general_agent"},
		{"chat", "你好，今天心情不错，聊两句吧。", "general_agent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			callCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
			defer cancel()
			answer, err := chatModel.Generate(callCtx, []*schema.Message{
				schema.SystemMessage(agentprompt.SupervisorSystemPrompt), schema.UserMessage(tc.request),
			})
			if err != nil {
				t.Fatal("live model request failed; check endpoint, credentials and connectivity")
			}
			if answer == nil {
				t.Fatal("live model returned no message")
			}
			var names []string
			for _, call := range answer.ToolCalls {
				names = append(names, call.Function.Name)
			}
			t.Logf("model=%s scenario=%s agent_tools=%v", cfg.Model, tc.name, names)
			if len(names) != 1 || names[0] != tc.want {
				t.Errorf("route=%v, want only %s", names, tc.want)
			}
		})
	}
}
