# AI 智能客服技术方案

## 结构化子 Agent 委派（2026-10-07）

AgentTool 输入不再接受 `{"request":"..."}`。顶层仅 `task` 必填，内部 `intent`、`objective` 必填，`done_when` 可选；`inputs`、`constraints`、`context`、`gaps`、`scope` 可选，不含 `schema_version`。可选对象一旦提供，其条目必要字段仍须完整；所有对象拒绝未知字段。最小输入为 `{"task":{"intent":"order_get","objective":"核实订单 O123 状态"}}`。

- 意图使用目标 Agent 的业务工具名称枚举，另有 `shopping_compare`、`inspect_order_issue`、`explain_policy`、`general_consultation`。inputs 复用实际业务字段类型及枚举；缺失参数可在只读查询后补齐，执行前仍需满足业务工具要求。
- 约束强度为 `must | prefer`；gap 的 resolution 为 `lookup | ask_user | return_to_parent`，blocks 为 `task | write`，field 使用目标 Agent 输入路径枚举。先前动作使用注册业务工具名枚举，排除动作使用目标 Agent 工具枚举。
- scope.mode 为 `read_only | prepare | execute`。未提供时读取及比较、诊断、咨询意图为只读，写意图为 execute；read_only 和 prepare 都拒绝写工具。checkout_prepare 可能占库存，属于写操作。execute 不是确认凭据，exclusions 在执行前强制检查。
- 省略 done_when 使用意图最低完成要求；省略集合按空集合处理，不补造证据、数量、ID 或批准状态。原始参数最多 32 KiB，单段文本最多 2,000 字符，上下文数组最多 20 项，业务条目最多 50 项；拒绝超限，不静默截断。
- message:<id>、tool_call:<id> 必须由服务端按认证用户及当前会话解析。缺失、损坏或跨归属引用统一不可用；存储故障保留错误语义。同轮未落库工具结果保存在可信运行上下文中，解析不依赖落库时序。合并引用最多 20 个，解析内容最多 64 KiB。
- 子 Agent 输入由本次委派及证据投影组成，引用内容按数据处理，不覆盖系统规则；不再次注入整段历史。可信用户、会话、确认状态和权限不属于模型输入契约。
- 结果包含 status、summary、evidence_refs、actions、missing_inputs、remaining_work。状态为 `completed | partial | needs_input | confirmation_required | failed`；真实动作区分 success、failed、unknown、not_executed，由服务端校验并补全，不能用模型声明替代真实记录。格式错误不自动重跑任务，仍保留已执行动作。
- 沿用原生 AgentTool 中断恢复；checkpoint 保存原委派范围、证据及动作，恢复时保留限制并验证确认。成功或结果未知的相同写操作不能直接重放，仍需业务幂等与真实状态核实。Supervisor 汇总自然语言最终回复。

实现取舍：业务 inputs 使用具体委派 struct 中的动态字段映射，其 schema 和执行前校验复用 Registry 的真实工具定义，避免维护第二份业务枚举。查询补缺目前仅自动接受简单字段路径、成功证据中的唯一匹配值；嵌套对象和数组缺口需要父 Agent 或用户补齐后重新委派。比较、诊断及咨询意图即使显式指定 execute 也不能发起写入；需要单独委派明确的写入意图。工具调用 ID 在当前运行中不可复用，原生确认恢复除外；数值等价的写参数按同一操作检查，拒绝重放时保留原始审计证据。

切换前排空旧版本确认中断任务，不兼容旧 request 或旧 checkpoint；公共 API、RPC 和数据库结构保持不变。观测只记录 Agent、校验错误码、状态及延迟，不记录未经脱敏的完整交接内容。

## Self-Evolving 最小架构增补（2026-10-01，最小版本已实现）

生产 Langfuse 新增显式 redacted 内容策略，保留 summary 默认值；采集脱敏对话、工具和业务证据。单次 Go 命令通过 Eino 独立分析生产轨迹并生成一个 Skill 候选，结果保存为本地文件，不增加 MySQL 演化表、常驻 worker、租约或复杂恢复系统。

评测使用独立维护的固定 Skill 专项测试集，复用 evalrunner、模拟工具和 Guard，按 trial 注入候选并记录真实 Skill 加载。A/B 只改变目标 Skill，分别检查触发与行为改善。测试集不进入生成器上下文，不根据候选临时生成标准；覆盖不足则报告无法判断。线上继续使用内嵌 Skill。完整设计见 [Self-Evolving 最小技术设计](ai-agent-self-evolving-design.md)，实现状态与未验证部分见其 8.1 节。

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

### 2.1 历史会话查询接口

接口：

- `GET /douyin/ai/sessions`：当前用户历史会话列表，支持 `page`、`page_size` 分页，按最后活跃时间倒序。
- `GET /douyin/ai/sessions/messages?conversation_id=<conversation_id>`：指定会话内全部历史消息，支持分页，按消息创建顺序正序。

鉴权：沿用现有认证中间件，用户 ID 从请求上下文获取，客户端不得传入 `user_id`。

RPC：`services/aiagent` 新增 `ListConversations` / `ListMessages`，负责归属校验、分页与数据组装；`apis/ai` 网关只做鉴权、协议转换和字段映射。

会话列表项：

```json
{
  "conversation_id": "conv_001",
  "title": "订单咨询",
  "last_message_preview": "你的订单已送达",
  "updated_at": "2026-08-20T15:06:37+08:00",
  "message_count": 6
}
```

历史消息项：

```json
{
  "message_id": "msg_001",
  "role": "tool",
  "content": "查询结果",
  "metadata": {
    "tool_name": "order_get",
    "status": "success",
    "tool_call_id": "call_001"
  },
  "client_message_id": "client_msg_0190f1f0e8a57000",
  "created_at": "2026-08-20T15:00:02+08:00"
}
```

说明：

- `role` 取值 `user` / `assistant` / `tool`，`tool` 即 tool_result 消息。
- `metadata` 透传 `ai_messages.metadata` 原始 JSON：工具消息包含 `tool_name`、`status`、`tool_call_id`、`data_json` 等；assistant 消息可能包含 RAG `sources`。
- 会话最后活跃时间取会话内最新消息 `created_at`，无消息时回退到会话创建时间。
- 消息查询强制按 `user_id + conversation_id` 过滤，会话归属校验在 `services/aiagent` 完成，跨用户访问直接拒绝。

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

- `MemoryProvider.Retrieve` 组合当前任务状态、待确认动作、近期消息、会话摘要、长期事件、最小用户画像和必要工具上下文，并接收 `AgentName` 作为轻量作用域，用于按 root / SubAgent 区分上下文组装。
- `MemoryMiddleware.BeforeModelRewriteState` 将 `HistoryMessages` 插在 system prompt 后，将动态 `ContextMessages` 追加到当前 user message 末尾。
- `MemoryMiddleware` 只做模型调用前上下文注入；记忆更新不放在 `AfterModelRewriteState`，避免 ReAct 多次模型调用重复触发。
- 通过滚动摘要和固定近期窗口从源头节省 token：默认向模型注入最近 12 条未压缩消息；摘要维护仍按 30 -> 10 + 20 推进，将最早 10 条与旧摘要合并成新摘要。
- `ChatLogic` 在 `runSupervisor` 正常返回后异步调用 `updateConversationMemory`；只有 `SummaryManager.MaybeRefresh` 创建新摘要时，才发布一个 `AiMemoryUpdates` Kafka 事件。
- Profile consumer 和 Memory Event consumer 订阅同一个 topic，分别使用独立结构化模型基于同一批 compressed message IDs 更新 `ai_user_profiles` 和 `ai_user_memory_events`。
- 每条消息只进入摘要或近期原文之一，不重复注入。
- 长期记忆采用“近期事件直接注入 + 当前语义混合召回”：Provider 默认注入最近 5 条长期事件，并基于当前 user message 和 `AgentName` 使用 `Match + Keywords` 召回最多 5 条相关历史事件；召回结果低置信或为空时不强塞上下文。
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
- 动态 ToolFact、库存、价格、订单、优惠券状态等业务事实只用于定位对象和理解历史，写操作或状态结论前必须重新调用对应业务 RPC 精确核实。
- `internal/memory` 不包含 Eino 类型；只有 `internal/eino` 的 MemoryMiddleware 可以把领域消息转换为 `schema.Message`。

当前在线链路不再由 ChatLogic 手动调用 `ContextManager.Build()`。Conversation Manager 继续负责会话归属校验和原始用户消息持久化；Supervisor Agent root 通过 MemoryMiddleware 自动注入上下文。业务/通用客服 SubAgent 通过结构化委派及服务端解析的证据获得上下文，不再次加载会话历史。旧 `contextmanager` store 继续作为 `CustomerServiceProvider` 的数据源。

### 3.5 Supervisor Agent
职责：
- 判断用户意图。
- 拆解多步骤业务任务。
- 路由到合适的业务或通用客服 SubAgent。
- 协调多个 SubAgent 的执行顺序。
- 汇总最终中文回复。

四个业务 Agent + 一个闲聊/通用客服 Agent 的业务工具白名单：

| Agent | 职责 | 业务工具 |
|---|---|---|
| `product_discovery_agent` 商品发现 | 澄清找货需求、搜索、推荐、详情和库存核实 | `product_search`、`product_recommend`、`product_detail`、`inventory_get` |
| `shopping_decision_agent` 购物决策 | 商品比较、取舍、优惠选择与领取、购物车调整 | `product_search`、`product_recommend`、`product_detail`、`inventory_get`、`coupon_list`、`coupon_detail`、`coupon_claim`、`coupon_my_list`、`coupon_usage_list`、`coupon_calculate`、`cart_list`、`cart_add`、`cart_sub`、`cart_delete` |
| `order_fulfillment_agent` 订单履约 | 地址管理、结算、下单确认、普通订单查询 | `address_list`、`address_get`、`address_add`、`address_update`、`address_delete`、`cart_list`、`checkout_prepare`、`checkout_detail`、`order_create`、`order_get`、`order_list` |
| `after_sales_agent` 售后服务 | 订单与优惠问题排查、取消订单确认、有依据的政策解释 | `order_get`、`order_list`、`order_cancel`、`coupon_usage_list`、`coupon_detail`、`product_detail` |
| `general_agent` 闲聊/通用客服 | 闲聊、通用说明、无法归类问题、用户资料维护 | `user_get`、`user_update` |

查询工具允许共享，每个写工具只有一个归属 Agent；共享能力工具继续遵循 Registry 的可见性规则。工具风险、确认、超时和审计策略保持现状，`cart_delete` 仍需要高风险确认。

实现方式：
- 使用 Eino ADK `ChatModelAgent + AgentTool`，不使用 `prebuilt/supervisor` / AgentTransfer。
- Supervisor Agent 不直接绑定业务 RPC 工具，只通过 AgentTool 调度 SubAgent。
- SubAgent 接收结构化任务契约与服务端解析的证据；AgentTool 使用自定义输入 schema，调用前校验，不再接受旧 `request`。
- SubAgent 只绑定上述业务白名单及 Registry 允许的共享能力工具，子 Agent 不直接互相调用。
- 工具调用必须经过 Eino InvokableTool、Execution Guard、确认拦截和审计链路。
- Prompt 文本集中放在 `services/aiagent/internal/prompts`，Eino 编排代码不得直接硬编码长 prompt。

路由与交接规则：

- 搜索、推荐、找商品优先进入商品发现；比较、取舍、优惠和购物车进入购物决策。购物决策可自行补查商品，避免为补充信息反复转交。
- 结算、下单、地址和普通订单状态查询进入订单履约；取消订单、订单异常和售后政策进入售后服务；资料维护与闲聊进入 general。
- 多意图请求按依赖执行，只调用必要 Agent。修改购物车后下单时先决策再履约；直接下单或查询订单不必经过选购阶段。
- 任务契约顶层仅 `task` 必填，内部 `intent`、`objective` 必填，`done_when` 可选；`inputs`、`constraints`、`context`、`gaps`、`scope` 全部可选，无 `schema_version`。按需要传真实商品 ID、数量、约束、证据及先前动作；省略业务参数不等于允许猜测。身份与批准状态只来自服务端。
- 最终金额、库存和优惠资格以结算链路重新校验的真实结果为准。`checkout_prepare` 可能占用库存，只用于明确结算意图，不用于比较试算。
- 高风险操作产生 `confirmation_required` 后暂停依赖它的后续步骤，批准后走既有 checkpoint 恢复链路。拒绝、过期和失败不得总结为成功。
- 未接入退款、退换货、物流操作，明确告知能力边界，不以其他写工具替代执行。政策解释使用现有知识上下文，无依据时说明无法核实。

Skill 与迁移约束：

- `purchase-advisor` 绑定 `shopping_decision_agent`，选购阶段保持只读；阶段结束后宿主按用户意图处理领券和购物车操作。`issue-triage` 继续绑定 Supervisor。
- MemoryMiddleware 为 Supervisor 注入上下文，子 Agent 只接收本次委派和相关证据；事件过滤、日志、前端展示和 mock 使用上述 AgentName，最终回答仍只来自 Supervisor。历史消息与工具调用记录保留原样。
- 本次按当前没有待确认任务的部署前提替换旧 Agent 结构，不增加旧版本 checkpoint 兼容路径。公共 API/RPC、工具参数、数据库和底层业务 handler 分层保持现状。

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
- user_get
- user_update
- address_list
- address_get
- address_add
- address_update
- address_delete

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
- 结算 RPC 采用购买意图 token 协议：handler 使用可信 ConversationID/RunID/ToolCallID 派生领取请求 ID，先调用 `IssueCheckoutToken` 再将服务端 token 传给 `PrepareCheckout`。token 不暴露在模型 schema 中；同次执行重试复用，新购买不按商品去重。业务状态 60018/60019、写 RPC 响应不明和取消均按 `business_outcome=unknown` 记录，禁止自动换新意图重试。完整状态机、字段 SQL 与人工核对边界见 [结算 token 设计](checkout-token-design.md)。
- `order_create` 接收必填 `pre_order_id`、`address_id`，`coupon_id` 可选；支付方式固定为支付宝，工具不接收 `payment_method`。
- 高风险 Tool 的普通 Eino 调用在 ChatModelAgent middleware 中创建确认记录后调用官方 `tool.StatefulInterrupt` 中断，不调用业务 RPC。只有结构化 `ConfirmAction` 携带 `confirmation_id`，成功领取 `pending -> approved` 后，服务端才使用 `runner.ResumeWithParams` 恢复同一次工具调用，并通过同一个 Execution Guard 调用业务 RPC；`pending -> rejected` 是确定性终态，由后端直接返回取消结果，不恢复 checkpoint，不调用 LLM。
- 使用优惠券创建订单时，确认前基于预结算商品快照调用 `coupon_calculate`，确认摘要展示该优惠券对应的最新应付金额；优惠券不可用时不创建确认。
- 用户资料工具 `user_get`、`user_update` 由 `general_agent` 绑定；地址工具由 `order_fulfillment_agent` 绑定，统一通过 `services/users` RPC 执行。`user_get`、`address_list`、`address_get` 为只读；`user_update`、`address_add`、`address_update`、`address_delete` 为低风险写操作，无需确认。账号删除、登出、修改密码不纳入 AI 工具范围。
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

### 3.8 工具失败治理

工具调用失败不能直接升级成“AI 服务不可用”，也不能在前端或模型侧伪造成成功。工具失败治理按四层设计，分阶段落地：

第一层：工具层重试。

- `Executor` 是唯一工具执行入口，负责将可预期工具失败转换为结构化 `tool_result`，而不是向 ADK 抛普通 Go error。
- 工具结果统一使用 envelope：`status`、`tool_name`、`attempt_count`、`retry_count`、`error`、`business_outcome`、`result`、`attempt_trail`。
- `status=failed` 时，`error` 只包含稳定错误码和安全提示，不向模型暴露底层网络、数据库、鉴权 token、连接串等原始错误。
- 瞬态异常可按工具策略有限重试；`MaxRetries` 不包含首次调用，`attempt_count` 包含首次调用，`retry_count` 只统计额外重试。
- 默认查询工具和 Capability Tool 最多重试 2 次，即最多 3 次总 attempt；默认退避为 150ms 起步、指数退避、full jitter、最大 1s、总耗时上限 5s。
- 所有写工具默认不自动重试，包括 `checkout_prepare`。只有完成明确幂等状态机并能返回同一份成功结果后，才允许逐个开启写工具重试。
- backoff 等待必须响应父 context 取消；如果下一次 delay 或 attempt 会超过父 deadline / 工具 `MaxElapsed`，立即停止重试。

错误分类规则：

- 参数错误、JSON 参数错误、权限/鉴权失败、工具未注册、业务拒绝、调用方 `context.Canceled`、未知错误默认永久失败，不重试。
- `bizerr.Parse` 优先识别 gRPC `codes.Aborted` 中的业务错误；`Aborted` 默认不重试，避免库存不足、订单状态不允许等业务错误触发无效重试。
- `Unavailable`、attempt `DeadlineExceeded`、网络超时、依赖空响应可按工具策略重试。
- `ResourceExhausted` 暂不默认重试，除非后续工具明确声明安全且有可用的 retry 信息。
- 写工具超时或结果无法确认时，`business_outcome=unknown`，不得声称成功或未执行；只有 `business_outcome=executed` 才能设置 `BusinessExecuted=true`。

第二层：conversation 全局兜底预算。

- 后续使用 Redis 按 `conversationID + runID` 管控本轮对话的总重试预算，防止多个工具连续瞬态失败导致资源被耗尽。
- 真正安排 retry 前占用预算；Redis 操作用 Lua 原子完成检查、递增和 TTL 设置。
- Redis 不可用时 fail-open 到单工具上限，并记录治理降级日志，不能让治理组件成为工具全面不可用的新单点。
- 除 retry 预算外，还需要 `max_tool_calls_per_run` 和同工具同参数终态失败短期去重，避免模型收到失败后在同一 run 内循环调用。

第三层：熔断降级。

- 后续以环境 + RPC service + method 作为主熔断 key，必要时再细分到工具名。
- 只累计依赖故障：网络错误、`Unavailable`、服务端超时；不累计参数、权限、业务拒绝、用户级限流和调用方取消。
- Redis Lua 维护 `closed/open/half_open` 状态；open 到期后只允许少量带租约的 half-open probe。
- probe 成功关闭熔断，失败重新打开并延长窗口；Redis 故障时 fail-open，并记录熔断治理降级指标。
- 高流量场景优先采用“最小样本数 + 失败率”，例如至少 20 次调用且失败率超过 50%；低流量可保留短窗口连续失败阈值。

第四层：可观测日志层。

- 每次最终工具调用仍只写一条 `ai_tool_calls`，`result` 保存最终 envelope。
- envelope 的 `attempt_trail` 记录每次 attempt 的状态、错误类别、错误码、耗时和是否发起 RPC。
- retry scheduled、retry exhausted、permanent failure、budget exceeded、circuit open/half-open/closed、治理组件降级都要有结构化日志。
- 日志字段至少包含 `conversation_id`、`run_id`、`tool_call_id`、`tool_name`、`attempt`、`retry_count`、`delay_ms`、`error_kind`、`error_code`。

当前阶段只落地第一层工具层重试；conversation 全局预算、熔断和更完整的观测指标作为后续阶段推进。

### 3.9 Confirmation Manager
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
| result | 最终工具结果 envelope；成功时 `result` 子字段保存真实工具返回 JSON，失败时保存安全错误与 attempt trail |
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
2. Supervisor 路由到商品发现 Agent，结合系统提示词和工具 schema 生成商品推荐工具调用。
3. Execution Guard 校验并注入用户 ID。
4. 调用 product_recommend。
5. 推荐不足时调用 product_search。
6. Eino ChatModel 基于工具结果生成简短推荐理由。
7. 返回商品列表和推荐理由。
8. 用户要求加入购物车时，Supervisor 交给购物决策 Agent 调用 cart_add。

### 5.2 查询订单流程
1. 用户提供订单号或描述“最近订单”，Supervisor 路由到订单履约 Agent。
2. 有订单号时调用 order_get。
3. 无订单号时调用 order_list，并将结果交给 Eino ChatModel 根据用户描述筛选和总结。
4. 多个候选订单时让用户选择。
5. 返回订单状态、商品、金额、地址、支付状态。

### 5.3 取消订单流程
1. 用户提出取消订单，Supervisor 路由到售后服务 Agent。
2. 查询订单详情。
3. 校验订单属于当前用户。
4. 判断订单是否允许取消。
5. 创建确认请求并通过 `tool.StatefulInterrupt` 返回 `confirmation_required`，事件必须包含 `confirmation_id`。
6. 用户通过结构化 `confirm_action` 回传 `confirmation_id` 后，服务端查到 `checkpoint_id/interrupt_id` 并调用 `runner.ResumeWithParams` 恢复 `order_cancel`。
7. 若用户批准，返回真实取消结果；若用户拒绝，后端直接返回取消结果，不恢复 checkpoint。
8. 记录审计日志。

### 5.4 创建订单流程
1. 用户表达明确结算或下单意图，Supervisor 路由到订单履约 Agent；需要修改购物车时先由购物决策 Agent 完成。
2. AI 确认商品、数量、优惠券、地址；支付方式固定为支付宝，缺少参数时先追问，不猜测。
3. 没有 `pre_order_id` 时调用 `checkout_prepare` 创建预结算。
4. 使用当前用户身份查询预结算详情，取得应付金额和商品数量。
5. 创建 `order_create` 确认请求并中断；使用优惠券时先调用 `coupon_calculate` 校验并取得对应应付金额，摘要同时展示优惠券 ID。
6. 用户确认后，由确认状态机的唯一 winner 使用 checkpoint 恢复原工具调用，并通过 Execution Guard 调用 `order-api POST /douyin/order/create`。
7. 成功标记确认记录为 `executed`，失败标记为 `failed`，并返回真实订单结果。

### 5.5 上下文构建流程

1. Conversation Manager 校验会话归属并保存当前用户原始消息。
2. ChatLogic 将当前用户输入作为最小 user message 交给 Supervisor Runner。
3. MemoryMiddleware 在 root model 调用前加载最新会话摘要、水位后的近期原文、长期事件、活跃 TaskState、pending confirmation、最近一次完整工具调用和最近工具调用最小列表。
4. MemoryProvider 返回 history 和 runtime context；runtime context 追加到当前 user message，不写入原始历史。
5. Supervisor 负责意图识别、任务拆解、SubAgent 路由和最终总结；SubAgent 围绕业务目标在白名单内选择与执行工具。
6. 工具和 assistant 结果持久化后，ChatLogic 在 `runSupervisor` 正常返回后异步调用 `updateConversationMemory`。
7. 如果未压缩消息达到 30 条，`SummaryManager` 将最早 10 条与旧摘要合并成新摘要，返回本次 compressed message IDs；否则结束。
8. 摘要创建时只发布一个 `AiMemoryUpdates` 事件；Profile consumer 和 Memory Event consumer 用同一批消息分别更新画像和长期事件。
9. 摘要、长期事件或画像不可用时使用近期消息降级；历史工具结果按需读取失败时，重新查询业务工具或向用户澄清。

工具结果事实来源是 `ai_tool_calls.result`，保存最终工具结果 envelope，并通过 `tool_call_id` 与模型工具调用关联；成功 envelope 的 `result` 子字段保存真实工具返回 JSON。`get_tool_call_result` 可按 `tool_call_id` 读取当前用户当前 conversation 的历史工具结果；`ai_messages` 仍可保存 role=tool 消息和展示 metadata，但 Context Manager / MemoryProvider 不再从 `ai_messages.metadata` 读取工具结果。

### 5.6 知识库检索（RAG）

- 触发：普通 `user_message` 进入 Supervisor 前，用轻量分类模型（复用现有 Eino ChatModel）判断是否需要 RAG；分类上下文为最近 6 条消息 + 会话摘要。
- 判断：输出 `{need_rag, confidence, reason}`，`need_rag=true` 且 `confidence>=0.6` 才检索；分类失败、检索失败或结果为空时静默降级为正常回答。
- 检索：调用知识库平台 `POST /api/ragent/open-api/v1/retrieve`，`topK=5`，超时 3 秒不重试。
- 缓存：先调知识库 Embedding API 生成 query 向量，再使用 Redis Vector（HNSW cosine）按 0.95 相似度查缓存；未命中才调检索，检索响应中的 `queryEmbedding` 用于更新缓存；缓存 TTL 24 小时，完整保存 topK 片段。
- 注入：检索片段通过 `RetrieveRequest.RAGContext` 合并进 MemoryProvider 的 `ContextMessages`，只影响当前轮，不写历史。
- 输出：`assistant_message` 的 `data` 携带按文档去重的 `sources`（document_id/title/document_url/chunks），前端显示“n篇来源”按钮，点击片段跳转知识库 `preview/doc/{documentId}`。
- 持久化：sources 随 assistant 消息 metadata 落库，历史会话回看仍可展示。

## 6. 测试方案
### 6.1 单元测试
- 五个子 Agent 注册、完整业务白名单、共享查询和写工具唯一归属。
- `purchase-advisor` 绑定购物决策、`issue-triage` 绑定 Supervisor，新 AgentName 上下文和内部消息过滤。
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
- 商品发现 → 购物决策 → 订单履约的组合流程；直接下单、地址修改、资料修改与闲聊路由。
- 创建订单、取消订单、删除购物车的中断和确认恢复，依赖步骤在确认前不执行。
- 未接入退款、退换货请求不执行替代写工具、不虚构办理成功；政策无依据时明确说明。
- 前端五类子 Agent、工具分组、mock 和事件处理一致。
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

本次重组新增及迁移的测试放在根目录 `tests/`，内部包测试使用 Go overlay；入口为 `python tests/ai_business_agents/run.py`。可控模型桩验证编排和执行行为，真实模型单独检查 Prompt 路由，桩测试不能证明真实模型路由准确率。验收同时运行 `go test ./services/aiagent/...`、`go test ./apis/ai/...`、`go test ./...` 以及前端 `npm test`、`npm run build`；具体结果记录在实施计划，未执行的验证保持待验收。

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

## 订单 API 内部调用补充

AI 智能客服的高风险订单写操作统一通过 order-api 内部 HTTP 入口执行，不再让 `order_cancel` 直接调用 order RPC。内部入口包括：

- `POST /douyin/order/internal/create`
- `POST /douyin/order/internal/cancel`

这两个入口只允许内部服务调用，不增加 token。order-api 必须基于 HTTP 连接来源 `RemoteAddr` 做 IP 白名单校验，不能把 `X-Real-IP`、`X-Forwarded-For` 等可伪造请求头作为授权依据。内部请求体携带 `user_id`，order-api 校验来源 IP 后把该 `user_id` 注入服务端 context，再复用现有创建订单和取消订单 logic。

`services/aiagent` 侧保留 Execution Guard 作为可信用户 ID 来源，模型参数中的 `user_id` 继续被忽略。`order_create` 和 `order_cancel` handler 都把 Guard 注入的用户 ID 写入内部 HTTP 请求体，并通过 common HTTP JSON client 统一发送和解析 `{code,msg,data}` 响应。
# Langfuse 与模拟评测架构增补

观测采用隔离在 `internal/eino` 的官方 callback v2.0.2 和独立 TracerProvider；普通观测辅助结构位于 `internal/observability`。保持原有审计、确认、RPC 和数据库契约。独立 `cmd/evalrunner` 复用正式编排与 Guard，在一次试验内隔离模拟状态，不向正式服务增加模拟开关。数据采集、关联、任务契约和评分规则见 [观测与评测说明](ai-observability-evaluation.md)。
