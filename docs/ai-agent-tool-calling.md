# AI Agent 工具调用实现与流程

## 1. 文档范围

本文说明本项目 AI 智能客服的工具调用实现，覆盖 WebSocket 入口、意图规划、工具注册、Execution Guard、业务 RPC、高风险确认和审计链路。内容以当前代码为准，而不是仅描述目标架构。

相关基线文档：

- `docs/ai-customer-service-prd.md`
- `docs/ai-customer-service-design.md`
- `docs/ai-customer-service-implementation-plan.md`

## 2. 当前实现结论

当前业务工具调用的实际主链路是：

```text
WebSocket user_message
  -> apis/ai（从鉴权上下文取得 user_id）
  -> AiAgent.Chat RPC
  -> ConversationManager（会话与消息持久化）
  -> IntentPlanner
       -> Eino ChatModel 输出结构化 JSON
       -> 失败时最多重试一次
       -> 再失败时进入中文规则 Planner
  -> Tool Registry 校验工具、参数和确认策略
  -> ChatLogic 按查询/低风险写/高风险写分流
  -> Executor（Execution Guard）
  -> 工具 Handler 转换参数并调用业务 RPC
  -> ai_tool_calls + 写操作 audit RPC
  -> AgentEvent -> AiAgent RPC -> WebSocket
```

工具已经实现为 Eino `InvokableTool`，并提供 Eino `ToolInfo` schema。当前 `internal/eino/agent.go` 的 AgentRunner 使用 Eino ADK ChatModelAgent：工具 schema 通过 ChatModel tool binding 给模型，可执行工具通过 `ToolsConfig.ToolsNodeConfig.Tools` 给 ADK ToolsNode。在线主链路已经运行“模型 tool call -> ToolsNode -> 工具结果回填 -> 最终回复”的循环，ADK assistant/tool 中间事件会转换为领域事件并写入 `ai_messages`。

## 3. 核心组件

| 组件 | 位置 | 职责 |
|---|---|---|
| WebSocket 网关 | `apis/ai/internal/logic/chatlogic.go` | 鉴权用户透传、协议校验、调用 Chat/ConfirmAction RPC、事件回写 |
| 会话管理 | `services/aiagent/internal/conversation/manager.go` | 创建/校验会话、保存用户消息、加载有界历史 |
| 意图 Planner | `services/aiagent/internal/planner/planner.go` | 使用 Eino ChatModel 生成结构化计划，校验后失败降级到规则识别 |
| Tool Registry | `services/aiagent/internal/tools/registry.go` | 同步维护 Eino schema 与本地风险、超时、读写和 RPC metadata |
| 工具集合 | `services/aiagent/internal/tools/*_tools.go` | 注册 Handler、转换参数、调用既有业务 RPC、压缩返回结果 |
| Executor | `services/aiagent/internal/tools/executor.go` | 工具白名单检查、敏感参数剔除、可信用户注入、超时、统一事件和记录 |
| 高风险工具 | `services/aiagent/internal/tools/high_risk_tools.go` | 首次调用只创建确认；批准后才执行真实 Handler |
| Confirmation Manager | `services/aiagent/internal/confirmation/manager.go` | 确认状态机、用户/会话归属校验、过期和幂等控制 |
| 审计 Recorder | `services/aiagent/internal/audit/recorder.go` | 所有调用写 `ai_tool_calls`，写操作额外调用 audit RPC |
| 普通回复 Runner | `services/aiagent/internal/eino/agent.go` | 无工具计划时调用 ChatModel 生成普通对话回复 |

`services/aiagent/internal/svc/servicecontext.go` 在服务启动时创建并连接上述对象。注册顺序很重要：先创建 Registry 和 Executor，再创建 QueryTools、WriteTools、HighRiskTools；各工具集合会把 Registry 中仅含 schema 的 `staticTool` 替换成可执行的 Eino `InvokableTool` 包装器。

## 4. 工具注册机制

### 4.1 双重注册

`Registry` 对每个工具同时保存两类信息：

1. Eino 工具：`map[string]tool.InvokableTool`，对外提供名称、描述和参数 schema。
2. 本地 metadata：`map[string]domain.Metadata`，用于不可交给模型决定的安全策略。

metadata 包含：

```go
type Metadata struct {
    Name                string
    Risk                string
    RequireConfirmation bool
    TimeoutSeconds      int64
    WriteOperation      bool
    RPCService          string
    RPCMethod           string
}
```

schema 不包含 `user_id`。用户身份不属于模型参数，而属于可信执行上下文。

### 4.2 Eino 可执行包装器

查询、写入和高风险工具分别使用 `queryInvokableTool`、`writeInvokableTool` 和 `highRiskInvokableTool` 实现 Eino `InvokableTool`：

```text
InvokableRun(JSON arguments)
  -> 从 context 读取 ToolExecutionContext
  -> 拒绝缺失可信 user_id 的调用
  -> JSON 解码
  -> 转为 ExecuteRequest
  -> QueryTools / WriteTools / HighRiskTools
  -> Executor 或 Confirmation Manager
  -> 返回结构化 data_json
```

调用这类 Eino 包装器前，编排器必须通过 `tools.WithToolExecutionContext` 注入 `UserID`、`ConversationID`、`MessageID` 和 `ClientIP`。当前在线 `ChatLogic` 直接构造 `ExecuteRequest`，尚未调用这些 Eino 包装器。

## 5. 意图与工具选择

### 5.1 LLM Planner

`IntentPlanner.Plan` 优先创建 Eino ChatModel，并向模型发送集中维护的 system prompt、最近最多 8 条压缩历史以及当前输入。模型必须返回 JSON：

```json
{
  "intent": "query|recommend|action|chat",
  "tool_name": "order.get",
  "arguments": {"order_id": "202406300001"},
  "missing_params": [],
  "assistant_message": ""
}
```

Planner 不直接信任该结果。`validatedPlan` 会：

- 校验 intent 是否允许；
- 通过 Registry 拒绝未注册工具；
- 删除 `user_id`、token、session、auth 等敏感参数；
- 根据 Eino schema 检查必填参数；
- 用本地 metadata 覆盖工具规范名称和 `RequireConfirmation`。

模型初始化、生成、JSON 解析或校验失败时最多尝试两次，之后进入规则 Planner。

### 5.2 规则兜底

规则 Planner 当前明确覆盖订单取消、订单查询、加入购物车和商品推荐。它仍通过同一个 Registry 生成计划，因此不会绕过工具白名单或确认策略。缺少订单号、商品 ID 等必填信息时返回 `assistant_message` 追问，不执行工具。

### 5.3 普通聊天

Planner 未选择工具时，`ChatLogic` 调用 `AgentRunner.Run`。该路径使用 Eino ADK ChatModelAgent 生成 assistant/tool 事件；tool 中间结果和最终 assistant 回复都会持久化到 `ai_messages`。模型不可用、返回空内容或调用失败时返回明确错误，不会编造业务结果。

## 6. 低风险工具执行流程

以 `cart.add` 为例：

```text
用户消息
  -> Planner: action + cart.add + {product_id, quantity}
  -> ChatLogic 删除任何 user_id 参数
  -> 从 ChatRequest.UserId 构造 ExecuteRequest.UserID
  -> WriteTools.Execute
  -> Executor 查 Registry metadata
  -> 再次清理敏感参数
  -> 根据 metadata 创建 5 秒 context timeout
  -> cart handler 从 HandlerRequest.UserID 生成 RPC UserId
  -> Cart.CreateCartItem RPC
  -> HandlerResult{Data, Summary}
  -> tool_result(success)
  -> ai_tool_calls 记录
  -> audit.CreateAuditLog（因为是写操作）
  -> 追加 assistant_message 摘要
```

`Executor` 是当前实现中的 Execution Guard。Handler 收到的 `UserID` 只能来自 `ExecuteRequest.UserID`，不会从模型 arguments 中读取。每个具体 Handler 负责参数类型、范围及 RPC 返回状态校验，并只返回生成用户摘要所需的紧凑字段。

默认查询超时为 3 秒，写操作超时为 5 秒，可由 `ToolTimeout` 配置覆盖。超时或 RPC 错误统一生成 `status=failed` 的 `tool_result`。

## 7. 高风险确认流程

高风险工具为 `cart.delete`、`order.create`、`order.cancel`。

### 7.1 创建确认

```text
Planner 选择高风险工具
  -> ChatLogic 以 Registry metadata 为最终确认依据
  -> HighRiskTools.RequestConfirmation
  -> 校验 risk=high、require_confirmation=true、write=true
  -> 清理敏感参数
  -> 查询必要业务信息并生成用户可读摘要
  -> ConfirmationManager.Create
  -> MySQL 写入 pending 记录和过期时间
  -> 返回 confirmation_required
```

首次请求不会调用目标写 RPC。`order.create` 在创建确认前还会以可信用户查询预结算详情；使用优惠券时调用 `coupon.calculate` 验证可用性并计算最新应付金额。

### 7.2 用户批准或拒绝

客户端发送：

```json
{
  "type": "confirm_action",
  "confirmation_id": "confirm_xxx",
  "approved": true
}
```

网关仍从鉴权上下文获取用户 ID，并调用 `AiAgent.ConfirmAction`。处理过程为：

```text
ConfirmAction
  -> ConfirmationManager.Decide
       -> 校验 confirmation_id 的 user_id 和 conversation_id 归属
       -> Redis 短锁合并并发请求
       -> Redis 故障时降级为 MySQL CAS
       -> 校验 pending 和 expires_at
       -> 原子 pending -> approved/rejected
  -> rejected：返回“操作已取消”，不调用业务 RPC
  -> approved：HighRiskTools.ExecuteConfirmed
       -> 同一个 Executor
       -> 真实业务 RPC
       -> 工具调用记录和写审计
  -> 成功或业务已执行：approved -> executed
  -> 真实执行失败：approved -> failed
```

Redis 锁不跨业务 RPC 持有。最终幂等由 MySQL 条件更新保证；已拒绝、已过期、已执行、失败或跨用户的确认不能再次执行。

## 8. 身份与安全边界

身份传递链如下：

```text
WrapperAuthMiddleware
  -> request context[biz.UserIDKey]
  -> apis/ai ChatLogic 的 userID
  -> ChatRequest.UserId / ConfirmActionRequest.UserId
  -> ExecuteRequest.UserID
  -> HandlerRequest.UserID
  -> 业务 RPC 请求 UserId
```

关键防护：

- WebSocket payload 一旦包含 `user_id`，网关直接返回错误。
- Planner 和 Executor 都会剔除 arguments 中的 `user_id` 及常见认证字段。
- Eino `InvokableTool` 没有可信 `ToolExecutionContext` 时拒绝执行。
- 用户数据类 Handler 使用认证用户 ID 调业务 RPC。
- 确认记录同时校验用户和会话归属。
- 高风险与确认策略来自本地 Registry，不采信模型声明。

## 9. 记录、审计与失败语义

所有进入 Executor 的工具调用都会尝试写入 `ai_tool_calls`，记录 conversation ID、user ID、脱敏参数、工具名、状态、结果摘要、错误和耗时。写操作还会调用 audit 服务写审计日志。

失败处理遵守以下语义：

- Handler/RPC 失败：返回 `tool_result.failed`，不会生成成功结果。
- 超时：明确返回“工具调用超时，未完成操作”。
- 查询审计记录失败：业务结果保持原状态，记录错误日志。
- 写操作业务成功但审计失败：事件改为 failed，`data_json.business_executed=true`，提示操作已完成但审计失败，防止用户盲目重试。
- 高风险业务已执行但后续记录失败：确认仍标为 `executed`，避免确认 ID 被重复使用。
- 消息持久化失败且业务已执行：返回“业务结果已产生，请勿重复操作”。

## 10. 已注册工具与 RPC 映射

| 工具 | 分类 | 确认 | RPC |
|---|---|---:|---|
| `product.search` | 查询 | 否 | `ProductCatalogService.QueryProduct` |
| `product.detail` | 查询 | 否 | `ProductCatalogService.GetProduct` |
| `product.recommend` | 查询/推荐 | 否 | `ProductCatalogService.RecommendProduct` |
| `inventory.get` | 查询 | 否 | `Inventory.GetInventory` |
| `order.get` | 查询 | 否 | `OrderService.GetOrder` |
| `order.list` | 查询 | 否 | `OrderService.ListOrders` |
| `checkout.prepare` | 低风险写 | 否 | `CheckoutService.PrepareCheckout` |
| `checkout.detail` | 查询 | 否 | `CheckoutService.GetCheckoutDetail` |
| `cart.list` | 查询 | 否 | `Cart.CartItemList` |
| `cart.add` | 低风险写 | 否 | `Cart.CreateCartItem` |
| `cart.sub` | 低风险写 | 否 | `Cart.SubCartItem` |
| `cart.delete` | 高风险写 | 是 | `Cart.DeleteCartItem` |
| `coupon.list` | 查询 | 否 | `Coupons.ListCoupons` |
| `coupon.detail` | 查询 | 否 | `Coupons.GetCoupon` |
| `coupon.claim` | 低风险写 | 否 | `Coupons.ClaimCoupon` |
| `coupon.my_list` | 查询 | 否 | `Coupons.ListUserCoupons` |
| `coupon.usage_list` | 查询 | 否 | `Coupons.ListCouponUsages` |
| `coupon.calculate` | 查询 | 否 | `Coupons.CalculateCoupon` |
| `order.create` | 高风险写 | 是 | `OrderService.CreateOrder` |
| `order.cancel` | 高风险写 | 是 | `OrderService.CancelOrder` |

## 11. WebSocket 输出

工具链最终统一转换为四种服务端事件：

- `assistant_message`：普通回答、追问或工具结果摘要；
- `tool_result`：结构化工具状态和 `data`；
- `confirmation_required`：确认 ID、action、摘要、过期时间和参数摘要；
- `error`：模型、协议或持久化等错误。

`apis/ai` 只接受上述事件类型，并验证 `data_json` 是合法 JSON 后再写回 WebSocket。

## 12. 新增工具时的实现清单

新增工具应按以下顺序完成：

1. 在 `internal/domain/tool.go` 定义稳定工具名。
2. 在 `defaultToolSpecs` 同时声明 schema、风险、确认、超时、读写分类和 RPC 映射；schema 不得包含 `user_id`。
3. 在对应 `*_tools.go` 增加 Handler，所有用户数据 RPC 使用 `HandlerRequest.UserID`。
4. 将 Handler 合并到 QueryTools、WriteTools 或 HighRiskTools；高风险工具同时提供确认摘要函数。
5. 确保结果结构紧凑，RPC 失败返回 error 而不是成功摘要。
6. 为 schema、参数转换、用户 ID 覆盖、超时、审计和确认策略补充测试。
7. 若工具需要被当前在线聊天链路选择，同步更新 intent prompt；明确中文意图还应按需要扩展规则 Planner。

Eino 原生 tool-calling 已通过 ADK ChatModelAgent 接入在线主链路。`internal/eino` 负责组装 ChatModel tool binding、ADK ToolsNode 和多轮执行；每次 `InvokableRun` 前仍必须注入可信 `ToolExecutionContext`。该改造不能绕开现有 Registry、Executor、Confirmation Manager 和 Recorder。

## 13. 设计目标与当前差异

当前实现与设计文档相比主要有以下差异：

1. Eino ADK ChatModelAgent 已接入在线主链路；结构化 Intent Planner 仍作为明确中文意图兜底。
2. Eino 工具 schema 和可执行包装器必须同时注册，分别供模型识别和 ToolsNode 执行。
3. 当前工具执行后由本地 Handler 的 `Summary` 生成 assistant message，而不是将工具结果再次交给 ChatModel 总结。
4. `AgentRunner.Stream` 已实现模型流读取，但当前 `AiAgent.Chat` 使用同步 `Run`，RPC 响应和 WebSocket 推送不是逐 token 的端到端流式链路。

这些差异不影响当前工具调用的身份隔离、确认和审计约束，但在声称完成“Eino 原生 Agent 工具编排”或“端到端流式输出”前需要补齐。
