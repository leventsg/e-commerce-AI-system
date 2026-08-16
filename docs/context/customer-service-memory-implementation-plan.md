# AI 客服 MemoryProvider 重构实施记录

## 已实施

- 新增 `services/aiagent/internal/memory`：
  - `provider.go` 定义 `MemoryProvider`、`RetrieveRequest`、`RetrieveResult`、`MemorizeRequest`、`UserMemoryEventSearcher`。
  - `session.go` 定义 conversation value keys、`ConversationMetadata`、`NewConversationValues` 和测试用 context metadata。
  - `customer_service_provider.go` 将现有 message、summary、tool result、memory、profile store 映射为 AGGO 风格 provider。
- 新增 `services/aiagent/internal/eino/memory_middleware.go`：
  - `BeforeModelRewriteState` 自动注入 history 和 runtime context。
  - `AfterModelRewriteState` 只识别最终自然语言 assistant，并保留原始 user message 边界。
- 调整 Eino supervisor：
  - root agent 挂载 MemoryMiddleware。
  - 子 agent 不挂完整会话上下文 middleware。
  - Runner 使用 Eino 固定 API `adk.WithSessionValues` 注入可信 user/conversation/run/message metadata，values 中只包含 `conversationID`，不包含旧会话 ID 键。
- 新增 `search_user_memory`：
  - 工具位于 `services/aiagent/internal/tools/memory_search_tool.go`。
  - 如果 provider 实现 `UserMemoryEventSearcher`，supervisor 自动注册该工具。
  - 工具忽略模型传入的 `user_id`，只使用后端注入的 ToolExecutionContext user ID。
- 调整在线入口：
  - `ChatLogic` 不再调用 `ContextManager.Build()`。
  - Chat 当前输入作为最小 user message 进入 Runner。
  - final `assistant_message` 持久化后下发 SSE。
  - `ConfirmActionLogic` 同样下发 final `assistant_message`。
- 收敛前端事件来源：
  - callback 不发送任何用户可见事件，只适合日志、trace、metrics 等只读观测。
  - `consumeEvents` 是唯一 ADK iterator -> `domain.AgentEvent` 转换入口。
  - reasoning 转为 `assistant_thinking_delta`，assistant tool call 转为 `tool_progress`，tool message 转为 `tool_result`。
  - 普通 streaming content 不发 `assistant_delta`，supervisor final assistant 是唯一最终回答事实。

## 后续可扩展项

- 为长期记忆事件分析器补充自动写入逻辑，将稳定 milestone/event 写入 `ai_user_memory_events`。
- 为 `search_user_memory` 增加更完整的 any/all、时间范围和类型过滤集成测试。
- 如果需要恢复逐字 final streaming，必须新增明确 final phase buffer，不能回到 callback content 直发。

## 验收测试

当前重构对应的重点测试：

- `services/aiagent/internal/memory/provider_test.go`
  - Provider 三槽位、摘要去重、runtime context、Memorize hooks。
- `services/aiagent/internal/eino/memory_middleware_test.go`
  - middleware 注入一次、runtime context 追加当前 user、原始 user 写回。
- `services/aiagent/internal/eino/iterator_events_test.go`
  - reasoning、工具进度、工具结果、错误诊断和 final assistant 均来自 ADK iterator。
- `services/aiagent/internal/logic/stream_visibility_test.go`
  - final assistant 落库并发送前端。
- `services/aiagent/internal/tools/memory_search_tool_test.go`
  - `search_user_memory` 使用可信 user ID。

目标命令：

```bash
go test ./services/aiagent/...
go test ./apis/ai/...
```
