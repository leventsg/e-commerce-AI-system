# NexusAgent · AI 智能客服 Agent 知识库

> 本文档描述本仓库中 AI 智能客服 Agent（NexusAgent）的设计与实现，供 AI 阅读理解：它是什么、如何分层、一次请求如何流转、安全与记忆如何保障、关键代码在哪里。技术栈：Go / go-zero / Eino(adk) / RAG / MCP / Skill / SSE / MySQL / Redis / Kafka / Elasticsearch / Prometheus。

---

## 1. 项目定位与全景

NexusAgent 是一个**企业级多 Agent 电商业务执行平台**：面向商品、库存、订单、购物车、结算、优惠券、售后等电商域，构建**可调用真实业务 RPC 的 AI 客服执行层**，覆盖：

- **知识检索**（RAG：意图分类 → 向量召回 → 上下文注入）
- **跨域任务规划**（Supervisor 拆解意图、依赖排序、路由领域 Agent）
- **交易操作**（27 个 Native 业务工具 + 2 个能力工具 + MCP 扩展工具，真实调用下游 RPC）
- **风险确认**（高风险写操作 Human-in-the-loop 确认拦截）
- **审计留痕**（每次工具调用落 `ai_tool_calls`，写操作额外走 audit 服务）
- **长期记忆**（会话摘要 + 用户画像 + 长期事件上下文管理）
- **分布式排队限流**（Redis 信号量 + ZSET + Pub/Sub，SSE 实时推送排队信息）
- **自动化评测**（tau2-bench + 30 个多轮任务样本，Pass@1 > 92%，Pass@3 > 99%）

### 分层架构总览

```
┌─────────────────────────────────────────────────────────────────┐
│ 前端 / 客户端（Web）                                              │
│   POST /douyin/ai/chat（SSE 流式）│ GET /sessions │ GET /messages │
└──────────────────────────────┬──────────────────────────────────┘
                               │ HTTP(SSE) + 鉴权中间件
┌──────────────────────────────▼──────────────────────────────────┐
│ apis/ai  对外 API 网关（go-zero）                                 │
│  · ChatHandler / ListSessions / ListMessages                    │
└──────────────────────────────┬──────────────────────────────────┘
                               │ gRPC（流式 AgentEvent）
┌──────────────────────────────▼──────────────────────────────────┐
│ services/aiagent  AI 编排 RPC 服务                                │
│  ┌───────────────────────────────────────────────────────────┐  │
│  │ ChatLogic（主流程编排）                                     │  │
│   1 ConversationManager.Prepare（幂等/会话初始化/落用户消息）    │  │
│   2 RAGService.Prepare（检索分类→向量召回→组装知识上下文）       │  │
│   3 ContextManager/MemoryProvider（分层上下文构建）             │  │
│   4 AgentRunner.Stream（Eino ADK Supervisor 多 Agent 执行）      │  │
│   5 事件持久化 + SSE 下发                                       │  │
│   6 SummaryManager.MaybeRefresh（滚动摘要压缩）                  │  │
│   7 Kafka 发布记忆更新事件 → 消费者异步抽取长期事件/画像          │  │
│  └───────────────────────────────────────────────────────────┘  │
│  排队限流层（Redis 信号量+ZSET+Pub/Sub，SSE 推送排队位置）        │
└──────────────────────────────┬──────────────────────────────────┘
                               │ 真实业务 RPC（事实源）
┌──────────────────────────────▼──────────────────────────────────┐
│ services/{product,inventory,order,cart,coupons,checkout,users,   │
│           audit}（既有电商微服务，不被替换，AI 只是编排层）        │
└─────────────────────────────────────────────────────────────────┘
存储：MySQL（dal/model/ai/**）｜Redis（checkpoint 缓存/锁/向量缓存）｜
      Kafka（记忆更新事件）｜Elasticsearch/Prometheus（检索与监控）
```

---

## 2. 一次聊天请求的完整生命周期

入口：`apis/ai/internal/handler/chathandler.go` → `apis/ai/internal/logic/chatlogic.go` → `services/aiagent/internal/logic/chatlogic.go`。

1. **鉴权与排队**：请求经 `WithClientMiddleware` / `WrapperAuthMiddleware` 完成登录态校验，从中取得**可信 UserID**；高峰期请求先进入网关排队限流层（见 §9），SSE 实时推送排队位置、预计等待与拒绝原因，拿到执行令牌后才进入编排。
2. **会话准备（幂等）**：`ConversationManager.Prepare`：校验参数、初始化新会话；以 `client_message_id` 做幂等键落用户消息（`ai_messages`）；命中重复请求时直接**重放历史助手回复**，不会二次执行 Agent。
3. **RAG 检索**：`rag.Service.Prepare`（见 §8）：LLM 分类器判定是否需要检索 → 向量 Embed → 外部知识库检索（`RagentClient`，对接知识库平台）→ Redis 向量缓存命中则直接返回 → 组装 `<知识库参考>` 上下文块 + 来源引用。
4. **上下文构建**：`ContextManager` / `MemoryProvider`（见 §6）按层组装：近期消息（滑窗 12 条）+ Session Summary + 工具结果压缩 + **长期事件上下文** + 用户画像 + RAG 块。
5. **Agent 执行**：`AgentRunner.Stream`（`internal/eino/agent.go`）启动 Eino ADK Runner：以 `messageID` 派生稳定 `checkpointID/runID`；向 ctx 注入 `ToolExecutionContext`（可信 UserID、会话、token 等）；`adk.NewRunner(...).Run(...)` + `WithSessionValues` 携带会话元数据；`consumeEvents` 把 Eino 内部事件翻译成领域 `AgentEvent` 流。
6. **事件流下发与持久化**：`shouldPersistAgentEvent` 的事件（assistant 消息、工具结果、确认请求等）写入 `ai_messages`，同时经 gRPC stream → SSE 推给前端；`BusinessExecuted` 标记本次 run 是否产生真实业务副作用。
7. **滚动摘要触发**：`SummaryManager.MaybeRefresh`：未摘要消息 ≥ 30 条触发，每轮压缩 10 条、最多 3 轮，保留最近 20 条；摘要带**水位线**（covered_until_message_id / created_at）。
8. **异步记忆巩固**：压缩完成后通过 Kafka 发布 `UpdateEvent`（`memoryupdate.KafkaPublisher`，按 UserID 分区保序）；`memory_event_update` 消费者调用 `MemoryEventExtractor` 用轻量 LLM 抽取**长期事件**（milestone/event，置信度 ≥ 0.75、必须引用真实 evidence_message_ids、敏感信息正则过滤），写入 `ai_user_memory_events`；`profile_update` 消费者同理更新 `ai_user_profiles` 用户画像。
9. **下一次请求**：MemoryProvider 检索"最近事件 + 关键词召回事件"注入 `<user_memory_events>` 块，实现跨会话长期记忆。

### 高风险确认的续跑生命周期

入口：`ConfirmActionLogic.ConfirmAction`（`services/aiagent/internal/logic/confirmactionlogic.go`）。

1. 用户对 `confirmation_required` 事件点击确认/拒绝；
2. `ConfirmationManager.Decide`：Redis 锁（`ai:confirmation:lock:<id>`）+ MySQL 条件更新（`ResolvePending`，只有 `pending` 且未过期才能变为 `approved/rejected`）→ 天然防重复执行、防跨用户（查询强制 `user_id` 过滤）、防过期；
3. 拒绝：生成 `tool_result(rejected)` + 助手消息并持久化，结束；
4. 批准：`AgentRunner.ResumeStream` 用 checkpointID + interruptID `ResumeWithParams` 恢复被 `StatefulInterrupt` 挂起的工具调用，注入 `ApprovalResult{Approved}`；
5. 工具真实执行：成功则 `MarkExecuted`，业务未执行则 `MarkFailed`；业务已执行但后续失败时，事件流携带 `business_executed=true`，前端提示"业务已生效，请勿重复操作"。

---

## 3. 多 Agent 编排（Eino ADK）

核心代码：`services/aiagent/internal/eino/agent.go`，提示词：`internal/prompts/agent/*.txt`（go:embed）。

### 3.1 结构：Supervisor + 领域 Agent

| Agent | 职责 | 可用工具 |
| --- | --- | --- |
| `supervisor_agent` | 意图识别、任务拆解、Agent 路由、结果汇聚、最终中文回复；不直接处理业务事实 | 6 个 AgentTool（即下述子 Agent）+ 根级 capability 工具 |
| `product_agent` | 商品搜索、详情、推荐、库存 | product_search / product_detail / product_recommend / inventory_get |
| `order_agent` | 订单查询、取消 | order_get / order_list / order_cancel* |
| `cart_checkout_agent` | 购物车、结算、下单 | cart_list / cart_add / cart_sub / cart_delete* / checkout_prepare / checkout_detail / order_create* |
| `coupon_agent` | 优惠券全流程 | coupon_list / coupon_detail / coupon_claim / coupon_my_list / coupon_usage_list / coupon_calculate |
| `user_agent` | 资料、收货地址 | user_get / user_update / address_list / address_get / address_add / address_update / address_delete |
| `general_agent` | 普通解释、闲聊、兜底 | 无业务工具（不允许编造业务事实） |

（* = 高风险需确认）

实现要点：

- **领域 Agent 封装为 AgentTool**：每个子 Agent 用 `adk.NewChatModelAgent` 创建，再 `adk.NewAgentTool` 包装成工具暴露给 Supervisor；子 Agent 的 ToolInfo 由 Name/Description 生成，Supervisor 通过原生 tool calling 路由。
- **动态工具暴露**：`Registry.RootAgentTools / AllAgentTools / SubAgentTools` 按 `Kind + Visibility`（root / all / subAgents）控制每个 Agent 实际可见的工具集；子 Agent 只拿到本领域业务工具 + 共享 capability 工具，避免工具选择互相干扰。
- **工具描述走原生 tool calling 通道**：ADK 通过 `model.WithTools(ToolInfos)` 传给模型，不拼进 prompt 文本。
- **MaxIterations = 8**：防止 Agent 循环；`EmitInternalEvents=true` 透传子 Agent 内部事件用于流式输出。
- **中间件链**（`adk.ChatModelAgentMiddleware`）：`adk.NewEventSenderModelWrapper()`（事件透传）、`newToolCallContextMiddleware()`（注入 Eino `ToolContext.CallID`）、`NewMemoryMiddlewareForAgent(provider, agentName)`（每次模型调用前分层组装记忆上下文，root 与子 Agent 各自触发、同 Agent 去重）、`newHighRiskApprovalMiddleware(approvalManager)`（子 Agent 高风险工具执行前确认拦截，见 §7）。

### 3.2 模型接入与降级

`internal/eino/model_factory.go`：

- `ModelFactory.NewChatModel` / `NewStructuredChatModel` 统一构建模型；provider 归一化为 OpenAI 兼容协议（支持 `openai-compatible` / `deepseek`）。
- 主模型、摘要模型、画像/事件抽取模型、RAG 分类器模型可独立配置（`Eino / SummaryModel / ProfileModel`），轻量任务用小模型。
- 结构化输出用 `response_format=json_object`（摘要、画像、事件抽取、RAG 分类）。
- 模型不可用返回 `ErrModelUnavailable`，Agent 层输出"AI 服务暂时不可用，请稍后重试"；RAG 分类失败则跳过检索（降级为无知识上下文），**绝不编造业务结果**。

---

## 4. 工具体系（Tool Gateway）

目录：`internal/tools/`（core 定义、registry 注册、executor 执行、business 各域 handler、capability 能力工具）。

### 4.1 工具清单（Native + MCP）

**Native 业务工具（27 个）**，按域组织在 `internal/tools/business/*`，metadata 在 `registry.go:defaultSchemaTools`：

- 商品/库存：`product_search`、`product_detail`、`product_recommend`、`inventory_get`
- 订单：`order_get`、`order_list`、`order_create`*、`order_cancel`*（走 OrderAPI HTTP 内部通道）
- 购物车：`cart_list`、`cart_add`、`cart_sub`、`cart_delete`*
- 结算：`checkout_prepare`、`checkout_detail`
- 优惠券：`coupon_list`、`coupon_detail`、`coupon_claim`、`coupon_my_list`、`coupon_usage_list`、`coupon_calculate`
- 用户/地址：`user_get`、`user_update`、`address_list/get/add/update/delete`
- Capability 工具（所有 Agent 可见）：`search_user_memory`（检索长期记忆事件）、`get_tool_call_result`（查询近期工具调用结果）

**MCP 扩展工具**：通过 MCP 协议接入外部通用能力，当前接入**网络搜索、天气查询**等非电商域工具，与 Native 工具统一注册进 Tool Registry，对 Agent 呈现一致的 schema；由 Supervisor 视意图选择调用，用于"配送地天气""比价搜索"等开放问题。

**Skill（场景化策略）**：`internal/skills/` 下两个 SKILL.md，通过 Eino 官方 `adk/middlewares/skill` 中间件按 Agent 绑定、渐进式披露加载（见 §10）。

### 4.2 Tool Registry：双注册（schema + 本地 metadata）

每个 AI 可调用动作同时注册：

1. **Eino Tool schema**（`invokableToolAdapter.Info`）：name / desc / `ParamsOneOf`（JSON 参数 schema），**schema 不含 `user_id`**；
2. **本地 metadata**（`domain.Metadata`）：`Risk`（low/high）、`RequireConfirmation`、`TimeoutSeconds`（查询 3s/写 5s 可配）、`WriteOperation`、`RPCService/RPCMethod` 映射、`RetryPolicy`（查询类：有界重试 + 指数退避 + 总超时上限）。

高风险判定（四条件同时满足才进入确认流程）：`Risk==high && RequireConfirmation && WriteOperation && ConfirmationSummary!=nil`。当前高风险工具：`cart_delete`、`order_create`、`order_cancel`。

### 4.3 Executor：统一执行链（Execution Guard）

`internal/tools/executor.go`，所有工具调用必经：

```
模型 tool call
  └─ invokableToolAdapter.InvokableRun
       ├─ 1 校验 ToolExecutionContext（UserID 必须来自可信注入，否则拒绝）
       ├─ 2 JSON 参数解析失败 → Reject（落 failed 记录）
       └─ 3 Executor.Execute
            ├─ registry.Metadata(tool)            // 工具存在性
            ├─ argx.SanitizeMapKeys(args, 敏感键)  // 剥离 user_id/token/auth
            ├─ timeout = metadata.TimeoutSeconds   // runHandlerWithTimeout（goroutine + ctx 双保险）
            ├─ 有界重试（retry.go，按错误分类决定是否重试、是否可重试于本轮）
            ├─ handler(ctx, HandlerRequest{可信 UserID, 参数, metadata}) → 调真实业务 RPC
            ├─ 结果封装 toolResultEnvelope（结构化紧凑 JSON）
            └─ record()：ai_tool_calls 落库 + 写操作发 audit RPC
```

**toolResultEnvelope**（模型看到的就是它，紧凑且结构化）：

```json
{
  "status": "success|failed",
  "tool_name": "order_create",
  "attempt_count": 1,
  "retry_count": 0,
  "error": { "kind": "...", "code": "...", "message": "安全文案", "retryable_in_current_run": false },
  "business_outcome": "executed|not_executed",
  "result": {},
  "attempt_trail": [{ "attempt": 1, "status": "...", "rpc_started": true, "latency_ms": 123 }]
}
```

**错误分类与幂等原则**（`internal/eino/errors.go` + executor）：

- 错误分 kind（参数错误/超时/上游异常/未知执行状态等），区分"可重试"与"终态"；查询类失败可安全有界重试，重试链路记录 `attempt_trail`。
- **RPC 已启动后超时/未知状态**：不盲目重试写操作（避免重复下单），标记 `business_outcome=unknown` 并终止本轮，提示用户稍后核实。
- **工具失败绝不总结为成功**：`status=failed` 时模型收到的就是失败 envelope，且系统提示要求如实告知用户。

### 4.4 审计

`internal/audit/recorder.go`：每次工具调用写 `ai_tool_calls`（conversation/user/tool/脱敏 arguments/status/真实 result JSON 或 error/latency_ms）；**写操作**额外调用 audit 服务 `CreateAuditLog`（action_type、target_table、target_id、new_data、client_ip、service_name=aiagent），使用 `context.WithoutCancel` 保证请求取消后审计仍落库。

---

## 5. 数据模型（MySQL，dal/model/ai/**）

| 表 | 用途 |
| --- | --- |
| `ai_conversations` | 会话元数据 |
| `ai_messages` | 全量消息（user/assistant/tool 结果/确认事件），幂等键 client_message_id |
| `ai_tool_calls` | 每次工具调用：参数（脱敏）、状态、结果 JSON、错误、延迟 |
| `ai_confirmations` | 高风险确认状态机：pending→approved/rejected/expired→executed/failed；绑定 run_id/checkpoint_id/interrupt_id；TTL |
| `ai_agent_runs` | run/checkpoint 持久化：checkpoint_blob、task_state（历史遗留字段）、idempotency_key、expires_at |
| `ai_conversation_summaries` | 滚动会话摘要 + 水位线（covered_until） |
| `ai_user_profiles` | 用户画像（版本化，敏感字段过滤） |
| `ai_user_memory_events` | **长期事件上下文**：user_id、type(milestone/event)、event_date、summary、keywords、status=active |

Redis：`ai:agent:checkpoint:<id>`（checkpoint 缓存，TTL 与确认一致）、`ai:confirmation:lock:<id>`（决策互斥锁）、RAG 向量缓存、排队限流结构（信号量/ZSET/Pub/Sub，见 §9）。

---

## 6. 上下文与长期记忆

> 设计口径（当前版本）：`MemoryProvider + ContextBuilder`，跨轮任务连续性由**长期事件上下文管理**承担；旧 `TaskState`（goal/pending_confirmation 等任务状态快照）仅作为 `ai_agent_runs` 表的兼容字段保留，不再参与在线上下文组装。

### 6.1 分层注入（每次模型调用前）

`MemoryMiddleware.BeforeModelRewriteState` 在 root 与子 Agent 的每次模型调用前触发，调用 `CustomerServiceProvider.Retrieve` 组装：

| 层 | 内容 | 边界 |
| --- | --- | --- |
| 近期消息 | 最近 12 条 user/assistant 消息（滑动窗口） | 过滤被摘要覆盖的消息（水位线去重）、脱敏（user_id/token/auth → [redacted]） |
| Session Summary | `<conversation_context>`：summary + key_facts + open_tasks | 摘要覆盖之前的消息不再全量进入 |
| 工具上下文 | 最近工具结果压缩块（`<tool_context>`） | 只保留最近 1 条完整结果 + 20 条调用引用，避免工具大 JSON 撑爆上下文 |
| **长期事件上下文** | `<user_memory_events>`：最近 5 条 + 关键词召回的相关事件（milestone/event 时间线） | 召回 query 取最近 2 条用户消息，关键词匹配 summary/keywords，去重合并 |
| 用户画像 | `<user_profile>`（LLM 抽取的偏好/事实，敏感字段过滤） | 版本化，可降级（失败仅记 metadata） |
| RAG | `<知识库参考>` + 来源引用 | 分类器判定需要才注入 |
| 当前时间 | `<current_time>` | 每次注入 |

组装结果：system 消息 → 摘要等系统块 → 历史 →（最近一条 user 消息后拼接运行时上下文块）。

### 6.2 滑动窗口与游标增量巩固

- **滑动窗口**：在线只带最近 12 条消息 + 摘要；摘要未覆盖且未入窗的消息由离线链路消化。
- **游标（水位线）**：`ai_conversation_summaries.covered_until_message_id/created_at` 是增量摘要的游标；`SummaryManager.MaybeRefresh` 只处理游标之后的消息，每轮压缩 10 条、单次最多 3 轮、保留最近 20 条，摘要 ≤ 4000 字符、open_tasks ≤ 12。
- **增量事件抽取**：压缩完成后发布 Kafka `UpdateEvent`（含被压缩的 MessageIDs），消费者按 UserID 分区保序处理；`MemoryEventExtractor` 只允许引用本次事件中的真实消息 ID 作为证据（防幻觉）、置信度 ≥ 0.75、日期必须可解析、敏感信息（密码/验证码/完整地址/token 等）正则拒绝，通过后写 `ai_user_memory_events`。画像更新走独立的 `profile_update` 消费者，同样有敏感字段过滤。
- 效果：跨轮任务连续性保持的同时，平均上下文 Token 降低约 31%。

---

## 7. 安全执行链路（重点）

### 7.1 可信身份与权限

- 登录态中的 UserID 是**唯一可信来源**：`RunRequest.UserID` 来自网关鉴权中间件；`agent.Stream/ResumeStream` 把它写入 `helper.ToolExecutionContext`，`adk.WithSessionValues` 只携带可信会话元数据。
- 任何工具 schema 都**不要求模型提供 `user_id`**；`argx.SanitizeMapKeys` 在执行前剥离参数中的 `user_id/token/auth` 等敏感键。
- 每个业务工具 handler 的 RPC 调用都以注入的 UserID 为准，AI 只能访问当前用户自己的数据。

### 7.2 高风险确认（Human-in-the-loop）

拦截点：`newHighRiskApprovalMiddleware.WrapInvokableToolCall`（子 Agent 工具中间件），流程：

```
模型请求高风险工具
  └─ ApprovalManager.RequestConfirmation
       ├─ registry 校验该工具确实属于"高风险+需确认+写操作"
       ├─ ConfirmationSummary(ctx, args) 生成中文确认摘要
       │    （如"删除购物车中商品 X""创建订单，应付 ¥Y"）
       ├─ confirmation.Manager.Create → ai_confirmations(pending, TTL)
       └─ 返回 einotool.StatefulInterrupt(ctx, ApprovalInfo, 原始参数)
            ├─ ADK 自动写 Checkpoint（Redis + MySQL ai_agent_runs）
            └─ 事件流下发 confirmation_required（含 confirmation_id/summary/expires_at）
```

恢复点：`ConfirmActionLogic` + `agent.ResumeStream`：

- `Decide` 用 Redis 锁 + MySQL 条件更新（仅 pending→approved/rejected）实现**状态机幂等**：已处理/已过期/已拒绝/跨用户（`loadOwned` 强制 user_id 匹配）一律拒绝；
- 同一 confirmation 只能被成功执行一次（executed 为终态）；
- 批准后 `ResumeWithParams` 从 checkpoint 恢复，`GetResumeContext[ApprovalResult]` 命中才放行 `endpoint(storedArgs)`——**用中断时保存的原始参数**执行，防止确认与执行间参数被替换；
- 拒绝/无恢复点/执行失败分别走 rejected 事件、MarkFailed、错误事件；业务已执行但摘要失败时事件带 `business_executed=true` 提示用户勿重复操作。

### 7.3 不变量清单

1. 不信任客户端/模型输出/工具参数/会话元数据中的 `user_id`；
2. 每次工具执行前从认证上下文注入/覆盖 UserID；
3. 高风险操作必须确认，`confirmation_required` 在 RPC 真正执行**前**返回；
4. 已过期/拒绝/执行/失败/跨用户的确认不得执行，同一 confirmation 不得重复执行；
5. 工具失败不得总结为成功；
6. 所有写操作必须审计；
7. 现有业务 RPC 是事实源，AI 层不复制业务逻辑。

---

## 8. RAG 知识检索

目录：`internal/rag/`（service / classifier / client / cache）。流程：

1. **检索前分类**：`ragClassifier`（轻量 LLM，JSON 输出 need_rag + confidence），输入 = 会话摘要 + 最近 6 条对话 + 当前问题；置信度低于阈值不检索；
2. **Embed**：调用配置的 embedding 模型生成查询向量；
3. **缓存**：Redis 向量缓存（相似向量命中直接返回来源集合）；
4. **检索**：`RagentClient.Retrieve` 调用外部知识库平台（KnowledgeFlow，OpenAPI + API Key），返回 chunks；
5. **组装**：按文档聚合为 `AgentSource`（document_id/title/preview_url + chunk 片段与 score），生成 `<知识库参考>` 系统块注入，同时把 sources 挂到 assistant 消息供前端展示引用；
6. **降级**：分类/Embed/检索任一失败 → 跳过 RAG 继续对话，LLM 不成为链路单点。

---

## 9. 分布式排队限流（网关层）

目标：高峰期避免用户请求直接击穿模型与业务工具执行层；排队状态对用户透明。

- **Redis 信号量**：全局并发上限（模型/工具执行层配额），请求先抢令牌，抢不到进入排队；
- **ZSET 公平排队**：`ZADD` 以入队时间为 score，支持优先级加权；出队按入队时间 + 优先级公平调度；
- **Lua 脚本原子性**：入队、抢占令牌、续期（心跳）、释放、超时拒绝全部封装为 Lua 脚本在 Redis 侧原子执行，避免多命令竞态与令牌泄漏；
- **Pub/Sub 排队推送**：队列状态变化（有人释放/超时清理）通过 Pub/Sub 广播，长连接订阅者收到后重新计算自己的排队位置；
- **SSE 实时推送**：排队期间通过 SSE 下发 `queue_position`、`estimated_wait`、`reject_reason`（超时拒绝/配额不足），用户可在界面上看到"前面还有 N 人，预计等待 X 秒"；被拒绝的请求不会进入 Agent 执行；
- 与会话幂等（client_message_id）配合：排队期间用户重试/重连不会重复入队执行。

---

## 10. Skill 体系（场景化策略）

目录：`internal/skills/`（纯数据解析）+ `internal/eino/skills.go`（Eino 适配层）。采用 Eino 官方 Skill 中间件（`adk/middlewares/skill`）的**渐进式披露**模式：Agent 平时只在 `skill` 工具描述里看到各 Skill 的 name/description；模型判断请求相关时调用 `skill` 工具，按需加载完整策略正文（默认 inline 模式），避免策略全文常驻 prompt。绑定关系：`issue-triage` → supervisor_agent；`purchase-advisor` → product_agent。Skill 是面向特定场景的策略资产（适用边界、证据选择规则、可用只读工具、输出约束）：

| Skill | 场景 | 核心策略 |
| --- | --- | --- |
| `issue-triage`（电商问题诊断） | 用户描述含糊的异常：优惠券无法使用、下单无响应、页面与业务记录不一致、不确定操作是否执行 | 把描述转成少量可验证的问题；只选能区分原因的**只读**证据；内部最多保留 3 个待验证假设；区分"页面反馈/业务执行/后续记录"异常；输出"已确认事实/未知事项/下一步"；优先用 order_id、pre_order_id 等真实关联字段，多候选无法区分时让用户选择 |
| `purchase-advisor`（购物决策建议） | 预算/偏好类选购咨询 | 区分硬约束与软偏好；每轮最多问 1-2 个最高区分度问题；只用 product_search/product_recommend/product_detail/inventory_get 核实候选（真实 ID/名称/价格/库存）；预算单位换算（分↔元）；推荐理由必须对应商品事实，标注库存未确认等未知项；不虚构分类/sku/质量结论 |

Skill 是策略层：工具范围、权限控制、确认规则始终以 Tool Gateway 为准，Skill 不能扩大权限。

---

## 11. 事件流协议（SSE）

`domain.AgentEvent`（`internal/domain/message.go`）→ proto → 前端：

| Type | 含义 |
| --- | --- |
| `assistant_message` / `assistant_delta` / `assistant_thinking_delta` | 最终/增量回复、思考过程 |
| `tool_progress` / `tool_result` | 工具开始/结束（含状态、摘要、原始 JSON） |
| `confirmation_required` | 高风险确认请求（confirmation_id、summary、expires_at） |
| `error` | 错误（含 business_executed 标记） |
| `a2ui` | 结构化 UI 指令（工具卡片更新、完成态等，见 `internal/eino/a2ui.go`） |
| 排队事件 | queue_position / estimated_wait / reject_reason（排队限流层） |

每个事件带 conversation_id、message_id、done 标记；`BusinessExecuted` 标记真实业务副作用，用于前端在模型总结失败时提醒用户。

---

## 12. 自动化评测（tau2-bench）

目录：`eval/tau2-ecommerce/`（基于 tau2-bench 定制电商域）：

- 领域资产：`domains/ecommerce/`（db.json 初始业务状态、policy.md 客服政策、tasks.json 多轮任务、split_tasks.json 划分）；
- 执行器：`tau2_ecommerce/agent.py / client.py / sse.py` 通过 SSE 与真实服务对话；
- 指标：`metrics.py` 计算任务通过率；
- 规模：**30 个自建多轮任务样本、60 次仿真执行**，覆盖 Tool Selection、参数补全、RAG、跨 Agent 协作、确认链路；
- 迭代闭环：通过 Prompt、Tool Schema、Context Retrieval 的迭代优化，最终 **Pass@1 > 92%、Pass@3 > 99%**。

---

## 13. 代码地图（快速导航）

| 关注点 | 位置 |
| --- | --- |
| API 网关（SSE、路由、鉴权） | `apis/ai/` |
| Chat 主流程 / 确认流程 | `services/aiagent/internal/logic/chatlogic.go`、`confirmactionlogic.go` |
| Supervisor 多 Agent 编排 | `services/aiagent/internal/eino/agent.go` |
| 确认拦截 / Checkpoint / 记忆中间件 | `internal/eino/approval.go`、`checkpoint_store.go`、`memory_middleware.go` |
| 模型工厂与降级 | `internal/eino/model_factory.go` |
| 工具注册 / 执行 / 重试 / 审计 | `internal/tools/registry.go`、`executor.go`、`retry.go`、`internal/audit/recorder.go` |
| 业务工具 handler | `internal/tools/business/{product,inventory,order,cart,coupon,checkout,user}` |
| 上下文 / 摘要 / 画像 | `internal/contextmanager/`、`internal/profileextractor/` |
| 长期事件记忆 | `internal/memory/`（provider、event_store）、`internal/memoryeventextractor/`、`internal/consumer/` |
| RAG | `internal/rag/` |
| Skill | `internal/skills/issue-triage/`、`internal/skills/purchase-advisor/` |
| 数据模型 | `dal/model/ai/**` |
| 评测 | `eval/tau2-ecommerce/` |

---

## 14. 关键设计决策（为什么这样做）

1. **AI 是编排层，不是事实源**：所有业务状态来自现有 RPC，防止双写与口径漂移；
2. **Supervisor + 领域 Agent 而非单 Agent 大工具表**：每个子 Agent 只见本领域工具，提升 Tool Selection 准确率（评测 Pass@1 提升的主因之一）；
3. **双注册（schema + metadata）**：schema 管模型可见性，metadata 管执行策略（风险/超时/重试/审计），职责分离；
4. **StatefulInterrupt + Checkpoint 而非"确认后重放"**：从挂起点恢复并使用中断时的原始参数，天然防参数替换与重复执行；
5. **长期事件上下文管理替代任务状态快照**：以时间线事件（milestone/event）+ 证据消息 ID + 置信度门控的方式沉淀长期记忆，比结构化 TaskState 更抗幻觉、可解释、可召回；
6. **摘要/事件/画像全部异步化（Kafka）**：在线链路只做检索注入，巩固工作不阻塞对话；
7. **排队限流前置在网关**：把过载保护挡在模型与业务工具执行层之前，且用 SSE 让排队可感知，避免用户无限等待黑盒。
