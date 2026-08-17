# AI 客服 MemoryProvider 上下文架构

## 目标

本项目的 AI 客服上下文工程已从手动 `ContextManager.Build()` 组装，重构为 AGGO 风格的 `MemoryProvider + MemoryMiddleware + ADK session values` 分层，并由 `ChatLogic` 统一触发异步长期上下文更新。

新的边界是：

- `ChatLogic` 负责会话准备、用户原文落库、幂等重放、durable 事件落库、SSE 转发和 agent 成功结束后的 memory update 触发。
- `services/aiagent/internal/memory` 定义领域级上下文 provider，不依赖 Eino 类型。
- `services/aiagent/internal/eino/memory_middleware.go` 是 Eino ADK middleware 适配层，只负责模型调用前注入上下文。
- `contextmanager` 中已有的 message、summary、tool result、profile store 继续复用，`ai_user_memory_events` 作为长期事件来源。

## 模型消息顺序

每次 supervisor root model 调用前，MemoryMiddleware 将消息重组为：

```text
system: 稳定客服系统提示词
history: 摘要水位后的近期 user/assistant 原文
current user: 原始用户输入 + runtime context
assistant/tool: ADK ReAct 中间消息
```

动态上下文不会写入 system prompt，也不会写回用户原始历史。runtime context 追加在当前 user message 后，格式为：

```text
用户原始输入

-----
<current_time>...</current_time>
<conversation_context>...</conversation_context>
<latest_tool_result>...</latest_tool_result>
<recent_tool_calls>...</recent_tool_calls>
<task_state>...</task_state>
<user_memory_recent_events>...</user_memory_recent_events>
<user_profile>...</user_profile>
```

## 关键组件

- `memory.MemoryProvider`
  - `Retrieve` 返回 `SystemMessages`、`HistoryMessages`、`ContextMessages` 三类槽位。
  - `Memorize` 不参与在线主链路；长期上下文更新由 `ChatLogic.updateConversationMemory` 统一触发。
  - `Close` 为后续外部 provider 预留生命周期。
- `memory.ConversationMetadata`
  - 统一表达 `userID`、`conversationID`、`runID`、`currentMessageID`、`clientMessageID`。
  - 一个 `conversationID` 就是一轮客服对话的唯一会话标识。
- `eino.MemoryMiddleware`
  - 从 Eino 固定 API `adk.WithSessionValues` 注入的 values 中读取可信身份。
  - 同一 run 只注入一次，避免 ReAct 多次调用重复追加上下文。
- `Capability Tool: search_user_memory`
  - supervisor root 可用的长期事件检索工具。
  - 模型参数里的 `user_id` 会被忽略，真实 user ID 只来自 ToolExecutionContext。
- `Capability Tool: get_tool_call_result`
  - supervisor root 和所有子 agent 可用的工具结果读取工具。
  - 根据 runtime context 中的 `tool_call_id` 读取当前用户当前 conversation 的历史工具调用真实结果。
  - 只接受 `tool_call_id` 参数，不接受 `user_id`、`conversation_id` 或 `session_id`。
  - Capability Tool 统一注册进 Registry，统一经过 Executor，并记录到 `ai_tool_calls`。

## 数据来源

`CustomerServiceProvider.Retrieve` 复用现有数据：

- `ai_messages`：摘要水位后的近期 user/assistant 原文。
- `ai_conversation_summaries`：会话滚动摘要。
- `ai_tool_calls`：最近一次完整工具调用和最近工具调用列表的事实来源，`result` 字段保存真实工具返回 JSON；`<recent_tool_calls>` 只注入工具名、参数和 `tool_call_id`，需要历史 result 时由模型调用 `get_tool_call_result` 按需读取。
- `ai_user_profiles`：聊天来源用户画像。
- `ai_user_memory_events`：最近长期事件和按需检索。

所有读取必须带认证 user ID 和 conversation ID。上下文数据是不可信事实，不能覆盖 system prompt、工具白名单、确认规则或 Execution Guard。

## Final Ownership

最终回答归属规则同步收敛：

- callback 不发送任何用户可见事件，仅适合作为日志、trace、metrics 等只读观测入口。
- `consumeEvents` 消费 ADK iterator，并统一转换 reasoning、tool call、tool result、confirmation interrupt、error 和 final assistant。
- 普通 streaming content 不转换为 `assistant_delta`；iterator 中 supervisor 的无 tool call assistant message 是唯一 final assistant fact。
- `ChatLogic` 和 `ConfirmActionLogic` 先落库 durable final assistant，再下发 `assistant_message` 给前端。

这样可以避免 supervisor 中间计划、阶段性判断和最终回答混在同一个用户可见输出里。

## 写回策略

记忆更新入口统一在 `ChatLogic.updateConversationMemory`。`runSupervisor` 正常返回后说明前面的 durable 消息落库没有返回错误，ChatLogic 直接异步执行：

1. 调用 `SummaryManager.MaybeRefresh(userID, conversationID)`。
2. 如果未创建新摘要，直接结束，不发布 Kafka。
3. 如果创建了新摘要，使用 `SummaryRefreshResult.CompressedMessageIDs` 发布一个 `memoryupdate.UpdateEvent` 到 `AiMemoryUpdates` topic。
4. Profile consumer 和 Memory Event consumer 订阅同一个 topic，各自基于同一批 compressed message IDs 调用独立结构化模型，分别更新 `ai_user_profiles` 和 `ai_user_memory_events`。

这样避免 ReAct 多次模型调用重复更新记忆，也保证画像和长期事件来自同一批被摘要压缩的消息。

## 长期事件检索

长期事件使用 `ai_user_memory_events` 存储，`SQLEventStore` 支持：

- `ListRecentUserMemoryEvents`：按 user ID 读取最近事件，进入 `<user_memory_recent_events>`。
- `SearchUserMemoryEvents`：按可信 user ID、关键词、类型、日期范围和 limit 检索。
- `search_user_memory`：Supervisor root 可见的 Capability Tool，模型传入的 `user_id` 被忽略。

## 验证命令

```bash
go test ./services/aiagent/internal/memory
go test ./services/aiagent/internal/eino
go test ./services/aiagent/internal/logic
go test ./services/aiagent/...
```
