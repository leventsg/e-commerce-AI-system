package eino

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/zeromicro/go-zero/core/logx"
)

// finalSummaryInstruction 是迭代耗尽前最后一次模型调用的约束指令：
// 此时工具列表已被清空，模型无法再发起工具调用，只能基于已有工具结果
// 输出诚实的最终答复。
const finalSummaryInstruction = "系统提示：本次任务已达到工具调用轮数上限，本轮不再提供任何工具。" +
	"请基于以上已完成的工具执行结果，直接向用户输出最终答复：" +
	"如实说明已经完成了什么、还有什么没有完成；不得虚构未执行的操作或结果；" +
	"如果任务未能完成，请给出下一步建议（例如请用户细化需求后重新提问）。"

// finalSummaryMiddleware 在 ChatModelAgent 达到 MaxIterations 前的最后一次
// 模型调用上做兜底：清空工具列表并注入总结指令，迫使模型输出一条基于
// 已有工具结果的最终文字答复。
//
// 背景：eino adk 的 react 图在 ChatModel 节点 StatePreHandler 中检查剩余
// 迭代数，耗尽时直接返回 ErrExceedMaxIterations 硬错误（adk/react.go），
// 且不会触发 AfterAgent 钩子，用户侧只能看到"AI 服务暂时不可用"。该中间件
// 把"第 MaxIterations 次调用"改造成"无工具的强制总结轮"，让会话正常收尾。
type finalSummaryMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
	agentName string
}

func newFinalSummaryMiddleware(agentName string) adk.ChatModelAgentMiddleware {
	return &finalSummaryMiddleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		agentName:                    agentName,
	}
}

// BeforeModelRewriteState 在每次模型调用前执行。
// eino react 图在 ChatModel 节点的 StatePreHandler 中先递减剩余迭代数，
// 再进入模型 wrapper 链，因此这里读到 RemainingIterations == 0 表示
// 当前调用是最后一次模型机会：本次若再输出工具调用，下一轮就会触发
// ErrExceedMaxIterations。
func (m *finalSummaryMiddleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if m == nil || state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	remaining, ok := graphRemainingIterations(ctx)
	if !ok || remaining != 0 {
		return ctx, state, nil
	}
	logx.Infow("agent 达到最大迭代，最后一次模型调用强制总结",
		logx.Field("component", "supervisor_agent"),
		logx.Field("agent_name", m.agentName),
		logx.Field("remaining_iterations", remaining),
	)
	// 清空工具：model.WithTools(nil) 会转为空工具列表，请求不再携带任何
	// 工具定义，模型在 API 层面无法产生 tool_calls，只能输出文字。
	state.ToolInfos = nil
	state.DeferredToolInfos = nil
	state.Messages = appendFinalSummaryInstruction(state.Messages)
	if m.agentName != supervisorAgentName {
		// Preserve the delegation result contract even on the forced final turn.
		state.Messages[len(state.Messages)-1].Content += "\n本轮仍须向 Supervisor 返回既定六字段 JSON 结果，不直接回复用户；未完成事项使用 partial 或 needs_input 表达。"
	}
	return ctx, state, nil
}

// graphRemainingIterations 读取 react 图 state 中已被本次调用 StatePreHandler
// 递减后的剩余迭代数。非 react 图上下文（无法获取 state）时返回 ok=false，
// 中间件安全放行。
func graphRemainingIterations(ctx context.Context) (int, bool) {
	remaining := -1
	if err := compose.ProcessState(ctx, func(_ context.Context, st *adk.State) error {
		if st != nil {
			remaining = st.RemainingIterations
		}
		return nil
	}); err != nil {
		return -1, false
	}
	return remaining, remaining >= 0
}

// appendFinalSummaryInstruction 在消息末尾追加总结指令。
// 使用 user 角色：tool 结果消息之后追加 user 消息符合 ChatML 协议约束。
// 不修改原切片，避免污染调用方持有的消息列表。
func appendFinalSummaryInstruction(messages []*schema.Message) []*schema.Message {
	next := make([]*schema.Message, 0, len(messages)+1)
	next = append(next, messages...)
	next = append(next, schema.UserMessage(finalSummaryInstruction))
	return next
}
