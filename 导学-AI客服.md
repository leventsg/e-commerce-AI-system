# AI 客服系统导学

## 1. 前置知识

| 知识点 | 为何需要 | 在本项目中的位置 | 高频度 |
| --- | --- | --- | --- |
| go-zero API/RPC 分层 | 理解对外 SSE 网关和内部 Agent 服务为什么拆开 | `apis/ai`、`services/aiagent` | 高 |
| SSE 流式响应 | AI 客服需要边执行边向前端推送事件 | `apis/ai/internal/logic/chatlogic.go` | 高 |
| Eino Agent/Tool | 项目不是普通 LLM 调用，而是基于工具调用的 Agent 编排 | `services/aiagent/internal/eino`、`services/aiagent/internal/tools` | 高 |
| 工具元数据与执行网关 | 防止模型越权、误调用高风险工具、把失败说成成功 | `services/aiagent/internal/tools` | 高 |
| 高风险确认状态机 | 下单、取消订单、删除购物车等操作必须先让用户确认 | `services/aiagent/internal/confirmation` | 高 |
| 上下文工程 | 多轮客服不能无限塞历史，需要摘要、近期窗口、工具结果引用 | `services/aiagent/internal/memory`、`services/aiagent/internal/contextmanager` | 高 |
| 审计与幂等 | 写操作要可追踪，重复消息不能重复执行 | `dal/model/ai`、`services/aiagent/internal/audit` | 中高 |
| RAG 检索降级 | 普通客服问题可接知识库，但检索失败不能影响主流程 | `services/aiagent/internal/rag` | 中 |

## 2. 重点亮点与学习顺序

| 亮点标题 | 为什么重要 | 通用技术关键词 | 先看哪些文件 | 建议学习顺序 |
| --- | --- | --- | --- | --- |
| 流式对话网关 | 把 HTTP 请求转成可持续推送的 AI 事件流，是用户可感知入口 | SSE、流式 RPC、事件协议、鉴权 | `apis/ai/ai.api`、`apis/ai/internal/logic/chatlogic.go`、`apis/ai/internal/types/protocol.go` | 1 |
| 可扩展 Agent 编排 | 将自然语言请求路由到不同领域子 Agent，降低单 Agent 工具选择混乱 | Supervisor、SubAgent、Tool Calling、职责隔离 | `services/aiagent/internal/eino/agent.go`、`services/aiagent/internal/prompts/agent` | 2 |
| 工具执行安全边界 | 所有业务动作都经过统一元数据、参数校验、用户身份注入和审计 | Tool Registry、Execution Guard、最小权限、失败封装 | `services/aiagent/internal/tools/registry.go`、`services/aiagent/internal/tools/executor.go`、`services/aiagent/internal/tools/helper/tool_helper.go` | 3 |
| 高风险操作确认 | 将模型建议和真实写操作隔离，只有用户确认后才恢复执行 | 状态机、CAS、Redis 锁、Checkpoint Resume | `services/aiagent/internal/eino/approval.go`、`services/aiagent/internal/confirmation/manager.go`、`services/aiagent/internal/logic/confirmactionlogic.go` | 4 |
| 上下文与记忆治理 | 避免多轮对话 token 爆炸，同时保证工具事实和用户偏好可复用 | 滚动摘要、近期窗口、工具结果引用、异步记忆更新 | `services/aiagent/internal/memory`、`services/aiagent/internal/contextmanager`、`services/aiagent/internal/memoryupdate` | 5 |
| 可追踪数据闭环 | 对话、消息、工具调用、确认、运行态都有持久化记录，便于排查 | 审计、幂等、运行态恢复、数据隔离 | `dal/model/ai`、`services/aiagent/internal/audit/recorder.go`、`services/aiagent/internal/eino/checkpoint_store.go` | 6 |

## 3. 必备知识点 Checklist

- 能画出 `用户 -> apis/ai -> services/aiagent -> Eino Agent -> Tool -> 业务 RPC/API -> SSE` 的主链路。
- 能解释为什么 AI 客服不能直接相信模型输出里的 `user_id`。
- 能说明 Tool Registry 保存了哪些元数据：名称、风险等级、是否确认、超时、读写分类、RPC/API 映射。
- 能讲清高风险工具为什么先创建确认记录，再通过 Eino interrupt 中断。
- 能讲清 `pending -> approved/rejected/expired/executed/failed` 的状态变化和重复确认防护。
- 能说明上下文为什么采用“最近消息 + 滚动摘要 + 工具结果引用”，而不是无限历史。
- 能说明工具失败时为什么必须返回结构化失败结果，不能让模型总结成成功。
- 能指出当前仍需补强的点：全量测试、RAG 完整验收、观测与熔断治理、线上指标。

## 4. 推荐阅读

| 主题 | 通用技术点 | 建议阅读位置 | 预计时间 | 读完能回答什么 |
| --- | --- | --- | --- | --- |
| 产品范围 | AI 客服能力边界、确认策略、安全要求 | `docs/ai-customer-service-prd.md` | 15 分钟 | 这个项目做什么，不做什么 |
| 总体架构 | Agent 编排层如何接入现有系统 | `docs/ai-customer-service-design.md` | 25 分钟 | 为什么新增 `apis/ai` 和 `services/aiagent` |
| 实施状态 | 已完成任务、未完成任务、验收命令 | `docs/ai-customer-service-implementation-plan.md` | 30 分钟 | 哪些能力能放心讲，哪些要说待完善 |
| SSE 网关 | 请求校验、用户身份、事件转发、流式输出 | `apis/ai/internal/logic/chatlogic.go` | 20 分钟 | 前端怎么收到 assistant/tool/confirmation/error |
| Agent 编排 | Supervisor、SubAgent、Eino 事件消费 | `services/aiagent/internal/eino/agent.go` | 35 分钟 | 为什么不用单 Agent 直接挂全部工具 |
| 高风险确认 | interrupt、确认记录、恢复执行 | `services/aiagent/internal/eino/approval.go`、`services/aiagent/internal/confirmation/manager.go` | 35 分钟 | 用户点确认后，系统怎么保证只执行一次 |
| 工具注册与执行 | 元数据白名单、超时、重试、审计 | `services/aiagent/internal/tools/registry.go`、`services/aiagent/internal/tools/executor.go` | 30 分钟 | 工具失败、越权、重试怎么治理 |
| 上下文工程 | MemoryProvider、摘要、用户画像、工具事实 | `services/aiagent/internal/memory`、`services/aiagent/internal/contextmanager` | 40 分钟 | 多轮对话如何控 token 且保留关键事实 |
| 数据模型 | 对话、消息、工具调用、确认、运行态 | `dal/model/ai` | 20 分钟 | 排障时查哪些表 |
| RAG 扩展 | 检索触发、缓存、来源注入、失败降级 | `services/aiagent/internal/rag` | 20 分钟 | 知识库如何接入但不绑死主链路 |

## 5. 自学提醒

如果某个文件或原理看不懂，请继续追问 AI；本导学负责给学习路径与题目，不提供逐行讲解。建议学习时每看完一个模块，自己口述一次“问题 -> 机制 -> 本项目落点 -> 风险边界”，这样比背文件名更接近面试状态。

## 6. 项目技术定位

AI + 后端方向。依据是：该系统核心不是商品、订单、支付等电商后端本身，而是在现有业务服务之上新增 AI Agent 编排、SSE 对话、安全工具调用、高风险确认、上下文记忆和审计追踪能力。

## 7. 核心原理解析

1. 问题 -> 用户自然语言意图不稳定，单纯 API 映射无法覆盖多轮咨询和组合任务。  
   机制 -> 使用 Supervisor Agent 判断意图、拆分任务，并通过领域 SubAgent 绑定有限工具集合。  
   在本项目中的落点 -> `services/aiagent/internal/eino/agent.go` 与 `services/aiagent/internal/prompts/agent`。

2. 问题 -> 模型输出不可作为权限事实，用户也可能在请求体中伪造身份字段。  
   机制 -> 网关拒绝客户端提交 `user_id`，工具执行前从服务端认证上下文注入可信用户 ID。  
   在本项目中的落点 -> `apis/ai/internal/logic/chatlogic.go`、`services/aiagent/internal/tools/helper/tool_helper.go`。

3. 问题 -> 下单、取消订单等写操作不能由模型一次推理直接落库。  
   机制 -> 高风险工具先创建确认记录并中断 Agent；用户批准后基于 checkpoint 恢复原调用。  
   在本项目中的落点 -> `services/aiagent/internal/eino/approval.go`、`services/aiagent/internal/logic/confirmactionlogic.go`。

4. 问题 -> 多轮聊天历史、工具结果、用户偏好如果全部塞进模型，会带来 token 压力和事实污染。  
   机制 -> 使用近期消息窗口、滚动摘要、长期记忆事件、用户画像和按需工具结果读取。  
   在本项目中的落点 -> `services/aiagent/internal/memory`、`services/aiagent/internal/contextmanager`。

5. 问题 -> 工具调用失败后，模型可能倾向于生成“看似成功”的自然语言回复。  
   机制 -> 工具执行结果统一 envelope 化，失败以稳定错误码和安全提示进入 `tool_result`，并写入工具调用记录。  
   在本项目中的落点 -> `services/aiagent/internal/tools/executor.go`、`services/aiagent/internal/audit/recorder.go`。

6. 问题 -> AI 对话中的中断、确认和恢复需要跨请求保持状态。  
   机制 -> Redis 保存热 checkpoint，MySQL 的 AgentRun 和 Confirmation 作为持久事实来源。  
   在本项目中的落点 -> `services/aiagent/internal/eino/checkpoint_store.go`、`dal/model/ai/agent_runs`、`dal/model/ai/confirmations`。

## 8. 关键设计决策

| 备选 | 取舍 | 风险 | 验证 |
| --- | --- | --- | --- |
| 单 Agent 绑定全部工具 vs Supervisor + SubAgent | 选择后者，减少工具集合过大导致的误选，并让不同领域提示词隔离 | 编排复杂度上升，需要更多测试覆盖路由和事件转换 | 看 `iterator_events_test.go`、Agent prompt、工具绑定范围 |
| 模型直接执行写操作 vs 高风险确认中断 | 选择确认中断，避免用户未确认时产生不可逆业务动作 | checkpoint/确认状态需要持久化，失败恢复更复杂 | 看 `approval.go`、`confirmactionlogic_test.go` |
| 相信工具参数里的用户 ID vs 服务端注入用户 ID | 选择服务端注入，保证用户隔离 | 所有工具 handler 必须统一经过执行上下文 | 看网关拒绝 `user_id` 和 helper 上下文测试 |
| 全量历史进模型 vs 分层上下文 | 选择摘要 + 近期窗口 + 工具引用 | 摘要可能丢细节，写操作前必须重查业务事实 | 看 `memory_middleware.go`、`summary.go`、`tool_results.go` |
| 工具失败抛 Go error vs 结构化失败结果 | 选择结构化失败，避免 Agent 主流程失控并便于用户可见 | 错误分类需要持续治理 | 看 `executor.go`、`retry.go` |
| RAG 强依赖 vs 静默降级 | 选择检索失败不阻塞基础聊天 | 知识库不可用时回答信息量下降 | 看 `internal/rag` 与配置项 |

## 9. 量化与验证

- 已有源码证据能验证：AI 数据表、SSE 网关、工具注册、确认状态、上下文组件、RAG 客户端和多类单测已存在。
- 当前不要对外宣称“提升转化率/降低人工成本/响应缩短多少”，仓库未提供真实线上指标。
- 建议本地验证命令：`go test ./services/aiagent/...`、`go test ./apis/ai/...`。
- 建议完整验证命令：`go test ./...`。若依赖服务或网络阻塞，需要记录失败包和原因。
- 建议补测场景：模型伪造 `user_id`、重复 `client_message_id`、确认过期、确认并发点击、工具失败被总结为成功、RAG 检索失败降级。
