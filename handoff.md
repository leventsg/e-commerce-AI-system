# AI 智能客服 Context / 摘要 / 记忆 / 画像交接文档

更新时间：2026-07-24

当前分支：`feat/context_optimization`

写给新会话：你不需要知道前面的聊天历史。从这份文档开始接手即可。先读 `AGENTS.md`，再读本文档里列出的任务和坑。

## 1. 我们在做什么任务

我们正在做 AI 智能客服的上下文工程优化。当前方案已经明确为轻量 Context Manager，而不是企业级 Context Snapshot 系统。

核心目标：

- 原始消息完整保存到 `ai_messages`。
- 每次模型调用前临时组装 `[]domain.ContextMessage`。
- Intent Planner 和 Agent Runner 都消费 Context Manager 产物。
- 长对话通过滚动摘要压缩。
- 默认保留最近 20 条未压缩 user/assistant 原文。
- 未摘要消息达到 30 条时，每轮压缩最早 10 条，并推进摘要水位。
- 工具上下文只直接注入最近一次完整 tool result。
- 更早工具调用只注入 ToolCallRef，需要完整结果时再按 `tool_call_id + user_id + conversation_id` 查询。
- 长期记忆使用 `UserMemory` 保存原子化偏好/指令/证据。
- 用户画像 `UserProfile` 已重新定义为“从 AI 聊天中由 LLM 异步抽取的 JSON 画像”，不再来自 users RPC。
- token 估算只用于日志/metadata，不做运行时裁剪，不阻塞模型调用。

优先阅读：

1. `AGENTS.md`
2. `docs/ai-customer-service-prd.md`
3. `docs/ai-customer-service-design.md`
4. `docs/ai-agent-context-optimization.md`
5. `docs/ai-customer-service-implementation-plan.md` 的 Task 18/19 相关部分

## 2. 已经完成了什么

### 2.1 Task 18：轻量 Context Manager 正式接入

已完成的效果：

- `ContextManager` 能构建 IntentContext 和 AgentContext。
- IntentContext 只包含当前输入、最近对话、当前 TaskState、长期记忆摘要。
- AgentContext 包含：
  - agent system prompt；
  - conversation summary；
  - 最近 20 条原文；
  - 最近一次完整工具结果；
  - 历史 ToolCallRef；
  - TaskState；
  - active UserMemory；
  - 可选 UserProfile JSON。
- `ChatLogic` 已分别构建 IntentContext 和 AgentContext。
- Planner / Runner 使用领域 `domain.ContextMessage`，Eino 类型仍隔离在 `internal/eino/**`。
- token 估算只记录，不裁剪、不阻塞。

关键文件：

- `services/aiagent/internal/contextmanager/manager.go`
- `services/aiagent/internal/domain/context.go`
- `services/aiagent/internal/logic/chatlogic.go`
- `services/aiagent/internal/eino/messages.go`

### 2.2 Agent system prompt 已重写

文件：

- `services/aiagent/internal/prompts/agent/agent_system_prompt.txt`

当前 prompt 已覆盖：

- 电商客服身份；
- 安全与信任边界；
- 工具使用纪律；
- 高风险确认规则；
- 不编造业务事实；
- 中文客服回答风格。

### 2.3 Task 19：滚动摘要与长期记忆已部分完成

已完成：

- 新增 `ai_conversation_summaries` SQL/model。
- 扩展 `ai_user_memories` 的 key/source/status/TTL/source message/last confirmed 等字段。
- `SummaryManager` 已实现滚动摘要。
- `MemoryPolicy` 已实现显式记忆、受控推断记忆、删除和过期。
- `MemoryStore` 已能读取 active memories，并为 IntentContext 生成简短记忆摘要。
- `ContextManager` 已接入摘要和 active memories。
- `ChatLogic` 在消息持久化后触发摘要刷新，失败只记录日志，不阻塞聊天。

关键文件：

- `services/aiagent/internal/contextmanager/summary.go`
- `services/aiagent/internal/contextmanager/summary_store.go`
- `services/aiagent/internal/contextmanager/summary_message_store.go`
- `services/aiagent/internal/contextmanager/memory_policy.go`
- `services/aiagent/internal/contextmanager/memory_store.go`
- `dal/model/ai/conversation_summaries/**`
- `dal/model/ai/user_memories/**`

### 2.4 摘要生成已改为 LLM 压缩

之前错误地用规则拼接摘要，已经改掉。

现在：

- `ExtractiveSummarizer` 已移除。
- 新增 Eino LLM summarizer。
- 摘要模型无工具权限。
- 输入：上一版摘要 + 本轮待压缩消息。
- 输出：严格 JSON：
  - `summary`
  - `key_facts`
  - `open_tasks`
- 摘要 prompt 单独放在 `services/aiagent/internal/prompts/summary/summary_system_prompt.txt`。

关键文件：

- `services/aiagent/internal/eino/summary_model.go`
- `services/aiagent/internal/prompts/summary/summary_system_prompt.txt`
- `services/aiagent/internal/svc/servicecontext.go`
- `services/aiagent/internal/config/config.go`
- `services/aiagent/etc/aiagent.yaml`
- `services/aiagent/etc/aiagent.prod.yaml`

配置新增：

- `SummaryModel config.EinoConfig`

未配置 `SummaryModel` 时，当前接线 fallback 到 `IntentModel` 配置。

### 2.5 摘要 token 计算已改进

之前 `ConversationSummary.TokenCount` 使用估算值。现在已经改为：

- `RollingSummarizer` 返回 `SummarizeResult`。
- Eino adapter 从 `response.ResponseMeta.Usage` 读取：
  - `PromptTokens`
  - `CompletionTokens`
  - `TotalTokens`
- `ConversationSummary.TokenCount` 优先使用真实 `CompletionTokens`。
- provider 未返回 usage 时才 fallback 到 `estimateSummaryContentTokens`。

注意语义：

- `ConversationSummary.TokenCount` 表示“摘要内容未来注入上下文时大约占多少 token”。
- 不表示“本次 LLM 调用总成本”。
- `PromptTokens` / `TotalTokens` 后续如果要做成本统计，应另设计日志或 usage 表。

### 2.6 摘要 backlog 问题已优化

用户指出：`ChatLogic` 批量插入消息可能一次让水位后未摘要消息超过 30，而旧 `MaybeRefresh` 每次只 `limit=30`，不能感知真实 backlog。

已完成优化：

- DAL 新增：
  - `CountUnsummarizedContextMessages`
  - `FindRecentUnsummarizedContextMessages`
- `SummaryMessagesStore` 新增：
  - `CountUnsummarized`
  - `FindRecentUnsummarized`
- `SummaryManager.MaybeRefresh` 现在：
  - 先按摘要水位 count；
  - 未满 30 不压缩；
  - 满 30 后每轮读取最早 30 条；
  - 每轮压缩最早 10 条；
  - 单次最多压缩 3 轮；
  - 最终 `RecentMessages` 返回最终水位后的最近 20 条。

测试覆盖：

- 29 条不压缩；
- 30 条压缩 1 轮；
- 45 条压缩多轮；
- 70 条最多压缩 3 轮；
- 第二轮 LLM 失败时保留第一轮已保存摘要，不推进失败轮水位；
- DAL count 查询与 watermark 条件一致；
- recent unsummarized 查询返回正序窗口。

### 2.7 存储职责边界已重构

用户质疑 `persistence.go` 放在业务目录且命名含混。已按职责边界拆分：

- `dal/model/ai/**` 只做 SQL / go-zero model / row 级 CRUD 和查询。
- `contextmanager` 保留业务策略和上下文组装。
- DB row 与 domain object 的转换放在明确命名的 store adapter。

现在相关文件：

- `summary_store.go`
- `memory_store.go`
- `summary_message_store.go`

已删除/不再使用：

- `services/aiagent/internal/contextmanager/persistence.go`

### 2.8 用户画像文档已重新定义

最后一轮用户明确：

- 现在不需要 RPC 来源的 `AccountProfile`。
- 不需要 users RPC 来源的账号画像。
- 只需要从聊天中提取的用户画像 `UserProfile`。
- UserProfile 保存为 JSON，便于 LLM 识别。
- 每轮聊天消息持久化后，通过 Kafka MQ 异步触发画像更新。

已经只更新文档，未改代码。

文档现已统一：

- `UserProfile` 只来源于 AI 聊天过程中的用户表达和稳定行为模式。
- `UserMemory` 是原子化记忆/证据。
- `UserProfile` 是面向 LLM 注入的聚合 JSON 画像。
- UserProfile 更新四类时机：
  1. 用户明确表达长期偏好；
  2. 多次行为表现出稳定模式；
  3. 用户主动纠正画像；
  4. 用户主动要求删除、遗忘或不再使用某类偏好。
- 画像更新通过 Kafka topic `AiUserProfileUpdates` 异步触发。
- 建议新增 `ai_user_profiles` 表。
- `docs/ai-agent-context-optimization.md` 和 `docs/ai-customer-service-implementation-plan.md` 已删除“users RPC 生成最小 UserProfile”的旧口径。

注意：代码里当前仍存在 users RPC `UserProfileSource` 实现和接线，这是旧实现残留，后续需要按新文档移除/替换。最后一轮用户只要求更新文档，所以没有改代码。

## 3. 当前卡在哪儿

没有外部阻塞，但有几个明确的未完成点：

### 3.1 文档新方案尚未实现：聊天来源 UserProfile JSON

现在文档已经改成：

- `ai_user_profiles`
- Kafka `AiUserProfileUpdates`
- 异步 Profile Extractor
- LLM JSON patch

但代码还没实现。

当前代码里仍有旧的：

- `services/aiagent/internal/contextmanager/user_profile.go`
- users RPC `UserProfileSource`
- `servicecontext.go` 里 `NewUserProfileSource(userRPC)` 接线

下一步实现画像任务时，必须移除/替换这些旧路径。

### 3.2 Task 19 文档状态是“部分完成”

`docs/ai-customer-service-implementation-plan.md` 里 Task 19：

- 摘要和记忆相关步骤已完成。
- `ai_user_profiles`、Kafka 异步画像、`profileextractor` 仍待实现。
- Step 2 / Step 5 / Step 6 已按新画像方案标成未完成。

不要把 Task 19 整体说成完全完成。

### 3.3 工作区有大量未提交/未跟踪文件

当前不是一个小 diff。接手先跑：

```bash
git status --short
```

预计会看到：

- `construct/depend/sql/init.sql`
- `dal/model/ai/messages/aimessagesmodel.go`
- `dal/model/ai/messages/aimessagesmodel_test.go`
- `dal/model/ai/user_memories/**`
- `dal/model/ai/conversation_summaries/**`
- `docs/ai-customer-service-implementation-plan.md`
- `docs/ai-agent-context-optimization.md`
- `services/aiagent/etc/aiagent*.yaml`
- `services/aiagent/internal/contextmanager/**`
- `services/aiagent/internal/eino/summary_model.go`
- `services/aiagent/internal/prompts/summary/**`
- 等等

不要随手 `git reset`、`git checkout --` 或删除未跟踪文件。这些是本轮连续任务的成果。

## 4. 下一步计划

建议下一会话按下面顺序推进。

### Step 1：先做只读状态确认

```bash
git status --short
rg -n "users RPC|UserProfileSource|NewUserProfileSource|UserProfileStore|ai_user_profiles|AiUserProfileUpdates|profileextractor" docs services dal construct
rg -n "CountUnsummarizedContextMessages|FindRecentUnsummarizedContextMessages|maxSummaryRefreshRounds|NewSummarySummarizer|SummarizeResult" services/aiagent dal/model/ai
```

### Step 2：实现聊天来源 UserProfile JSON 的数据库层

按文档新增：

- `dal/model/ai/user_profiles/ai_user_profiles.sql`
- `dal/model/ai/user_profiles/**`
- migration 和 init SQL。

建议表结构以 `docs/ai-agent-context-optimization.md` 的 `ai_user_profiles` 为准：

- `user_id`
- `profile_json`
- `version`
- `source`
- `status`
- `last_event_id`

### Step 3：实现 UserProfileStore 和策略层

目标：

- 不再从 users RPC 读画像。
- 从 `ai_user_profiles.profile_json` 读画像。
- 保存时校验 JSON 合法性、用户隔离、敏感信息和删除请求。
- `UserProfile` 是非可信上下文，不能覆盖 system prompt、工具白名单、user ID 或确认策略。

可能新增：

- `services/aiagent/internal/contextmanager/user_profile_store.go`
- `services/aiagent/internal/profileextractor/**`

### Step 4：实现 Kafka 异步画像更新

项目已有 Kafka 封装：

- `common/mq`
- `common/config.KafkaConfig`

建议沿用现有服务模式：

- `Config` 增加 `KafkaMQ config.KafkaConfig`
- `aiagent.yaml` / `aiagent.prod.yaml` 增加 `AiUserProfileUpdates` topic
- `ChatLogic` 在 `MessagesModel.InsertBatch` 成功后投递事件
- key 使用 `user_id`
- payload 包含：
  - `event_id`
  - `user_id`
  - `conversation_id`
  - `message_ids`
  - `created_at`

投递失败只记录日志，不影响聊天响应。

### Step 5：实现 LLM Profile Extractor

要求：

- 异步 consumer 收到 Kafka 事件后执行。
- 输入：本轮消息、已有 UserProfile JSON、相关 UserMemory/最近消息。
- 输出严格 JSON，建议字段：
  - `should_update`
  - `update_type`
  - `profile_patch`
  - `evidence_message_ids`
  - `confidence`
  - `reason`
- `update_type`：
  - `explicit_preference`
  - `stable_pattern`
  - `correction`
  - `delete_or_forget`
  - `none`
- LLM 不能直接写库，只能产生候选 patch。

### Step 6：替换旧 users RPC UserProfile

删除或停用：

- `UserRPCProfileSource`
- `NewUserProfileSource(userRPC)`
- `WithUserProfileSource(contextmanager.NewUserProfileSource(userRPC))`

替换为：

- `UserProfileStore`
- 读取 `ai_user_profiles.profile_json`

注意：如果代码里 `UserProfile` domain 结构只有 `DisplayName/Locale`，需要改成 JSON 画像结构或 RawMessage/结构化 profile。

### Step 7：验证

至少跑：

```bash
GOCACHE=/private/tmp/go-build-task19 go test ./services/aiagent/internal/contextmanager -count=1
GOCACHE=/private/tmp/go-build-task19 go test ./dal/model/ai/... -count=1
GOCACHE=/private/tmp/go-build-task19 go test ./services/aiagent/... -count=1
git diff --check
```

如果改到 WebSocket/API：

```bash
GOCACHE=/private/tmp/go-build-task19 go test ./apis/ai/... -count=1
```

注意：之前 `apis/ai/...` 在 sandbox 中可能因为 `httptest` 监听本地端口失败：

```text
bind: operation not permitted
```

这不是业务断言失败。如果需要验证，要在允许本地端口监听的环境里重跑。

## 5. 绝对不要再踩的坑

1. 不要再把 `UserProfile` 设计成 users RPC 来源的账号资料。

   用户已经明确：当前不需要 RPC 来源 AccountProfile。`UserProfile` 只表示从 AI 聊天中抽取的 JSON 画像。

2. 不要把 `UserMemory` 和 `UserProfile` 混成一个东西。

   `UserMemory` 是原子化记忆/证据；`UserProfile` 是给 LLM 使用的聚合 JSON 画像。

3. 不要同步阻塞聊天主流程来更新画像。

   用户明确希望每轮聊天消息持久化后异步执行，可通过 Kafka MQ。

4. 不要让 LLM 直接写数据库。

   LLM 只能输出候选 JSON patch。后端策略必须校验来源、置信度、敏感信息、删除请求和用户隔离。

5. 不要保存敏感画像。

   禁止保存 token、session、auth、密码、支付凭据、银行卡、身份证、完整地址等。

6. 不要把单次订单状态、库存状态、临时优惠写成长期画像。

   这些是动态业务事实，不是用户长期偏好。

7. 不要忽略“删除/遗忘偏好”。

   第四类画像更新时机是用户主动要求删除、遗忘或不再使用某类偏好。这个优先级必须高。

8. 不要恢复规则拼接摘要。

   摘要必须由 LLM 压缩生成，规则拼接已经被明确否定。

9. 不要把摘要 token 主路径改回估算。

   LLM 返回 usage 时优先用 `CompletionTokens`；估算只作为 provider 不返回 usage 时的 fallback。

10. 不要把摘要刷新又改回固定 `limit=30` 的隐式判断。

    现在应先 count 水位后未摘要数量，再最多 3 轮压缩 backlog。

11. 不要一次性读取全部未摘要消息。

    长会话/异常批量插入会造成大查询。每轮只读最早 30 条即可。

12. 不要无限循环压缩。

    单次 `MaybeRefresh` 最多 3 轮，避免一次 chat 触发过多 LLM 调用。

13. 不要让同一条消息既进入摘要又作为近期原文注入。

    摘要水位推进后，被压缩消息不能再进 recent window。

14. 不要恢复 Context Snapshot / TokenBudget / Shadow 灰度方案。

    当前是轻量 Context Manager。本地开发不需要 `ai_context_snapshots`、`SnapshotStore`、`TokenBudget`、`ShadowMode`、`RolloutPercent`、`Context.Enabled`。

15. 不要信任客户端、模型输出、历史消息或 metadata 里的 `user_id`。

    认证上下文是唯一可信来源。工具执行和上下文读取都必须强制 user ID + conversation ID 隔离。

16. 不要把旧 `metadata.data_json` 当完整成功 tool_result envelope。

    旧数据最多生成有限 ToolCallRef。完整结果只能来自合法 `metadata.tool_result` envelope。

17. 不要对任何非 LLM 回复字符串调用 `strings.TrimSpace` 或 `strings.ToLower`。

    只有处理 LLM 回复字符串时允许使用 `strings.TrimSpace` / `strings.ToLower`。业务入参、用户消息、ID、状态、工具参数、数据库字段、上下文内容等字符串都不要做这两个转换；需要校验时保留原值，用明确业务规则处理。

18. 不要手改 go-zero 生成文件，除非本仓库对该文件已有手改惯例。

    新 model 用 goctl 生成；自定义查询放在 custom model 文件。

19. 不要声称测试通过，除非刚刚实际跑过。

    必须给出命令和结果。环境失败要说明具体原因。

## 6. 本轮最后验证过的命令

最近一次代码验证曾通过：

```bash
GOCACHE=/private/tmp/go-build-task19 go test ./services/aiagent/internal/contextmanager -count=1
GOCACHE=/private/tmp/go-build-task19 go test ./dal/model/ai/messages -count=1
GOCACHE=/private/tmp/go-build-task19 go test ./dal/model/ai/... -count=1
GOCACHE=/private/tmp/go-build-task19 go test ./services/aiagent/... -count=1
git diff --check
```

最后一轮只改了文档和本 handoff，没有改 Go 代码；针对文档跑过：

```bash
git diff --check -- docs/ai-customer-service-implementation-plan.md docs/ai-agent-context-optimization.md
```

结果通过。

## 7. 快速恢复命令

新会话建议先执行：

```bash
git status --short
git rev-parse --abbrev-ref HEAD
rg -n "users RPC|UserProfileSource|NewUserProfileSource|UserProfileStore|ai_user_profiles|AiUserProfileUpdates|profileextractor" docs services dal construct
rg -n "NewExtractiveSummarizer|ExtractiveSummarizer|NewSummarySummarizer|SummarizeResult|CountUnsummarizedContextMessages|FindRecentUnsummarizedContextMessages|maxSummaryRefreshRounds" services/aiagent dal/model/ai
rg -n "strings\\.TrimSpace|strings\\.ToLower" services/aiagent/internal/contextmanager dal/model/ai -g '*.go'
git diff --check
```

接手实现前，先确认用户要的是“只继续文档”还是“开始实现聊天来源 UserProfile JSON + Kafka 异步画像更新”。如果要实现，优先从 `docs/ai-agent-context-optimization.md` 的第 11 章和第 13.4 节开始。
