# AI Agent 上下文优化交接文档

更新时间：2026-07-23

当前分支：`feat/context_optimization`

当前最新提交：`62c4430 实现tool_result工具结果保存与查找`

写给新会话：你不需要知道前面的聊天历史，从这份文档开始接手即可。

## 1. 我们在做什么任务

我们正在做 AI 智能客服的上下文工程优化，但方案已经从早先的“企业级 Context Snapshot + Token 预算打包 + 快照持久化”瘦身为更适合本地开发阶段的轻量 Context Manager。

当前目标不是做复杂治理系统，而是：

- 原始消息完整保存；
- 每次模型调用前临时组装 ContextMessages；
- 长对话用滚动摘要压缩；
- 默认保留最近 20 条未压缩消息；
- 未压缩消息达到 `10 + 20 = 30` 条时，把最早 10 条与旧摘要合并成新摘要，之后仍只保留最近 20 条原文；
- 每条消息要么在摘要里，要么在近期原文里，不能重复注入；
- 工具上下文只直接放最近一次完整 tool result；
- 更早工具调用只放 ToolCallRef；
- 需要历史完整工具结果时，再通过 `tool_call_id + user_id + conversation_id` 从数据库读取；
- token 只做估算日志和排查，不做运行时裁剪，不阻塞模型调用；
- 不再设计或扩展独立 `Context Snapshot` 持久化。

新会话开始前建议按顺序阅读：

1. `AGENTS.md`
2. `docs/ai-customer-service-prd.md`
3. `docs/ai-customer-service-design.md`
4. `docs/ai-customer-service-implementation-plan.md`，重点 `## 11. 上下文工程优化`
5. `docs/ai-agent-context-optimization.md`

## 2. 已经完成了什么

### 2.1 方案文档已经瘦身

核心文档已改为轻量方案：

- `docs/ai-agent-context-optimization.md`
- `docs/ai-customer-service-design.md`
- `docs/ai-customer-service-implementation-plan.md`

当前文档口径是：

- Context Manager 只临时组装模型输入，不持久化模型输入。
- `ContextMessages` 是本次模型调用的临时领域消息，不是落库快照。
- 不使用 `Context Snapshot`、`SnapshotStore`、`ai_context_snapshots`、`TokenBudget`、预算打包、mandatory 超预算阻断。
- 可审计性改为结构化日志：摘要版本、近期消息范围、最近 tool call、ToolCallRef 数量、估算 token、构建耗时等。
- IntentContext 只包含当前用户输入、最近对话、当前 TaskState、长期记忆摘要。
- AgentContext 包含摘要、最近 20 条未压缩消息、最近一次完整工具结果、历史 ToolCallRef、TaskState、UserMemory 和可选 UserProfile。

一致性检查结果：旧重型关键词只允许以“已删除/不采用/避免继续扩展”的历史说明出现。

### 2.2 灰度和 Shadow 控制方向已经明确

之前用户已经明确：`ShadowMode` 不需要灰度模型和生产模式，这只是本地开发。

当前方向：

- 不保留 `ShadowMode`。
- 不保留 `RolloutPercent`。
- 不保留 `RolloutUserIDs`。
- 不保留 `Context.Enabled` 作为旧路径开关。
- Chat / Planner / Runner 后续应强制使用 Context Manager 输出，不再回退到旧历史窗口。

如果新会话继续实现代码，先用 `rg` 确认这些旧字段没有重新出现。

### 2.3 ToolResult envelope 与查询能力已经有代码基础

当前最新提交说明已经实现了 tool_result 工具结果保存与查找。

已存在的关键文件包括：

- `services/aiagent/internal/contextmanager/tool_results.go`
- `services/aiagent/internal/contextmanager/tool_results_test.go`
- `services/aiagent/internal/tools/result_projector.go`
- `services/aiagent/internal/tools/result_projector_test.go`
- `services/aiagent/internal/domain/context.go`
- `services/aiagent/internal/logic/chatlogic.go`
- `services/aiagent/internal/logic/chatlogic_test.go`

当前代码里可以看到：

- 成功工具结果会保存为 `metadata.tool_result` envelope。
- 旧 `metadata.data_json` 只作为兼容读取来源，不能伪造成完整成功 envelope。
- `ToolResultStore` 支持：
  - `FindLatestResult`
  - `FindRecentRefs`
  - `FindResultByCallID`
- 查询完整历史工具结果必须带 user ID、conversation ID 和 tool call ID。
- 失败、非法 JSON、未知 tool、跨用户、跨会话结果不能作为成功事实返回给模型。

### 2.4 本轮最后确认过的验证

文档瘦身完成后曾跑过：

```bash
rg -n "Context Snapshot|SnapshotStore|ai_context_snapshots|TokenBudget|ErrContextBudgetExceeded|mandatory.*超预算|预算打包|SourceManifest" docs
git diff --check
go test ./services/aiagent/...
go test ./apis/ai/...
```

当时结果：

- `git diff --check` 通过；
- `go test ./services/aiagent/...` 通过；
- `go test ./apis/ai/...` 通过；
- 旧重型关键词只剩两处“删除/避免继续扩展”的说明。

接手后如果要声称完成新的代码改动，必须重新跑相关测试，不要沿用这次结果当作新改动的证明。

## 3. 当前卡在哪儿

严格说当前没有外部阻塞；真正的“卡点”是实现范围需要继续收敛，不能被旧重方案拖回去。

当前状态：

- 工作区在写这份 handoff 前是干净的。
- 写完后预期只有 `handoff.md` 被修改。
- 文档目标已经收敛。
- ToolResult 保存和查找已有基础实现。
- 轻量 Context Manager 的完整组装链路还没完全落地。
- 滚动摘要 `30 -> 10 + 20` 策略还需要实现和测试。
- Planner / Runner 是否已经完全只消费轻量 Context Manager 输出，需要接手后用代码和测试确认，不能靠印象。
- `ai_context_snapshots` 后续如果还存在于 SQL/model/代码里，应在实现阶段移除；但上一轮文档修改明确“不在文档瘦身那一轮改 Go/SQL/model”。

新会话第一件事应该先看真实状态：

```bash
git status --short
rg -n "ShadowMode|RolloutPercent|RolloutUserIDs|Context.Enabled|Context Snapshot|SnapshotStore|TokenBudget|ErrContextBudgetExceeded|ai_context_snapshots" services apis dal construct docs
rg -n "FindLatestResult|FindRecentRefs|FindResultByCallID|ToolCallRef|BuildContextResult|BuildContextRequest" services/aiagent
```

## 4. 下一步计划

建议按文档里的 Task 17-21 继续，但实现时小步走，不要一次性大重构。

### Step 1：确认 Task 17 真实完成度

检查并补齐：

- ToolResult envelope 保存是否覆盖 Chat 和 ConfirmAction 两条路径；
- `FindLatestResult` 是否只返回最近一次合法成功完整结果；
- `FindRecentRefs` 是否跳过失败、未知 tool、非法 JSON；
- `FindResultByCallID` 是否强制 user ID + conversation ID + tool call ID；
- ToolCallRef projector 是否保留关键 entity ID，例如 product_id、cart_item_id、order_id、coupon_id；
- 旧 `data_json` 只能生成有限 ref，不能当完整 envelope。

建议测试：

```bash
go test ./services/aiagent/internal/contextmanager -run 'TestToolResult|TestToolCallRef' -count=1
go test ./services/aiagent/internal/tools -run TestResultProjector -count=1
```

### Step 2：实现轻量 Context Manager 主链路

目标：

- 新增或完善 `BuildContextRequest`、`BuildContextResult`、`ContextMessage`。
- `BuildContextResult` 至少包含：
  - messages；
  - summary version；
  - recent message start/end；
  - latest tool call ID；
  - ToolCallRef count；
  - estimated input tokens。
- token estimate 只能用于日志/metadata，不能裁剪，不能返回预算错误。
- IntentContext 只包含当前输入、最近对话、TaskState、长期记忆摘要。
- AgentContext 包含摘要、最近 20 条原文、最近一次完整工具结果、历史 ToolCallRef、TaskState、UserMemory、可选 UserProfile。

### Step 3：切 Planner / Runner 输入

目标：

- Planner 只消费 Context Manager 组装的 IntentContext。
- Agent Runner 只消费 Context Manager 组装的 AgentContext。
- 删除旧固定历史窗口模型输入路径。
- Context Manager 缺失或构建失败时返回明确错误，不悄悄 fallback。

注意：Conversation Manager 仍然负责会话归属校验、原始消息持久化和历史读取；只是不要再让它决定模型输入窗口。

### Step 4：实现滚动摘要

目标策略：

```text
未压缩消息 < 30：不压缩
未压缩消息 >= 30：最早 10 条 + 旧摘要 -> 新摘要
摘要成功后：水位推进，保留最新 20 条原文
```

必须测：

- 少于 30 条不触发；
- 达到 30 条压缩最早 10 条；
- 成功后只保留 20 条原文；
- 同一条消息不会同时出现在摘要和近期原文；
- 摘要失败/非法 JSON 时保留旧摘要和近期原文。

### Step 5：清理旧重方案残留

只在实现阶段做，不要在纯文档任务里偷改 SQL/model。

要清理：

- `ai_context_snapshots` 表、model、recorder；
- Snapshot failure 指标；
- TokenBudget / ErrContextBudgetExceeded；
- Shadow/rollout/context enabled 控制；
- Planner/Runner 旧 history fallback。

保留：

- `ai_conversation_summaries`；
- `ai_agent_runs`；
- `ai_user_memories`；
- ToolResult envelope；
- TaskState；
- structured logs。

### Step 6：验证

每个阶段至少跑聚焦测试：

```bash
go test ./services/aiagent/internal/contextmanager -count=1
go test ./services/aiagent/internal/eino -count=1
go test ./services/aiagent/internal/logic -count=1
go test ./services/aiagent/... -count=1
go test ./apis/ai/... -count=1
git diff --check
```

如果本地依赖齐全，再跑：

```bash
go test ./...
```

如果全仓测试因为 RPC、MySQL、ES、七牛、固定测试数据等环境失败，要记录具体命令和失败原因，不要说“全部通过”。

## 5. 绝对不要再踩的坑

1. 不要把轻量方案又做回 `Context Snapshot` 持久化系统。

   本地开发不需要独立上下文快照表，不要新增/扩展 `ai_context_snapshots`、`SnapshotStore`、snapshot recorder。

2. 不要恢复 TokenBudget 预算打包。

   token 估算只用于日志和排查。不要做 mandatory item、优先级打包、超预算裁剪，也不要返回 `ErrContextBudgetExceeded`。

3. 不要做 Shadow / 灰度 / 生产观察模型。

   用户已经明确：这是本地开发，`ShadowMode`、`RolloutPercent`、`RolloutUserIDs`、`Context.Enabled` 都不要。

4. 不要让模型输入回退到旧 History。

   Planner 和 Runner 后续应该只消费 Context Manager 输出。Context 构建失败就明确失败，修 Context Manager；不要悄悄用 `prepared.History` 或 legacy messages。

5. 不要让同一条消息既进摘要又进近期原文。

   水位推进后，被压缩的 10 条消息不能再作为近期原文注入模型。

6. 不要把历史工具结果全塞进上下文。

   只放最近一次完整工具结果；历史工具调用放固定数量 ToolCallRef。需要完整结果时再按需读取。

7. 不要把旧 `metadata.data_json` 当完整成功 envelope。

   旧数据最多生成有限 ToolCallRef。完整结果只能来自合法的 `metadata.tool_result` envelope。

8. 不要把失败工具结果变成成功事实。

   工具失败、非法 JSON、未知 schema、未知 tool、过期动态事实，都不能作为成功业务事实进入模型。

9. 不要放松用户和会话隔离。

   所有上下文读取都必须同时带认证 user ID 和 conversation ID。按 tool call ID 读取完整结果也一样。

10. 不要信任模型、客户端 payload、metadata 里的 `user_id`。

    认证上下文是唯一可信来源。工具执行前必须覆盖或注入真实 user ID。

11. 不要改 generated model 文件，除非本仓库对此类文件已有明确手改惯例。

    go-zero 生成文件要通过 goctl 生成；自定义查询和 CAS 方法放在项目约定的可手改文件里。

12. 不要声称全仓测试通过，除非真的跑过 `go test ./...` 并贴出结果。

    这个仓库部分集成测试依赖本地 MySQL、RPC、ES、七牛和固定数据；失败要如实说明。

13. 不要再函数里反复判断校验和执行string.TrimSpace()、string.ToLower()函数，只在最外层做一次校验即可

## 6. 快速恢复命令

新会话可以先执行：

```bash
git status --short
git rev-parse --abbrev-ref HEAD
git log -1 --oneline
rg -n "ShadowMode|RolloutPercent|RolloutUserIDs|Context.Enabled|Context Snapshot|SnapshotStore|TokenBudget|ErrContextBudgetExceeded|ai_context_snapshots" services apis dal construct docs
rg -n "ToolResult|ToolCallRef|FindLatestResult|FindRecentRefs|FindResultByCallID|BuildContextResult|BuildContextRequest" services/aiagent
go test ./services/aiagent/... -count=1
go test ./apis/ai/... -count=1
git diff --check
```

如果只想继续实现，优先从 `docs/ai-customer-service-implementation-plan.md` 的 Task 17/18 开始，对照 `docs/ai-agent-context-optimization.md`，保持轻量方案，不要被旧企业级设计的幽灵拽回去。
