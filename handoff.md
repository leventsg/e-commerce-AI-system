# AI 智能客服 Agent 交接文档

更新时间：2026-08-04

当前分支：`feat/context_optimization`

写给新会话：你不需要知道之前聊天历史。先读 `AGENTS.md`，再读本文档。当前工作区是脏的，里面有连续多轮 AI 客服改造成果；不要随手 `reset`、`checkout`、删除文件或回退未理解的改动。

## 1. 我们在做什么任务

我们在改造 `services/aiagent` 这套电商 AI 客服 Agent，让它真正基于 Eino/ADK 完成：

- 聊天上下文组装与滚动摘要。
- 聊天来源用户画像抽取与注入。
- `ai_messages` 幂等与 UUIDv7 消息 ID。
- Eino Tool Calling 与 Execution Guard。
- 多 Agent 编排：Supervisor 负责意图识别、任务拆解、领域 Agent 路由和最终总结。
- 子 Agent 负责本领域工具选择、参数抽取、业务 RPC 调用和工具失败处理。

必读文档：

1. `AGENTS.md`
2. `docs/ai-customer-service-prd.md`
3. `docs/ai-customer-service-design.md`
4. `docs/ai-customer-service-implementation-plan.md`
5. `docs/ai-agent-context-optimization.md`
6. `docs/ai-agent-tool-calling.md`

## 2. 已经完成了什么

### 2.1 Context Manager / 摘要 / 记忆

当前在线聊天只构建 `AgentContext`，旧 `IntentContext` / `IntentPlanner` / `IntentModel` 已移除。

上下文构建入口：

- `services/aiagent/internal/logic/chatlogic.go`
  - 保存用户消息后调用 `ContextManager.Build(...)`。
  - 然后调用 `runSupervisor(..., agentContext.Messages)`。
- `services/aiagent/internal/contextmanager/manager.go`
  - 组装 `[]domain.ContextMessage`。

当前 `AgentContext` 组装顺序：

1. system：`agentprompt.SystemPrompt`
2. conversation summary
3. 最近 user/assistant 消息，最多 20 条
4. latest tool result
5. historical tool refs
6. active task state
7. active user memories
8. active user profile
9. 当前用户输入

注意：`ContextManager` 只负责每轮用户请求进入 Supervisor 前的初始上下文快照。本轮运行中 Supervisor、AgentTool、子 Agent、ToolsNode 的内部消息由 ADK runSession/state 在内存里维护，不会回写到原来的 `agentContext.Messages` slice。

跨轮上下文靠持久化实现：`ChatLogic.runSupervisor` 的 `OnEvent` 会边运行边把可转换的 assistant/tool event 写入 `ai_messages`，下一轮再由 `ContextManager` 从 DB 重新组装。

### 2.2 Supervisor Agent + AgentTool

最新完成：移除了 ADK `prebuilt/supervisor` / AgentTransfer，改为 `ChatModelAgent + AgentTool`。

关键文件：

- `services/aiagent/internal/eino/agent.go`
- `services/aiagent/internal/eino/agent_test.go`
- `services/aiagent/internal/prompts/agent/*.txt`
- `services/aiagent/internal/svc/servicecontext.go`

当前结构：

- Root：`supervisor_agent`
  - 普通 ADK `ChatModelAgent`
  - 只绑定 5 个 AgentTool：
    - `product_agent`
    - `order_agent`
    - `cart_checkout_agent`
    - `coupon_agent`
    - `general_agent`
  - 不直接绑定业务 RPC 工具。
  - `ToolsConfig.EmitInternalEvents = true`，用于把子 Agent 内部真实业务 tool event 暴露给外层 Runner。

- 子 Agent：
  - 都是普通 ADK `ChatModelAgent`
  - 只绑定各自领域业务工具。
  - 默认只接收 Supervisor 传入的紧凑 `request`，没有使用 `adk.WithFullChatHistoryAsInput()`，不会共享完整聊天历史。

当前领域划分：

- `product_agent`：`product_search`、`product_detail`、`product_recommend`、`inventory_get`
- `order_agent`：`order_get`、`order_list`、`order_cancel`
- `cart_checkout_agent`：`cart_list`、`cart_add`、`cart_sub`、`cart_delete`、`checkout_prepare`、`checkout_detail`、`order_create`
- `coupon_agent`：`coupon_list`、`coupon_detail`、`coupon_claim`、`coupon_my_list`、`coupon_usage_list`、`coupon_calculate`
- `general_agent`：无业务工具，用于普通客服解释、闲聊、无法归类问题

ADK event 转换规则：

- 跳过 assistant 中带 `ToolCalls` 的中间消息。
- 跳过非 Supervisor 的 assistant 消息，避免子 Agent 内部回复直接展示给用户。
- 跳过 AgentTool 包装层 tool event，例如 `product_agent` 返回。
- 保留真实业务工具 event，例如 `product_search`、`order_get`，并写入 `ai_messages`。

### 2.3 Eino Tool Calling / Execution Guard

已完成：

- 工具封装为 Eino `InvokableTool`。
- `Registry.ToolsByNames(...)` 和 `Registry.ToolInfosByNames(...)` 可按领域取工具。
- `ModelFactory.NewChatModel(ctx, cfg, tools...)` 支持 tools 参数。
- tools 非空时使用 `ToolCallingChatModel.WithTools(tools)`，不用 deprecated `BindTools`。
- 工具执行前由 Runner 注入可信 `ToolExecutionContext`：
  - authenticated `user_id`
  - `conversation_id`
  - 当前 `message_id`
  - `client_ip`
- Eino tool arguments 里的 `user_id` 不可信，Execution Guard 必须覆盖或清理。

关键文件：

- `services/aiagent/internal/eino/model_factory.go`
- `services/aiagent/internal/tools/registry.go`
- `services/aiagent/internal/tools/query_tools.go`
- `services/aiagent/internal/tools/write_tools.go`
- `services/aiagent/internal/tools/high_risk_tools.go`
- `services/aiagent/internal/tools/executor.go`

### 2.4 UserProfile / UserMemory

方向已确定：

- 不再从 users RPC 获取账号资料当画像。
- `ai_user_memories` 保存原子化长期记忆/证据。
- `ai_user_profiles` 保存面向模型注入的聚合画像 JSON。
- 每轮聊天消息持久化后投递 Kafka topic `ai-user-profile-updates`。
- Profile Extractor 异步读取本轮消息、现有 profile、相关 active memories，调用 LLM 生成候选 patch。
- 后端负责 JSON 校验、证据归属、敏感信息拒绝、用户隔离、删除/遗忘优先级和 upsert。

结构化输出已改为 DeepSeek/OpenAI-compatible `json_object`：

- 不再使用 `json_schema`。
- `NewStructuredChatModel` 应设置 `response_format: {"type":"json_object"}`。
- prompt 必须明确要求只输出 JSON 对象，并给示例。

关键文件：

- `services/aiagent/internal/eino/profile_model.go`
- `services/aiagent/internal/profileextractor/**`
- `services/aiagent/internal/consumer/profile_update/**`
- `services/aiagent/internal/contextmanager/user_profile.go`
- `dal/model/ai/user_profiles/**`

### 2.5 ai_messages 幂等与 UUIDv7

已按用户要求完成：

- 前端聊天请求增加 `client_message_id`。
- 同一轮 user/assistant/tool 消息保存同一个 `client_message_id`。
- `msg_id` 使用 UUIDv7。
- `id` 作为 DB 内部自增顺序 ID。
- 重复提交按同一用户的 user 消息幂等判断。
- 重放旧响应时只查同一会话、同一 `client_message_id` 的 assistant 消息，并按 `id asc` 返回。

重要概念：

- `client_message_id`：前端生成的一轮请求幂等 ID。
- `dedupe_client_message_id`：MySQL 生成列，只用于 user 消息唯一索引。
- 不能唯一约束 `(user_id, client_message_id)`，因为同一轮 assistant/tool 也要保存相同 `client_message_id`。

## 3. 当前卡在哪儿

当前没有明确代码阻塞，最近一轮任务“移除 ADK prebuilt Supervisor，改用 ChatModelAgent + AgentTool”已经完成并通过目标测试。

需要注意的环境问题：

- 系统 `/tmp` / 默认 Go build cache 所在卷曾满过，`go test` 报：
  - `link: mapping output file failed: no space left on device`
- workaround 是临时使用仓库所在卷：
  - `GOCACHE=/Volumes/macOS/VSCodeProject/GoProject/project/go-mall/.cache/go-build`
  - `GOTMPDIR=/Volumes/macOS/VSCodeProject/GoProject/project/go-mall/.cache/go-tmp`
- 验证后 `.cache` 已被清理。
- `apis/ai/...` 测试在 sandbox 下可能因为 `httptest` 绑定本地端口失败：
  - `bind: operation not permitted`
  - 需要按规则申请非 sandbox 运行同一 `go test` 命令。

当前工作区仍有很多未提交改动，其中大部分来自前序任务，不要误判为本轮新增。

最新 `git status --short` 只显示本轮直接相关改动为：

- `services/aiagent/internal/eino/agent.go`
- `services/aiagent/internal/eino/agent_test.go`

但 docs 中关于 AgentTool 的更新也已经存在于工作区；请以实际 `git diff` 为准。

## 4. 最近验证结果

最近一次完成 AgentTool 替换后验证：

```bash
go test ./services/aiagent/internal/eino -count=1
go test ./services/aiagent/internal/logic -count=1
go test ./services/aiagent/... -count=1
go test ./apis/ai/... -count=1
git diff --check
```

结果：

- `services/aiagent/internal/eino` 通过。
- `services/aiagent/internal/logic` 通过。
- `services/aiagent/...` 通过。
- `apis/ai/...` sandbox 下因本地端口绑定失败，非 sandbox 重跑通过。
- `git diff --check` 通过。

残留检查：

```bash
rg -n "prebuilt/supervisor|supervisoragent|TransferToAgent|AgentTransfer|Successfully transferred" services docs
```

结果：

- `services/**` 无旧 supervisor/transfer 代码残留。
- `docs/**` 中只允许出现“当前不使用 `prebuilt/supervisor` / AgentTransfer”的说明。

## 5. 下一步计划

建议新会话接手后先做这几件事：

1. 只读确认当前状态：

```bash
git status --short
git rev-parse --abbrev-ref HEAD
rg -n "prebuilt/supervisor|supervisoragent|TransferToAgent|AgentTransfer|Successfully transferred" services docs
rg -n "NewSupervisorAgent|NewAgentTool|EmitInternalEvents|WithFullChatHistoryAsInput" services/aiagent/internal/eino
```

2. 确认文档和实现是否完全一致：

- `docs/ai-agent-tool-calling.md`
- `docs/ai-customer-service-design.md`
- `docs/ai-customer-service-implementation-plan.md`
- `docs/ai-agent-context-optimization.md`

3. 如果继续优化上下文，要明确区分三层：

- ContextManager：每轮开始前组装初始模型输入。
- ADK runSession/state：本轮内部 supervisor/subagent/tool 消息传递。
- DB：跨轮持久化上下文来源，下一轮再被 ContextManager 读取。

4. 如果继续优化事件持久化，重点检查：

- 是否需要持久化更多子 Agent 内部 assistant 消息。
- AgentTool wrapper event 是否仍应跳过。
- 真实业务 tool event 是否都能通过 `EmitInternalEvents` 暴露并写入 `ai_messages`。

5. 如果继续查 DeepSeek JSON Output，必须写真实 HTTP 请求体测试：

- 不要只测本地 config struct。
- 用 `httptest.Server` 捕获请求 body。
- 断言真实请求包含：

```json
"response_format": {"type":"json_object"}
```

## 6. 踩过的坑，绝对不要再踩

1. 不要再用 ADK `prebuilt/supervisor` / AgentTransfer。

   已确认不适合当前客服场景：全量上下文共享、注意力稀释、transfer 成功消息污染上下文、强制注入 Transfer Tool。当前采用 `ChatModelAgent + AgentTool`。

2. 不要给子 Agent 使用 `WithFullChatHistoryAsInput()`。

   当前设计要求子 Agent 默认只收到 Supervisor 传入的紧凑 `request`，避免全量历史共享和上下文污染。

3. 不要让 Supervisor 直接绑定业务 RPC 工具。

   Supervisor 只绑定 AgentTool。业务工具只给领域子 Agent，才能保持领域边界和工具列表可控。

4. 不要把 AgentTool wrapper event 当业务工具结果落库。

   `product_agent`、`order_agent` 等 wrapper event 只是协调层返回。应跳过。真正要落库的是 `product_search`、`order_get` 等业务工具 event。

5. 不要再恢复 IntentPlanner / IntentContext。

   用户已经明确：Intent agent 改为 Supervisor Agent，具备意图识别、路由、任务拆解能力；旧 planner 职责过大，已经移除。

6. 不要信任模型、客户端、metadata 或 tool arguments 里的 `user_id`。

   登录态用户 ID 是唯一可信来源；工具执行前必须由后端注入/覆盖。

7. 不要再用 `json_schema` 结构化输出。

   DeepSeek 报过：`This response_format type is unavailable now`。当前统一使用 `json_object`。

8. 不要把 prompt 约束当成 `response_format`。

   DeepSeek JSON Output 需要两者都满足：请求体带 `response_format: {"type":"json_object"}`，prompt 明确要求输出 JSON 并给示例。

9. 不要只测本地配置对象。

   如果用户质疑真实请求参数，必须用 `httptest.Server` 捕获真实 HTTP body。

10. 不要直接唯一约束 `(user_id, client_message_id)`。

    同一轮 assistant/tool 也要保存相同 `client_message_id`。幂等唯一约束要只作用在 user 消息。

11. 不要把 `ContextManager` 理解成本轮运行时动态上下文容器。

    它只负责每轮开始前组装初始上下文；本轮内部消息由 ADK 管，跨轮靠 `ai_messages` / summary / memory / profile 再组装。
