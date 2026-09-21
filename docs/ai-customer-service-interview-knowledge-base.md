# AI 智能客服项目面试知识库

> 用途：供面试辅助智能体理解本仓库，并据此准备项目介绍、追问与源码定位。本文依据当前仓库代码和文档整理；它不证明生产上线、个人独立负责范围或业务指标。阅读时始终区分“代码已实现”“有代码但未接入主链路”“仅设计”。

## 0. 先读：事实边界与回答规则

1. **项目定位**：在现有电商微服务之上增加自然语言客服编排层，不替换商品、库存、订单、购物车、结算、优惠券、用户等业务服务。业务 RPC/内部 API 仍是业务事实来源。
2. **实际对外协议是 SSE**：`POST /douyin/ai/chat` 接收一次 JSON 请求，通过 `text/event-stream` 返回事件；内部是 go-zero server-streaming RPC。不要说当前聊天入口是 WebSocket。见 `apis/ai/internal/logic/chatlogic.go`。
3. **Agent 编排是 Eino ADK**：Supervisor 把领域 Agent 包装成 AgentTool；默认只传父模型生成的 `request`，领域 Agent 再通过自己的 MemoryMiddleware 读取受控上下文。见 `services/aiagent/internal/eino/agent.go`。
4. **工具不是模型直接访问 RPC**：模型只能调用注册的 Tool；本地 Registry、适配器、Executor、确认机制和 Handler 决定是否以及如何访问业务服务。
5. **人工确认只适用于标记为高风险的工具**；当前高风险工具包括删除购物车项、创建订单、取消订单。批准不是重新让模型猜参数，而是恢复中断的原工具调用。
6. **长期事件版 TaskState 还未落地**：`docs/ai-customer-service-task-state-design-and-implementation.md` 是目标设计。当前代码里仍有旧的工作流式 `domain.TaskState` 和注入插槽，但生产 `CustomerServiceProvider` 未配置 TaskStateStore。
7. **不得虚构**：用户的具体职责、团队人数、生产用户量、转化率、准确率、延迟改善、上线效果均未由仓库证明。遇到此类问题，回答“仓库中无法确认，需要补充个人或生产证据”。

资料优先级：**当前代码与测试 > 已记录的架构文档 > 实施计划勾选状态 > PRD 目标 > 本知识库总结**。若发生冲突，以代码为准，并指出文档可能滞后。当前工作区存在未提交改动，因此以下结论只代表阅读时的工作区状态，不代表某个正式发布版本。

## 1. 三种讲法

### 30 秒

这是一个基于 Go、go-zero 和 Eino ADK 的电商 AI 客服编排层。用户通过 SSE 聊天，AI Agent 负责理解问题并调用已有业务服务；Supervisor 把商品、订单、购物车结算、优惠券、用户和通用问题分给领域 Agent。项目重点是把模型的不确定性限制在受控工具边界内：可信用户身份注入、工具超时与审计、高风险操作人工确认，以及分层上下文和异步长期记忆。不能把模型输出当成业务执行成功。

### 1 分钟

系统入口在 `apis/ai`，负责鉴权、协议转换、SSE 心跳和事件转发；`services/aiagent` 负责会话持久化、Eino Agent 编排、工具执行、确认与记忆。正常请求先保存用户消息，再做可选 RAG，随后启动 Supervisor。Supervisor 通过 AgentTool 调用领域 Agent，领域 Agent 只暴露本领域工具。工具调用经过 Registry 和 Executor，从认证上下文获取用户 ID，调用已有 RPC 或订单内部 API，并记录真实结果。高风险工具先创建确认记录并通过 Eino StatefulInterrupt 暂停；用户批准后按 checkpoint/interrupt 恢复，拒绝则由服务端直接收口。上下文由 MemoryProvider 在模型调用前聚合近期消息、摘要、工具事实、画像和长期事件，回复完成后异步滚动摘要并更新长期记忆。

### 3 分钟展开顺序

按“业务问题 → 组件边界 → 两条关键时序 → 安全可靠性 → 上下文工程 → 现存缺口”讲。先说明不替换业务微服务；再画出 SSE 网关、AI RPC、Supervisor/子 Agent、工具、业务服务；重点展开普通工具链与高风险确认链；最后讲分层记忆、失败语义和仍未实施的 TaskState 投影。不要把所有细节一口气堆进开场。

## 2. 项目要解决的问题与非目标

现有电商系统已经有商品、库存、订单、购物车、结算、优惠券、用户等能力，但用户需要理解菜单和流程才能完成咨询与操作。AI 客服提供自然语言入口，将“帮我查订单”“推荐一款耳机”“把商品加购物车”“取消订单”等请求转为受控业务调用。产品目标还包括连续对话、历史会话、商品推荐、工具可审计和高风险操作确认。出处：`docs/ai-customer-service-prd.md`。

这不是“让大模型直接管理订单”的系统。模型负责意图理解、工具选择和解释；商品价格、库存、订单状态与操作结果必须来自现有业务服务。支付、退款、账号删除和改密码不应被表述为当前已实现的 AI 工具。

## 3. 架构地图：谁负责什么

```text
浏览器/客户端
  │ POST /douyin/ai/chat，接收 SSE
  ▼
apis/ai：鉴权、参数校验、SSE 格式/心跳/超时、RPC 事件转发
  │ Chat / ConfirmAction server-streaming RPC
  ▼
services/aiagent
  ├─ Conversation Manager：会话归属、消息持久化、幂等重放
  ├─ RAG Service：必要时检索知识库
  ├─ Eino Supervisor：领域路由、协调与最终回复
  │    ├─ product_agent
  │    ├─ order_agent
  │    ├─ cart_checkout_agent
  │    ├─ coupon_agent
  │    ├─ user_agent
  │    └─ general_agent
  ├─ MemoryProvider/Middleware：模型调用前组装上下文
  ├─ Registry/Executor：工具白名单、参数边界、超时、重试、记录
  ├─ Approval/Confirmation：高风险确认与中断恢复
  └─ MySQL/Redis/Kafka：持久事实、checkpoint 热缓存、异步记忆更新
       │
       ▼
现有业务 RPC / 订单内部 API：业务真相与实际写入
```

| 层 | 主要职责 | 不能承担的职责 |
| --- | --- | --- |
| `apis/ai` | 身份入口、JSON/SSE 协议、流式转发、历史接口 | 不决定工具权限、不直接操作订单 |
| `services/aiagent/internal/logic` | 请求时序、Agent 事件持久化、确认决策编排 | 不把模型推测当业务结果 |
| `internal/eino` | ChatModel、AgentTool、ReAct、Middleware、interrupt/checkpoint | 不把 Eino 类型泄漏到业务服务 |
| `internal/tools` | 工具定义、可见性、执行和风险边界 | 不自行成为订单等业务事实源 |
| `internal/memory` / `contextmanager` | 上下文读取、摘要、画像、事件存储 | 不决定实时库存、订单状态 |
| 业务服务 | 查询和写入真实业务状态 | 不接触 Eino 模型协议 |

## 4. 一次普通聊天如何运行

1. 网关从已认证的 HTTP Context 获取用户 ID，拒绝客户端提交 `user_id`；校验请求类型和内容。对外采用 `fetch + ReadableStream` 可消费的 POST SSE，而不是浏览器原生 EventSource 的 GET 模式。见 `apis/ai/internal/logic/chatlogic.go`。
2. `ChatLogic` 通过 Conversation Manager 准备会话、保存原始用户消息，并用 `client_message_id` 处理重复请求。重复请求走既有回答重放，不再次发起 Agent 执行。见 `services/aiagent/internal/logic/chatlogic.go`、`internal/conversation/manager.go`。
3. 可选 RAG 先做分类，判断是否需要知识库；失败不阻断普通聊天。RAG 结果只服务当前轮。见 `internal/rag/service.go`。
4. Runner 只接收当前用户消息，并通过 Eino session values 携带可信会话元信息。Supervisor 模型调用前，MemoryMiddleware 从 Provider 加载历史和动态上下文。见 `internal/eino/agent.go`、`internal/eino/memory_middleware.go`。
5. Supervisor 可以自己回复，或把任务以 AgentTool 的 `request` 参数交给领域 Agent。子 Agent 有独立提示词、工具白名单和 Middleware。见 `internal/eino/agent.go`。
6. ADK iterator 输出 reasoning、工具进度、工具结果、确认请求、错误和最终回答等事件。只有符合条件的 Supervisor final assistant 被当作最终用户回答；中间推理不冒充最终回复。见 `internal/eino/agent.go`、`internal/logic/chatlogic.go`。
7. durable 事件先落库，再由 RPC 流和 SSE 推给客户端。网关每 10 秒发送 `: ping`，单连接最长 5 分钟。见 `apis/ai/internal/logic/chatlogic.go`。
8. 正常回复结束后异步检查是否需要滚动摘要和长期记忆更新，不阻塞这次回答。

面试官追问“是真流式吗”：可以说服务端使用 Eino ADK iterator → server-streaming RPC → SSE 逐事件 flush；但要区分“事件级流式”和“最终自然语言逐 token 流式”。当前代码会转发 reasoning/tool progress 等事件，最终自然语言以 durable `assistant_message` 为准，不应声称最终回答每个 token 都实时推送。

## 5. Supervisor、子 Agent 与上下文隔离

Supervisor 是路由和汇总层；商品、订单、购物车结算、优惠券、用户资料及地址、通用问题各有领域 Agent。每个子 Agent 有自己的 Instruction 和允许调用的业务工具。项目使用 `adk.NewAgentTool(ctx, subAgent)` 包装，不打开 `WithFullChatHistoryAsInput()`；按 Eino 默认行为，父 Agent 生成 `request` 传给子 Agent，而不是自动转交父 Agent 全量历史。

为了避免子 Agent 只拿到孤立的 `request`，项目给 Supervisor 和各子 Agent 都挂了以 `AgentName` 区分的 MemoryMiddleware。子 Agent 在自己的模型调用前再读取可信用户/会话上下文。这里的安全分工是：父模型生成的 `request` 是任务描述，不是可信身份或业务事实；用户 ID 来自认证与运行上下文；真实订单等由本领域工具核实。

当前“按 Agent 区分”主要体现在 Middleware 能拿到 `AgentName`、系统提示词不同、工具集合不同，长期事件检索关键词也参考 AgentName；但长期事件的类型白名单、画像字段级裁剪和严格的 Agent 专属 TaskState 投影尚未实施。不能描述为已实现精细化 ABAC 式记忆权限。源码：`internal/eino/agent.go`、`internal/eino/memory_middleware.go`、`internal/memory/customer_service_provider.go`。

## 6. 工具从注册到执行

```text
统一 Tool Catalog
  → Registry（名称、Schema、metadata、可见性）
  → Eino InvokableTool Adapter（模型参数 → 服务端执行请求）
  → 高风险确认拦截（必要时 interrupt）
  → Executor（超时、失败分类、只读重试、结果 envelope、记录）
  → Handler（转换参数并调用业务 RPC/内部 API）
  → 结构化结果回到 Agent
```

`core.Tool` 是统一定义：名称、描述、参数 Schema、业务/能力工具类型、可见性、metadata、重试策略、Handler、可选确认摘要。启动时由业务工具 catalog 和能力工具 catalog 构造 Registry。Registry 分发 root 可用的能力工具与子 Agent 本领域工具，并输出 Eino ToolInfo；没有注册的名称不能通过正常 Registry 找到 Handler。见 `internal/tools/core/types.go`、`internal/tools/catalog.go`、`internal/tools/capability_catalog.go`、`internal/tools/registry.go`。

Eino 适配器解析 JSON 参数，但从 ToolExecutionContext 取得可信用户 ID；模型参数中的身份不能成为 RPC 的身份。Executor 还会剔除敏感键，按 metadata 选择超时，运行 Handler，记录工具名、脱敏参数、状态、真实结果/错误与耗时。结果封装包含 `status`、`business_outcome`、尝试次数和结构化 `result`。见 `internal/tools/tool.go`、`internal/tools/executor.go`。

读工具遇到可判定为暂时性的失败时可有限退避重试；写工具默认不自动重试。尤其写 RPC 超时可能是“结果未知”，不能简单告诉用户失败并再次执行；成功写入但审计记录失败时会返回“业务已执行”的失败 envelope，防止用户盲目重试。见 `internal/tools/retry.go`、`internal/tools/executor.go`。

术语校准：`Execution Guard` 在设计文档中是安全边界的统称；当前代码没有一个单独名为 `ExecutionGuard` 的类。可信上下文、Registry metadata、适配器、Executor、ApprovalManager 和业务 Handler 共同实现该边界。`Tool Gateway` 也不要误讲成独立对外服务；它可用于概括“模型工具调用进入业务系统前的统一入口”。

### 风险分级举例

| 类别 | 示例 | 处理 |
| --- | --- | --- |
| 查询/推荐 | 商品搜索、订单查询、库存查询 | 不需确认；结果以业务查询为准 |
| 低风险写 | 添加/减少购物车、领取优惠券、更新用户资料、地址操作 | 不需确认；仍需可信身份、超时和记录 |
| 高风险写 | 删除购物车项、创建订单、取消订单 | 先确认；批准后恢复原执行 |

“是否需要确认”由 metadata 和确认摘要能力共同决定，不能依赖模型在话术里说“请确认”。见 `internal/tools/registry.go`。

## 7. 高风险人工确认：真正的安全闸

### 7.1 创建确认

子 Agent 选择高风险工具后，Approval middleware 在 RPC 执行前拦截。ApprovalManager 校验工具允许确认，生成面向用户的操作摘要，并写入 `ai_confirmations`：用户、会话、工具、脱敏参数、摘要、状态、过期时间、run/checkpoint 信息。随后通过 Eino StatefulInterrupt 暂停原工具调用，将 `confirmation_required` 事件发给前端。此时业务写 RPC 尚未执行。源码：`internal/eino/approval.go`、`internal/tools/approval_manager.go`、`internal/confirmation/manager.go`。

### 7.2 用户决策

用户以 `confirm_action` 提交 `confirmation_id` 和 `approved`。网关仍只认认证用户 ID；Confirmation Manager 校验用户、会话、状态与过期时间，并用 Redis 锁加数据库条件更新使 `pending` 只能被一个决策抢占。跨用户、过期、重复、已拒绝或已执行请求不能继续执行。拒绝时服务端直接完成拒绝结果，不恢复 LLM 和工具。见 `internal/logic/confirmactionlogic.go`、`internal/confirmation/manager.go`。

### 7.3 批准与恢复

批准后，服务端按确认记录绑定的 `checkpoint_id + interrupt_id` 调用 Eino `ResumeWithParams`，恢复原先暂停的那次工具调用，再经同一工具执行链路访问业务服务。checkpoint Redis 热存储、MySQL `ai_agent_runs.checkpoint_blob` 回退；这解决的是确认等待期间的执行现场恢复，不代表完整长任务状态机。业务结果产生后，确认记录进入 `executed` 或 `failed`；具体对账异常需看真实业务结果，不能把确认状态当作业务真相。见 `internal/eino/agent.go`、`internal/eino/checkpoint_store.go`、`internal/logic/confirmactionlogic.go`。

面试时可用一句话概括：“确认不是让模型问一遍‘是否确定’，而是在业务 RPC 前中断原调用，保存有期限的服务端确认事实，用户决策后按同一恢复点继续；身份、时效和重复执行由服务端校验。”

## 8. 上下文工程：模型最终看到什么

模型输入不是全量历史，而是分层读取、临时组装：

```text
Agent 专属 System Prompt
摘要水位之后的近期 user/assistant 历史（默认最多 12 条）
当前 User Message
  -----
  <current_time>...</current_time>
  <conversation_context>会话滚动摘要</conversation_context>
  <latest_tool_result>最近真实工具结果</latest_tool_result>
  <recent_tool_calls>近期工具引用</recent_tool_calls>
  <task_state>当前未实际接入</task_state>
  <user_memory_recent_events>近期长期事件</user_memory_recent_events>
  <user_memory_relevant_events>按当前消息/AgentName 召回的事件</user_memory_relevant_events>
  <user_profile>用户画像</user_profile>
  【知识库参考】可选 RAG 片段
```

不存在的数据块会省略。`MemoryProvider` 负责读取和初步组织；`MemoryMiddleware.BeforeModelRewriteState` 把近期历史插在系统指令后，把动态块追加到当前 User Message，原始用户消息不因此改写入库。`ContextBuilder` 目前不是独立类，其组装职责分散在 Provider 与 Middleware。Middleware 做同一 Agent/运行中的防重复注入；本段描述以当前代码行为为准。见 `internal/memory/customer_service_provider.go`、`internal/eino/memory_middleware.go`。

### 各类记忆的职责

| 内容 | 回答的问题 | 存储/来源 | 更新时机 |
| --- | --- | --- | --- |
| 最近消息 | 刚才具体说了什么 | `ai_messages` | 每次 durable 消息写入 |
| 会话摘要 | 本会话较早内容的要点 | `ai_conversation_summaries` | 未摘要消息达阈值后滚动生成 |
| 工具事实 | 工具实际返回什么 | `ai_tool_calls` | 每次工具执行记录 |
| 用户画像 | 当前相对稳定的偏好 | `ai_user_profiles` | 摘要压缩后异步抽取 |
| 长期事件 | 跨会话值得记住的历史 | `ai_user_memory_events` | 摘要压缩后异步抽取 |
| RAG | 平台/商品知识是什么 | 外部知识库 | 当前请求按需检索 |
| 新语义 TaskState | 哪些长期事件与本次任务相关 | 目标设计：事件动态投影 | 目标设计：每次 Agent 调用前 |

摘要策略是未摘要消息达到 30 条时，将最早 10 条与旧摘要合并，留下 20 条未压缩消息；在线 Provider 默认读取最多 12 条近期原文，两者不要混成同一个数字。摘要水位保证同一原文不会同时以“摘要”和“近期消息”重复注入。见 `internal/contextmanager/summary.go`。

更早的工具完整结果可通过 `get_tool_call_result` 按工具调用 ID、当前用户和会话读取；更早的长期事件可用 `search_user_memory` 按需查询。主动检索工具并不意味着它们可绕过用户隔离。见 `internal/tools/capability/`。

### 当前长期事件与新 TaskState 设计的差距

现在 Provider 固定注入最近 5 条长期事件，再用最近用户消息和 AgentName 的关键词检索最多 5 条相关事件，并做简单去重；这还不是“只注入当前任务相关事件”。当前 `ai_user_memory_events` 字段较少，近期事件查询的 SQL 未过滤 `status=active`，而关键词检索有该过滤；如果后续引入删除/过期语义，必须先补此缺口。见 `internal/memory/customer_service_provider.go`、`internal/memory/event_store.go`。

目标方案把 `TaskState` 定义为面向当前任务的客户连续性视图，复用长期事件事实表，经用户隔离、有效性、Agent 范围、任务相关性和数量预算筛选后生成 `<task_state>`。它不再存长任务步骤，也不替代订单、库存等实时事实。**该方案目前只是文档，不可写成已完成的个人实现。**见 `docs/ai-customer-service-task-state-design-and-implementation.md`。

## 9. 记忆如何更新

回复正常完成后，`ChatLogic.updateConversationMemory` 异步调用 SummaryManager。只有真正生成新摘要才发布一条 `AiMemoryUpdates` Kafka 消息，消息携带本批被压缩的 message IDs。Profile consumer 和 Memory Event consumer 分别基于这批消息运行结构化抽取、校验并写入 `ai_user_profiles` 与 `ai_user_memory_events`。在线 `MemoryProvider.Memorize` 当前不是这条更新主链路。见 `internal/logic/chatlogic.go`、`internal/memoryupdate/`、`internal/consumer/profile_update/`、`internal/consumer/memory_event_update/`。

长期事件抽取会校验类型、日期、置信度、证据消息所属用户/会话，以及敏感内容；模型只提出候选，服务端决定是否保存。当前实现仍缺少设计文档要求的业务成功后即时事件、成熟的去重/替代/过期/遗忘闭环，因此不能声称“重大事件实时写入且可完整治理”。见 `internal/memoryeventextractor/extractor.go`、`internal/memory/event_store.go`。

## 10. RAG、记忆与工具为什么分开

RAG 解决“外部知识怎么回答”，长期记忆解决“这个用户过去发生过什么”，业务工具解决“现在真实状态是什么”。三者不能互相代替。例如用户问“上次坏掉的耳机现在还有替代品吗”：长期事件帮助理解“上次坏掉”；RAG 可提供选购知识；商品/库存工具决定当前可售商品与库存。

当前 RAG 在进入 Agent 前分类，只有达到配置阈值才做 Embedding、Redis Vector 缓存和知识库检索；失败静默降级。结果以当前轮 `RAGContext` 注入，回答 sources 可随最终 assistant 消息 metadata 保存，供前端展示。是否实际启用取决于配置与外部知识库，不应仅因代码存在就声称生产始终开启。见 `internal/rag/service.go`、`internal/logic/chatlogic.go`。

## 11. 数据、审计与故障语义

| 主要表 | 保存什么 | 面试说明 |
| --- | --- | --- |
| `ai_conversations` | 会话归属和基础信息 | 历史会话要按认证用户过滤 |
| `ai_messages` | 用户、助手、工具消息 | 原始事实；模型动态上下文不应写回原文 |
| `ai_tool_calls` | 工具入参、状态、返回、错误、耗时 | 排查与审计依据；不能由自然语言回复反推成功 |
| `ai_confirmations` | 高风险确认状态、参数摘要、恢复目标 | 决策事实和防重复边界 |
| `ai_agent_runs` | checkpoint 等运行数据 | 服务确认中断恢复；非完整 TaskState |
| `ai_conversation_summaries` | 滚动摘要和覆盖水位 | 与近期原文互斥 |
| `ai_user_profiles` | 稳定画像 JSON | 来源于聊天异步抽取 |
| `ai_user_memory_events` | 用户长期事件 | 当前为较简化事件模型 |

最关键的失败语义：

- 模型初始化或运行失败：明确提示稍后重试，不编造业务结果。
- RAG、摘要、画像或长期事件读取失败：尽量降级到当前消息和近期会话，而不让可选记忆阻塞基础对话。
- 工具失败：返回结构化失败与安全提示，不能总结为成功。
- 写操作超时：可能是“执行结果未知”，不能自动重试或断言未执行。
- 业务已执行但审计或消息保存失败：向用户明确“可能/已经产生业务结果，请勿重复操作”，需要业务事实核对。
- 确认已批准但恢复点缺失：不直接构造新的写工具调用；返回失败/重新发起提示。

## 12. 面试高频追问速答

**Q1：为什么需要 Supervisor + 子 Agent，而不是一个大 Agent？** 让路由、领域工具权限和提示词边界分离，避免单个模型同时看到全部业务工具。代价是委派信息可能丢失，所以子 Agent 通过自身 Middleware 补充受控上下文。

**Q2：父 Agent 会把全部上下文传给子 Agent 吗？** 不会。当前 `NewAgentTool` 默认传父模型生成的 `request`；子 Agent 在模型调用前依据可信 session values 与 AgentName 自行加载上下文。不要把 `request` 当身份事实。

**Q3：Execution Guard 是哪个文件？** 设计上的统一安全边界，不是单一同名结构体；适配器取可信执行上下文，Registry 控制工具，Executor 执行超时/错误/审计，Approval 拦截高风险，Handler 再调用 RPC。

**Q4：高风险操作如何确保确认前不执行？** 在工具调用前创建确认并发出 StatefulInterrupt；没有批准就不会进入业务写 RPC。批准后绑定原 checkpoint/interrupt 恢复；拒绝服务端直接结束。

**Q5：用户连点两次确认怎么办？** Confirmation Manager 通过用户/会话/状态/过期校验、锁与数据库条件更新争夺唯一 `pending` 决策；已处理确认不能再次进入执行。应避免把这说成跨业务服务绝对 exactly-once 保证。

**Q6：进程在确认期间崩溃怎么办？** checkpoint 在 Redis 热缓存、MySQL 持久回退；确认记录保存恢复目标。恢复时先校验确认的归属和状态。若外部业务写操作结果未知，不能仅靠 checkpoint 推断成功，需要业务侧查询/幂等机制对账。

**Q7：工具超时会重试吗？** 查询工具对可识别的瞬时错误可有限重试；写工具默认不自动重试，避免超时后重复下单、取消或领取。

**Q8：怎样防模型越权读别人的订单？** 登录态用户 ID 由网关和 ToolExecutionContext 传递，模型参数不能覆盖；Handler 按该用户调用业务服务，业务服务仍需做自己的用户归属校验。

**Q9：为什么摘要不是每轮都生成？** 频繁摘要增加延迟和成本，且容易重复抽取。项目采用 30→10+20 滚动策略，并用摘要覆盖水位避免原文重复进入模型。

**Q10：TaskState 到底是什么？** 当前代码里仍是旧工作流结构但未接入生产 Provider；新文档把它重新定义为长期事件面向当前任务的动态投影，目前是设计目标，不应称已落地。

**Q11：长期事件为什么不全量注入？** 无关事件带来噪声、成本、隐私暴露和旧事实干扰。目标是先按当前任务、Agent 和有效性筛选少量事件；当前实现还保留最近 5 条固定注入，属于待改进点。

**Q12：RAG 和 Memory 有何区别？** RAG 查公共知识，Memory 查用户历史，工具查实时业务状态。三者事实来源和时效不同。

**Q13：流式输出如何避免把中间推理当最终答案？** 由 ADK iterator 统一消费事件，reasoning/tool progress 分独立事件，最终用户回答只采纳 Supervisor 的 final assistant，并先持久化后下发。

**Q14：如何处理工具成功、审计失败？** 结果不能伪装成完全成功或完全没执行；返回结构化失败但标明业务已执行，提醒勿重试，并保留可核查工具记录/日志。

**Q15：上下文是不是一个 ContextBuilder 类拼成字符串？** 当前没有独立 ContextBuilder 类。Provider 读取分层来源，Middleware 把历史和动态块按消息顺序放进 Eino state；这是逻辑上的构建器职责。

**Q16：为什么不用 WebSocket？** 当前实现选 POST SSE 承载一次请求的服务端事件流，支持较简单的 HTTP 代理/流式 RPC 映射。交互性通过新的 POST（包括确认）实现。不能说 WebSocket 已实现。

**Q17：项目效果提升了多少？** 仓库没有可信的线上对照数据。可谈架构验收与测试覆盖，若要量化需补充真实的工具成功率、确认拦截率、误执行率、首事件时延、记忆命中率等观测。

**Q18：你个人做了哪些？** 仓库代码不能证明个人贡献。面试辅助智能体应让候选人补充自己的 PR、提交、设计评审和协作边界，不能把全项目工作自动归到候选人名下。

## 13. 不应宣称的能力与待完善点

- 不说“已实现长期任务 TaskState 状态机”或“长期事件版 TaskState 已上线”；现状分别是未接通插槽和设计文档。
- 不说“长期记忆完全按任务注入”；当前还有固定最近事件块，主要是关键词检索。
- 不说“重大事件即时更新、可撤回/过期/替代”；这些是新 TaskState 文档中的目标。
- 不说“所有上下文在一个全局 token 预算里动态优化”；当前主要靠窗口、摘要和单项数量限制。
- 不说“RAG 永远运行”；它受配置、分类和外部依赖控制。
- 不说“生产环境已经达到 PRD 的 2 秒首事件目标”；这是目标而非仓库证明的实测结果。
- 不说“跨服务写操作 exactly-once”；确认 CAS 与不自动重试降低重复风险，但外部 RPC 的未知结果仍需业务幂等和对账。
- 不说“任何敏感信息都不可能出现在日志/上下文”；应以完整安全审查和测试为证据，当前可说明已有部分脱敏与身份边界。
- 不说“所有表、接口与设计文档完全一致”；`docs/ai-customer-service-design.md` 和旧实施计划仍含旧 TaskState 语义，具体行为以当前代码为准。

## 14. 建议的源码阅读顺序

1. `docs/ai-customer-service-prd.md`：先理解业务范围与确认分级。
2. `apis/ai/internal/logic/chatlogic.go`：看真实对外 SSE 协议和身份边界。
3. `services/aiagent/internal/logic/chatlogic.go`：看一次聊天的服务端时序。
4. `services/aiagent/internal/eino/agent.go`：看 Supervisor、AgentTool、子 Agent 和 iterator。
5. `services/aiagent/internal/tools/core/types.go`、`registry.go`、`tool.go`、`executor.go`：看工具注册到执行的边界。
6. `services/aiagent/internal/eino/approval.go`、`internal/confirmation/manager.go`、`internal/logic/confirmactionlogic.go`：看确认中断恢复。
7. `services/aiagent/internal/memory/customer_service_provider.go`、`internal/eino/memory_middleware.go`：看最终模型上下文。
8. `services/aiagent/internal/contextmanager/summary.go`、`internal/memoryeventextractor/extractor.go`、`internal/profileextractor/extractor.go`：看异步记忆闭环。
9. `services/aiagent/internal/rag/service.go`：看按需知识库检索。
10. `docs/ai-customer-service-task-state-design-and-implementation.md`：只作为下一阶段设计，不当作代码事实。

若某文件或原理看不懂，请继续追问 AI；这份知识库提供学习路径、事实边界和面试题，不提供逐行源码讲解。

## 15. 验证与取证清单

面试前建议以当前工作区重新验证，而非引用旧实施计划中的勾选：

```bash
go test ./services/aiagent/...
go test ./apis/ai/...
go test ./...
```

还应手动取证：一条普通查询的 SSE 事件序列、一条高风险确认前/批准后工具调用记录、一条重复确认拒绝记录、跨用户历史读取拒绝、模型不可用降级、RAG 失败降级、摘要生成前后上下文变化。文档创建时未运行这些验收命令，因此此处均是**建议验证项**，不是已通过声明。

可量化指标建议但尚未给出数据：首业务事件时延、最终回答时延、工具成功/失败/未知结果占比、高风险确认拦截率、重复确认拒绝数、记忆提取有效率、长期事件注入相关率、RAG 命中与降级率、平均模型输入 token。没有实测就不要编百分比。

## 16. 给面试辅助智能体的使用指令

1. 先问候选人其**个人负责范围**和可验证证据，再把项目能力转换为第一人称经历。
2. 回答时先给业务目的，再讲关键机制、约束和取舍，最后落到实际代码证据或待验证点。
3. 若被问当前实现，以本知识库的“当前代码”叙述；若被问改进方案，可以引用长期事件版 TaskState，但必须明确“尚未实施”。
4. 对结果未知、跨用户、重复确认、写操作超时等高风险问题，优先解释保守语义，不为了说得漂亮而宣称绝对保证。
5. 不把设计文档、测试名、PRD 目标转换成生产上线事实；不能确认时直接说不知道并指出应查的源码、日志或指标。
