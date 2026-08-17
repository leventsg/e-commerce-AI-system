# AI 智能客服技术方案

## 1. 总体设计

AI 客服作为新的编排层接入现有电商系统，不侵入商品、库存、订单、优惠券、购物车等核心服务。

新增模块：

- `apis/ai`：对外 SSE 聊天入口。
- `services/aiagent`：AI Agent 服务，基于 Eino 负责模型接入、意图编排、工具调用、会话管理、确认管理和审计。
- 数据表：保存会话、消息、工具调用、确认记录和用户记忆。

调用链路：

用户 SSE 请求  
-> `apis/ai`  
-> `services/aiagent` server-streaming RPC  
-> Eino Agent / Chain / Graph  
-> Eino ChatModel  
-> Eino Tool / ToolsNode  
-> 现有业务 RPC  
-> 返回结构化结果  
-> SSE 推送给用户

## 2. SSE 接口设计

接口：

`POST /douyin/ai/chat`

鉴权：

- 沿用现有认证中间件。
- 用户 ID 从请求上下文获取。
- 未登录用户不允许使用 AI 客服。

客户端发送用户消息：
```json
{
  "type": "user_message",
  "conversation_id": "conv_001",
  "client_message_id": "client_msg_0190f1f0e8a57000",
  "content": "帮我查一下订单 202406300001",
  "metadata": {
    "source": "web"
  }
}
```

服务端返回 AI 消息：
```json
{
  "type": "assistant_message",
  "conversation_id": "conv_001",
  "message_id": "msg_001",
  "content": "我帮你查到该订单当前处于待支付状态。",
  "done": true
}
```


服务端返回工具结果：
```json
{
  "type": "tool_result",
  "tool": "order_get",
  "status": "success",
  "data": {}
}
```


服务端返回确认请求：
```json
{
  "type": "confirmation_required",
  "confirmation_id": "confirm_001",
  "action": "order_cancel",
  "summary": "确认取消订单 202406300001？",
  "expires_at": 1719730000
}
```

客户端确认操作：
```json
{
  "type": "confirm_action",
  "conversation_id": "conv_001",
  "confirmation_id": "confirm_001",
  "approved": true
}
```

服务端响应为 `text/event-stream`。每个事件使用 `event: <type>`、`id: <message_id>` 和 `data: <ServerEvent JSON>` 输出，并在事件产生后立即 flush；空闲连接每 10 秒发送 `: ping`，单次连接最长 5 分钟；当事件没有 `message_id` 时不输出 `id:` 行。前端使用 `fetch + ReadableStream` 发送 POST JSON 并消费 SSE。模型 reasoning 或工具调用前的中间过程按无消息 ID 的 `assistant_thinking_delta` 推送，前端按会话本地折叠展示，不与最终回答拼接，不落库。最终自然语言回答只来自 ADK iterator 中 supervisor 的 final assistant message，后端先持久化为 `assistant_message`，再在同一次 SSE 中下发给前端。工具调用前发送 `tool_progress`，工具完成后发送中文摘要 `tool_result`。

## 3. 核心模块
### 3.1 Eino 模型接入
AI Agent 使用 Eino 的 ChatModel 抽象接入模型，不在业务代码中自定义一套平行的 LLM Provider 接口。`services/aiagent` 只保留薄适配层，用于读取配置、创建 Eino ChatModel、按需通过 `WithTools` 绑定工具、统一错误降级和超时控制。

首期支持：
- OpenAI-compatible ChatModel。
- DeepSeek ChatModel。

配置项：
- Provider 名称。
- api key
- base url
- model
- timeout
- max tokens
- temperature

### 3.2 Eino Agent Orchestrator
职责：
- 通过 MemoryMiddleware 在模型调用前自动注入会话历史和动态运行上下文。
- 构建系统提示词，约束模型只能调用已注册工具。
- 使用 Eino ADK ChatModelAgent 编排“模型推理 -> ToolsNode 工具调用 -> 工具结果回填 -> 最终回复”流程。
- 通过 ADK iterator 统一消费 `AgentEvent`，将 reasoning 转为 `assistant_thinking_delta`，将 assistant tool call 转为 `tool_progress`，将 tool message 转为 `tool_result`，并只把 supervisor final assistant 作为最终回答事实。
- callback 不再承担用户可见输出职责；后续仅可用于日志、trace、metrics 等只读观测。
- 将工具执行链路中的结构化工具调用事件写入 `ai_tool_calls`。
- 在 Eino 执行工具前调用本地风险策略，拦截高风险工具并创建确认请求。

设计约束：
- Eino ADK 负责模型、工具调用协议和 ChatModelAgent 编排流程。
- 本地代码负责用户身份、权限隔离、确认状态、审计、限流和业务 RPC 参数转换。
- 模型和 Eino 工具入参中的 `user_id` 不可信，执行前必须由本地 Execution Guard 覆盖。

### 3.3 Conversation Manager
职责：
- 创建会话。
- 恢复历史会话。
- 保存完整的用户消息、AI 消息和工具事件。
- 校验会话属于当前登录用户。
- 为 Context Manager 提供原始消息读取能力。

Conversation Manager 不再直接决定模型上下文。原始消息是不可变事实记录；模型输入由独立 Context Manager 根据调用场景临时组装。

### 3.4 MemoryProvider / MemoryMiddleware

AI 客服上下文工程已采用 `docs/model-context.md` 的分层范式，详细方案见 `docs/context/customer-service-memory-architecture.md`。

职责：

- `MemoryProvider.Retrieve` 组合当前任务状态、待确认动作、近期消息、会话摘要、长期事件、最小用户画像和必要工具上下文。
- `MemoryMiddleware.BeforeModelRewriteState` 将 `HistoryMessages` 插在 system prompt 后，将动态 `ContextMessages` 追加到当前 user message 末尾。
- `MemoryMiddleware` 只做模型调用前上下文注入；记忆更新不放在 `AfterModelRewriteState`，避免 ReAct 多次模型调用重复触发。
- 通过滚动摘要和固定近期窗口从源头节省 token：默认保留最近 20 条未压缩消息；未压缩消息达到 30 条时，将最早 10 条与旧摘要合并成新摘要。
- `ChatLogic` 在 `runSupervisor` 正常返回后异步调用 `updateConversationMemory`；只有 `SummaryManager.MaybeRefresh` 创建新摘要时，才发布一个 `AiMemoryUpdates` Kafka 事件。
- Profile consumer 和 Memory Event consumer 订阅同一个 topic，分别使用独立结构化模型基于同一批 compressed message IDs 更新 `ai_user_profiles` 和 `ai_user_memory_events`。
- 每条消息只进入摘要或近期原文之一，不重复注入。
- 工具上下文直接来自 `ai_tool_calls`：`<latest_tool_result>` 注入最近一次完整工具调用；`<recent_tool_calls>` 只注入工具名、参数和 `tool_call_id`，需要历史 result 时调用 `get_tool_call_result`。
- 记录上下文来源、摘要覆盖水位、近期消息范围、最近工具调用数量、Token 估算和构建耗时。
- 摘要、长期事件或画像不可用时按策略降级，不阻塞基础聊天。

上下文优先级：

1. 系统安全指令和工具协议。
2. 当前用户输入。
3. 当前任务状态和待确认动作。
4. 经过校验的结构化工具事实。
5. 近期对话。
6. 会话摘要。
7. 长期事件和最小用户画像。

安全约束：

- MemoryProvider 的 user ID 只能来自认证上下文和 ADK session values。
- 所有上下文 Store 必须同时按 user ID 和 conversation ID 查询。
- ConversationSummary、ToolFact、UserMemoryEvent 和 UserProfile 都是不可信数据，不能覆盖 system prompt、工具白名单、确认规则和 Execution Guard。
- 动态 ToolFact 过期后只用于理解历史，写操作前必须重新调用业务 RPC 校验。
- `internal/memory` 不包含 Eino 类型；只有 `internal/eino` 的 MemoryMiddleware 可以把领域消息转换为 `schema.Message`。

当前在线链路不再由 ChatLogic 手动调用 `ContextManager.Build()`。Conversation Manager 继续负责会话归属校验和原始用户消息持久化；Supervisor Agent root 通过 MemoryMiddleware 自动注入上下文。旧 `contextmanager` store 继续作为 `CustomerServiceProvider` 的数据源。

### 3.5 Supervisor Agent
职责：
- 判断用户意图。
- 拆解多步骤业务任务。
- 路由到合适的领域 SubAgent。
- 协调多个 SubAgent 的执行顺序。
- 汇总最终中文回复。

领域 SubAgent：
- product_agent：商品搜索、商品详情、商品推荐、库存查询。
- order_agent：订单查询、订单列表、取消订单确认。
- cart_checkout_agent：购物车、结算、创建订单确认。
- coupon_agent：优惠券查询、领取、我的券、优惠计算。
- general_agent：普通客服解释、闲聊、无法归类问题。

实现方式：
- 使用 Eino ADK `ChatModelAgent + AgentTool`，不使用 `prebuilt/supervisor` / AgentTransfer。
- Supervisor Agent 不直接绑定业务 RPC 工具，只通过 AgentTool 调度 SubAgent。
- SubAgent 默认只接收 Supervisor 传入的紧凑 `request`，不共享完整聊天历史。
- SubAgent 只绑定本领域 ToolInfo 和可执行工具。
- 工具调用必须经过 Eino InvokableTool、Execution Guard、确认拦截和审计链路。
- Prompt 文本集中放在 `services/aiagent/internal/prompts`，Eino 编排代码不得直接硬编码长 prompt。

### 3.6 Tool Registry
所有业务工具必须注册为 Eino Tool，并同步维护本地工具元数据白名单，模型不能调用未注册工具。

工具层统一链路为：`Tool Catalog -> Registry -> Eino adapter -> Executor -> Handler -> RPC/Store`。`services/aiagent/internal/tools.Tool` 是工具定义的单一事实来源，统一承载 `Name`、`Desc`、`Params`、`Kind`、`Visibility`、`Metadata`、`Handler` 和可选 `ConfirmationSummary`。启动时由 `DefaultBusinessTools(clients, timeout)` 与 `DefaultCapabilityTools(deps)` 生成统一 catalog，Registry 负责导出 root/sub agent 可见工具列表、ToolInfo、InvokableTool adapter、metadata 和确认摘要。旧的 `QueryTools`、`WriteTools`、`HighRiskTools` 运行时 manager 已删除，不再有 schema-only 占位工具和二次绑定。

首期工具：
- product_search
- product_detail
- product_recommend
- inventory_get
- order_get
- order_list
- order_cancel
- checkout_prepare
- checkout_detail
- order_create
- cart_list
- cart_add
- cart_sub
- cart_delete
- coupon_list
- coupon_detail
- coupon_claim
- coupon_my_list
- coupon_usage_list
- coupon_calculate

每个工具需要定义：
- 工具名称。
- 工具描述。
- 风险等级。
- 参数 schema。
- 是否需要确认。
- 超时时间。
- 对应 RPC 调用。
- Handler。
- 结果转换逻辑。
- Eino Tool schema。
- 高风险确认摘要函数。

首期下单工具契约：

- `checkout_prepare` 接收必填 `order_items[]`，每项包含 `product_id`、`quantity`，`coupon_id` 可选。
- `order_create` 接收必填 `pre_order_id`、`address_id`、`payment_method`，`coupon_id` 可选；`payment_method` 使用现有 RPC 枚举值 1（微信）或 2（支付宝）。
- 高风险 Tool 的普通 Eino 调用在 ChatModelAgent middleware 中创建确认记录后调用官方 `tool.StatefulInterrupt` 中断，不调用业务 RPC。只有结构化 `ConfirmAction` 携带 `confirmation_id`，成功领取 `pending -> approved` 后，服务端才使用 `runner.ResumeWithParams` 恢复同一次工具调用，并通过同一个 Execution Guard 调用业务 RPC；`pending -> rejected` 是确定性终态，由后端直接返回取消结果，不恢复 checkpoint，不调用 LLM。
- 使用优惠券创建订单时，确认前基于预结算商品快照调用 `coupon_calculate`，确认摘要展示该优惠券对应的最新应付金额；优惠券不可用时不创建确认。
- 业务 RPC 成功但审计记录失败时，工具结果返回失败并明确标记业务已经执行，确认状态仍转为 `executed`，避免用户重试造成重复写入。

### 3.7 Execution Guard / Engine
职责：
- 校验工具参数。
- 强制注入当前登录用户 ID。
- 屏蔽模型传入的 user_id。
- 调用现有 RPC 服务。
- 处理超时、错误和失败降级。
- 将 RPC 返回转换成 AI 可读结构。
- 写操作完成后记录审计日志。

Execution Guard 位于 Eino Tool 的业务处理函数内部或外层包装器中。任何 Eino 工具实际调用 RPC 前，都必须先经过该 Guard。

### 3.8 Confirmation Manager
高风险操作必须进入确认流程。
职责：
- 创建确认记录。
- 生成确认 ID。
- 保存待执行工具和参数。
- 绑定 Eino `checkpoint_id` 与 root-cause `interrupt_id`；客户端只感知 `confirmation_id`。
- 设置确认过期时间。
- 用户确认后重新校验权限和状态。
- 防止过期确认、重复确认、跨用户确认。

状态：
- pending：待确认
- approved：已确认
- rejected：已拒绝
- expired：已过期
- executed：已执行
- failed：执行失败

并发与幂等：
- 使用 `ai:confirmation:lock:<confirmation_id>` Redis 短锁合并同一确认 ID 的同时请求，默认锁超时 5 秒。
- Redis 锁只覆盖确认状态读取和更新，不跨业务 RPC 持有。
- Redis 锁竞争时直接返回稍后重试，不访问 MySQL；Redis 基础设施错误时降级到 MySQL 条件更新。
- MySQL 使用带 `user_id`、旧状态和过期条件的原子更新，是确认状态与最终幂等的事实来源。
- `approved` 是高风险操作的一次性执行领取状态；业务成功后更新为 `executed`，失败后更新为 `failed`。
- `rejected` 不执行也不恢复 Eino checkpoint；后端直接持久化取消 `tool_result` 和最终 `assistant_message`，避免模型继续复述旧确认请求。

## 4. 数据库设计
### 4.1 ai_conversations
| 字段 | 说明 |
|---|---|
| id | 会话 ID |
| user_id | 用户 ID |
| title | 会话标题 |
| status | 会话状态 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

### 4.2 ai_messages
| 字段 | 说明 |
|---|---|
| id | 数据库内部自增序号 |
| msg_id | 消息唯一 ID，对外作为 `message_id` 返回，服务端 UUIDv7 生成 |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| role | user / assistant / tool |
| content | 消息内容 |
| metadata | 扩展信息 |
| client_message_id | 前端生成的用户消息幂等 ID，同一轮 user/assistant/tool 消息保存相同值 |
| dedupe_client_message_id | 仅 user 消息参与幂等唯一约束的生成列 |
| created_at | 创建时间 |

`user_message` 必须携带 `client_message_id`。同一用户重复提交相同 `client_message_id` 时，AI Agent 不再执行模型、工具或写操作，只重放同一会话、同一 `client_message_id` 下已保存的 assistant 消息。

### 4.3 ai_tool_calls
| 字段 | 说明 |
|---|---|
| id | 自增主键 |
| conversation_id | 会话 ID |
| tool_call_id | 模型真实工具调用 ID |
| user_id | 用户 ID |
| tool_name | 工具名称 |
| arguments | 工具参数 |
| result | 真实工具返回 JSON |
| status | success / failed |
| error_message | 错误信息 |
| latency_ms | 耗时 |
| created_at | 创建时间 |

### 4.4 ai_confirmations
| 字段 | 说明 |
|---|---|
| id | 确认 ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| tool_name | 工具名称 |
| arguments | 待执行参数 |
| summary | 确认摘要 |
| status | 确认状态 |
| run_id | Agent Run ID |
| checkpoint_id | Eino checkpoint ID |
| interrupt_id | Eino root-cause interrupt ID |
| expires_at | 过期时间 |
| executed_at | 执行时间 |
| created_at | 创建时间 |

### 4.5 ai_user_memory_events
| 字段 | 说明 |
|---|---|
| id | 事件 ID |
| user_id | 用户 ID |
| type | milestone / event |
| event_date | 事件时间 |
| summary | 事件摘要 |
| keywords | 检索关键词 |
| status | active / deleted |
| created_at | 创建时间 |
| updated_at | 更新时间 |

长期事件记录用户时间线、关键里程碑和可检索历史事实。`MemoryProvider.Retrieve` 只注入最近事件；更早事件通过 `search_user_memory` 按需检索。结构化偏好和稳定画像保存在 `ai_user_profiles`。

### 4.6 ai_conversation_summaries

| 字段 | 说明 |
|---|---|
| id | 摘要 ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| covered_until_created_at | 已覆盖消息时间水位 |
| covered_until_message_id | 已覆盖消息 ID 水位 |
| summary | 会话摘要 |
| key_facts | 稳定关键事实 JSON |
| open_tasks | 未完成事项 JSON |
| token_count | 摘要 Token 估算 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

### 4.7 ai_agent_runs

| 字段 | 说明 |
|---|---|
| run_id | Agent Run ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| status | running / interrupted / completed / failed / expired |
| task_state | 结构化任务状态 JSON |
| checkpoint_id | Eino checkpoint ID |
| checkpoint_blob | Eino checkpoint payload，MySQL 持久化回退 |
| idempotency_key | 运行幂等键 |
| expires_at | 过期时间 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

## 5. 关键流程
### 5.1 商品推荐流程
1. 用户描述购买需求。
2. Eino Agent 结合系统提示词和工具 schema 生成商品推荐工具调用。
3. Execution Guard 校验并注入用户 ID。
4. 调用 product_recommend。
5. 推荐不足时调用 product_search。
6. Eino ChatModel 基于工具结果生成简短推荐理由。
7. 返回商品列表和推荐理由。
8. 用户要求加入购物车时调用 cart_add。

### 5.2 查询订单流程
1. 用户提供订单号或描述“最近订单”。
2. 有订单号时调用 order_get。
3. 无订单号时调用 order_list，并将结果交给 Eino ChatModel 根据用户描述筛选和总结。
4. 多个候选订单时让用户选择。
5. 返回订单状态、商品、金额、地址、支付状态。

### 5.3 取消订单流程
1. 用户提出取消订单。
2. 查询订单详情。
3. 校验订单属于当前用户。
4. 判断订单是否允许取消。
5. 创建确认请求并通过 `tool.StatefulInterrupt` 返回 `confirmation_required`，事件必须包含 `confirmation_id`。
6. 用户通过结构化 `confirm_action` 回传 `confirmation_id` 后，服务端查到 `checkpoint_id/interrupt_id` 并调用 `runner.ResumeWithParams` 恢复 `order_cancel`。
7. 若用户批准，返回真实取消结果；若用户拒绝，后端直接返回取消结果，不恢复 checkpoint。
8. 记录审计日志。

### 5.4 创建订单流程
1. 用户表达购买意图。
2. AI 确认商品、数量、优惠券、地址、支付方式；缺少参数时先追问，不猜测。
3. 没有 `pre_order_id` 时调用 `checkout_prepare` 创建预结算。
4. 使用当前用户身份查询预结算详情，取得应付金额和商品数量。
5. 创建 `order_create` 确认请求并中断；使用优惠券时先调用 `coupon_calculate` 校验并取得对应应付金额，摘要同时展示优惠券 ID。
6. 用户确认后，由确认状态机的唯一 winner 使用 checkpoint 恢复原工具调用，并通过 Execution Guard 调用 `order_create`。
7. 成功标记确认记录为 `executed`，失败标记为 `failed`，并返回真实订单结果。

### 5.5 上下文构建流程

1. Conversation Manager 校验会话归属并保存当前用户原始消息。
2. ChatLogic 将当前用户输入作为最小 user message 交给 Supervisor Runner。
3. MemoryMiddleware 在 root model 调用前加载最新会话摘要、水位后的近期原文、长期事件、活跃 TaskState、pending confirmation、最近一次完整工具调用和最近工具调用最小列表。
4. MemoryProvider 返回 history 和 runtime context；runtime context 追加到当前 user message，不写入原始历史。
5. Supervisor 负责意图识别、任务拆解、SubAgent 路由和最终总结；SubAgent 负责本领域工具选择与执行。
6. 工具和 assistant 结果持久化后，ChatLogic 在 `runSupervisor` 正常返回后异步调用 `updateConversationMemory`。
7. 如果未压缩消息达到 30 条，`SummaryManager` 将最早 10 条与旧摘要合并成新摘要，返回本次 compressed message IDs；否则结束。
8. 摘要创建时只发布一个 `AiMemoryUpdates` 事件；Profile consumer 和 Memory Event consumer 用同一批消息分别更新画像和长期事件。
9. 摘要、长期事件或画像不可用时使用近期消息降级；历史工具结果按需读取失败时，重新查询业务工具或向用户澄清。

工具结果事实来源是 `ai_tool_calls.result`，保存真实工具返回 JSON，并通过 `tool_call_id` 与模型工具调用关联。`get_tool_call_result` 可按 `tool_call_id` 读取当前用户当前 conversation 的历史工具结果；`ai_messages` 仍可保存 role=tool 消息和展示 metadata，但 Context Manager / MemoryProvider 不再从 `ai_messages.metadata` 读取工具结果。

## 6. 测试方案
### 6.1 单元测试
- Supervisor Agent 路由、任务拆解和 SubAgent 协调。
- Eino Tool schema 注册和本地工具风险等级。
- Confirmation Manager 确认创建、确认、拒绝、过期。
- Execution Guard 用户 ID 注入和参数校验。
- Eino ChatModel 创建、超时和降级逻辑。
- Eino 工具调用事件到审计记录的转换。
- Context Manager 的 Intent/Agent 组装来源。
- ToolFact 的完整 JSON 恢复和关键 ID 保留。
- 摘要复合水位推进和失败降级。
- 长期事件检索、画像更新、删除/遗忘语义和用户隔离。
- TaskState 状态条件更新和 checkpoint 恢复。

### 6.2 集成测试
- SSE 请求成功返回事件流。
- 未登录 SSE 请求被拒绝。
- 用户查询商品。
- 用户查询订单。
- 用户获取商品推荐。
- 用户添加购物车。
- 用户取消订单时必须先确认。
- 用户创建订单时必须先确认。

### 6.3 风控测试
- 模型传入伪造 user_id 时必须被覆盖。
- 用户不能查询他人订单。
- 过期确认不能执行。
- 重复确认不能重复执行。
- 工具调用失败时不能返回成功话术。
- 用户不能读取其他用户的摘要、记忆、TaskState、工具结果或工具引用。
- 记忆和摘要中的提示注入文本不能覆盖 system prompt 或确认策略。
- 过期动态 ToolFact 不能直接驱动写操作。

## 7. 实施建议
建议分阶段实现：
第一阶段：AI Agent 基础骨架  
- 新增 services/aiagent。
- 接入 Eino 依赖和 ChatModel 工厂。
- 实现 Eino Tool Registry 与本地工具元数据。
- 实现 Confirmation Manager。
- 实现基础单元测试。

第二阶段：SSE 聊天入口  
- 新增 apis/ai。
- 实现 SSE 鉴权、消息接收和事件流输出。
- 接入 AI Agent 服务。

第三阶段：业务工具接入  
- 接入商品、库存、订单、购物车、优惠券、结算 RPC。
- 实现查询、推荐和低风险自动操作。

第四阶段：高风险操作和审计  
- 实现确认流程。
- 接入取消订单、创建订单。
- 接入审计日志。
- 完成风控测试。

第五阶段：增强能力  
- 按 `docs/context/customer-service-memory-architecture.md` 接入 MemoryProvider / MemoryMiddleware 上下文工程。
- 滚动摘要、长期事件检索和 Token 估算日志。
- 会话增量摘要、长期事件和最小用户画像。
- Agent Run、TaskState 和可恢复 checkpoint。
- 运营配置。
- 模型切换。
- 限流和监控。
