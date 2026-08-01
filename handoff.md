# AI 智能客服 Agent 交接文档

更新时间：2026-08-01

当前分支：`feat/context_optimization`

写给新会话：你不需要知道之前聊天历史。先读 `AGENTS.md`，再读本文档。这个仓库当前工作区是脏的，里面有连续多轮 AI 客服改造成果；不要随手 reset、checkout 或删除文件。

## 1. 我们在做什么任务

我们在改造 `services/aiagent` 这套电商 AI 客服 Agent，目标是让它真正基于 Eino 做模型、工具、上下文、摘要、用户画像和幂等聊天编排。

最近主线包括：

- 去掉 users RPC 来源的账号画像，改成从聊天消息异步抽取 `UserProfile` JSON。
- `ai_messages` 增加 `client_message_id`、`seq`、`msg_id`，实现重复提交幂等。
- 将工具注册为 Eino 可执行工具，并把 AgentRunner 从单次 LLM 调用改成 Eino ADK ChatModelAgent。
- ModelFactory 支持 tools 参数和结构化输出模型。
- Profile Extractor 改为 DeepSeek/OpenAI-compatible JSON Output：`response_format: {"type":"json_object"}`，不再使用 `json_schema`。

优先阅读：

1. `AGENTS.md`
2. `docs/ai-customer-service-prd.md`
3. `docs/ai-customer-service-design.md`
4. `docs/ai-customer-service-implementation-plan.md`
5. `docs/ai-agent-context-optimization.md`
6. `docs/ai-agent-tool-calling.md`

## 2. 已经完成了什么

### 2.1 Context Manager / 摘要 / 记忆

已完成轻量 Context Manager 方向：

- 原始消息完整保存到 `ai_messages`。
- 模型调用前临时组装 `[]domain.ContextMessage`。
- 在线聊天只构建 AgentContext；旧 IntentContext / IntentPlanner 已被 Supervisor Agent 入口取代。
- 长对话通过滚动摘要压缩。
- 摘要由 Eino LLM summarizer 生成，不再用规则拼接。
- active memories 和 active user profile 可注入上下文。
- token 估算只做日志/metadata，不做运行时阻断。

关键文件：

- `services/aiagent/internal/contextmanager/**`
- `services/aiagent/internal/eino/summary_model.go`
- `services/aiagent/internal/prompts/summary/summary_system_prompt.txt`
- `services/aiagent/internal/logic/chatlogic.go`

### 2.2 聊天来源 UserProfile JSON

旧方向已经废弃：不要再从 users RPC 取昵称、邮箱、locale 这类账号资料作为画像。

当前方向：

- `UserProfile` 是从聊天消息里异步抽取的长期偏好/稳定模式 JSON。
- `UserMemory` 是原子化记忆/证据。
- `UserProfile` 是面向 LLM 注入的聚合画像 JSON。
- 每轮聊天消息持久化后通过 Kafka topic `ai-user-profile-updates` 触发画像更新。
- LLM 只能输出候选 patch，后端负责校验、敏感信息拒绝、证据归属、用户隔离和 upsert。

已实现的核心文件包括：

- `services/aiagent/internal/profileextractor/**`
- `services/aiagent/internal/eino/profile_model.go`
- `services/aiagent/internal/prompts/profile/profile_system_prompt.txt`
- `services/aiagent/internal/consumer/profile_update/**`
- `dal/model/ai/user_profiles/**`
- `services/aiagent/internal/contextmanager/user_profile.go`

注意：画像抽取现在仍在修结构化输出透传问题，见第 3 节。

### 2.3 ai_messages 幂等与 UUIDv7

已按用户要求完成：

- 前端聊天请求增加 `client_message_id`。
- 同一轮 user/assistant/tool 消息保存同一个 `client_message_id`。
- `msg_id` 使用 UUIDv7。
- `seq` 作为 DB 内部自增顺序 ID。
- 重复提交按同一用户的 user 消息幂等判断。
- 重放旧响应时只查同一会话、同一 `client_message_id` 的 assistant 消息，并按 `seq asc` 返回。

重要概念：

- `client_message_id` 是前端生成的一轮请求幂等 ID。
- `dedupe_client_message_id` 是 MySQL 生成列，只用于 user 消息唯一索引。
- 不能直接唯一约束 `(user_id, client_message_id)`，因为同一轮 assistant/tool 也要保存相同 `client_message_id`。

### 2.4 Eino Tool Calling / ADK

已按用户要求把工具接入 Eino：

- 不新增 `NewToolCallingChatModel`。
- 原地改了：
  - `NewChatModel(ctx, cfg, tools ...*schema.ToolInfo)`
  - `NewStructuredChatModel(ctx, cfg, structured, tools ...*schema.ToolInfo)`
- tools 非空时使用 `ToolCallingChatModel.WithTools(tools)`，不用 deprecated `BindTools`。
- Query/Write/HighRisk 工具包装为 Eino `InvokableTool`。
- `AgentRunner` 改为 Eino ADK ChatModelAgent，内部使用 ToolsNode 执行工具。
- Runner 执行前注入可信 `ToolExecutionContext`：authenticated user ID、conversation ID、message ID、client IP。
- Eino tool 参数里的 `user_id` 仍不可信，Execution Guard 必须覆盖。

关键文件：

- `services/aiagent/internal/eino/model_factory.go`
- `services/aiagent/internal/eino/agent.go`
- `services/aiagent/internal/tools/query_tools.go`
- `services/aiagent/internal/tools/write_tools.go`
- `services/aiagent/internal/tools/high_risk_tools.go`
- `services/aiagent/internal/svc/servicecontext.go`

重要限制：

- ADK assistant/tool 中间事件需要写入 `ai_messages`；工具审计仍由现有 recorder/Execution Guard 负责。

### 2.5 ModelFactory 结构化输出改为 json_object

最近已把结构化输出从 `json_schema` 改成 `json_object`：

- `StructuredOutputConfig` 只保留 `Name`、`Description`。
- 删除 profile schema 构造函数。
- 不再发送 `json_schema`。
- 目标是 DeepSeek JSON Output：`response_format: {"type":"json_object"}`。
- profile prompt 已明确要求只返回 JSON 对象，并提供固定示例格式。

关键文件：

- `services/aiagent/internal/eino/model_factory.go`
- `services/aiagent/internal/eino/profile_model.go`
- `services/aiagent/internal/eino/model_factory_test.go`
- `services/aiagent/internal/eino/profile_model_test.go`

最近验证通过：

```bash
go test ./services/aiagent/internal/eino -count=1
go test ./services/aiagent/internal/profileextractor -count=1
go test ./services/aiagent/... -count=1
git diff --check
```

## 3. 当前卡在哪儿

当前最新问题：用户指出 DeepSeek JSON Output 要求真实请求体里传：

```json
"response_format": {"type": "json_object"}
```

但他观察到当前没有传 `response_format` 参数。

已检查到的事实：

- `services/aiagent/internal/eino/model_factory.go` 里 `buildOpenAICompatibleModelConfig(..., structured != nil)` 当前设置了：
  - `ResponseFormat.Type = openai.ChatCompletionResponseFormatTypeJSONObject`
- Eino 依赖源码 `github.com/cloudwego/eino-ext/libs/acl/openai@v0.1.17/chat_model.go` 会在请求组装时读取 `c.config.ResponseFormat` 并设置 `req.ResponseFormat`。
- 但是我们自己的测试目前只断言本地 config struct，没有真实 HTTP 请求体级别的回归测试。

所以真正卡点不是“代码里完全没写 response_format”，而是：

- 还没有证明真实 DeepSeek/OpenAI-compatible HTTP body 一定带了 `response_format`。
- 如果用户抓包确实没看到，可能是结构化模型没有走 `NewStructuredChatModel`、配置被覆盖、Eino 传参链路没有按预期进入请求体，或测试没覆盖实际调用路径。

下一步必须先写真实请求体测试，不要继续靠猜。

## 4. 下一步计划

### Step 1：只读确认当前状态

新会话先跑：

```bash
git status --short
git rev-parse --abbrev-ref HEAD
rg -n "ChatCompletionResponseFormatTypeJSONObject|ResponseFormat|NewStructuredChatModel|profileStructuredOutputConfig|response_format|json_object" services/aiagent/internal/eino docs
```

确认当前修改还在。

### Step 2：给 response_format 写真实 HTTP 请求体测试

在 `services/aiagent/internal/eino/model_factory_test.go` 增加一个 `httptest.Server` 测试，名字建议：

```go
TestStructuredChatModelSendsJSONObjectResponseFormatInRequestBody
```

测试目标：

- 用真实 `NewModelFactory()`。
- 配置 `Provider: "deepseek"` 或 `openai-compatible`。
- `BaseURL` 指向 `httptest.Server`。
- 调用：
  - `factory.NewStructuredChatModel(ctx, cfg, StructuredOutputConfig{Name: "profile"})`
  - `chatModel.Generate(ctx, []*schema.Message{...})`
- 服务端捕获 JSON request body。
- 断言：
  - `response_format.type == "json_object"`
  - 不存在 `response_format.json_schema`
  - messages 里包含明确 JSON 输出要求或至少包含测试 system prompt。

如果这个测试通过，说明用户看到“没传”的路径不是 ModelFactory 结构化模型路径，需要继续查实际运行调用的是不是 `NewChatModel` 而不是 `NewStructuredChatModel`。

如果这个测试失败，按失败请求体修。

### Step 3：如果真实请求没有 response_format，就在工厂层强制注入

优先使用 Eino/OpenAI 官方支持路径：

- 初始化 `openai.ChatModelConfig.ResponseFormat`。

如果真实请求体仍没有，退一步使用 Eino OpenAI 的 generation option 或 request payload modifier，在结构化模型 wrapper 层注入：

```json
"response_format": {"type": "json_object"}
```

最终以 Step 2 的真实 HTTP 请求体测试为准。

### Step 4：检查 Profile Extractor 是否真的走结构化模型

确认：

- `profile_model.go` 调用的是 `NewStructuredChatModel`。
- `svc/servicecontext.go` 创建 `NewProfileExtractorModel(modelFactory, selectProfileModelConfig(...))`。
- consumer 使用的是这个 profile extractor。

相关搜索：

```bash
rg -n "NewProfileExtractorModel|ProfileExtractor|NewStructuredChatModel|NewChatModel" services/aiagent/internal
```

### Step 5：跑验证

修完后跑：

```bash
go test ./services/aiagent/internal/eino -count=1
go test ./services/aiagent/internal/profileextractor -count=1
go test ./services/aiagent/... -count=1
git diff --check
```

## 5. 踩过的坑，绝对不要再踩

1. 不要再用 `json_schema`。

   DeepSeek 当前报过：`This response_format type is unavailable now`。本项目结构化输出统一使用 `json_object`。

2. 不要只测本地 config struct。

   用户现在质疑的是“真实请求没有传 response_format”。必须用 `httptest.Server` 捕获真实 HTTP body。

3. 不要把 prompt 约束当成 response_format。

   DeepSeek JSON Output 需要两者都满足：请求体 `response_format: {"type":"json_object"}`，prompt 明确要求输出 JSON 并给示例。

4. 不要让 Profile Extractor 回退到普通 `NewChatModel`。

   画像抽取必须走 `NewStructuredChatModel`，否则可能没有 `response_format`。

5. 不要让 LLM 直接写用户画像。

   LLM 只能输出候选 JSON。后端必须校验证据消息归属、置信度、敏感信息、删除/遗忘优先级和用户隔离。

6. 不要再从 users RPC 获取 UserProfile。

   用户画像只来自聊天消息抽取，不是账号昵称、邮箱、locale。

7. 不要混淆 `ai_user_memories` 和 `ai_user_profiles`。

   memories 是原子记忆/证据；profiles 是聚合 JSON 画像。

8. 不要同步阻塞聊天主流程更新画像。

   消息落库后投 Kafka，画像异步更新；投递失败只记录日志，不影响聊天响应。

9. 不要破坏 `client_message_id` 的一轮关联。

   同一轮 user/assistant/tool 都要保存相同 `client_message_id`。幂等唯一只约束 user 消息。

10. 不要把 `dedupe_client_message_id` 当业务字段。

    它只是 MySQL 生成列，用于让 user 消息唯一，同时允许 assistant/tool 多行复用同一 `client_message_id`。

11. 不要信任客户端、模型输出、metadata 或工具参数里的 `user_id`。

    认证上下文是唯一可信来源。

12. 不要绕开 Execution Guard。

    Query/Write/HighRisk Eino tools 调业务 RPC 前必须经过 Guard，写操作必须审计，高风险操作必须确认。

13. 不要把工具失败总结成成功。

    失败就是失败，不能生成“已完成”“已取消”“已加入购物车”之类的假成功话术。

14. 不要恢复单次 ChatModel runner。

    用户已经指出必须使用 ADK ChatModelAgent，让模型看到 tools，并让 ADK ToolsNode 执行工具。

15. 不要随手清理当前脏工作区。

    这里包含多轮未提交成果。只改和当前任务相关的文件。

## 6. 当前工作区提醒

最近 `git status --short` 显示有大量修改，包括但不限于：

- `docs/ai-customer-service-design.md`
- `docs/ai-customer-service-implementation-plan.md`
- `go.mod`
- `services/aiagent/internal/eino/model_factory.go`
- `services/aiagent/internal/eino/profile_model.go`
- `services/aiagent/internal/eino/agent.go`
- `services/aiagent/internal/tools/**`
- `services/aiagent/internal/logic/chatlogic.go`
- `services/aiagent/internal/svc/servicecontext.go`
- `dal/model/ai/conversation_summaries/aiconversationsummariesmodel.go`

实际接手前一定重新跑：

```bash
git status --short
git diff --check
```

## 7. 推荐恢复命令

```bash
git status --short
git rev-parse --abbrev-ref HEAD
rg -n "response_format|json_object|json_schema|NewStructuredChatModel|NewProfileExtractorModel" services/aiagent/internal/eino services/aiagent/internal/svc services/aiagent/internal/consumer docs
rg -n "WithTools|NewADKRunner|ChatModelAgent|ToolExecutionContext|InvokableTool" services/aiagent/internal/eino services/aiagent/internal/tools services/aiagent/internal/svc
rg -n "client_message_id|dedupe_client_message_id|msg_id|uuidv7|Replay" apis services dal construct
go test ./services/aiagent/internal/eino -count=1
```

如果下一会话要继续实现，第一件事就是补 `httptest.Server` 请求体测试，确认 `response_format.type=json_object` 到底有没有真实发出去。
