# AI Agent 记忆与上下文机制技术说明

本文档描述 AI 客服当前代码已经实现的记忆、上下文持久化和裁剪行为。文中将“当前实现”与“设计目标”明确区分，避免把尚未接入的长期记忆、会话摘要或结构化 Tool 上下文误认为可用能力。

## 1. 总体架构

当前系统没有独立的 Agent Memory 引擎。实际记忆能力主要由 MySQL 消息历史和每次请求时的上下文转换完成：

```text
WebSocket user_message
-> apis/ai
-> AiAgent.Chat RPC
-> ConversationManager 保存用户消息
-> 从 ai_messages 加载最近 20 条消息
-> Intent Planner 选择最多 8 条近期消息
-> 生成执行计划
-> Tool Dispatcher 或普通 ChatModel
-> 保存 assistant/tool 事件
-> WebSocket 返回事件
```

当前存在三类记忆数据：

| 类型 | 当前状态 | 数据来源 |
| --- | --- | --- |
| 会话短期记忆 | 已实现 | `ai_conversations`、`ai_messages` |
| 工具与确认操作记录 | 已实现，但不会全部进入模型上下文 | `ai_tool_calls`、`ai_confirmations` |
| 用户长期记忆 | 只有数据表和 Model，尚未接入业务流程 | `ai_user_memories` |

## 2. 会话生命周期

会话管理位于 `services/aiagent/internal/conversation/manager.go`。

### 2.1 创建会话

首次请求没有 `conversation_id` 时：

1. 创建 `ai_conversations` 记录。
2. 生成 `conv_` 前缀的会话 ID。
3. 使用认证上下文中的用户 ID绑定会话。
4. 将会话状态设置为 `active`。
5. 保存本次用户消息。
6. 查询该会话最近消息作为本次上下文。

当前会话标题始终为空，没有自动标题生成和更新逻辑。

### 2.2 恢复会话

请求携带 `conversation_id` 时：

1. 查询 `ai_conversations`。
2. 校验会话的 `user_id` 是否等于当前认证用户。
3. 跨用户访问返回 `ErrConversationForbidden`。
4. 校验通过后保存新用户消息并加载历史。

WebSocket 连接会在内存中保存 Chat RPC 返回的最新 `conversation_id`，但连接状态本身不会持久化。重新连接时必须由客户端重新提供会话 ID。

## 3. 消息持久化

消息存储在 `ai_messages`，当前使用 `user`、`assistant` 和 `tool` 三种 role。

### 3.1 User 消息

保存字段包括：

- 后端生成的 `msg_` 前缀消息 ID；
- 会话 ID；
- 认证用户 ID；
- 用户原始文本；
- `metadata.source`，例如 `web`；
- 创建时间。

用户消息在 Planner 和模型调用之前写入数据库。因此后续模型或工具失败时，用户输入仍会保留。

### 3.2 Assistant 消息

以下内容以 `assistant` role 保存：

- 普通聊天模型回复；
- Planner 缺少参数时的追问；
- 工具执行后的用户可读摘要；
- 模型不可用和其他错误提示。

当前没有独立的 `error` role。`error` 事件持久化后同样表现为 assistant 消息。

### 3.3 Tool 消息

工具结果和确认事件以 `tool` role 保存。

`content` 保存用户可读摘要，例如：

```text
购物车共有 1 件条目。
```

`metadata` 保存结构化信息：

```json
{
  "tool_name": "cart.list",
  "status": "success",
  "confirmation_id": "",
  "data_json": "{\"items\":[{\"cart_item_id\":2,\"product_id\":2,\"quantity\":1}],\"total\":1}"
}
```

因此 `cart_item_id`、`product_id`、数量、订单状态等结构化 Tool 数据实际已经写入数据库。

### 3.4 批量原子保存

一次 Chat 或 ConfirmAction 响应产生的多个服务端事件会转换为 `[]*AiMessages`，再通过一条多值 `INSERT` 保存。单条 SQL 由 InnoDB 保证全部成功或全部失败。

用户消息的保存发生在调用 Planner 之前，与后续 assistant/tool 事件不属于同一个数据库事务。外部业务 RPC 也不属于消息保存事务。

## 4. 工具、确认与审计记录

### 4.1 ai_tool_calls

每次工具执行记录：

- conversation ID和用户 ID；
- Tool名称；
- 脱敏后的 arguments；
- success/failed 状态；
- 结果摘要或错误；
- 调用延迟。

这些记录用于审计和排障，当前不会在后续请求中加载到 Planner或 ChatModel。

### 4.2 ai_confirmations

高风险确认保存：

- confirmation ID；
- conversation ID和用户 ID；
- Tool名称和持久化执行参数；
- 操作摘要；
- pending/approved/rejected/executed/failed 状态；
- 过期时间和执行时间。

确认事件的用户可读结果会写入 `ai_messages`，但完整 `ai_confirmations` 记录不会自动注入后续模型上下文。

## 5. Intent Planner 上下文

Planner 的上下文构造位于 `services/aiagent/internal/planner/planner.go`。

ConversationManager先加载最近 20 条消息，Planner再执行以下裁剪：

1. 从历史末尾向前选择消息。
2. 只接受 `user`、`assistant` 和 `tool` role。
3. 最多保留最近 8 条有效消息。
4. 当前用户消息已在历史中，按文本去重后在末尾重新追加。
5. 每条消息先压缩空白并进行敏感字段脱敏。
6. 每条消息最多保留 300 个 Unicode 字符。
7. 将选中的消息恢复为时间正序。
8. 在最前面添加 Intent System Prompt。

最终请求结构为：

```text
Intent System Prompt
+ 最多 8 条近期消息
+ 当前用户消息
```

当前 System Prompt约 3.7 KB，包含 Intent、Tool名称、参数和安全规则。

## 6. 普通 ChatModel 上下文

普通聊天通过 `services/aiagent/internal/eino/messages.go` 将最近历史转换为 Eino `schema.Message`。

与 Planner 相比，普通 ChatModel：

- 使用 ConversationManager返回的最多 20 条消息；
- 不再裁剪为 8 条；
- 不对每条内容执行 300 字符限制；
- 不计算总 token 数；
- 当前没有额外添加客服 System Prompt；
- 当前用户消息已经包含在 History 中，因此不会重复追加。

这意味着普通 ChatModel可以接收较完整的近期文本，但上下文总长度可能超过 Provider限制。

## 7. 已保存但未进入模型的上下文

这是当前实现最重要的限制。

Planner 和 ChatModel恢复 Tool 消息时只读取：

```go
type messageMetadata struct {
    ToolCallID string `json:"tool_call_id"`
    ToolName   string `json:"tool_name"`
}
```

虽然 `metadata.data_json` 已经保存在 MySQL，但没有被拼接到 Tool Message 的 `content`。模型实际只能看到用户可读摘要。

例如数据库中保存：

```json
{
  "cart_item_id": 2,
  "product_id": 2,
  "quantity": 1
}
```

Planner 实际看到的可能只有：

```text
购物车共有 1 件条目。
```

因此当前系统可能无法从上一轮购物车查询中解析“这个商品”“机械键盘”“减少一件”等指代。这不是 Tool 结果没有持久化，而是上下文转换时丢弃了结构化字段。

## 8. 超过上下文阈值时的处理

### 8.1 数据库存储阈值

数据库层当前没有消息数量上限：

- 不删除旧消息；
- 不自动归档；
- 不生成会话摘要；
- 不压缩历史记录。

### 8.2 ConversationManager 阈值

默认只查询最近 20 条消息：

```go
const defaultHistoryLimit = 20
```

超过 20 条后，旧消息仍保留在数据库，但不再进入本次请求上下文。

一次工具调用通常产生两条消息：

```text
tool_result
assistant 可读摘要
```

因此工具操作会消耗两个历史槽位，旧上下文会比纯聊天更快被挤出最近 20 条。

### 8.3 Planner 阈值

Planner在最近 20 条中只选择最后 8 条有效消息，并把每条限制在 300 个字符。某个 Tool结果即使还处于数据库最近 20 条中，也可能因为不在最后 8 条而无法用于意图识别。

### 8.4 Token 阈值

当前没有 token级上下文预算：

- 不计算 Prompt token 数；
- 不根据模型上下文窗口动态裁剪；
- 不按“系统提示、当前问题、关键 Tool 状态、普通历史”设置优先级；
- 不在超阈值前生成摘要；
- 超过 Provider限制时依赖外部模型返回错误。

## 9. 长期记忆状态

`ai_user_memories` 表和 Model已经生成，并注入 `ServiceContext`，但当前没有实际读写调用。

尚未实现：

- 从对话提取用户偏好；
- 写入或更新长期记忆；
- 按用户加载长期记忆；
- 将长期记忆注入 Planner或 ChatModel；
- 记忆置信度、冲突、过期和删除策略；
- 跨会话记忆。

所以当前 Agent仅具备会话内的近期消息记忆，不具备真正的用户长期记忆。

## 10. 当前能力与缺口

### 已实现

- 会话创建和恢复；
- 会话用户隔离；
- 用户、assistant和 Tool消息持久化；
- 最近 20 条历史加载；
- Planner最近 8 条和单条 300 字符裁剪；
- 简单敏感文本脱敏；
- Tool调用、确认和审计记录；
- 服务端事件批量原子保存。

### 尚未实现或未接通

- `tool_result.data_json` 恢复到模型上下文；
- Token级上下文预算；
- 会话摘要和滑动摘要；
- 关键业务状态优先保留；
- 基于语义的历史检索；
- 商品名称与购物车条目的稳定映射；
- 跨会话用户长期记忆；
- 长期记忆生命周期管理。

## 11. 结论

当前所谓的 Agent记忆，本质上是基于 `ai_messages` 的近期消息窗口，而不是完整的状态化 Memory系统。

系统已经能够保存完整业务事件，但模型实际看到的上下文主要是用户和 assistant文本以及 Tool可读摘要。结构化 Tool结果、完整确认状态、工具审计记录和长期用户记忆尚未进入推理上下文。

购物车场景暴露的核心问题是：结构化数据已经保存，但在上下文转换阶段被丢弃；同时固定消息数量裁剪会继续把较早但仍重要的业务状态移出上下文。
