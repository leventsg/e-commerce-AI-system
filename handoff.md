# 交接文档

## 我们在做什么任务

我们在做电商 AI 智能客服的前后端接入与 Agent 事件链路收敛。

当前重点已经从“前端 API 接入”推进到“后端 Eino Agent run 生命周期、SSE 事件来源、最终回答归属 final ownership”的梳理与修正。核心目标是让一次用户消息或确认操作触发的 Agent run 有清晰边界：

- 工具进度、工具结果、确认卡片、思考过程分别作为状态事件给前端展示。
- 最终 assistant_message 作为唯一事实消息，用于落库、摘要、画像和幂等重放。
- 前端可见的最终回答不能混入 supervisor 中间推理、阶段性计划、工具前置判断。
- callback、ADK iterator、logic 持久化层各自职责要清楚，避免一个事件被处理两次。

相关主要目录：

- `services/aiagent/internal/eino/**`：Eino Agent、callback、iterator 消费、Agent run 状态机。
- `services/aiagent/internal/logic/**`：Chat / ConfirmAction RPC 逻辑、消息落库、SSE/RPC 事件转发。
- `apis/ai/**`：AI API/SSE 网关。
- `frontend/**`：AI 客服前端页面、SSE 消费、三状态栏展示。
- `frontend_docs/**`：前端 API 接入文档和实施方案。

## 已经完成了什么

前端方向已经完成过以下方案与实现：

- 在 `frontend_docs/frontend-api-integration.md` 梳理了前端需要接入的认证、AI SSE、确认流程、商城 REST 接口。
- 在 `frontend_docs/frontend-code-implementation-plan.md` 写过前端代码实施计划。
- 修正过本地 Vite 多后端代理方案：前端继续请求 `/douyin/**`，开发代理按路径分流到不同服务端口，例如 user 到 `8001`，ai 到 `8007`，product 到 `8002` 等。
- 前端曾接入真实登录、SSE AI 聊天、确认卡片、工具结果展示、三状态栏分组展示。
- 前端新对话不应传 `conversation_id`；`conversation_id` 应以后端返回为准。
- `client_message_id` 应使用 uuidv7，不应使用时间戳拼数字。
- 确认操作里的 `conversation_id`、`confirmation_id` 必须使用后端事件返回值绑定，不能前端自行猜。
- token 续期后应覆盖保存新的 access/refresh token。
- SSE 收到 `done=true` 后，前端输出中的 streaming 光标应停止。

后端方向已经完成或已改动的内容：

- AI API 与 aiagent 超时已调整到 5 分钟以内：
  - `apis/ai/etc/ai-api.yaml`
  - `apis/ai/etc/ai-api.prod.yaml`
  - `services/aiagent/etc/aiagent.yaml`
  - `services/aiagent/etc/aiagent.prod.yaml`
  - `apis/ai/internal/logic/chatlogic.go` 中 `sseRequestTimeout = 5 * time.Minute` 已存在。
- Eino 事件来源已做过一次简化：
  - callback 是工具实时事件唯一来源。
  - `consumeEvents` 不再把 ADK iterator 里的 Tool message 转成 `tool_result`。
  - 删除了 `adkEventToDomainEvent`，改为 `iteratorAssistantEventToDomainEvent`，只处理 supervisor 最终 assistant。
  - 删除了工具结果/进度去重逻辑，允许模型真实连续调用同一个工具多次。
  - 删除了 `RunRequest.OnEvent` / `ResumeRequest.OnEvent`，事件统一走 `out` channel。
- logic 层事件处理已收敛：
  - `EventAssistantMessage`、`EventToolResult`、`EventConfirmationRequired`、`EventError` 才持久化。
  - transient 事件如 `assistant_delta`、`assistant_thinking_delta`、`tool_progress` 只发前端，不落库。
  - `assistant_message` 当前只落库，不转发给前端。
  - `ChatLogic.runSupervisor` 和 `ConfirmActionLogic` 都已改成持久化事件时单条 `Insert`，不再依赖 `InsertBatch`。
- 之前跑过并通过：
  - `go test ./services/aiagent/internal/eino`
  - `go test ./services/aiagent/internal/logic`
  - `go test ./services/aiagent/...`

注意：当前工作树仍是 dirty 状态，主要改动文件包括：

- `apis/ai/etc/ai-api.yaml`
- `apis/ai/etc/ai-api.prod.yaml`
- `services/aiagent/etc/aiagent.yaml`
- `services/aiagent/etc/aiagent.prod.yaml`
- `services/aiagent/internal/eino/agent.go`
- `services/aiagent/internal/eino/callbacks.go`
- `services/aiagent/internal/eino/callbacks_test.go`
- `services/aiagent/internal/logic/chatlogic.go`
- `services/aiagent/internal/logic/confirmactionlogic.go`
- `services/aiagent/internal/logic/confirmactionlogic_test.go`
- `services/aiagent/internal/tools/coupon_tools.go`
- `docs/model-context.md`

## 当前卡在哪

当前最大问题是：Agent final ownership 没有定义清楚。

现象：

用户看到的最终客服回答里混入了 supervisor 的中间过程，例如：

- 先根据工具结果做了一个“初步判断”。
- 又说“还需要 product_id，请补充信息”。
- 接着又自己继续调用试算工具。
- 最后再输出真正结论。

这些内容被拼成一个 assistant 输出，导致最终回答像是把“中间思考、阶段性计划、最终答案”全混在一起。

当前后端原因：

- `services/aiagent/internal/eino/callbacks.go` 的 `onModelEndWithStreamOutput` 会把 supervisor 暴露出来的 `chunk.Message.Content` 直接发成 `assistant_delta`。
- 但 supervisor 的一次 Agent run 中，模型可能多次输出普通 content：路由说明、工具前判断、阶段性总结、最终总结。
- callback 只知道“这一轮模型 stream 出了 content”，不知道这是不是整个顶层 run 的最终输出。
- `iter.Next()` 中 supervisor 的 `tool_call=0` assistant message 更像 final fact，但目前 logic 层对 `assistant_message` 只落库、不转发前端。

还有一个关联问题：

- 子 agent 是作为 tool 执行的。
- supervisor 调用子 agent 时，`chunk.Message.ToolCalls` 可能有值，但 `chunk.Message.Content` 为空。
- 所以不能依赖 `ToolCalls + Content` 作为稳定思考过程来源。
- 真正的 reasoning 来源是 `ReasoningContent` 或 `Extra["reasoning-content"]`，但这个内容目前可能是英文；中文那段更多来自普通 `Content`。

另一个已排查过的问题：

- trace `84200e07ce408b076851641f3615e0a2` 中，`aiagent.log` 显示 `ai supervisor returned no events`，不是工具失败。
- 该请求约 26 秒后结束，API 层 HTTP 200。
- `runSupervisor` 进入 `events == 0` 是因为它消费的是 Eino 包装后的 `out` channel，不是直接消费 `iter.Next()`。
- 如果 `consumeEvents` 没有成功 emit 任何 `domain.AgentEvent`，`out` 关闭后外层 `events` 仍为 0。
- 这不是单纯因为 `aiagent.rpc Timeout: 0`。`Timeout: 0` 更像 aiagent RPC server 层不主动设置总超时。

## 下一步计划是什么

优先不要继续零散改 callback。下一步应先确定 final ownership 方案，再写测试，再改代码。

建议方案：

1. 明确定义顶层 Agent run 的最终输出：
   - `RunCompleted.FinalOutput` 或等价的 `iter.Next()` 中 supervisor `assistant_message tool_call=0`，应是 final assistant fact 的唯一来源。
   - final assistant fact 必须落库。
   - 是否发送前端，需要根据流式策略明确决定。

2. 收敛 callback 职责：
   - `reasoningContent` 只发 `assistant_thinking_delta`。
   - tool callback 只发 `tool_progress` / `tool_result`。
   - callback 不应无条件把 supervisor `chunk.Message.Content` 发成 `assistant_delta`。
   - 如果要保留逐字最终输出，必须能确认当前 chunk 属于 final phase，否则宁可不发。

3. 设计 final streaming 策略，二选一：
   - 保守方案：前端不展示最终逐字流；只展示思考、工具、确认状态，等 `iter.Next()` final assistant 出来后再一次性发送最终回答。
   - 进阶方案：增加明确状态机/turn buffer，只有判断该 supervisor model turn 不会再触发 tool call 且是最终回答时，才把 buffer 作为 final delta 释放给前端。

4. 增加测试覆盖：
   - supervisor 先输出普通 content，再 tool_call，再 final assistant：前端不应把前面的 content 当最终回答。
   - 子 agent tool call 只有 `ToolCalls` 没有 `Content`：不能导致 thinking 丢失或误发 final。
   - `ReasoningContent` 进入 thinking，不进入 assistant final。
   - `iter.Next()` final assistant 进入落库。
   - 若没有任何可 emit 事件，应明确发 error，并记录 why，避免只有外层 `events == 0`。

5. 增强诊断日志：
   - 在 `consumeEvents` 中记录 iterator 事件类型、agent name、role、tool_calls 数量、content 是否为空。
   - 在 callback 中记录 exposed model name、是否有 reasoning、是否有 content、是否有 tool_calls。
   - 对 `emit(ctx, event)` 的错误不要全部忽略，至少 debug/error 打出来，尤其是 ctx canceled。

6. 再做一次真实模型对比测试：
   - 使用之前放在 `tests/` 下的真实 Agent stream compare 测试。
   - 观察 `output.Recv()` 和 `iter.Next()` 在多工具、多子 agent 场景下各自输出什么。

## 有哪些踩过的坑，绝对不要再踩

- 不要把 `output.Recv()` 的所有 `chunk.Message.Content` 都当最终 assistant 输出。它可能只是 supervisor 中间阶段内容。
- 不要把 ADK iterator 中的 Tool message 和 callback 工具结果同时转成 `tool_result`，否则前端会显示两张工具卡片。
- 不要做工具结果去重来掩盖重复。模型连续调用同一个工具两次可能是合法行为；重复的根因应该通过单一处理入口解决。
- 不要在 Eino 层直接写数据库。Eino 层没有用户消息 dedupe、`prepared.ClientMessageID`、持久化失败 SSE 策略和会话幂等上下文。
- 不要恢复 `RunRequest.OnEvent` / `ResumeRequest.OnEvent` 这种双路径事件 hook。事件统一走 `out` channel 更清楚。
- 不要认为 `iter.Next()` 有事件就等于前端会有事件。只有被转换并 `emit` 成 `domain.AgentEvent` 的事件，`runSupervisor` 才能收到。
- 不要忽略 `emit(ctx, event)` 错误。ctx canceled 时兜底错误可能发不出去，外层只看到 `events == 0`。
- 不要依赖 `ToolCalls + Content` 作为思考过程来源。子 agent 作为 tool 调用时常常只有 `ToolCalls`，没有 `Content`。
- 不要把英文 reasoning 直接展示给用户。要么 prompt/model 配置确保 reasoning 中文，要么前端/后端区分内部 reasoning 与用户可见状态。
- 不要把“优惠券已被领取”视为工具系统失败。它是业务上的成功响应或可解释业务状态，应该传给 LLM，让模型告诉用户已经领过，而不是触发工具失败。
- 不要把高风险操作落库放到发送之后，除非有明确批量保存和失败补偿策略。当前已经倾向于 durable 事件先落库再发送，避免前端看到成功但数据库没有事实。
- 不要把 `aiagent.rpc Timeout: 0` 当成这次 26 秒 `events == 0` 的直接原因。真正要查的是 Eino iterator/callback 为什么没有成功产出 domain event。
- 不要在声称完成前跳过测试。AI 客服相关至少跑：
  - `go test ./services/aiagent/internal/eino`
  - `go test ./services/aiagent/internal/logic`
  - `go test ./services/aiagent/...`

