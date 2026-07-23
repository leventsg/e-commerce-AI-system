# AI Agent 轻量上下文优化方案（问题 1、3、5 一体化）

## 1. 文档目的

本文针对 `docs/question.txt` 中的问题 1（上下文工程）、问题 3（工具协议）和问题 5（工具结果被上下文拦截）设计一套适合本地开发阶段的轻量方案。

新的目标不是做企业级上下文治理，也不是持久化每次模型输入，而是把原始消息、工具结果和模型输入解耦：

- 原始消息完整保存；
- 长对话通过滚动摘要压缩；
- 每次模型调用前由统一 Context Manager 临时组装输入；
- 近期原文默认保留 20 条；
- 工具结果采用“最近一次完整结果 + 历史 Tool Call 引用 + 按需读取完整结果”；
- token 只做估算日志，不参与运行时裁剪，不阻塞模型调用。

本文是目标设计；`docs/ai-agent-memory-context.md` 描述当前实现状态。

## 2. 当前问题

当前上下文链路主要依赖固定历史窗口：

```text
ConversationManager
  -> 从 ai_messages 读取最近 20 条
  -> Intent Planner 再选最近 8 条
  -> 每条最多 300 个 Unicode 字符
  -> 普通 ChatModel 使用最近 20 条
```

这套实现存在以下问题：

1. 超出窗口的消息仍在数据库，但模型无法再看到。
2. 没有滚动摘要，过去已经确认的事实和未完成任务会随窗口滚动丢失。
3. 旧消息的 `ai_messages.metadata.data_json` 保存了结构化工具结果，但旧消息转换只向模型提供可读摘要，商品 ID、订单号、购物车条目 ID 等事实可能丢失。
4. Planner 和普通 ChatModel 使用不同的手写历史裁剪逻辑，输入口径不一致。
5. `ai_user_memories` 只有表和 Model，尚未接入读写、冲突、过期和用户隔离流程。
6. 没有轻量的上下文组装日志，无法快速解释一次模型调用用了哪些摘要、近期消息和工具引用。

## 3. 目标与非目标

### 3.1 目标

- 原始消息完整保存，模型输入不再等同于数据库最近 N 条记录。
- 使用统一 Context Manager 为 Planner 和 Agent 临时组装模型输入。
- 用滚动摘要控制长对话规模：默认保留最近 20 条未压缩消息；当未压缩消息达到 30 条时，将最早 10 条与旧摘要合并成新摘要。
- 保证每条消息只属于一种上下文来源：要么仍在近期原文窗口，要么已经进入摘要，不重复注入。
- 保留结构化工具结果，不对 ID、订单号、数量和状态做字符串截断。
- 上下文中只直接放最近一次完整工具结果；其他历史工具调用以引用形式进入上下文，需要完整结果时按 `tool_call_id` 从数据库读取。
- 支持长期用户记忆、最小任务状态和等待确认状态。
- 通过结构化日志记录每次 context 组装来源、摘要覆盖水位、近期消息范围、最近工具结果和历史工具引用。
- 所有上下文读取都使用认证用户 ID 查询，禁止模型或客户端指定用户范围。

### 3.2 非目标

- 本阶段不持久化每次模型输入。
- 本阶段不设计独立的上下文快照表。
- 本阶段不做运行时 token 上限裁剪，也不因为估算 token 偏高而拒绝模型调用。
- 本阶段不实现向量数据库、Embedding 或语义检索基础设施。
- 本阶段不重构 Intent Planner 的职责，也不实现 ReAct 多轮循环。
- Context Manager 不替代 Tool Registry、Execution Guard 或 Confirmation Manager。

## 4. 总体架构

```text
WebSocket / AiAgent.Chat
          |
          v
ConversationManager ---------> ai_messages（完整原始消息）
          |
          v
Context Manager
  |-- MessageStore
  |-- SummaryStore
  |-- MemoryStore
  |-- TaskStateStore
  |-- ToolResultStore
  |-- ToolCallRefStore
  `-- UserProfileSource（可选）
          |
          v
临时 ContextMessages
  |-- IntentContext -> Intent Planner -> Eino IntentModel 适配器
  `-- AgentContext  -> Eino 消息适配器 -> ChatModel / 后续 Agent Graph
          |
          v
工具执行 / assistant 输出
  |-- 持久化 ToolResult envelope
  |-- 更新 TaskState
  |-- 异步评估摘要水位
  `-- 按策略提取长期记忆候选
```

MySQL 是原始消息、工具调用、工具结果、摘要、记忆和运行状态的事实来源。Redis 只缓存可重建的热数据和 Agent checkpoint，不作为唯一事实来源。

## 5. 上下文对象

### 5.1 ConversationMessage

来自 `ai_messages` 的不可变原始记录。保存 user、assistant、tool 消息和事件 metadata。旧消息不因模型窗口缩小而删除。

### 5.2 ConversationSummary

对已压缩历史片段的增量摘要，包含：

- 自然语言摘要；
- 稳定关键事实；
- 未完成事项；
- 已覆盖消息水位；
- 估算 token 数。

摘要是可重建派生数据，不能覆盖原始消息，也不能作为订单、库存、金额等动态业务数据的事实来源。

### 5.3 RecentMessages

近期原文窗口默认保留 20 条未压缩消息。进入摘要水位之前的消息不再作为近期原文进入模型输入，避免同一事实既出现在摘要里，又出现在原文里。

### 5.4 UserMemory

跨会话保存的用户明确指令和稳定偏好。首期类型：

- `instruction`：用户明确要求长期遵守的自定义指令；
- `preference`：品类、品牌或购物偏好；
- `price`：价格区间偏好；
- `profile_fact`：经允许保存的稳定用户事实。

记忆不是系统指令。模型必须将其视为可能过期的用户数据，不能用它覆盖安全、权限、确认或工具策略。

### 5.5 TaskState

当前 Agent Run 的工作状态，包括：

- 当前目标；
- 已确认和缺失的参数；
- 已完成步骤；
- 未完成工具调用；
- pending confirmation ID；
- 最近错误和可恢复点。

TaskState 是业务状态，不依赖模型从自然语言历史中重新猜测。

### 5.6 ToolResultEnvelope

工具执行完成后保存结构化 envelope：

```json
{
  "tool_call_id": "call_01",
  "tool_name": "cart.list",
  "status": "success",
  "data": {
    "items": [
      {"cart_item_id": 2, "product_id": 12, "quantity": 1}
    ]
  },
  "summary": "购物车共有 1 件条目。",
  "expires_at": "2026-07-22T12:05:00+08:00"
}
```

失败工具不能生成成功事实。订单状态、库存、优惠券可用性等动态结果必须有 TTL；过期后只能用于理解历史，执行写操作前仍需重新调用业务 RPC 校验。

### 5.7 ToolCallRef

历史工具调用默认不把完整 `data` 放入上下文，只保留引用：

```json
{
  "tool_call_id": "call_01",
  "tool_name": "cart.list",
  "status": "success",
  "summary": "购物车共有 1 件条目。",
  "entity_ids": {
    "cart_item_ids": [2],
    "product_ids": [12]
  },
  "created_at": "2026-07-22T12:00:00+08:00"
}
```

当模型或规则判断需要某次历史工具完整结果时，通过 `tool_call_id + user_id + conversation_id` 从数据库读取完整 envelope。

### 5.8 ContextMessages

ContextMessages 是每次模型调用前临时组装出的领域消息数组，不单独落库。`internal/eino/messages.go` 负责在模型调用边界转换为 Eino `schema.Message`，Context Manager 不依赖 Eino 类型。

## 6. 两种上下文模式

### 6.1 IntentContext

用于 Intent Planner，目标是低延迟、低成本和足够判断意图。只包含：

1. Intent system prompt；
2. 当前用户输入；
3. 最近对话；
4. 当前 TaskState；
5. 长期记忆摘要。

IntentContext 不加载完整工具结果、用户画像、完整会话摘要全文或复杂来源清单。如果意图识别需要历史工具结果，Planner 应输出缺信息、澄清问题或读取历史工具结果的计划，而不是猜测参数。

### 6.2 AgentContext

用于普通 ChatModel 和后续 Eino Agent Graph，包含：

1. 系统安全指令和工具协议；
2. 当前用户输入；
3. 当前 TaskState 和 PendingConfirmation；
4. 会话滚动摘要；
5. 最近 20 条未压缩消息；
6. 最近一次完整工具调用结果；
7. 最近固定数量的 ToolCallRef；
8. 有效 UserMemory；
9. 最小化 UserProfile（可选）。

近期消息按数据库消息条数计数，不再按“轮”做复杂折算。

## 7. Context Manager 接口

代码建议位于 `services/aiagent/internal/contextmanager/**`，避免使用 `context` 包名与 Go 标准库冲突。领域消息类型放在 `services/aiagent/internal/domain/context.go`，遵守 Eino 类型只能出现在 `internal/eino/**` 和 `internal/tools/**` 的仓库约束。

```go
type ContextMode string

const (
	IntentContextMode ContextMode = "intent"
	AgentContextMode  ContextMode = "agent"
)

type BuildContextRequest struct {
	UserID         uint64
	ConversationID string
	RunID          string
	Mode           ContextMode
	CurrentInput   string
}

type ContextMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolName   string
}

type BuildContextResult struct {
	Messages                     []ContextMessage
	SummaryCoveredMessageID      string
	SummaryCoveredUntilCreatedAt time.Time
	RecentMessageStartID         string
	RecentMessageEndID           string
	LatestToolCallID             string
	ToolCallRefCount             int
	EstimatedInputTokens         int
}

type Manager interface {
	Build(ctx context.Context, req BuildContextRequest) (*BuildContextResult, error)
	UpdateTaskState(ctx context.Context, state TaskState) error
}
```

Intent Planner 只依赖领域接口：

```go
type IntentModel interface {
	Generate(ctx context.Context, messages []domain.ContextMessage) (string, error)
}
```

该接口由 `internal/eino/intent_model.go` 实现，在 Eino 边界内把 `domain.ContextMessage` 转为 `schema.Message`。Planner 和 Context Manager 都不得直接导入 Eino package。

依赖接口：

```go
type MessageStore interface {
	FindUnsummarized(ctx context.Context, userID uint64, conversationID string, limit int) ([]Message, error)
	FindRecentUnsummarized(ctx context.Context, userID uint64, conversationID string, limit int) ([]Message, error)
}

type SummaryStore interface {
	FindLatest(ctx context.Context, userID uint64, conversationID string) (*ConversationSummary, error)
	SaveNext(ctx context.Context, summary ConversationSummary) error
}

type MemoryStore interface {
	ListActive(ctx context.Context, userID uint64, limit int) ([]UserMemory, error)
	SummarizeForIntent(ctx context.Context, userID uint64, limit int) (string, error)
}

type TaskStateStore interface {
	FindActive(ctx context.Context, userID uint64, conversationID, runID string) (*TaskState, error)
	CompareAndSwap(ctx context.Context, state TaskState) error
}

type ToolResultStore interface {
	FindLatestResult(ctx context.Context, userID uint64, conversationID string) (*ToolResultEnvelope, error)
	FindRecentRefs(ctx context.Context, userID uint64, conversationID string, limit int) ([]ToolCallRef, error)
	FindResultByCallID(ctx context.Context, userID uint64, conversationID, toolCallID string) (*ToolResultEnvelope, error)
}
```

所有 Store 方法必须显式接收认证 `userID`，查询条件必须同时包含 user ID 和 conversation ID。不能先按资源 ID 查询后再依赖调用方过滤。

## 8. Token 策略

本方案不做运行时 token 上限裁剪，也不因为估算 token 偏高而拒绝模型调用。

Context Manager 通过以下方式从源头节省 token：

- 长对话滚动摘要；
- 近期原文固定 20 条；
- 历史工具结果只放引用；
- 只保留最近一次完整工具结果；
- IntentContext 使用轻量来源。

仍保留轻量估算日志：

- `estimated_input_tokens`；
- context mode；
- summary covered message ID；
- recent message range；
- latest tool call ID；
- tool call ref count；
- build latency。

token 估算只用于排查和趋势观察，不参与上下文选择，不触发错误。

## 9. 会话摘要

### 9.1 触发条件

默认保留最近 20 条未压缩消息。当未压缩消息达到 30 条时，触发下一版摘要：

```text
旧摘要 + 最早 10 条未压缩消息 -> 新摘要
剩余 20 条未压缩消息继续作为近期原文
```

会话关闭或 Agent Run 完成明确任务时，也可以触发摘要评估；但仍遵守 10 条批量合并策略，避免频繁小摘要。

### 9.2 去重规则

每条消息只允许属于一种上下文来源：

- 已进入摘要水位的消息，不再进入近期原文窗口；
- 未进入摘要水位的消息，仍作为近期原文候选；
- Context Manager 组装时不得同时注入“摘要中的同一事实”和“对应原文消息”。

### 9.3 水位

摘要使用复合水位：`covered_until_created_at + covered_until_message_id`。查询下一批消息必须使用稳定排序：

```sql
ORDER BY created_at ASC, id ASC
```

摘要不使用业务序号。最新摘要通过会话 ID、覆盖水位和更新时间判断。

### 9.4 摘要输出

摘要模型使用独立、无工具权限、低温度的 ChatModel，输出严格 JSON：

```json
{
  "summary": "用户正在选购机械键盘，已比较两款商品。",
  "key_facts": [
    {"type": "product", "id": "12", "fact": "用户倾向商品 12"}
  ],
  "open_tasks": [
    {"type": "cart_add", "status": "waiting_quantity"}
  ]
}
```

生成新摘要时输入“当前摘要 + 本次要压缩的 10 条消息”。保存前校验 JSON、字段长度和引用实体 ID。摘要失败时保留当前摘要，近期原文继续保留，不阻塞聊天。

## 10. 工具结果上下文

### 10.1 持久化

工具结果必须继续以结构化 envelope 保存到 `ai_messages.metadata.tool_result`。为兼容 WebSocket 和旧消费者，投影后的业务 JSON 可以继续保存在 `metadata.data_json`，`content` 只保存用户可读 summary。

### 10.2 进入上下文的规则

- 最近一次成功工具结果：可以完整进入 AgentContext。
- 更早的工具调用：只进入 ToolCallRef。
- 失败工具结果：可以作为最近错误进入 TaskState 或近期消息，但不能变成成功业务事实。
- 动态结果：即使出现在上下文中，写操作前也必须重新调用真实业务 RPC 校验。

### 10.3 按需读取

当模型或规则需要历史工具完整结果时，必须通过受控读取接口按 `tool_call_id` 查询：

```text
FindResultByCallID(ctx, authenticated_user_id, conversation_id, tool_call_id)
```

查询必须同时校验 user ID 和 conversation ID。跨用户、跨会话、未知工具、失败结果或非法 envelope 都不得返回给模型。

## 11. 长期记忆和用户画像

### 11.1 写入策略

采用“显式 + 受控推断”：

- 用户明确说“记住、以后都、不要再”等长期指令时，可直接写入 `source=explicit` 的记忆。
- 推断偏好必须 `confidence >= 0.85`、至少有来源消息、非敏感并设置 TTL。
- 模型只能提交记忆候选，不能直接写数据库；MemoryPolicy 校验后执行 upsert。
- 单次行为、订单状态、库存、地址、支付信息、认证信息不得写成长期记忆。
- 用户明确纠正时，新值覆盖同一 `memory_key`，旧值保留审计状态但不再注入上下文。

### 11.2 生命周期

状态为 `active / superseded / deleted / expired`。默认 TTL：

- `instruction`：无默认过期，但允许用户删除；
- 显式 preference：365 天；
- 推断 preference/price：90 天；
- profile_fact：按字段策略配置，敏感事实默认不保存。

AgentContext 每次注入最多 12 条。IntentContext 只注入长期记忆摘要，不注入完整记忆列表。

### 11.3 Prompt 注入防护

记忆和画像放入标记为“非可信用户数据”的独立 context block。包含类似“忽略系统指令”内容的记忆只能作为原文数据，不能改变 system prompt、工具白名单、用户 ID 或确认策略。

## 12. Agent Run、TaskState 与 Checkpoint

每个需要多个步骤或等待确认的请求创建 `ai_agent_runs`。当前固定工作流阶段只记录状态，不改变执行编排；后续 ReAct Graph 可复用同一模型。

状态：`running / waiting_confirmation / completed / failed / expired`。

高风险工具请求确认时：

1. TaskState 写入 tool name、脱敏参数和 confirmation ID；
2. AgentRun 进入 `waiting_confirmation`；
3. Redis 保存可重建 checkpoint 热缓存；
4. MySQL 保存最终状态；
5. ConfirmAction 恢复前重新校验 user ID、conversation ID、确认状态、参数、过期时间和幂等键；
6. 真实执行仍调用 Confirmation Manager 和 Execution Guard。

Redis 丢失不能导致状态丢失；可以从 MySQL 的 AgentRun、confirmation 和 tool call 记录恢复。

## 13. 数据模型

### 13.1 ai_conversation_summaries

保留摘要表，用于保存滚动摘要和摘要水位。

```sql
CREATE TABLE `ai_conversation_summaries` (
  `id` varchar(64) NOT NULL,
  `conversation_id` varchar(64) NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `covered_until_sequence_id` bigint unsigned NOT NULL,
  `covered_until_created_at` datetime(3) NOT NULL,
  `covered_until_message_id` varchar(64) NOT NULL,
  `summary` text NOT NULL,
  `key_facts` json NOT NULL,
  `open_tasks` json NOT NULL,
  `token_count` int unsigned NOT NULL DEFAULT 0,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_conversation_watermark` (`user_id`, `conversation_id`, `covered_until_created_at`, `covered_until_message_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 13.2 ai_agent_runs

保留 Agent Run 表，用于保存 TaskState、等待确认状态和 checkpoint。

```sql
CREATE TABLE `ai_agent_runs` (
  `id` varchar(64) NOT NULL,
  `conversation_id` varchar(64) NOT NULL,
  `user_id` bigint unsigned NOT NULL,
  `status` varchar(32) NOT NULL,
  `current_step` varchar(64) NOT NULL DEFAULT '',
  `task_state` json NOT NULL,
  `checkpoint_id` varchar(64) NOT NULL DEFAULT '',
  `idempotency_key` varchar(128) NOT NULL,
  `expires_at` datetime DEFAULT NULL,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_idempotency_key` (`idempotency_key`),
  KEY `idx_user_conversation_status` (`user_id`, `conversation_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 13.3 ai_user_memories 扩展

```sql
ALTER TABLE `ai_user_memories`
  ADD COLUMN `memory_key` varchar(128) NOT NULL AFTER `user_id`,
  ADD COLUMN `source` varchar(32) NOT NULL DEFAULT 'explicit' AFTER `confidence`,
  ADD COLUMN `source_message_id` varchar(64) NOT NULL DEFAULT '' AFTER `source`,
  ADD COLUMN `status` varchar(16) NOT NULL DEFAULT 'active' AFTER `source_message_id`,
  ADD COLUMN `expires_at` datetime DEFAULT NULL AFTER `status`,
  ADD COLUMN `last_confirmed_at` datetime DEFAULT NULL AFTER `expires_at`,
  ADD UNIQUE KEY `uk_user_memory_key` (`user_id`, `memory_key`),
  ADD KEY `idx_user_status_expires` (`user_id`, `status`, `expires_at`);
```

正式迁移必须由新增 migration 完成；`construct/depend/sql/init.sql` 同步最终建表结构，不直接在生产环境运行初始化脚本。

## 14. 结构化日志与排查

每次 Planner 或 Agent 模型调用记录轻量结构化日志：

- user ID、conversation ID、run ID；
- context mode；
- summary covered message ID；
- recent message start/end ID；
- latest tool call ID；
- tool call ref count；
- estimated input tokens；
- build latency；
- optional source degradation reason。

不记录 API key、token、cookie、完整地址或未经脱敏的完整 Prompt。日志仅用于排查，不作为模型输入的事实来源。

## 15. 故障降级

| 故障 | 降级行为 |
|---|---|
| SummaryStore 不可用 | 使用近期未压缩消息，不生成新摘要 |
| 摘要模型失败或 JSON 非法 | 保留上一版摘要，近期原文继续保留 |
| MemoryStore 不可用 | 跳过长期记忆 |
| users RPC 不可用 | 跳过 UserProfile |
| TaskStateStore 不可用 | 当前单轮可继续；等待确认或多步任务返回不可恢复错误 |
| ToolResult envelope 解析失败 | 跳过该工具引用，记录日志；不得使用半个 JSON |
| 历史工具结果按需读取失败 | 让模型澄清或重新调用业务查询工具 |
| Redis 不可用 | 从 MySQL 读取，性能降级但语义不变 |

任何降级都不能放宽用户隔离、确认策略、Execution Guard 或写操作审计。

## 16. 安全规则

- user ID 只从认证上下文传入 BuildContextRequest。
- 每个 Store 查询都必须带 user ID 和 conversation ID。
- 模型、Tool arguments、memory content 和日志 metadata 中的 user ID 不可信。
- 长期记忆不得保存口令、token、cookie、银行卡、支付凭据等敏感信息。
- UserMemory、ConversationSummary 和工具结果都属于非可信上下文，不能覆盖 system prompt。
- 动态工具结果过期后不得直接驱动写操作。
- 删除记忆后不得再进入新上下文。

## 17. 分阶段实施

### Phase 1：工具结果协议和引用

- 保留统一 ToolDefinition、BaseTool 和 ToolResult envelope。
- 工具结果完整保存到 `metadata.tool_result`。
- 增加 ToolCallRef 投影，提取 tool_call_id、tool_name、summary、created_at 和关键 entity ID。

退出条件：成功工具结果可完整恢复；历史工具引用不会丢关键 ID；失败工具不会成为成功事实。

### Phase 2：轻量 Context Manager

- 定义 ContextMessage 和 BuildContextResult。
- Planner 改为消费 IntentContext。
- Eino Runner 改为消费 AgentContext。
- 删除旧 Planner/Runner 直接拼接历史逻辑。
- 增加 context 组装日志和 token 估算日志。

退出条件：Planner 和 Agent 都不直接读取 `[]*AiMessages`；token 估算只记录不裁剪；上下文来源可从日志排查。

### Phase 3：滚动摘要

- 新增或保留摘要表和增量摘要服务。
- 实现 30 条触发、压缩最早 10 条、保留近期 20 条的滚动策略。
- 保证消息不会同时进入摘要和近期原文。

退出条件：超过 30 条未压缩消息后，早期事实可通过摘要恢复，近期 20 条仍以原文进入上下文。

### Phase 4：长期记忆与 TaskState

- 扩展 `ai_user_memories`，实现显式和受控推断 MemoryPolicy。
- 增加长期记忆摘要供 IntentContext 使用。
- 新增 AgentRun 和 TaskStateStore。
- 把 confirmation、工具执行状态和当前参数写入 TaskState。

退出条件：等待确认状态可跨请求恢复；长期记忆支持更新、过期、删除和用户隔离。

### Phase 5：收敛与验收

- 删除旧上下文拼接逻辑。
- 删除快照持久化相关设计和实现。
- 保留轻量指标和日志。
- 评估是否需要向量检索；只有摘要和结构化引用不能满足场景时才引入。

## 18. 指标与日志

至少记录：

- `ai_context_build_latency_ms{mode}`；
- `ai_context_estimated_tokens{mode}`；
- `ai_context_summary_hit_total`；
- `ai_context_tool_ref_selected_total`；
- `ai_context_tool_result_loaded_total`；
- `ai_context_degraded_total{component}`；
- `ai_memory_candidate_total{decision}`；
- `ai_checkpoint_restore_total{result}`。

告警条件：上下文构建错误率、摘要连续失败、跨用户查询拒绝异常上升、历史工具结果读取失败率异常。

## 19. 测试与验收

### 19.1 单元测试

- IntentContext 只包含当前输入、最近对话、TaskState 和长期记忆摘要。
- AgentContext 包含摘要、最近 20 条未压缩消息、最近一次完整工具结果和历史工具引用。
- 30 条未压缩消息触发摘要，把最早 10 条合并进新摘要。
- 同一条消息不会同时出现在摘要和近期原文。
- ToolCallRef 保留 tool_call_id、tool_name、summary、created_at 和关键 entity ID。
- 历史工具完整结果只能通过 user ID + conversation ID + tool_call_id 读取。
- 失败工具不会成为成功事实。
- 摘要水位按 `created_at + id` 单调推进。
- 摘要失败保留当前摘要。
- 显式记忆可 upsert、删除和过期。
- 推断记忆低置信或敏感时拒绝。
- 所有 Store 拒绝跨用户读取。

### 19.2 集成测试

- 超过 30 条消息后，早期关键事实通过摘要进入 AgentContext。
- Planner 使用轻量 IntentContext，仍能处理当前输入和当前任务。
- 查询购物车后，最近一次工具结果完整进入上下文。
- 更早工具调用以 ToolCallRef 进入上下文，需要完整结果时再按需读取。
- SummaryStore、MemoryStore、users RPC 或 Redis 故障时基础聊天仍可用。
- 等待确认状态在服务重启后恢复，确认后仍经过 Execution Guard。

### 19.3 目标命令

```bash
go test ./services/aiagent/...
go test ./apis/ai/...
go test ./...
```

## 20. 完成标准

- 所有 AI 工具通过统一 ToolDefinition、BaseTool 和 ToolResult 契约注册与执行。
- Eino 只负责边界 schema/调用适配，不向 domain、contextmanager 或业务 RPC handler 泄漏类型。
- 工具结果以结构化 envelope 持久化，用户摘要和机器事实分离。
- Planner 和 Agent 的模型输入统一由 Context Manager 临时组装。
- Context Manager 不做运行时 token 裁剪；token 估算只进入日志和指标。
- 长对话通过滚动摘要恢复早期关键事实。
- 近期未压缩消息默认保留 20 条。
- 每条消息只进入摘要或近期原文之一，不重复注入。
- 上下文中只直接保留最近一次完整工具结果，其他历史工具调用以引用形式保留。
- 历史工具完整结果只能通过受控接口按需读取。
- 长期记忆具备来源、置信度、冲突、过期、删除和用户隔离。
- 活跃任务和待确认状态可以跨请求、跨实例恢复。
- 上下文降级不绕过认证、确认、Execution Guard 和审计。

## 21. 问题 1、3、5 一体化实施计划

### 21.1 为什么仍然作为一个计划

这三个问题仍存在依赖关系：

```text
统一 ToolDefinition / ToolResult（问题 3）
                 |
                 v
无损保存和按需恢复工具结果（问题 5）
                 |
                 v
轻量 Context Manager 组装上下文（问题 1）
```

- Context Manager 需要稳定 ToolResult schema，才能安全生成最近工具结果和历史 ToolCallRef。
- ToolCallRef 需要统一 envelope 才能保存关键 entity ID，而不是依赖自然语言摘要。
- 长对话摘要解决窗口外事实丢失；工具结果按需读取解决大型结构化结果不适合常驻上下文的问题。

### 21.2 交付边界

本计划包含：

- 统一的领域级 ToolDefinition、ToolResult 和 ToolResultEnvelope；
- 现有 QueryTools、WriteTools、HighRiskTools 的兼容适配；
- 结构化工具结果持久化、projector、TTL、ToolCallRef 和按需读取；
- Context Manager、IntentContext、AgentContext、滚动摘要和轻量日志；
- 显式/受控推断长期记忆和最小任务状态；
- 指标、验收和旧路径收敛。

本计划不包含：

- 每次模型输入持久化；
- 独立上下文快照表；
- 运行时 token 上限裁剪；
- IntentClassifier 与 ActionPlanner 的职责拆分；
- ReAct 多轮推理和并发工具 DAG；
- 向量数据库、Embedding 和语义检索基础设施；
- 支付、退款、地址修改等新业务工具。

### 21.3 阶段总览与完成门禁

| 阶段 | 解决内容 | 关键产物 | 进入下一阶段的门禁 |
|---|---|---|---|
| P0 基线和契约冻结 | 三个问题的共同边界 | 结果样例、字段 allowlist、黄金输入 | 现有工具行为和安全测试通过 |
| P1 统一工具协议 | 问题 3 | ToolDefinition、ToolResult、兼容适配器 | 所有工具可导出同一 schema，旧 RPC 行为不变 |
| P2 工具结果引用和按需读取 | 问题 5 | Envelope、ToolCallRef、TTL、读取接口 | 关键 ID 在 20 条以上历史后仍可定位 |
| P3 轻量 Context Manager | 问题 1 | Intent/Agent Context、组装日志 | Planner/Runner 使用统一上下文 |
| P4 滚动摘要 | 问题 1 | 摘要水位、30->10+20 策略 | 早期事实可恢复，近期消息不重复 |
| P5 记忆和任务状态 | 问题 1 | MemoryPolicy、TaskState | 记忆可过期/删除，等待确认可恢复 |
| P6 监控和旧路径收敛 | 三个问题 | 指标、排查手册 | 关键场景通过，旧上下文路径已删除 |

### 21.4 P0：基线、样例和契约冻结

**目标：** 在改代码前固定问题定义和可观测基线。

**工作项：**

1. 从 `ai_messages.metadata.data_json`、`metadata.tool_result` 和 `ai_tool_calls` 采集现有工具结果样例。
2. 为 `product.detail`、`product.recommend`、`cart.list`、`order.get`、`checkout.detail`、`coupon.calculate` 建立结果字段 allowlist。
3. 标记每个字段的类型、是否关键标识、是否动态、TTL 和是否允许进入模型。
4. 建立端到端黄金场景：
   - 多轮后引用“刚才那个商品”；
   - 查询购物车后减少指定条目；
   - 查询订单后继续询问订单状态；
   - 查询优惠券后进入结算；
   - 工具失败后不得生成成功事实。

**产物：** `ToolResult` 字段清单、动态 TTL 清单和黄金测试输入。

### 21.5 P1：统一领域工具协议（问题 3）

**目标：** 让模型 schema、执行入口、风险策略和结果格式由一个领域协议描述；Eino 只作为边界适配器。

**核心契约：**

```go
type ToolDefinition struct {
	Name                string
	Description         string
	ParametersSchema    json.RawMessage
	Risk                string
	RequireConfirmation bool
	TimeoutSeconds      int64
	WriteOperation      bool
}

type ToolCall struct {
	ID             string
	Name           string
	Arguments      json.RawMessage
	ConversationID string
	UserID         uint64
}

type ToolResult struct {
	ToolCallID       string
	ToolName         string
	Status           string
	Data             json.RawMessage
	Summary          string
	ErrorCode        string
	ErrorMessage     string
	BusinessExecuted bool
	ExpiresAt        *time.Time
}

type BaseTool interface {
	Definition() ToolDefinition
	Execute(context.Context, ToolCall) (ToolResult, error)
}
```

**实现步骤：**

1. 在 `services/aiagent/internal/domain/tool_contract.go` 定义领域契约和状态常量。
2. 在 `services/aiagent/internal/tools/handler_tool.go` 编写兼容适配器，把现有 `HandlerFunc` 包装为 `BaseTool`。
3. 修改 `internal/tools/registry.go`，Registry 同时返回 `ToolDefinition` 和现有风险 metadata。
4. 修改 `internal/tools/executor.go`，统一完成参数校验、可信 user ID 注入、超时、ToolResult envelope 和审计入口。
5. 在 `internal/eino` 增加 Eino adapter，将 `ToolDefinition` 转为 Eino Tool schema。

### 21.6 P2：工具结果引用和按需读取（问题 5）

**目标：** 让工具结果同时满足用户摘要、审计和后续模型推理，同时避免把所有历史大结果常驻上下文。

**实现步骤：**

1. 固定 ToolResult envelope 的字段语义。
2. 为每个工具注册 allowlist projector，只删除明确无关字段，不截断 ID、订单号、数量和状态。
3. 保存完整合法 envelope 到 `ai_messages.metadata.tool_result`。
4. 从 envelope 中提取 ToolCallRef，保留 tool_call_id、tool_name、summary、created_at 和关键 entity ID。
5. AgentContext 只注入最近一次完整工具结果和最近固定数量 ToolCallRef。
6. 增加按 `tool_call_id` 读取完整工具结果的受控接口，并强制 user ID + conversation ID 校验。
7. 失败 ToolResult 只能进入错误上下文和 TaskState，不能作为成功业务事实。

### 21.7 P3：轻量 Context Manager（问题 1）

**目标：** Planner 和普通 ChatModel 的输入统一由 Context Manager 临时组装。

**实现步骤：**

1. 定义领域 `ContextMessage`、`BuildContextRequest` 和 `BuildContextResult`。
2. 实现 MessageStore、ToolResultStore、SummaryStore、MemoryStore、TaskStateStore。
3. IntentContext 只加载当前输入、最近对话、当前 TaskState 和长期记忆摘要。
4. AgentContext 加载摘要、近期 20 条原文、最近一次完整工具结果、历史 ToolCallRef、TaskState、UserMemory 和可选 UserProfile。
5. 增加 context 组装日志和 token 估算日志，估算值不参与裁剪。

### 21.8 P4：滚动摘要（问题 1）

**目标：** 通过固定窗口摘要策略恢复跨窗口事实，避免重复注入。

**实现步骤：**

1. 新增或保留 `ai_conversation_summaries`。
2. 当未压缩消息达到 30 条时，取最早 10 条与旧摘要合并成新摘要。
3. 摘要成功后推进水位，保留剩余 20 条未压缩消息。
4. Context Manager 只注入最新摘要和水位后的近期原文。
5. 摘要失败时保留旧摘要和当前未压缩消息，不阻塞聊天。

### 21.9 P5：记忆和任务状态补齐（问题 1）

**目标：** 让 Context Manager 能恢复稳定偏好和当前任务，但不让记忆取代业务事实。

**实现步骤：**

1. 扩展 `ai_user_memories` 的 key、source、confidence、status、TTL 和 source message。
2. 明确指令直接保存；推断偏好必须高置信、非敏感、有来源和 TTL；模型只能提交候选。
3. IntentContext 使用长期记忆摘要；AgentContext 使用最多 12 条有效记忆。
4. 使用现有 users RPC 生成最小 UserProfile；RPC 不可用时跳过画像。
5. 以 `ai_agent_runs` 保存最小 TaskState、pending confirmation、当前参数、幂等键和 checkpoint。

### 21.10 P6：监控和旧路径收敛

**收敛顺序：**

1. 启用 P1 的协议和无损结果保存；
2. 启用 ToolCallRef 和按需读取；
3. 启用轻量 Context Manager；
4. 启用滚动摘要；
5. 删除旧 Planner/Runner 历史拼接逻辑；
6. 保留指标和日志作为本地开发排查入口。

**必须观测：**

- `ai_context_tool_ref_selected_total`；
- `ai_context_tool_result_loaded_total`；
- `ai_context_build_latency_ms{mode}`；
- `ai_context_estimated_tokens{mode}`；
- `ai_context_degraded_total{component}`；
- WebSocket 首包延迟、工具成功率和错误率。

### 21.11 一体化验收矩阵

| 场景 | 问题覆盖 | 必须验证 |
|---|---|---|
| 30 条消息后引用早期商品 | 1 + 5 | Summary 找回早期 product ID |
| 查询购物车后减少指定条目 | 1 + 3 + 5 | 最近工具结果或 ToolCallRef 能定位 cart item ID |
| 查询订单后继续询问状态 | 1 + 3 | order ID 可定位，动态状态按 TTL 重新查询 |
| 工具返回超大商品列表 | 3 + 5 | projector 保留 allowlist，历史上下文只保留引用 |
| 需要历史工具完整结果 | 5 | 按 tool_call_id 受控读取，必须校验用户和会话 |
| 工具 RPC 超时 | 3 + 5 | failed ToolResult 不进入成功事实 |
| 审计失败但业务成功 | 3 + 5 | `business_executed=true`，不得重复执行 |
| 跨用户读取上下文 | 1 + 5 | Summary、Memory、ToolResult、TaskState 全部拒绝 |
| Summary/Memory/Redis 不可用 | 1 | 降级到近期消息，基础聊天仍可用 |

**最终命令：**

```bash
go test ./services/aiagent/...
go test ./apis/ai/...
go test ./...
```

## 22. 实现状态（2026-07-23）

本方案已从重型上下文治理收敛为轻量 Context Manager 目标设计。后续实现应优先删除或避免继续扩展以下内容：

- 独立上下文快照持久化；
- 上下文快照表和 recorder；
- 运行时 token 上限裁剪；
- 复杂优先级打包器；
- 因 token 估算超限而阻塞模型调用。

后续实现应保留或补齐：

- ToolResult envelope；
- ToolCallRef；
- 按需读取历史工具完整结果；
- 30 条触发、10 条压缩、20 条保留的滚动摘要；
- IntentContext 轻量来源；
- AgentContext 最近一次完整工具结果和历史工具引用；
- context 组装日志和 token 估算日志。
