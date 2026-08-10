# AI Tool 链路重构交接文档

更新时间：2026-08-10  
当前分支：`main`  
当前最新提交：`e5919e6 Refactor/tools (#35)`  

写给新会话：你不需要知道之前聊天历史。先读仓库根目录 `AGENTS.md`，再读本文档。当前 AGENTS 规则里仍写着 `apis/ai` 是 WebSocket 网关，但代码和 AI 客服设计文档已经改成 SSE；遇到冲突时，先检查代码，再同步文档，不要靠旧描述猜。

## 1. 我们在做什么任务

最近一轮任务是彻底重构 AI 智能客服的工具层，让工具执行链路从旧的多组 manager 收敛成一条清晰路径：

```text
Tool Catalog -> Registry -> Eino adapter -> Executor -> Handler -> RPC
```

目标是删除原来的运行时分组和二次绑定机制，不再维护 `QueryTools`、`WriteTools`、`HighRiskTools` 这三套 manager。统一使用 `services/aiagent/internal/tools.Tool` 作为 schema、metadata、handler 和确认摘要的单一事实来源。

本轮没有做 tool schema 数据库动态注册。之前用户明确取消了 “tool schema 动态注册/DB 配置化” 方案，这次只做代码内工具链路收敛。

## 2. 已经完成了什么

### 2.1 统一 Tool Catalog

已新增/保留的核心文件：

- `services/aiagent/internal/tools/tool.go`
- `services/aiagent/internal/tools/catalog.go`
- `services/aiagent/internal/tools/registry.go`
- `services/aiagent/internal/tools/approval_manager.go`
- `services/aiagent/internal/tools/confirmation_summaries.go`

`tools.Tool` 现在统一承载：

- `Name`
- `Desc`
- `Params`
- `Metadata`
- `Handler`
- `ConfirmationSummary`

`DefaultTools(clients, timeout)` 负责生成完整 `[]Tool` catalog。当前 20 个 AI 工具都从这里进入 Registry。

### 2.2 删除旧运行时 manager

旧文件已删除：

- `services/aiagent/internal/tools/write_tools.go`
- `services/aiagent/internal/tools/high_risk_tools.go`

旧概念已从生产代码清掉：

- `QueryTools`
- `WriteTools`
- `HighRiskTools`
- `queryInvokableTool`
- `writeInvokableTool`
- `highRiskInvokableTool`
- `toolSpec`
- `defaultToolSpecs`
- `ToolDefinition`

注意：文档中仍允许出现“旧模块已删除”的说明，不代表代码里还有旧模块。

### 2.3 Registry 与 Eino adapter

`Registry` 现在只保存：

```go
map[string]tools.Tool
```

它负责：

- 返回本地 `domain.Metadata`
- 返回 Eino `ToolInfo`
- 返回统一 `invokableToolAdapter`
- 查找 handler
- 判断是否需要高风险确认
- 构建确认摘要

统一 Eino adapter 的执行路径：

```text
InvokableRun
  -> ToolExecutionContext 取可信 UserID
  -> JSON 参数解析
  -> Executor.Execute(ctx, req, tool.Handler)
  -> Handler 调业务 RPC
  -> 返回 DataJSON 给 Eino
```

`InvokableRun` 只强制要求可信 `UserID`；`ConversationID/MessageID/ClientIP` 尽量传，用于审计和链路追踪，但测试里允许为空。

### 2.4 Executor 保持 Execution Guard 职责

`Executor` 仍负责：

- 工具白名单 metadata 检查
- 参数脱敏
- 覆盖模型传入的 `user_id`
- 超时
- 调用 handler
- 生成统一 `tool_result`
- 写入 `ai_tool_calls`
- 写操作审计
- `BusinessExecuted` 标记

改动点：`NewExecutor(registry, ...)` 会把 executor 回填到 registry，供 `Registry.Tool(...)` 返回的统一 adapter 调用。

### 2.5 高风险确认链路

新增 `ApprovalManager`，只负责确认相关轻量能力：

- `RequiresConfirmation`
- `RequestConfirmation`
- `BindResumeTarget`

Eino ChatModelAgent middleware 不再依赖 `HighRiskTools`，而是通过 `ApprovalManager + Registry` 判断和创建确认。

高风险工具仍是：

- `cart_delete`
- `order_create`
- `order_cancel`

首次调用：

```text
middleware
  -> Registry.Metadata / RequiresConfirmation
  -> Registry.ConfirmationSummary
  -> ConfirmationManager.Create
  -> tool.StatefulInterrupt
  -> confirmation_required
```

批准后：

```text
ConfirmAction approved=true
  -> ResumeStream / ResumeWithParams
  -> 同一个 invokableToolAdapter
  -> Executor
  -> Handler
  -> 业务 RPC
```

拒绝后：

```text
ConfirmAction approved=false
  -> 后端直接返回 rejected tool_result + assistant_message
  -> 不恢复 checkpoint
  -> 不调用 LLM
  -> 不调用业务 RPC
```

### 2.6 ServiceContext 简化

`services/aiagent/internal/svc/servicecontext.go` 初始化顺序已改成：

```text
业务 RPC clients
  -> tool recorder
  -> tools.DefaultTools(...)
  -> tools.NewRegistry(...)
  -> tools.NewExecutor(...)
  -> confirmation manager
  -> tools.NewApprovalManager(...)
  -> eino.NewSupervisorAgent(... WithApprovalManager ...)
```

`ServiceContext.HighRiskTools` 字段已删除。

### 2.7 文档已同步

已同步更新：

- `docs/ai-agent-tool-calling.md`
- `docs/ai-customer-service-design.md`
- `docs/ai-customer-service-implementation-plan.md`

文档现在描述统一工具链路和旧 manager 删除状态。

## 3. 当前卡在哪儿

当前没有代码阻塞。工具链路重构已经合入当前 `main` 最新提交 `e5919e6 Refactor/tools (#35)`，工作树里没有这次工具重构的未提交 diff。

当前 `git status --short` 只看到两个未跟踪目录：

```text
?? frontend/
?? frontend_docs/
```

这两个目录不是本轮 AI tool 重构产生的内容。不要在不了解来源的情况下删除、提交或重置。

还有一个环境提示：code-review graph 显示它是在 `refactor/tools` 分支上构建的，但当前在 `main`。如果新会话要做代码审查或依赖 code-review graph，请先重建 graph，不要信旧 graph。

## 4. 最近验证结果

计划内测试已经通过：

```bash
go test ./services/aiagent/internal/tools -count=1
go test ./services/aiagent/internal/eino -count=1
go test ./services/aiagent/internal/logic -count=1
go test ./services/aiagent/... ./apis/ai/... -count=1
git diff --check
```

结果：

- `services/aiagent/internal/tools` 通过
- `services/aiagent/internal/eino` 通过
- `services/aiagent/internal/logic` 通过
- `services/aiagent/... ./apis/ai/...` 通过
- `git diff --check` 通过

完整测试也跑过：

```bash
go test ./... -count=1
```

结果：失败，但失败集中在外部集成测试环境，不是 AI tool 重构本身：

- `test/rpc/audit`：`127.0.0.1:10008 connection refused`
- `test/rpc/inventory`：`127.0.0.1:10011 connection refused`
- `test/rpc/order`：`0.0.0.0:10004 connection refused`
- `test/rpc/payment`：`0.0.0.0:10006 connection refused`
- `test/rpc/product`：`0.0.0.0:10002 connection refused`
- `test/rpc/users/*`：`0.0.0.0:10001 connection refused`
- `test/rpc/product` 还依赖 Elasticsearch、MySQL 用户权限和本地 `a.jpg`

如果要让 `go test ./...` 全绿，需要先启动这些 RPC 服务、Elasticsearch、MySQL，并补齐测试资源。

## 5. 下一步计划

建议新会话接手后按这个顺序做：

1. 先确认当前状态：

```bash
git branch --show-current
git log --oneline -5
git status --short
rg "NewQueryTools|QueryTools|NewWriteTools|WriteTools|NewHighRiskTools|HighRiskTools|HighRiskTool|WithHighRiskTools|toolSpec|defaultToolSpecs|ToolDefinition" services/aiagent/internal apis/ai/internal -n
```

2. 如果要继续做 AI tool 相关改动，先读：

```text
docs/ai-customer-service-prd.md
docs/ai-customer-service-design.md
docs/ai-customer-service-implementation-plan.md
docs/ai-agent-tool-calling.md
```

3. 如果新增工具：

- 在 `domain` 定义稳定工具名。
- 在 `DefaultTools` catalog 中声明 schema、metadata、RPC 映射。
- 在对应 `*_tools.go` 增加 handler。
- 高风险工具必须提供 `ConfirmationSummaryFunc`。
- schema 绝对不能暴露 `user_id`。
- 业务 RPC user id 必须来自 `HandlerRequest.UserID`。

4. 如果新增测试：

当前 AGENTS 最新规则要求“所有测试文件一律放在 workspace 根目录 `tests/` 下”。仓库已有大量历史测试仍在包目录内，这是既有状态；新会话如果新增测试，应先和用户确认是否要严格执行新规则，避免一边遵守新规则、一边破坏 Go 包内测试惯例。

5. 如果做审查或继续重构，优先跑：

```bash
go test ./services/aiagent/internal/tools -count=1
go test ./services/aiagent/internal/eino -count=1
go test ./services/aiagent/internal/logic -count=1
go test ./services/aiagent/... ./apis/ai/... -count=1
```

完整 `go test ./...` 只有在外部服务准备好后才有意义。

## 6. 绝对不要再踩的坑

1. 不要再恢复 `QueryTools/WriteTools/HighRiskTools`

这次重构的目标就是删除它们。不要为了“兼容”又加回旧 manager、旧 invokable wrapper 或 registry 二次绑定。

2. 不要让 Registry 自动生成 schema-only 占位工具

旧 `NewRegistry(config)` 会注册默认 schema，然后其他 manager 反向替换可执行工具。这个模式已经删除。现在必须显式传入 `DefaultTools(...)`。

3. 不要让模型或客户端提供 `user_id`

Tool schema 不能有 `user_id`。handler 调业务 RPC 前必须使用登录态注入的 `HandlerRequest.UserID`。

4. 不要把高风险拒绝交回 LLM

`approved=false` 是确定性终态，后端直接收口。否则模型会被旧 confirmation 上下文带偏，继续问用户是否确认。

5. 不要在高风险首次调用时执行业务 RPC

首次调用只能创建 confirmation 并 `StatefulInterrupt`。批准后 resume 才能进入真实 handler。

6. 不要吞掉写操作审计失败

写操作业务 RPC 成功但审计失败时，事件必须标记失败并带 `business_executed=true`，防止用户重复执行造成二次写入。

7. 不要把 `assistant_delta` / `tool_progress` 当作持久化消息

它们是 SSE 瞬时事件。完整持久化只应该保存 `assistant_message`、`tool_result`、`confirmation_required`、`error`。

8. 不要相信旧 AGENTS 里的 WebSocket 描述

代码和文档当前主链路是 SSE：`POST /douyin/ai/chat`，aiagent 的 `Chat/ConfirmAction` 是 server-streaming。WebSocket 已删除。

9. 不要用旧 code-review graph 直接下结论

当前 graph 提示建在 `refactor/tools`，现在分支是 `main`。要用 graph 就先 rebuild。

10. 不要随手处理 `frontend/` 和 `frontend_docs/`

它们当前是未跟踪目录，来源不属于本轮工具重构。没有用户明确指令前，不要删除、格式化或提交。
