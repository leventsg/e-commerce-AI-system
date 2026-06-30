# AGENTS.md

本文档定义参与本仓库开发的 agent 工作规则。

## 适用范围

这些规则适用于整个仓库。当任务涉及以下内容时，必须遵守 **AI 智能客服** 相关规则：

- `docs/ai-customer-service-prd.md`
- `docs/ai-customer-service-design.md`
- `docs/ai-customer-service-implementation-plan.md`
- `apis/ai/**`
- `services/aiagent/**`
- `dal/model/ai/**`
- 现有 product、inventory、order、checkout、carts、coupons、users、audit、auth 模块中与 AI 智能客服相关的集成代码

对于无关改动，遵循现有代码风格，并保持修改范围尽量小。

## 项目基线

- 语言：Go。
- 框架：go-zero API/RPC。
- AI 编排：Eino。
- 存储：MySQL model 位于 `dal/model/**`；Redis 按现有服务模式使用。
- 服务分层：API 网关代码放在 `apis/**`，RPC 服务代码放在 `services/**`，共享工具放在 `common/**`。
- 不要替换现有 product、inventory、order、coupon、cart、checkout、payment、users、auth、audit 服务。AI 智能客服只是基于现有 RPC 的编排层。

## 必读资料

实现 AI 智能客服相关任务前，按顺序阅读：

1. `docs/ai-customer-service-prd.md`
2. `docs/ai-customer-service-design.md`
3. `docs/ai-customer-service-implementation-plan.md`

如果代码行为与文档冲突，先检查代码，再更新文档或实施计划，然后再进行大规模实现。不要让实现悄悄偏离已记录的架构。

## AI 智能客服架构规则

- `apis/ai` 是对外 WebSocket 网关。
- `services/aiagent` 是 AI 编排 RPC 服务。
- `services/aiagent` 必须使用 Eino 实现 ChatModel、Tool、ToolsNode、Agent/Chain/Graph 风格的编排。
- Eino 相关代码只允许隔离在：
  - `services/aiagent/internal/eino/**`
  - `services/aiagent/internal/tools/**`，用于定义 Eino Tool handler
- 不要把 Eino 类型泄漏到现有业务服务中。
- 现有业务 RPC 仍然是 product、inventory、order、checkout、cart、coupon、user、audit 行为的事实来源。

## 安全规则

- 登录态中的用户 ID 是唯一可信来源。
- 不要信任来自客户端、模型输出、Eino tool 参数或会话 metadata 的 `user_id`。
- 每次工具执行前，必须从认证上下文注入或覆盖 `user_id`，然后才能调用业务 RPC。
- AI 智能客服只能访问当前用户自己的数据。
- 高风险操作必须在有效确认被批准后才能执行。
- 已过期、已拒绝、已执行、执行失败或跨用户的确认请求不得执行。
- 同一个 confirmation ID 不得重复执行。
- 工具调用失败时，不得总结为成功。
- 所有写操作都必须产出审计记录。

## 确认策略

无需确认：

- 闲聊。
- 查询。
- 商品推荐。
- 添加商品到购物车。
- 减少购物车商品数量。
- 领取优惠券。

必须确认：

- 删除购物车商品。
- 创建订单。
- 取消订单。
- 下单时使用优惠券。
- 后续支付、退款、地址修改或类似敏感操作。

确认响应必须包含：

- `confirmation_id`
- action/tool 名称
- 用户可读的操作摘要
- 过期时间
- 待执行参数摘要

## Eino Tool 规则

- 每个 AI 可调用的业务动作都必须注册为 Eino Tool，并同步注册本地工具 metadata。
- 本地 metadata 必须包含工具名称、风险等级、是否需要确认、超时时间、读写分类和 RPC 映射。
- Tool schema 不得要求模型提供 `user_id`。
- Tool handler 在调用业务 RPC 前必须经过 Execution Guard。
- Execution Guard 负责参数校验、用户 ID 注入、超时选择、限流检查、确认拦截和审计钩子。
- 工具结果 payload 必须结构化且紧凑。返回足够生成用户侧摘要的数据，但不要暴露无关内部字段。

## 模型与 Planner 规则

- Eino ChatModel 是主要模型抽象。
- 保留一个小型规则兜底 Planner，用于明确中文意图，例如订单查询、购物车添加/减少、领取优惠券、取消订单、创建订单。
- 兜底 Planner 必须使用与 Eino tool calling 相同的 Tool Registry 和 Execution Guard。
- 模型不可用时必须降级为明确的稍后重试提示。不要编造业务结果。
- 发送给模型的上下文必须有边界。优先使用最近消息加摘要，而不是无限历史。

## WebSocket 协议规则

接口：

```text
GET /douyin/ai/chat/ws?conversation_id=optional
```

客户端输入类型：

- `user_message`
- `confirm_action`

服务端输出类型：

- `assistant_message`
- `tool_result`
- `confirmation_required`
- `error`

WebSocket 网关必须从现有鉴权中间件或请求上下文获取用户身份，不得从 WebSocket payload 中接受用户身份。

## 数据与审计规则

AI 智能客服数据放在 `dal/model/ai/**` 下。

必需数据表：

- `ai_conversations`
- `ai_messages`
- `ai_tool_calls`
- `ai_confirmations`
- `ai_user_memories`

每次工具调用都要记录：

- conversation ID
- user ID
- tool name
- 脱敏后的 arguments
- status
- result summary 或 error message
- latency

对于写操作，还必须通过 audit 服务或本仓库既有审计路径记录审计事件。

## 实施流程

- 按 `docs/ai-customer-service-implementation-plan.md` 逐任务推进。
- 新行为优先采用测试先行。
- 提交或变更批次要小到可以独立审查。
- API、RPC、model 代码使用现有 go-zero 生成模式。
- 不要手动修改生成文件，除非本项目对该类型文件已有手改惯例。
- 实现 AI 智能客服时，不要重构无关服务。
- 除非 AI 智能客服文档明确要求兼容性扩展，否则保留现有 API 和 RPC 行为。

## 测试要求

AI 智能客服改动至少应覆盖：

- Eino ChatModel factory 和模型不可用降级。
- Eino Tool schema 注册和本地 metadata。
- Execution Guard 用户 ID 注入。
- 工具超时行为。
- 查询工具参数转换。
- 低风险写操作审计。
- 确认请求创建、批准、拒绝、过期、重复执行拒绝和跨用户拒绝。
- WebSocket 鉴权拒绝。
- WebSocket 聊天消息流程。
- 高风险操作在 RPC 执行前返回 `confirmation_required`。

目标测试命令：

```bash
go test ./services/aiagent/...
go test ./apis/ai/...
```

在声称整个功能完成前，运行：

```bash
go test ./...
```

如果依赖下载或网络访问被阻塞，报告具体命令和失败原因，然后请求所需授权，不要静默跳过验证。

## 文档规则

当架构、工具策略、确认规则或实施顺序变化时，更新：

- `docs/ai-customer-service-design.md`
- `docs/ai-customer-service-implementation-plan.md`

当产品范围变化时，更新：

- `docs/ai-customer-service-prd.md`

不要让设计文档和实施文档互相不一致。

## 完成定义

AI 智能客服相关工作只有满足以下条件，才算完成：

- 相关测试通过。
- 满足上述安全规则。
- 高风险操作需要确认。
- 工具失败不会被报告为成功。
- 写操作可审计。
- 文档与实现行为一致。
