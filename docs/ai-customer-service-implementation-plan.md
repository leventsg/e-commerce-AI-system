# AI 智能客服实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 基于现有电商微服务新增 AI 智能客服能力，支持 WebSocket 多轮对话、业务查询、商品推荐、低风险自动操作、高风险确认执行和审计追踪。

**Architecture:** 新增 `apis/ai` 作为对外 WebSocket 网关，新增 `services/aiagent` 作为 AI 编排 RPC 服务。`services/aiagent` 基于 Eino 提供 ChatModel、Tool、ToolsNode、Agent/Chain/Graph 编排能力，本地代码负责会话、工具风险元数据、Execution Guard、确认管理和审计。AI Agent 不侵入商品、库存、订单、优惠券、购物车、结算等核心服务，只通过现有 RPC 客户端调用业务能力，并强制从登录态注入用户 ID。

**Tech Stack:** Go、go-zero API/RPC、Eino、WebSocket、MySQL、Redis、OpenAI-compatible/DeepSeek ChatModel、现有 product/inventory/order/carts/coupons/checkout/audit RPC。

---

## 1. 交付范围

首期交付 PRD 中的核心闭环：

- WebSocket 实时聊天入口：`GET /douyin/ai/chat/ws?conversation_id=optional`
- 多轮会话、消息持久化、工具调用记录。
- 通过 Eino ChatModel 接入模型，首期配置兼容 OpenAI-compatible API 与 DeepSeek。
- 查询工具：商品、库存、订单、优惠券、购物车、结算单。
- 推荐工具：商品推荐与商品搜索兜底。
- 低风险写操作：添加购物车、减少购物车数量、领取优惠券。
- 高风险写操作确认：删除购物车、创建订单、取消订单、使用优惠券下单。
- 审计：工具调用、确认行为、所有写操作执行结果。
- 降级：模型不可用、工具超时、业务 RPC 失败时返回明确失败，不伪造成功。

不在首期强制交付：

- 运营后台配置页面。
- 支付、退款、地址修改等后续敏感操作。
- 复杂长期记忆推荐策略。首期只保留 `ai_user_memories` 表和最小读写接口。

## 2. 文件与模块规划

### 2.1 新增 API 网关

- Create: `apis/ai/ai.api`
- Create: `apis/ai/ai.go`
- Create: `apis/ai/etc/ai-api.yaml`
- Create: `apis/ai/etc/ai-api.prod.yaml`
- Create: `apis/ai/internal/config/config.go`
- Create: `apis/ai/internal/svc/servicecontext.go`
- Create: `apis/ai/internal/handler/routes.go`
- Create: `apis/ai/internal/handler/chathandler.go`
- Create: `apis/ai/internal/logic/chatwslogic.go`
- Create: `apis/ai/internal/types/types.go`

职责：

- 沿用 `WithClientMiddleware,WrapperAuthMiddleware` 获取登录态。
- 建立 WebSocket 连接。
- 校验客户端消息类型。
- 将 `user_id`、`conversation_id`、消息体转发给 `services/aiagent`。
- 将 Agent 返回的 assistant/tool/confirmation/error 事件推送给客户端。

### 2.2 新增 AI Agent RPC 服务

- Create: `services/aiagent/aiagent.proto`
- Create: `services/aiagent/aiagent.go`
- Create: `services/aiagent/etc/aiagent.yaml`
- Create: `services/aiagent/etc/aiagent.prod.yaml`
- Create: `services/aiagent/internal/config/config.go`
- Create: `services/aiagent/internal/svc/servicecontext.go`
- Create: `services/aiagent/internal/logic/chatlogic.go`
- Create: `services/aiagent/internal/logic/confirmactionlogic.go`
- Create: `services/aiagent/aiagentclient/aiagent.go`

核心 RPC：

```proto
service AiAgent {
  rpc Chat(ChatRequest) returns (ChatResponse);
  rpc ConfirmAction(ConfirmActionRequest) returns (ConfirmActionResponse);
}
```

### 2.3 Agent 内部包

- Create: `services/aiagent/internal/domain/message.go`
- Create: `services/aiagent/internal/domain/tool.go`
- Create: `services/aiagent/internal/domain/confirmation.go`
- Create: `services/aiagent/internal/eino/model_factory.go`
- Create: `services/aiagent/internal/eino/agent.go`
- Create: `services/aiagent/internal/eino/messages.go`
- Create: `services/aiagent/internal/eino/callbacks.go`
- Create: `services/aiagent/internal/conversation/manager.go`
- Create: `services/aiagent/internal/planner/planner.go`
- Create: `services/aiagent/internal/tools/registry.go`
- Create: `services/aiagent/internal/tools/executor.go`
- Create: `services/aiagent/internal/tools/product_tools.go`
- Create: `services/aiagent/internal/tools/inventory_tools.go`
- Create: `services/aiagent/internal/tools/order_tools.go`
- Create: `services/aiagent/internal/tools/cart_tools.go`
- Create: `services/aiagent/internal/tools/coupon_tools.go`
- Create: `services/aiagent/internal/tools/checkout_tools.go`
- Create: `services/aiagent/internal/confirmation/manager.go`
- Create: `services/aiagent/internal/audit/recorder.go`

职责：

- `eino`: Eino ChatModel 工厂、Agent/Chain/Graph 编排、消息转换、callback 事件转换、超时和降级。
- `conversation`: 会话创建、历史加载、消息保存、上下文裁剪。
- `planner`: 规则兜底意图识别、参数抽取、缺参追问和确认策略判断。
- `tools`: Eino Tool 注册、本地工具白名单、参数 schema、风险等级、RPC 执行、结果转换。
- `confirmation`: 确认记录创建、审批、拒绝、过期、幂等执行。
- `audit`: 调用 `services/audit` 或写入审计记录，覆盖所有写操作。

### 2.4 数据模型与 SQL

- Create: `dal/model/ai/conversations/ai_conversations.sql`
- Create: `dal/model/ai/messages/ai_messages.sql`
- Create: `dal/model/ai/tool_calls/ai_tool_calls.sql`
- Create: `dal/model/ai/confirmations/ai_confirmations.sql`
- Create: `dal/model/ai/user_memories/ai_user_memories.sql`
- Modify: `construct/depend/sql/init.sql`

表名：

- `ai_conversations`
- `ai_messages`
- `ai_tool_calls`
- `ai_confirmations`
- `ai_user_memories`

生成模型：

```bash
goctl model mysql ddl -src dal/model/ai/conversations/ai_conversations.sql -dir dal/model/ai/conversations -c
goctl model mysql ddl -src dal/model/ai/messages/ai_messages.sql -dir dal/model/ai/messages -c
goctl model mysql ddl -src dal/model/ai/tool_calls/ai_tool_calls.sql -dir dal/model/ai/tool_calls -c
goctl model mysql ddl -src dal/model/ai/confirmations/ai_confirmations.sql -dir dal/model/ai/confirmations -c
goctl model mysql ddl -src dal/model/ai/user_memories/ai_user_memories.sql -dir dal/model/ai/user_memories -c
```

## 3. 数据库实施

### Task 1: 创建 AI 数据表

**Files:**

- Create: `dal/model/ai/conversations/ai_conversations.sql`
- Create: `dal/model/ai/messages/ai_messages.sql`
- Create: `dal/model/ai/tool_calls/ai_tool_calls.sql`
- Create: `dal/model/ai/confirmations/ai_confirmations.sql`
- Create: `dal/model/ai/user_memories/ai_user_memories.sql`
- Modify: `construct/depend/sql/init.sql`

- [x] **Step 1: 新增会话表**

```sql
CREATE TABLE `ai_conversations` (
  `id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `title` varchar(128) NOT NULL DEFAULT '' COMMENT '会话标题',
  `status` varchar(32) NOT NULL DEFAULT 'active' COMMENT 'active/closed',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_updated` (`user_id`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [x] **Step 2: 新增消息表**

```sql
CREATE TABLE `ai_messages` (
  `id` varchar(64) NOT NULL COMMENT '消息ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `role` varchar(16) NOT NULL COMMENT 'user/assistant/tool',
  `content` text NOT NULL COMMENT '消息内容',
  `metadata` json DEFAULT NULL COMMENT '扩展信息',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`),
  KEY `idx_user_created` (`user_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [x] **Step 3: 新增工具调用表**

```sql
CREATE TABLE `ai_tool_calls` (
  `id` varchar(64) NOT NULL COMMENT '调用ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `tool_name` varchar(64) NOT NULL COMMENT '工具名称',
  `arguments` json NOT NULL COMMENT '工具参数',
  `result_summary` text COMMENT '结果摘要',
  `status` varchar(16) NOT NULL COMMENT 'success/failed',
  `error_message` varchar(512) NOT NULL DEFAULT '',
  `latency_ms` bigint NOT NULL DEFAULT 0,
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`),
  KEY `idx_user_tool_created` (`user_id`, `tool_name`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [x] **Step 4: 新增确认记录表**

```sql
CREATE TABLE `ai_confirmations` (
  `id` varchar(64) NOT NULL COMMENT '确认ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `tool_name` varchar(64) NOT NULL COMMENT '工具名称',
  `arguments` json NOT NULL COMMENT '待执行参数',
  `summary` varchar(512) NOT NULL COMMENT '确认摘要',
  `status` varchar(16) NOT NULL COMMENT 'pending/approved/rejected/expired/executed/failed',
  `expires_at` datetime NOT NULL COMMENT '过期时间',
  `executed_at` datetime DEFAULT NULL COMMENT '执行时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_status_expires` (`user_id`, `status`, `expires_at`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [x] **Step 5: 新增用户记忆表**

```sql
CREATE TABLE `ai_user_memories` (
  `id` varchar(64) NOT NULL COMMENT '记忆ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `memory_type` varchar(32) NOT NULL COMMENT 'preference/category/price',
  `content` text NOT NULL COMMENT '记忆内容',
  `confidence` decimal(5,4) NOT NULL DEFAULT 0.0000 COMMENT '置信度',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_type_updated` (`user_id`, `memory_type`, `updated_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [x] **Step 6: 生成 go-zero model 并运行编译检查**

Run:

```bash
go test ./dal/model/ai/...
```

Expected: `ok` 或无测试文件但包可编译。

## 4. AI Agent 基础骨架

### Task 2: 定义 RPC 契约与服务配置

**Files:**

- Create: `services/aiagent/aiagent.proto`
- Create: `services/aiagent/etc/aiagent.yaml`
- Create: `services/aiagent/etc/aiagent.prod.yaml`
- Create/Generate: `services/aiagent/**`

- [x] **Step 1: 定义 proto**

```proto
syntax = "proto3";
package aiagent;
option go_package = "./aiagent";

message ChatRequest {
  uint32 user_id = 1;
  string conversation_id = 2;
  string message_id = 3;
  string content = 4;
  string source = 5;
}

message AgentEvent {
  string type = 1;
  string conversation_id = 2;
  string message_id = 3;
  string content = 4;
  string tool = 5;
  string status = 6;
  string data_json = 7;
  string confirmation_id = 8;
  string action = 9;
  string summary = 10;
  int64 expires_at = 11;
  bool done = 12;
}

message ChatResponse {
  int32 status_code = 1;
  string status_msg = 2;
  repeated AgentEvent events = 3;
}

message ConfirmActionRequest {
  uint32 user_id = 1;
  string conversation_id = 2;
  string confirmation_id = 3;
  bool approved = 4;
}

message ConfirmActionResponse {
  int32 status_code = 1;
  string status_msg = 2;
  repeated AgentEvent events = 3;
}

service AiAgent {
  rpc Chat(ChatRequest) returns (ChatResponse);
  rpc ConfirmAction(ConfirmActionRequest) returns (ConfirmActionResponse);
}
```

- [x] **Step 2: 生成 RPC 代码**

Run:

```bash
goctl rpc protoc services/aiagent/aiagent.proto --go_out=services/aiagent --go-grpc_out=services/aiagent --zrpc_out=services/aiagent
```

Expected: 生成 `services/aiagent/internal`、`services/aiagent/aiagent`、`services/aiagent/aiagentclient`。

- [x] **Step 3: 配置 ServiceContext 依赖**

在 `services/aiagent/internal/config/config.go` 增加：

```go
type EinoConfig struct {
	Provider    string
	APIKey      string
	BaseURL     string
	Model       string
	Timeout     int64
	MaxTokens   int
	Temperature float64
}

type ToolTimeoutConfig struct {
	QuerySeconds int64
	WriteSeconds int64
}

type ConfirmationConfig struct {
	ExpireSeconds int64
}
```

并在 `Config` 中挂载 MySQL、Redis、Eino、工具超时、确认超时、现有业务 RPC。

- [x] **Step 4: 编译检查**

Run:

```bash
go test ./services/aiagent/...
```

Expected: 所有包可编译。

### Task 3: 接入 Eino ChatModel 与 Agent 编排

**Files:**

- Create: `services/aiagent/internal/eino/model_factory.go`
- Create: `services/aiagent/internal/eino/agent.go`
- Create: `services/aiagent/internal/eino/messages.go`
- Create: `services/aiagent/internal/eino/callbacks.go`
- Test: `services/aiagent/internal/eino/model_factory_test.go`
- Test: `services/aiagent/internal/eino/agent_test.go`

- [ ] **Step 1: 添加 Eino 依赖**

Run:

```bash
GOTOOLCHAIN=local go get github.com/cloudwego/eino@latest
```

Expected: `go.mod` 和 `go.sum` 新增 Eino 依赖。

- [ ] **Step 2: 写 ChatModel 工厂单测**

覆盖：

- Provider 名称为空时创建失败。
- `openai-compatible` 使用配置中的 `base_url`、`api_key`、`model`。
- `deepseek` 使用 OpenAI-compatible 协议配置。
- 超时时返回降级错误。

- [ ] **Step 3: 实现 Eino ChatModel 工厂**

```go
type ModelFactory interface {
	NewChatModel(ctx context.Context, cfg config.EinoConfig) (model.ChatModel, error)
}
```

- [ ] **Step 4: 实现消息转换**

将 `ai_messages` 历史转换为 Eino message：

- `user` -> user message
- `assistant` -> assistant message
- `tool` -> tool message 或追加为 assistant 可读上下文

- [ ] **Step 5: 实现 Agent Runner**

Agent Runner 输入当前用户消息、会话历史和工具集合，输出 `AgentEvent` 列表。首期允许内部使用 Eino Chain/Graph 或 ADK Agent，但对外只暴露稳定接口：

```go
type Runner interface {
	Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error)
	Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error)
}
```

- [ ] **Step 6: 实现降级策略**

模型不可用时返回业务错误：

```go
ErrModelUnavailable = errors.New("ai model unavailable, please retry later")
```

- [ ] **Step 7: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/eino -run Test -count=1
```

Expected: Eino ChatModel 创建、消息转换、Agent Runner、超时降级均通过。

## 5. 会话、意图与工具编排

### Task 4: 实现 Conversation Manager

**Files:**

- Create: `services/aiagent/internal/conversation/manager.go`
- Test: `services/aiagent/internal/conversation/manager_test.go`

- [ ] **Step 1: 单测会话创建**

输入空 `conversation_id` 时创建新会话，返回 `conv_` 前缀 ID，并保存用户消息。

- [ ] **Step 2: 单测跨用户隔离**

用户 A 传入用户 B 的 `conversation_id` 时返回权限错误。

- [ ] **Step 3: 实现上下文裁剪**

默认取最近 20 条消息，超过后只传递摘要和最近消息给 Eino ChatModel。

- [ ] **Step 4: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/conversation -run Test -count=1
```

Expected: 创建、恢复、隔离、裁剪均通过。

### Task 5: 实现 Eino Tool Registry

**Files:**

- Create: `services/aiagent/internal/domain/tool.go`
- Create: `services/aiagent/internal/tools/registry.go`
- Test: `services/aiagent/internal/tools/registry_test.go`

- [ ] **Step 1: 定义风险等级**

```go
const (
	RiskLow  = "low"
	RiskHigh = "high"
)
```

- [ ] **Step 2: 定义本地工具元数据**

每个工具保留本地元数据，用于风险控制、超时、审计和 RPC 路由：

```go
type Metadata struct {
	Name                string
	Risk                string
	RequireConfirmation bool
	TimeoutSeconds      int64
	WriteOperation       bool
}
```

- [ ] **Step 3: 注册首期 Eino Tool**

低风险：

- `product.search`
- `product.detail`
- `product.recommend`
- `inventory.get`
- `order.get`
- `order.list`
- `checkout.prepare`
- `checkout.detail`
- `cart.list`
- `cart.add`
- `cart.sub`
- `coupon.list`
- `coupon.detail`
- `coupon.claim`
- `coupon.my_list`
- `coupon.usage_list`
- `coupon.calculate`

高风险：

- `cart.delete`
- `order.create`
- `order.cancel`

- [ ] **Step 4: 单测确认策略**

断言 `cart.delete`、`order.create`、`order.cancel` 必须确认，查询和低风险写操作不需要确认。

- [ ] **Step 5: 单测 Eino Tool schema**

断言每个工具都能导出 Eino 可识别的 schema，且 schema 中不要求模型传入 `user_id`。

- [ ] **Step 6: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/tools -run TestRegistry -count=1
```

Expected: Eino Tool 注册、本地白名单、风险等级、超时配置全部符合 PRD。

### Task 6: 实现 Intent Planner

**Files:**

- Create: `services/aiagent/internal/planner/planner.go`
- Test: `services/aiagent/internal/planner/planner_test.go`

- [ ] **Step 1: 单测核心意图**

覆盖中文输入：

- “你好” -> `chat`
- “推荐几款适合学生党的手机” -> `recommend` + `product.recommend`
- “查一下订单 202406300001” -> `query` + `order.get`
- “帮我加入购物车，商品 12 买 2 件” -> `action` + `cart.add`
- “取消订单 202406300001” -> `action` + `order.cancel` + 需要确认

- [ ] **Step 2: 实现规则兜底 Planner**

首期以 Eino Tool Calling 作为主路径，同时实现关键词/参数抽取兜底。规则 Planner 必须在模型不可用时仍能处理明确工具意图，并把结果交给同一套 Tool Registry 和 Execution Guard。

- [ ] **Step 3: 缺参数时返回追问**

例如“帮我取消订单”缺少订单号时返回 assistant message，询问用户提供订单号，不创建确认。

- [ ] **Step 4: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/planner -run Test -count=1
```

Expected: 核心意图、工具选择、缺参追问均通过。

## 6. 业务工具接入

### Task 7: 实现 Execution Guard 安全边界

**Files:**

- Create: `services/aiagent/internal/tools/executor.go`
- Test: `services/aiagent/internal/tools/executor_test.go`

- [ ] **Step 1: 单测 user_id 注入**

模型或 Eino Tool 参数中即使包含 `user_id: 999`，执行前也必须覆盖为登录态 `user_id`。

- [ ] **Step 2: 单测超时策略**

查询类工具使用 3 秒超时，写操作使用 5 秒超时。

- [ ] **Step 3: 单测失败话术**

RPC 返回错误时，`AgentEvent.status` 必须为 `failed`，assistant message 不允许包含“已成功”。

- [ ] **Step 4: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/tools -run TestExecutor -count=1
```

Expected: 用户隔离、超时、失败降级均通过。

### Task 8: 接入查询与推荐工具

**Files:**

- Create: `services/aiagent/internal/tools/product_tools.go`
- Create: `services/aiagent/internal/tools/inventory_tools.go`
- Create: `services/aiagent/internal/tools/order_tools.go`
- Create: `services/aiagent/internal/tools/cart_tools.go`
- Create: `services/aiagent/internal/tools/coupon_tools.go`
- Create: `services/aiagent/internal/tools/checkout_tools.go`
- Test: `services/aiagent/internal/tools/query_tools_test.go`

- [ ] **Step 1: 实现商品 Eino Tool handler**

RPC 对应：

- `product.search` -> `ProductCatalogService.QueryProduct`
- `product.detail` -> `ProductCatalogService.GetProduct`
- `product.recommend` -> `ProductCatalogService.RecommendProduct`

- [ ] **Step 2: 实现订单 Eino Tool handler**

RPC 对应：

- `order.get` -> `OrderService.GetOrder`
- `order.list` -> `OrderService.ListOrders`

- [ ] **Step 3: 实现购物车、优惠券、结算 Eino Tool handler**

RPC 对应：

- `cart.list` -> `Cart.CartItemList`
- `coupon.list` -> `Coupons.ListCoupons`
- `coupon.detail` -> `Coupons.GetCoupon`
- `coupon.my_list` -> `Coupons.ListUserCoupons`
- `coupon.usage_list` -> `Coupons.ListCouponUsages`
- `coupon.calculate` -> `Coupons.CalculateCoupon`
- `checkout.detail` -> `CheckoutService.GetCheckoutDetail`

- [ ] **Step 4: 运行工具单测**

Run:

```bash
go test ./services/aiagent/internal/tools -run TestQueryTools -count=1
```

Expected: Eino Tool 入参转换、用户 ID 注入、RPC 调用、结果摘要字段均正确。

### Task 9: 接入低风险写操作

**Files:**

- Modify: `services/aiagent/internal/tools/cart_tools.go`
- Modify: `services/aiagent/internal/tools/coupon_tools.go`
- Test: `services/aiagent/internal/tools/write_tools_test.go`

- [ ] **Step 1: 实现低风险写操作 Eino Tool handler**

RPC 对应：

- `cart.add` -> `Cart.CreateCartItem`
- `cart.sub` -> `Cart.SubCartItem`
- `coupon.claim` -> `Coupons.ClaimCoupon`

- [ ] **Step 2: 写操作记录审计**

每次成功或失败都写入 `ai_tool_calls`，并调用审计记录器记录：

- `user_id`
- `tool_name`
- `arguments`
- `status`
- `error_message`
- `latency_ms`

- [ ] **Step 3: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/tools -run TestWriteTools -count=1
```

Expected: 低风险写操作无需确认，但必须记录审计。

## 7. 高风险确认流程

### Task 10: 实现 Confirmation Manager

**Files:**

- Create: `services/aiagent/internal/confirmation/manager.go`
- Test: `services/aiagent/internal/confirmation/manager_test.go`

- [ ] **Step 1: 单测创建确认**

创建确认返回：

- `confirmation_id`
- `action`
- `summary`
- `expires_at`
- 参数摘要

- [ ] **Step 2: 单测过期确认**

超过 `expires_at` 后确认，返回失败，状态更新为 `expired`。

- [ ] **Step 3: 单测重复确认**

同一个确认 ID 第二次执行时返回失败，不再次调用业务 RPC。

- [ ] **Step 4: 单测跨用户确认**

用户 A 不能执行用户 B 的确认 ID。

- [ ] **Step 5: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/confirmation -run Test -count=1
```

Expected: pending、approved、rejected、expired、executed、failed 状态流转正确。

### Task 11: 接入高风险 Eino Tool

**Files:**

- Modify: `services/aiagent/internal/tools/cart_tools.go`
- Modify: `services/aiagent/internal/tools/order_tools.go`
- Modify: `services/aiagent/internal/logic/confirmactionlogic.go`
- Test: `services/aiagent/internal/logic/confirmactionlogic_test.go`

- [ ] **Step 1: 首次请求只创建确认**

这些工具首次规划后只返回 `confirmation_required`，不调用业务 RPC：

- `cart.delete`
- `order.create`
- `order.cancel`

- [ ] **Step 2: 用户确认后通过 Execution Guard 执行业务 RPC**

RPC 对应：

- `cart.delete` -> `Cart.DeleteCartItem`
- `order.create` -> `OrderService.CreateOrder`
- `order.cancel` -> `OrderService.CancelOrder`

- [ ] **Step 3: 创建订单前置结算**

若用户表达购买意图且没有 `pre_order_id`：

1. 调用 `checkout.prepare` 创建预结算。
2. 返回结算金额与 `pre_order_id`。
3. 再创建 `order.create` 确认请求。

- [ ] **Step 4: 使用优惠券下单必须确认**

当 `order.create` 参数包含 `coupon_id` 时，确认摘要必须展示优惠券 ID、应付金额和商品数量。

- [ ] **Step 5: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/logic -run TestConfirmAction -count=1
```

Expected: 未确认不执行、确认后执行、过期和重复确认被拒绝。

## 8. WebSocket API 接入

### Task 12: 新增 `apis/ai`

**Files:**

- Create: `apis/ai/ai.api`
- Generate/Create: `apis/ai/**`
- Modify: `apis/ai/internal/handler/routes.go`
- Modify: `apis/ai/internal/logic/chatwslogic.go`

- [ ] **Step 1: 定义 API**

```go
syntax = "v1"

@server (
  middleware: WithClientMiddleware,WrapperAuthMiddleware
  prefix: /douyin/ai
)
service ai-api {
  @handler ChatHandler
  get /chat/ws
}
```

- [ ] **Step 2: 生成 API 代码**

Run:

```bash
goctl api go -api apis/ai/ai.api -dir apis/ai
```

Expected: 生成 handler、logic、svc、types 目录。

- [ ] **Step 3: 实现 WebSocket 升级和消息协议**

客户端输入类型：

- `user_message`
- `confirm_action`

服务端输出类型：

- `assistant_message`
- `tool_result`
- `confirmation_required`
- `error`

- [ ] **Step 4: 强制从登录态读取用户 ID**

禁止使用客户端消息体中的 `user_id`。缺少登录态时关闭连接并返回未授权错误。

- [ ] **Step 5: 运行 API 编译检查**

Run:

```bash
go test ./apis/ai/...
```

Expected: API 包全部可编译。

### Task 13: WebSocket 集成测试

**Files:**

- Create: `apis/ai/internal/logic/chatwslogic_test.go`

- [ ] **Step 1: 测试未登录拒绝**

无认证上下文连接 `/douyin/ai/chat/ws`，期望返回 401 或连接关闭。

- [ ] **Step 2: 测试普通聊天**

发送：

```json
{"type":"user_message","message_id":"client-msg-001","content":"你好","metadata":{"source":"web"}}
```

期望收到 `assistant_message`，`done=true`。

- [ ] **Step 3: 测试高风险确认**

发送取消订单消息后，期望收到 `confirmation_required`，且没有直接调用 `order.cancel`。

- [ ] **Step 4: 运行测试**

Run:

```bash
go test ./apis/ai/internal/logic -run TestChatWebSocket -count=1
```

Expected: 鉴权、聊天、确认请求流程通过。

## 9. 审计、限流与观测

### Task 14: 审计记录器

**Files:**

- Create: `services/aiagent/internal/audit/recorder.go`
- Test: `services/aiagent/internal/audit/recorder_test.go`

- [ ] **Step 1: 记录工具调用**

所有工具调用写入 `ai_tool_calls`。

- [ ] **Step 2: 记录写操作审计**

写操作额外调用 `services/audit` 的 `CreateAuditLog`，记录操作名称、用户 ID、参数摘要和结果。

- [ ] **Step 3: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/audit -run Test -count=1
```

Expected: 成功、失败、超时均有审计记录。

### Task 15: 限流与超时

**Files:**

- Modify: `services/aiagent/internal/tools/executor.go`
- Modify: `apis/ai/internal/logic/chatwslogic.go`
- Test: `services/aiagent/internal/tools/ratelimit_test.go`

- [ ] **Step 1: 用户级限流**

基于 Redis 对 `user_id` 维度限制聊天请求频率，例如每分钟 30 次。

- [ ] **Step 2: 工具级限流**

对写操作限制频率，例如每分钟 10 次。

- [ ] **Step 3: 超时配置落地**

查询工具 3 秒，写操作 5 秒，Eino ChatModel 调用使用配置项 `Eino.Timeout`。

- [ ] **Step 4: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/tools -run TestRateLimit -count=1
```

Expected: 超限返回明确错误，未超限请求正常执行。

## 10. 端到端验收

### Task 16: 验收场景

**Files:**

- Create: `test/ai-customer-service-e2e.md`

- [ ] **Step 1: WebSocket 连续对话**

用户可创建会话、继续传入 `conversation_id`，上下文不丢失。

- [ ] **Step 2: 商品查询与推荐**

输入“推荐几款适合学生党的手机”，返回商品 ID、名称、价格、库存、图片、分类和推荐理由。

- [ ] **Step 3: 查询订单**

输入订单号，只能查询当前登录用户订单。

- [ ] **Step 4: 添加购物车和领取优惠券**

无需确认，成功后返回工具结果和 assistant 总结。

- [ ] **Step 5: 取消订单**

首次请求必须返回 `confirmation_required`；确认后才调用 `OrderService.CancelOrder`。

- [ ] **Step 6: 创建订单**

先创建预结算，再返回确认请求；确认后调用 `OrderService.CreateOrder`。

- [ ] **Step 7: 风控验证**

覆盖伪造 `user_id`、跨用户确认、过期确认、重复确认、工具失败不伪造成功。

## 11. 推荐实施顺序

1. 数据库与 model：Task 1。
2. Agent RPC 骨架与配置：Task 2。
3. Eino ChatModel/Agent、会话、Planner、Registry：Task 3-6。
4. Execution Guard 与查询工具：Task 7-8。
5. 低风险写操作与审计：Task 9、Task 14。
6. 确认流程与高风险工具：Task 10-11。
7. WebSocket API：Task 12-13。
8. 限流、超时、端到端验收：Task 15-16。

每完成一个阶段执行：

```bash
go test ./services/aiagent/... ./apis/ai/...
```

最终全量验证：

```bash
go test ./...
```

## 12. 风险与处理策略

- 模型结果不稳定：首期保留规则 Planner 兜底，明确意图不依赖模型。
- WebSocket 流式与 RPC 非流式不匹配：首期 RPC 返回事件数组，API 网关逐条推送；后续再升级为服务端流式 RPC。
- 用户越权：所有工具执行前统一覆盖 `user_id`，工具层不信任模型和客户端参数。
- Eino 抽象泄漏到业务层：只允许 `internal/eino` 和 `internal/tools` 直接依赖 Eino，业务 RPC 转换和确认审计保持本地接口稳定。
- 确认重复执行：确认状态更新与执行需要在事务或 Redis 锁保护下完成。
- 业务服务返回字段不完整：工具结果转换层只暴露 PRD 要求字段，缺失字段返回空值并记录日志。
- 模型不可用：返回“AI 服务暂时不可用，请稍后重试”，查询/写操作不自动编造结果。

## 13. 完成标准

- `apis/ai` 可通过 WebSocket 完成连续对话。
- `services/aiagent` 可基于 Eino 编排模型、工具调用、确认和审计。
- 查询、推荐、低风险操作、高风险确认执行均满足 PRD。
- 过期或重复确认不能执行。
- 所有写操作有审计记录。
- `go test ./services/aiagent/... ./apis/ai/...` 通过。
- 风控场景在 `test/ai-customer-service-e2e.md` 中有明确验收记录。
