# AI 智能客服实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 基于现有电商微服务新增 AI 智能客服能力，支持 SSE 多轮对话、业务查询、商品推荐、低风险自动操作、高风险确认执行和审计追踪。

**Architecture:** 新增 `apis/ai` 作为对外 SSE 网关，新增 `services/aiagent` 作为 AI 编排 RPC 服务。`services/aiagent` 基于 Eino 提供 ChatModel、Tool、ToolsNode、Agent/Chain/Graph 编排能力，本地代码负责会话、工具风险元数据、Execution Guard、确认管理和审计。AI Agent 不侵入商品、库存、订单、优惠券、购物车、结算等核心服务，只通过现有 RPC 客户端调用业务能力，并强制从登录态注入用户 ID。

**Tech Stack:** Go、go-zero API/RPC、Eino、SSE、MySQL、Redis、OpenAI-compatible/DeepSeek ChatModel、现有 product/inventory/order/carts/coupons/checkout/audit RPC。

---

## 当前架构修订

本文早期任务中关于 `IntentPlanner`、`IntentContext`、`IntentModel` 和 `services/aiagent/internal/planner` 的实现步骤已被当前 Supervisor Agent 架构取代。在线聊天主链路现在直接构建 `AgentContext`，进入 Eino ADK `ChatModelAgent + AgentTool` 编排；Supervisor 负责意图识别、任务拆解、领域 SubAgent 路由和最终总结。业务工具只绑定到对应 SubAgent，并继续经过 Eino InvokableTool、Execution Guard、确认拦截和审计链路。当前不使用 ADK `prebuilt/supervisor` / AgentTransfer。

## 1. 交付范围

首期交付 PRD 中的核心闭环：

- SSE 实时聊天入口：`POST /douyin/ai/chat`
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
- Create: `apis/ai/internal/logic/chatlogic.go`
- Create: `apis/ai/internal/types/types.go`

职责：

- 沿用 `WithClientMiddleware,WrapperAuthMiddleware` 获取登录态。
- 接收 POST JSON 并建立 SSE 响应流。
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
  rpc Chat(ChatRequest) returns (stream AgentEvent);
  rpc ConfirmAction(ConfirmActionRequest) returns (stream AgentEvent);
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
  `id` bigint unsigned NOT NULL AUTO_INCREMENT COMMENT '消息自增序号',
  `msg_id` varchar(64) NOT NULL COMMENT '消息ID',
  `conversation_id` varchar(64) NOT NULL COMMENT '会话ID',
  `user_id` bigint unsigned NOT NULL COMMENT '用户ID',
  `role` varchar(16) NOT NULL COMMENT 'user/assistant/tool',
  `content` text NOT NULL COMMENT '消息内容',
  `metadata` json DEFAULT NULL COMMENT '扩展信息',
  `client_message_id` varchar(128) DEFAULT NULL COMMENT '前端生成的用户消息幂等ID',
  `dedupe_client_message_id` varchar(128) GENERATED ALWAYS AS (case when `role` = 'user' then `client_message_id` else NULL end) STORED COMMENT '仅用户消息参与幂等唯一约束',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_msg_id` (`msg_id`),
  UNIQUE KEY `uk_user_client_message` (`user_id`, `dedupe_client_message_id`),
  KEY `idx_conversation_id` (`conversation_id`, `id`),
  KEY `idx_user_id` (`user_id`, `id`),
  KEY `idx_user_conversation_client_role_id` (`user_id`, `conversation_id`, `client_message_id`, `role`, `id`)
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
  `run_id` varchar(64) NOT NULL DEFAULT '' COMMENT 'Agent run ID',
  `checkpoint_id` varchar(128) NOT NULL DEFAULT '' COMMENT 'Eino checkpoint ID',
  `interrupt_id` varchar(128) NOT NULL DEFAULT '' COMMENT 'Eino interrupt ID',
  `expires_at` datetime NOT NULL COMMENT '过期时间',
  `executed_at` datetime DEFAULT NULL COMMENT '执行时间',
  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  `updated_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`),
  KEY `idx_user_status_expires` (`user_id`, `status`, `expires_at`),
  KEY `idx_conversation_created` (`conversation_id`, `created_at`),
  KEY `idx_checkpoint_interrupt` (`checkpoint_id`, `interrupt_id`)
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
  string client_message_id = 6;
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
  rpc Chat(ChatRequest) returns (stream AgentEvent);
  rpc ConfirmAction(ConfirmActionRequest) returns (stream AgentEvent);
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

- [x] **Step 1: 添加 Eino 依赖**

Run:

```bash
GOTOOLCHAIN=local go get github.com/cloudwego/eino@latest
```

Expected: `go.mod` 和 `go.sum` 新增 Eino 依赖。

- [x] **Step 2: 写 ChatModel 工厂单测**

覆盖：

- Provider 名称为空时创建失败。
- `openai-compatible` 使用配置中的 `base_url`、`api_key`、`model`。
- `deepseek` 使用 OpenAI-compatible 协议配置。
- 超时时返回降级错误。

- [x] **Step 3: 实现 Eino ChatModel 工厂**

```go
type ModelFactory interface {
	NewChatModel(ctx context.Context, cfg config.EinoConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error)
	NewStructuredChatModel(ctx context.Context, cfg config.EinoConfig, structured StructuredOutputConfig, tools ...*schema.ToolInfo) (model.BaseChatModel, error)
}
```

- [x] **Step 4: 实现消息转换**

将 `ai_messages` 历史转换为 Eino message：

- `user` -> user message
- `assistant` -> assistant message
- `tool` -> tool message 或追加为 assistant 可读上下文

- [x] **Step 5: 实现 Agent Runner**

Agent Runner 输入当前用户消息、会话历史和工具集合，输出 `AgentEvent` 列表。当前实现使用 Eino ADK ChatModelAgent：ChatModel 通过 `WithTools` 绑定 ToolInfo，ChatModelAgent 通过 `ToolsConfig.ToolsNodeConfig.Tools` 接收可执行工具，ADK 内部调用 ToolsNode 执行已注册工具，并将工具结果回填模型生成最终回复。ADK assistant/tool 中间事件会转换为领域事件并写入 `ai_messages`。对外只暴露稳定接口：

```go
type Runner interface {
	Run(ctx context.Context, req RunRequest) ([]domain.AgentEvent, error)
	Stream(ctx context.Context, req RunRequest) (<-chan domain.AgentEvent, error)
}
```

- [x] **Step 6: 实现降级策略**

模型不可用时返回业务错误：

```go
ErrModelUnavailable = errors.New("ai model unavailable, please retry later")
```

- [x] **Step 7: 运行测试**

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

- `product_search`
- `product_detail`
- `product_recommend`
- `inventory_get`
- `order_get`
- `order_list`
- `checkout_prepare`
- `checkout_detail`
- `cart_list`
- `cart_add`
- `cart_sub`
- `coupon_list`
- `coupon_detail`
- `coupon_claim`
- `coupon_my_list`
- `coupon_usage_list`
- `coupon_calculate`

高风险：

- `cart_delete`
- `order_create`
- `order_cancel`

- [ ] **Step 4: 单测确认策略**

断言 `cart_delete`、`order_create`、`order_cancel` 必须确认，查询和低风险写操作不需要确认。

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
- Create: `services/aiagent/internal/prompts/intent.go`
- Modify: `services/aiagent/internal/config/config.go`
- Modify: `services/aiagent/etc/aiagent.yaml`
- Modify: `services/aiagent/etc/aiagent.prod.yaml`
- Modify: `docs/ai-customer-service-design.md`
- Test: `services/aiagent/internal/planner/planner_test.go`
- Test: `services/aiagent/internal/prompts/intent_test.go`

- [ ] **Step 1: 单测 fast LLM 优先与重试**

覆盖：

- fake LLM 返回 `product_recommend` JSON 时优先使用 LLM 结果。
- fake LLM 返回 `order_cancel` JSON 时确认策略来自 Tool Registry metadata。
- fake LLM 参数包含 `user_id` 时必须删除。
- 第一次 LLM 错误或返回非法 JSON，第二次成功时使用第二次结果。
- 两次 LLM 都失败或都返回未注册工具时回退规则 Planner。

- [ ] **Step 2: 单测核心规则意图**

覆盖中文输入：

- “你好” -> `chat`
- “推荐几款适合学生党的手机” -> `recommend` + `product_recommend`
- “查一下订单 202406300001” -> `query` + `order_get`
- “帮我加入购物车，商品 12 买 2 件” -> `action` + `cart_add`
- “购物车条目 8 减少 2 件” -> `action` + `cart_sub`
- “取消订单 202406300001” -> `action` + `order_cancel` + 需要确认

- [ ] **Step 3: 实现 fast LLM + 重试 + 规则兜底 Planner**

首期以 Eino Tool Calling 作为主路径，Intent Planner 内部优先使用 fast LLM 返回结构化 JSON 做意图识别。LLM 调用失败、JSON 解析失败、工具未注册或参数不合格时重试一次；第二次仍失败后使用关键词/参数抽取规则兜底。所有结果必须经过 Tool Registry 校验，且不得保留模型输出中的 `user_id`。

- [ ] **Step 4: 抽离 Prompt 文本**

将 Intent Planner system prompt 放入 `services/aiagent/internal/prompts`，Planner 只引用 prompt 函数，不直接硬编码长 prompt。

- [ ] **Step 5: 缺参数时返回追问**

例如“帮我取消订单”缺少订单号时返回 assistant message，询问用户提供订单号，不创建确认。

- [ ] **Step 6: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/prompts -run Test -count=1
go test ./services/aiagent/internal/planner -run Test -count=1
go test ./services/aiagent/... -run Test -count=1
```

Expected: fast LLM 优先、一次重试、规则兜底、工具选择、缺参追问均通过。

## 6. 业务工具接入

### Task 7: 实现 Execution Guard 安全边界

**Files:**

- Create: `services/aiagent/internal/tools/executor.go`
- Test: `services/aiagent/internal/tools/executor_test.go`

- [x] **Step 1: 单测 user_id 注入**

模型或 Eino Tool 参数中即使包含 `user_id: 999`，执行前也必须覆盖为登录态 `user_id`。

- [x] **Step 2: 单测超时策略**

查询类工具使用 3 秒超时，写操作使用 5 秒超时。

- [x] **Step 3: 单测失败话术**

RPC 返回错误时，`AgentEvent.status` 必须为 `failed`，assistant message 不允许包含“已成功”。

- [x] **Step 4: 运行测试**

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

- [x] **Step 1: 实现商品 Eino Tool handler**

RPC 对应：

- `product_search` -> `ProductCatalogService.QueryProduct`
- `product_detail` -> `ProductCatalogService.GetProduct`
- `product_recommend` -> `ProductCatalogService.RecommendProduct`

- [x] **Step 2: 实现订单 Eino Tool handler**

RPC 对应：

- `order_get` -> `OrderService.GetOrder`
- `order_list` -> `OrderService.ListOrders`

- [x] **Step 3: 实现购物车、优惠券、结算 Eino Tool handler**

RPC 对应：

- `cart_list` -> `Cart.CartItemList`
- `coupon_list` -> `Coupons.ListCoupons`
- `coupon_detail` -> `Coupons.GetCoupon`
- `coupon_my_list` -> `Coupons.ListUserCoupons`
- `coupon_usage_list` -> `Coupons.ListCouponUsages`
- `coupon_calculate` -> `Coupons.CalculateCoupon`
- `checkout_detail` -> `CheckoutService.GetCheckoutDetail`

- [x] **Step 4: 运行工具单测**

Run:

```bash
go test ./services/aiagent/internal/tools -run TestQueryTools -count=1
```

Expected: Eino Tool 入参转换、用户 ID 注入、RPC 调用、结果摘要字段均正确。

### Task 9: 接入低风险写操作

**Files:**

- Modify: `services/aiagent/internal/tools/cart_tools.go`
- Modify: `services/aiagent/internal/tools/coupon_tools.go`
- Modify: `services/aiagent/internal/tools/executor.go`
- Create: `services/aiagent/internal/tools/write_tools.go`
- Create: `services/aiagent/internal/audit/recorder.go`
- Modify: `services/aiagent/internal/svc/servicecontext.go`
- Test: `services/aiagent/internal/tools/write_tools_test.go`
- Test: `services/aiagent/internal/audit/recorder_test.go`

- [x] **Step 1: 实现低风险写操作 Eino Tool handler**

RPC 对应：

- `cart_add` -> `Cart.CreateCartItem`
- `cart_sub` -> `Cart.SubCartItem`
- `coupon_claim` -> `Coupons.ClaimCoupon`

`Cart.CreateCartItem` 和 `Cart.SubCartItem` 的现有 RPC 语义均为单次增减 1。AI 工具层按 `quantity` 重复调用以适配 Tool schema；`cart_sub` 先通过当前用户的 `CartItemList` 将 `cart_item_id` 转换为 RPC 所需的 `product_id`，并禁止将数量减少到 0，删除操作仍由后续高风险确认流程处理。

- [x] **Step 2: 写操作记录审计**

每次成功或失败都写入 `ai_tool_calls`，并调用审计记录器记录：

- `user_id`
- `tool_name`
- `arguments`
- `status`
- `error_message`
- `latency_ms`

Task 9 前置最小可复用 recorder：共享 Executor 的所有工具调用写入 `ai_tool_calls`，metadata 标记为写操作的调用额外写入 audit 服务。Task 14 继续负责覆盖后续高风险工具和完整观测测试。

- [x] **Step 3: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/tools -run TestWriteTools -count=1
go test ./services/aiagent/internal/audit -run TestRecorder -count=1
```

Expected: 低风险写操作无需确认，但必须记录审计。

## 7. 高风险确认流程

### Task 10: 实现 Confirmation Manager

**Files:**

- Create: `services/aiagent/internal/confirmation/manager.go`
- Create: `services/aiagent/internal/confirmation/locker.go`
- Create: `services/aiagent/internal/domain/confirmation.go`
- Modify: `dal/model/ai/confirmations/aiconfirmationsmodel.go`
- Modify: `services/aiagent/internal/config/config.go`
- Modify: `services/aiagent/internal/svc/servicecontext.go`
- Test: `services/aiagent/internal/confirmation/manager_test.go`

- [x] **Step 1: 单测创建确认**

创建确认返回：

- `confirmation_id`
- `action`
- `summary`
- `expires_at`
- 参数摘要

- [x] **Step 2: 单测过期确认**

超过 `expires_at` 后确认，返回失败，状态更新为 `expired`。

- [x] **Step 3: 单测重复确认**

同一个确认 ID 第二次领取时返回失败。Redis 短锁合并同时请求，MySQL `pending -> approved` 条件更新保证最终只有一个 winner；Task 11 只允许 winner 调用业务 RPC。

- [x] **Step 4: 单测跨用户确认**

用户 A 不能执行用户 B 的确认 ID。

- [x] **Step 5: 实现混合幂等状态机**

- Redis 锁 key：`ai:confirmation:lock:<confirmation_id>`，默认 TTL 5 秒。
- 锁竞争返回 busy 且不访问 MySQL；Redis 错误降级到 MySQL CAS。
- MySQL 条件更新覆盖 pending、approved、rejected、expired、executed、failed 状态流转。
- Redis 锁在状态变更后释放，不跨 Task 11 的高风险业务 RPC 持有。

- [ ] **Step 6: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/confirmation -run Test -count=1
```

Expected: pending、approved、rejected、expired、executed、failed 状态流转正确。

### Task 11: 接入高风险 Eino Tool

**Files:**

- Create/Modify: `services/aiagent/internal/eino/approval.go`
- Create/Modify: `services/aiagent/internal/eino/checkpoint_store.go`
- Modify: `services/aiagent/internal/tools/cart_tools.go`
- Modify: `services/aiagent/internal/tools/order_tools.go`
- Modify: `services/aiagent/internal/logic/confirmactionlogic.go`
- Test: `services/aiagent/internal/logic/confirmactionlogic_test.go`

- [x] **Step 1: 首次请求创建确认并中断**

这些工具首次规划后由 ChatModelAgent middleware 创建 `ai_confirmations`，再调用官方 `tool.StatefulInterrupt` 返回 `confirmation_required`，不调用业务 RPC。响应必须包含 `confirmation_id`，并在服务端绑定 `checkpoint_id/interrupt_id`：

- `cart_delete`
- `order_create`
- `order_cancel`

- [x] **Step 2: 用户确认后通过 Eino Resume 与 Execution Guard 执行业务 RPC**

客户端只发送结构化 `confirm_action`，携带 `confirmation_id` 和 `approved`。`ConfirmAction` 通过 Confirmation Manager CAS 领取确认记录后，使用确认记录里的 `checkpoint_id/interrupt_id` 调用 `runner.ResumeWithParams`，approved resume 才执行原工具 endpoint，rejected resume 返回取消结果且不调用业务 RPC。

RPC 对应：

- `cart_delete` -> `Cart.DeleteCartItem`
- `order_create` -> `OrderService.CreateOrder`
- `order_cancel` -> `OrderService.CancelOrder`

- [x] **Step 3: 创建订单前置结算**

若用户表达购买意图且没有 `pre_order_id`：

1. 调用 `checkout_prepare` 创建预结算。
2. 返回结算金额与 `pre_order_id`。
3. 再创建 `order_create` 确认请求。

工具参数契约同步为真实 RPC 结构：

- `checkout_prepare` 必填 `order_items[]`，每项包含 `product_id`、`quantity`，`coupon_id` 可选。
- `order_create` 必填 `pre_order_id`、`address_id`、`payment_method`，`coupon_id` 可选。
- `payment_method` 使用 1（微信）或 2（支付宝）。

- [x] **Step 4: 使用优惠券下单必须确认**

当 `order_create` 参数包含 `coupon_id` 时，确认摘要必须展示优惠券 ID、应付金额和商品数量。

应付金额通过当前用户、预结算商品快照和该 `coupon_id` 调用 `coupon_calculate` 得到；优惠券不可用或计算失败时不得创建确认。业务 RPC 已执行但审计写入失败时返回明确失败事件并标记 `business_executed=true`，确认记录仍进入 `executed`，防止重复执行。

- [x] **Step 5: 运行测试**

Run:

```bash
go test ./services/aiagent/internal/logic -run TestConfirmAction -count=1
```

Expected: 未确认不执行、确认后执行、过期和重复确认被拒绝。

## 8. SSE API 接入

### Task 12: 新增 `apis/ai`

**Files:**

- Create: `apis/ai/ai.api`
- Generate/Create: `apis/ai/**`
- Modify: `apis/ai/internal/handler/routes.go`
- Modify: `apis/ai/internal/logic/chatlogic.go`
- Modify: `services/aiagent/internal/logic/chatlogic.go`
- Test: `services/aiagent/internal/logic/chatlogic_test.go`

- [x] **Step 1: 定义 API**

```go
syntax = "v1"

@server (
  middleware: WithClientMiddleware,WrapperAuthMiddleware
  prefix: /douyin/ai
)
service ai-api {
  @handler ChatHandler
  post /chat
}
```

- [x] **Step 2: 生成 API 代码**

Run:

```bash
goctl api go -api apis/ai/ai.api -dir apis/ai
```

Expected: 生成 handler、logic、svc、types 目录。

- [x] **Step 3: 实现 SSE 消息协议**

客户端输入类型：

- `user_message`
- `confirm_action`

服务端输出类型：

- `assistant_message`
- `tool_result`
- `confirmation_required`
- `error`

`ChatLogic` 负责会话准备、Eino Runner 或受控 Tool 分发、事件持久化；API 网关只负责鉴权、协议转换与 SSE flush。

- [x] **Step 4: 强制从登录态读取用户 ID**

禁止使用客户端消息体中的 `user_id`。缺少登录态时关闭连接并返回未授权错误。

- [x] **Step 5: 运行 API 编译检查**

Run:

```bash
go test ./apis/ai/...
```

Expected: API 包全部可编译。

### Task 13: SSE 集成测试

**Files:**

- Modify: `apis/ai/internal/logic/chatlogic_test.go`

- [ ] **Step 1: 测试未登录拒绝**

无认证上下文请求 `/douyin/ai/chat`，期望返回 401。

- [ ] **Step 2: 测试普通聊天**

发送：

```json
{"type":"user_message","conversation_id":"conv_001","client_message_id":"client_msg_0190f1f0e8a57000","content":"你好","metadata":{"source":"web"}}
```

期望收到 `assistant_message`，`done=true`。

同一用户重复提交相同 `client_message_id` 时，不再执行模型、工具或写操作，只重放同一会话、同一 `client_message_id` 下已保存的 assistant 消息。

- [ ] **Step 3: 测试高风险确认**

发送取消订单消息后，期望收到 `confirmation_required`，且没有直接调用 `order_cancel`。

- [ ] **Step 4: 运行测试**

Run:

```bash
go test ./apis/ai/internal/logic -run TestChatSSE -count=1
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
- Modify: `apis/ai/internal/logic/chatlogic.go`
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

- [ ] **Step 1: SSE 连续对话**

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

## 11. 上下文工程优化

上下文优化的目标设计见 `docs/ai-agent-context-optimization.md`。原始消息继续由 Conversation Manager 完整保存；Planner 和 Agent 的模型输入统一由轻量 Context Manager 临时组装。首期不引入向量数据库，不持久化模型输入，不做运行时 token 上限裁剪，也不在这些任务中重构 ReAct 循环。

**实现状态（2026-07-24）：** 上下文方案已从重型治理收敛为轻量 Context Manager。Task 18 已正式接入：Chat 分别构建 IntentContext 和 AgentContext，Planner/Runner 只消费领域 ContextMessages，token 估算不参与裁剪或阻断。后续继续补齐 Task 19-21 的滚动摘要、长期记忆、TaskState 持久化、观测和旧路径清理。

### Task 17: 工具结果引用和按需读取

**Files:**

- Create/Modify: `services/aiagent/internal/contextmanager/tool_results.go`
- Modify: `services/aiagent/internal/tools/result_projector.go`
- Modify: `services/aiagent/internal/logic/chatlogic.go`
- Test: `services/aiagent/internal/contextmanager/tool_results_test.go`
- Test: `services/aiagent/internal/tools/result_projector_test.go`

- [ ] **Step 1: 先写 ToolCallRef 与按需读取测试**

覆盖：

- 成功工具结果完整保存到 `ai_messages.metadata.tool_result`。
- ToolCallRef 包含 `tool_call_id`、`tool_name`、`status`、`summary`、`created_at` 和关键 entity ID。
- 最近一次成功工具结果可完整进入 AgentContext。
- 更早工具调用只以 ToolCallRef 进入上下文。
- 按 `tool_call_id` 读取完整结果必须同时校验 user ID 和 conversation ID。
- 失败、非法 JSON、未知工具名、跨用户或跨会话结果不得返回给模型。

- [ ] **Step 2: 实现 ToolResultStore**

ToolResultStore 从 `ai_messages.metadata.tool_result` 读取完整 envelope；旧记录兼容读取 `metadata.data_json`，但旧数据只能用于生成有限 ToolCallRef，不能伪造成完整成功 envelope。

- [ ] **Step 3: 实现 ToolCallRef projector**

按工具注册 allowlist projector，只输出后续推理需要的关键 ID、状态、摘要和时间字段。projector 只删除无关字段，不截断 ID、订单号、数量和状态。

- [ ] **Step 4: 运行测试**

```bash
go test ./services/aiagent/internal/contextmanager -run 'TestToolResult|TestToolCallRef' -count=1
go test ./services/aiagent/internal/tools -run TestResultProjector -count=1
```

Expected: ToolResult envelope 可恢复，ToolCallRef 不丢关键 ID，按需读取强制用户和会话隔离。

### Task 18: 轻量 Context Manager 正式接入

**Files:**

- Create/Modify: `services/aiagent/internal/contextmanager/manager.go`
- Create/Modify: `services/aiagent/internal/domain/context.go`
- Modify: `services/aiagent/internal/logic/chatlogic.go`
- Modify: `services/aiagent/internal/planner/planner.go`
- Modify: `services/aiagent/internal/eino/agent.go`
- Test: `services/aiagent/internal/contextmanager/manager_test.go`
- Test: `services/aiagent/internal/planner/planner_test.go`
- Test: `services/aiagent/internal/eino/agent_test.go`
- Test: `services/aiagent/internal/logic/chatlogic_test.go`

- [x] **Step 1: 先写 Context 组装测试**

覆盖：

- IntentContext 只包含当前输入、最近对话、当前 TaskState 和长期记忆摘要。
- AgentContext 包含摘要、最近 20 条未压缩消息、最近一次完整工具结果、历史 ToolCallRef、TaskState、UserMemory 和可选 UserProfile JSON。
- Context Manager 只返回临时 `[]domain.ContextMessage` 和轻量 build metadata，不落库模型输入。
- token 估算只记录在 build metadata 或日志中，不参与裁剪，不返回错误。

- [x] **Step 2: 定义轻量 Context 类型**

使用 `ContextMode`、`BuildContextRequest`、`ContextMessage` 和 `BuildContextResult`。`BuildContextResult` 至少包含 messages、summary covered watermark、recent message range、latest tool call ID、tool ref count 和 estimated input tokens。

- [x] **Step 3: Planner 接入 IntentContext**

Planner 只接收 Context Manager 已组装的 IntentContext，不再自行做固定 8 条或单条 300 字符裁剪。缺少历史工具完整结果时，Planner 返回澄清问题或读取工具结果的计划，不猜测参数。

- [x] **Step 4: AgentRunner 接入 AgentContext**

Runner 接收 AgentContext 的 `[]domain.ContextMessage`，由 `internal/eino/messages.go` 转换为 Eino `schema.Message`，不再直接把 `[]*AiMessages` 转换为模型输入。

- [x] **Step 5: 运行测试**

```bash
go test ./services/aiagent/internal/contextmanager -run TestManager -count=1
go test ./services/aiagent/internal/planner -run Test -count=1
go test ./services/aiagent/internal/eino -run Test -count=1
go test ./services/aiagent/internal/logic -run TestChat -count=1
```

Expected: Planner 和 Runner 均消费轻量 Context Manager 输出；Context 组装不会因 token 估算触发裁剪或错误。

### Task 19: 滚动摘要与长期记忆

**Files:**

- Create/Modify: `dal/model/ai/conversation_summaries/ai_conversation_summaries.sql`
- Create/Modify: `dal/model/ai/conversation_summaries/**`
- Modify: `dal/model/ai/user_memories/ai_user_memories.sql`
- Regenerate: `dal/model/ai/user_memories/**`
- Create/Modify: `dal/model/ai/user_profiles/ai_user_profiles.sql`
- Create/Modify: `dal/model/ai/user_profiles/**`
- Modify: `construct/depend/sql/init.sql`
- Modify: `construct/depend/sql/migrations/20260722_ai_context_engineering.sql`
- Create/Modify: `services/aiagent/internal/contextmanager/summary.go`
- Create/Modify: `services/aiagent/internal/contextmanager/memory_policy.go`
- Create/Modify: `services/aiagent/internal/contextmanager/user_profile.go`
- Create/Modify: `services/aiagent/internal/profileextractor/**`
- Modify: `services/aiagent/internal/logic/chatlogic.go`
- Modify: `services/aiagent/etc/aiagent.yaml`
- Modify: `services/aiagent/etc/aiagent.prod.yaml`
- Test: `services/aiagent/internal/contextmanager/summary_test.go`
- Test: `services/aiagent/internal/contextmanager/memory_policy_test.go`

- [x] **Step 1: 先写 30 -> 10 + 20 摘要测试**

覆盖：

- 未压缩消息少于 30 条时不生成新摘要。
- 未压缩消息达到 30 条时，将最早 10 条与旧摘要合并成新摘要。
- 摘要成功后水位推进，剩余 20 条仍作为近期原文。
- 同一条消息不会同时出现在摘要和近期原文。
- 摘要失败或非法 JSON 时保留上一版摘要，近期原文继续保留。
- 显式记忆可以保存、更新、删除和过期。
- 推断记忆只有 `confidence >= 0.85`、存在来源消息、非敏感且有 TTL 时才写入。

- [x] **Step 2: 新增或保留摘要表、扩展记忆表并新增画像表**

按设计文档保留 `ai_conversation_summaries`，扩展 `ai_user_memories` 的 memory key、source、source message、status、expires 和 last confirmed 字段，并新增 `ai_user_profiles` 保存聊天来源 UserProfile JSON。生成 go-zero model；生成文件不手改。

```bash
goctl model mysql ddl -src dal/model/ai/conversation_summaries/ai_conversation_summaries.sql -dir dal/model/ai/conversation_summaries -c
goctl model mysql ddl -src dal/model/ai/user_memories/ai_user_memories.sql -dir dal/model/ai/user_memories -c
goctl model mysql ddl -src dal/model/ai/user_profiles/ai_user_profiles.sql -dir dal/model/ai/user_profiles -c
```

- [x] **Step 3: 实现滚动摘要服务**

摘要模型无工具权限，输入上一版摘要和本次要压缩的 10 条消息，输出 `summary/key_facts/open_tasks` 严格 JSON。保存前校验 JSON、字段长度和引用实体 ID。

- [x] **Step 4: 实现显式和受控推断 MemoryPolicy**

模型只能产生候选，MemoryPolicy 负责脱敏、置信度、来源、TTL、冲突和 upsert。禁止保存认证信息、支付凭据、完整地址、瞬时库存和单次订单状态。

- [x] **Step 5: 接入聊天来源 UserProfile JSON**

UserProfile 不再来自 users RPC。每轮聊天消息持久化成功后投递 Kafka 画像更新事件，异步 consumer 调用 LLM Profile Extractor 判断是否需要更新画像。画像以 JSON 保存，便于后续注入给 LLM。

更新时机：

1. 用户明确表达长期偏好。
2. 多次行为表现出稳定模式。
3. 用户主动纠正画像。
4. 用户主动要求删除、遗忘或不再使用某类偏好。

LLM 只能输出严格 JSON patch 或候选更新，不能直接写数据库；后端策略负责校验来源、置信度、敏感信息、删除请求和用户隔离。

- [x] **Step 6: 运行测试**

```bash
go test ./dal/model/ai/...
go test ./services/aiagent/internal/contextmanager -run 'TestSummary|TestMemory|TestUserProfile' -count=1
go test ./services/aiagent/internal/profileextractor -count=1
```

Expected: 摘要窗口、消息去重、记忆生命周期、聊天来源画像 JSON、Kafka 异步触发、用户隔离、失败降级和提示注入防护全部通过。

**实现状态（2026-07-25）：** Task 19 已接入滚动摘要、长期记忆和聊天来源 UserProfile JSON。新增 `ai_conversation_summaries` 与 `ai_user_profiles` 表和 model，扩展 `ai_user_memories` 的 key/source/status/TTL 字段；SummaryManager 按 30 -> 10 + 20 推进摘要水位，摘要失败保留旧摘要和未压缩原文；MemoryPolicy 支持显式记忆保存/更新/删除/过期，并约束推断记忆必须高置信、有来源、非敏感且有 TTL；Context Manager 已使用摘要、active memories 和 DB-backed UserProfile JSON。Chat 消息持久化后触发摘要刷新，并投递 Kafka `AiUserProfileUpdates` 事件；异步 Profile Extractor 通过无工具权限 LLM 生成候选 patch，经后端策略校验证据、置信度、敏感信息、删除请求和用户隔离后保存画像，失败不阻塞聊天。

### Task 20: Agent Run、TaskState 与 Checkpoint

**Files:**

- Create/Modify: `dal/model/ai/agent_runs/ai_agent_runs.sql`
- Create/Modify: `dal/model/ai/agent_runs/**`
- Modify: `construct/depend/sql/init.sql`
- Modify: `construct/depend/sql/migrations/20260722_ai_context_engineering.sql`
- Create/Modify: `services/aiagent/internal/contextmanager/task_state.go`
- Create/Modify: `services/aiagent/internal/eino/checkpoint_store.go`
- Modify: `services/aiagent/internal/tools/high_risk_tools.go`
- Modify: `services/aiagent/internal/logic/confirmactionlogic.go`
- Test: `services/aiagent/internal/contextmanager/task_state_test.go`
- Test: `services/aiagent/internal/logic/confirmactionlogic_test.go`

- [x] **Step 1: 先写状态和恢复测试**

覆盖 confirmation resume target 绑定、Redis 丢失后从 MySQL checkpoint blob 恢复、批准 resume 执行业务 RPC、拒绝 resume 不进入完成标记。跨用户、跨会话、过期和重复确认继续由 Confirmation Manager 的 CAS 状态机拒绝。

- [x] **Step 2: 新增 AgentRun model**

按设计文档建立 `ai_agent_runs`，保存 `run_id`、`conversation_id`、`user_id`、`status`、`checkpoint_id`、`checkpoint_blob`、`task_state`、`idempotency_key`、`expires_at`。

- [x] **Step 3: 接入高风险确认状态**

创建 confirmation 时保存 tool name、脱敏参数、`run_id/checkpoint_id`；Eino 返回 root-cause interrupt 后回写 `interrupt_id`。ConfirmAction 恢复时重新校验用户、会话、确认状态、过期时间，并用 `confirmation_id -> checkpoint_id/interrupt_id` 恢复。

- [x] **Step 4: 实现 Redis 热 checkpoint**

Redis 热缓存 checkpoint blob，MySQL `ai_agent_runs.checkpoint_blob` 作为持久回退；Redis 故障或丢失时恢复语义不变。

- [ ] **Step 5: 运行测试**

```bash
go test ./services/aiagent/internal/contextmanager -run 'TestTaskState|TestCheckpoint' -count=1
go test ./services/aiagent/internal/logic -run TestConfirmAction -count=1
```

Expected: 等待确认可以跨实例恢复，重复或越权恢复不能执行业务 RPC。

### Task 21: 观测、清理和旧路径收敛

**Files:**

- Modify: `services/aiagent/internal/contextmanager/metrics.go`
- Modify: `services/aiagent/internal/logic/chatlogic.go`
- Modify: `services/aiagent/internal/eino/agent.go`
- Modify: `docs/ai-agent-context-optimization.md`
- Test: `services/aiagent/internal/contextmanager/manager_test.go`
- Test: `services/aiagent/internal/logic/chatlogic_test.go`

- [ ] **Step 1: 增加轻量观测**

记录 context 构建耗时、估算 token、摘要命中、ToolCallRef 数量、按需读取工具结果次数、降级原因、记忆候选决策和 checkpoint 恢复结果。估算 token 只进入日志和指标，不参与裁剪。

- [ ] **Step 2: 删除重型上下文设计**

删除独立上下文快照持久化、相关表/model/recorder、复杂预算打包和超限阻断。保留原始消息、ToolResult envelope、摘要、记忆、TaskState 和结构化日志。

- [ ] **Step 3: 收敛旧逻辑**

删除 Planner 和 Runner 的旧直接历史构建逻辑。Planner 和 AgentRunner 只消费轻量 Context Manager 输出；Conversation Manager 保留会话归属和原始消息持久化职责。

- [ ] **Step 4: 运行验收**

```bash
go test ./services/aiagent/...
go test ./apis/ai/...
go test ./...
```

Expected: 上下文工程测试、原 AI 客服安全测试和全仓测试全部通过。

## 12. 推荐实施顺序

1. 数据库与 model：Task 1。
2. Agent RPC 骨架与配置：Task 2。
3. Eino ChatModel/Agent、会话、Planner、Registry：Task 3-6。
4. Execution Guard 与查询工具：Task 7-8。
5. 低风险写操作与审计：Task 9、Task 14。
6. 确认流程与高风险工具：Task 10-11。
7. SSE API：Task 12-13。
8. 限流、超时、端到端验收：Task 15-16。
9. 工具结果引用和按需读取：Task 17。
10. 轻量 Context Manager 正式接入：Task 18。
11. 滚动摘要和长期记忆：Task 19。
12. Agent Run 与 Checkpoint：Task 20。
13. 观测、清理和旧路径收敛：Task 21。

每完成一个阶段执行：

```bash
go test ./services/aiagent/... ./apis/ai/...
```

最终全量验证：

```bash
go test ./...
```

## 13. 风险与处理策略

- 模型结果不稳定：首期保留规则 Planner 兜底，明确意图不依赖模型。
- SSE 流式与 RPC 必须保持一致：`Chat` 和 `ConfirmAction` 使用同名 server-streaming RPC，API 网关收到事件后立即 flush。
- 用户越权：所有工具执行前统一覆盖 `user_id`，工具层不信任模型和客户端参数。
- Eino 抽象泄漏到业务层：只允许 `internal/eino` 和 `internal/tools` 直接依赖 Eino，业务 RPC 转换和确认审计保持本地接口稳定。
- 确认重复执行：确认状态更新与执行需要在事务或 Redis 锁保护下完成。
- 业务服务返回字段不完整：工具结果转换层只暴露 PRD 要求字段，缺失字段返回空值并记录日志。
- 模型不可用：返回“AI 服务暂时不可用，请稍后重试”，查询/写操作不自动编造结果。
- 上下文过长：不做运行时裁剪；通过 30 -> 10 + 20 滚动摘要、近期 20 条原文和历史工具引用从源头节省 token，并记录估算 token 便于排查。
- 工具结果过大：只把最近一次完整工具结果放入上下文，历史工具调用保留 ToolCallRef；需要完整结果时按 `tool_call_id` 重新读取。
- 摘要、记忆或画像错误：原始消息始终保留；摘要、记忆和 UserProfile JSON 失败降级到近期消息，不阻塞基础聊天。
- 记忆污染和提示注入：模型只能提交候选，MemoryPolicy 负责来源、置信度、敏感信息、TTL 和冲突校验；记忆不能覆盖 system prompt。
- checkpoint 丢失：Redis 只做热缓存，MySQL AgentRun、confirmation 和 tool call 记录可重建执行状态。

## 14. 完成标准

- `apis/ai` 可通过 SSE 完成连续对话。
- `services/aiagent` 可基于 Eino 编排模型、工具调用、确认和审计。
- 查询、推荐、低风险操作、高风险确认执行均满足 PRD。
- 过期或重复确认不能执行。
- 所有写操作有审计记录。
- `go test ./services/aiagent/... ./apis/ai/...` 通过。
- 风控场景在 `test/ai-customer-service-e2e.md` 中有明确验收记录。
- Planner 和 Agent 的模型输入统一由轻量 Context Manager 临时组装。
- token 估算只进入日志和指标，不参与裁剪、不阻塞模型调用。
- 结构化工具结果不因上下文裁剪损坏，动态事实写操作前重新校验。
- 超出近期 20 条原文窗口的关键事实可通过会话摘要恢复。
- 历史工具调用以 ToolCallRef 保留，完整结果只能通过 user ID + conversation ID + tool_call_id 按需读取。
- 长期记忆具备来源、置信度、冲突、过期、删除和用户隔离。
- UserProfile 只来源于 AI 聊天过程，异步抽取为 JSON，并支持明确偏好、稳定模式、主动纠正和删除/遗忘偏好四类更新。
- waiting_confirmation 可以跨实例恢复，重复恢复不会重复执行写 RPC。
- 每次模型调用可以通过结构化日志追踪摘要覆盖水位、近期消息范围、最近工具结果和历史工具引用数量。
