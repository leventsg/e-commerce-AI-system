# AI 智能客服技术方案

## 1. 总体设计

AI 客服作为新的编排层接入现有电商系统，不侵入商品、库存、订单、优惠券、购物车等核心服务。

新增模块：

- `apis/ai`：对外 WebSocket 聊天入口。
- `services/aiagent`：AI Agent 服务，基于 Eino 负责模型接入、意图编排、工具调用、会话管理、确认管理和审计。
- 数据表：保存会话、消息、工具调用、确认记录和用户记忆。

调用链路：

用户 WebSocket  
-> `apis/ai`  
-> `services/aiagent`  
-> Eino Agent / Chain / Graph  
-> Eino ChatModel  
-> Eino Tool / ToolsNode  
-> 现有业务 RPC  
-> 返回结构化结果  
-> WebSocket 推送给用户

## 2. WebSocket 接口设计

接口：

`GET /douyin/ai/chat?conversation_id=optional`

鉴权：

- 沿用现有认证中间件。
- 用户 ID 从请求上下文获取。
- 未登录用户不允许使用 AI 客服。

客户端发送用户消息：
```json
{
  "type": "user_message",
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
  "tool": "order.get",
  "status": "success",
  "data": {}
}
```


服务端返回确认请求：
```json
{
  "type": "confirmation_required",
  "confirmation_id": "confirm_001",
  "action": "order.cancel",
  "summary": "确认取消订单 202406300001？",
  "expires_at": 1719730000
}
```

客户端确认操作：
```json
{
  "type": "confirm_action",
  "confirmation_id": "confirm_001",
  "approved": true
}
```

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
- 将会话上下文转换为 Eino message。
- 构建系统提示词，约束模型只能调用已注册工具。
- 使用 Eino ADK ChatModelAgent 编排“模型推理 -> ToolsNode 工具调用 -> 工具结果回填 -> 最终回复”流程。
- 对流式输出进行事件转换，推送为 WebSocket `assistant_message`。
- 将 Eino callback 或本地包装器中的工具调用事件写入 `ai_tool_calls`。
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

### 3.4 Context Manager

Context Manager 是 Supervisor Agent 的统一上下文入口，详细方案见 `docs/ai-agent-context-optimization.md`。

职责：

- 组合 system prompt、当前用户输入、当前任务状态、待确认动作、近期消息、会话摘要、长期记忆、最小用户画像和必要工具上下文。
- 生成 `AgentContext` 领域无关临时 ContextMessages。
- 通过滚动摘要和固定近期窗口从源头节省 token：默认保留最近 20 条未压缩消息；未压缩消息达到 30 条时，将最早 10 条与旧摘要合并成新摘要。
- 每条消息只进入摘要或近期原文之一，不重复注入。
- 工具结果只直接保留最近一次完整结果；其他历史工具调用只注入固定数量的 ToolCallRef，需要完整结果时按 `tool_call_id` 受控读取。
- 记录上下文来源、摘要覆盖水位、近期消息范围、最近工具调用、工具引用数量、Token 估算和构建耗时。
- 摘要、记忆或画像不可用时按策略降级，不阻塞基础聊天。

上下文优先级：

1. 系统安全指令和工具协议。
2. 当前用户输入。
3. 当前任务状态和待确认动作。
4. 经过校验的结构化工具事实。
5. 近期对话。
6. 会话摘要。
7. 长期用户记忆和最小用户画像。

安全约束：

- Context Manager 的 user ID 只能来自认证上下文。
- 所有上下文 Store 必须同时按 user ID 和 conversation ID 查询。
- UserMemory、ConversationSummary、ToolFact 和 UserProfile 都是不可信数据，不能覆盖 system prompt、工具白名单、确认规则和 Execution Guard。
- 动态 ToolFact 过期后只用于理解历史，写操作前必须重新调用业务 RPC 校验。
- Context Manager 不包含 Eino 类型；只有 `internal/eino` 的 Supervisor Runner 适配器可以把领域消息转换为 `schema.Message`。

当前方案已收敛为轻量 Context Manager：Planner 和 Agent Runner 默认消费临时组装的 ContextMessages；不持久化模型输入，不做运行时 token 上限裁剪。Conversation Manager 继续负责会话归属校验和原始消息持久化，不再决定模型输入窗口。

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
首期工具：
- product.search
- product.detail
- product.recommend
- inventory.get
- order.get
- order.list
- order.cancel
- checkout.prepare
- checkout.detail
- order.create
- cart.list
- cart.add
- cart.sub
- cart.delete 
- coupon.list
- coupon.detail
- coupon.claim
- coupon.my_list
- coupon.usage_list
- coupon.calculate

每个工具需要定义：
- 工具名称。
- 风险等级。
- 参数 schema。
- 是否需要确认。
- 超时时间。
- 对应 RPC 调用。
- 结果转换逻辑。
- Eino Tool schema。

首期下单工具契约：

- `checkout.prepare` 接收必填 `order_items[]`，每项包含 `product_id`、`quantity`，`coupon_id` 可选。
- `order.create` 接收必填 `pre_order_id`、`address_id`、`payment_method`，`coupon_id` 可选；`payment_method` 使用现有 RPC 枚举值 1（微信）或 2（支付宝）。
- 高风险 Tool 的普通 Eino 调用只创建确认记录。只有 `ConfirmAction` 成功领取 `pending -> approved` 后，才能通过同一个 Execution Guard 调用业务 RPC。
- 使用优惠券创建订单时，确认前基于预结算商品快照调用 `coupon.calculate`，确认摘要展示该优惠券对应的最新应付金额；优惠券不可用时不创建确认。
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
| seq | 数据库内部自增序号 |
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
| id | 调用 ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| tool_name | 工具名称 |
| arguments | 工具参数 |
| result_summary | 结果摘要 |
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
| expires_at | 过期时间 |
| executed_at | 执行时间 |
| created_at | 创建时间 |

### 4.5 ai_user_memories
| 字段 | 说明 |
|---|---|
| id | 记忆 ID |
| user_id | 用户 ID |
| memory_key | 用户内稳定记忆键 |
| memory_type | instruction / preference / price / profile_fact |
| content | 记忆内容 |
| confidence | 置信度 |
| source | explicit / inferred |
| source_message_id | 来源消息 ID |
| status | active / superseded / deleted / expired |
| expires_at | 过期时间 |
| last_confirmed_at | 最近确认时间 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

长期记忆采用“显式 + 受控推断”策略。用户明确要求记住的内容可直接保存；推断偏好必须经过置信度、来源、敏感信息和 TTL 策略校验。模型只能生成候选，不能直接写库。

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
| id | Agent Run ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| status | running / waiting_confirmation / completed / failed / expired |
| current_step | 当前步骤 |
| task_state | 结构化任务状态 JSON |
| checkpoint_id | Eino/Redis checkpoint ID |
| idempotency_key | 运行幂等键 |
| expires_at | 过期时间 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

## 5. 关键流程
### 5.1 商品推荐流程
1. 用户描述购买需求。
2. Eino Agent 结合系统提示词和工具 schema 生成商品推荐工具调用。
3. Execution Guard 校验并注入用户 ID。
4. 调用 product.recommend。
5. 推荐不足时调用 product.search。
6. Eino ChatModel 基于工具结果生成简短推荐理由。
7. 返回商品列表和推荐理由。
8. 用户要求加入购物车时调用 cart.add。

### 5.2 查询订单流程
1. 用户提供订单号或描述“最近订单”。
2. 有订单号时调用 order.get。
3. 无订单号时调用 order.list，并将结果交给 Eino ChatModel 根据用户描述筛选和总结。
4. 多个候选订单时让用户选择。
5. 返回订单状态、商品、金额、地址、支付状态。

### 5.3 取消订单流程
1. 用户提出取消订单。
2. 查询订单详情。
3. 校验订单属于当前用户。
4. 判断订单是否允许取消。
5. 创建确认请求。
6. 用户确认后调用 order.cancel。
7. 返回取消结果。
8. 记录审计日志。

### 5.4 创建订单流程
1. 用户表达购买意图。
2. AI 确认商品、数量、优惠券、地址、支付方式；缺少参数时先追问，不猜测。
3. 没有 `pre_order_id` 时调用 `checkout.prepare` 创建预结算。
4. 使用当前用户身份查询预结算详情，取得应付金额和商品数量。
5. 创建 `order.create` 确认请求；使用优惠券时先调用 `coupon.calculate` 校验并取得对应应付金额，摘要同时展示优惠券 ID。
6. 用户确认后，由确认状态机的唯一 winner 通过 Execution Guard 调用 `order.create`。
7. 成功标记确认记录为 `executed`，失败标记为 `failed`，并返回真实订单结果。

### 5.5 上下文构建流程

1. Conversation Manager 校验会话归属并保存当前用户原始消息。
2. Context Manager 构建 `AgentContext`。
3. 加载最新会话摘要、水位后的近期原文、有效长期记忆、活跃 TaskState、pending confirmation、最近一次完整工具结果和历史 ToolCallRef。
4. 如果未压缩消息达到 30 条，异步将最早 10 条与旧摘要合并成新摘要，之后仍保留最近 20 条原文。
5. AgentContext 包含当前用户输入、摘要、近期 20 条原文、最近一次完整工具结果、历史工具引用、TaskState、UserMemory 和可选 UserProfile。
6. Supervisor Runner 在 `internal/eino` 边界转换消息后调用 ADK Supervisor。
7. Supervisor 负责意图识别、任务拆解、SubAgent 路由和最终总结；SubAgent 负责本领域工具选择与执行。
8. 工具和 assistant 结果持久化后更新 TaskState，并异步评估是否需要生成新摘要或长期记忆候选。
9. 摘要、记忆或画像不可用时使用近期消息降级；历史工具结果按需读取失败时，重新查询业务工具或向用户澄清。

工具结果持久化采用双字段兼容：`metadata.tool_result` 保存结构化机器 envelope，`metadata.data_json` 保留投影后的业务 JSON 供现有 WebSocket/旧消费者使用。Context Manager 优先读取 envelope，旧记录才回退读取 `data_json`。

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
- 长期记忆的显式写入、受控推断、冲突、过期、删除和用户隔离。
- TaskState 状态条件更新和 checkpoint 恢复。

### 6.2 集成测试
- WebSocket 建连成功。
- 未登录 WebSocket 被拒绝。
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

第二阶段：WebSocket 聊天入口  
- 新增 apis/ai。
- 实现 WebSocket 建连、鉴权、消息收发。
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
- 按 `docs/ai-agent-context-optimization.md` 分阶段接入 Context Manager。
- 轻量 Context Manager、滚动摘要和 Token 估算日志。
- 会话增量摘要、长期记忆和最小用户画像。
- Agent Run、TaskState 和可恢复 checkpoint。
- 运营配置。
- 模型切换。
- 限流和监控。
