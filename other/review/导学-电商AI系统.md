# 电商AI系统项目导学

## 1. 前置知识

| 知识点 | 为何需要 | 在本项目中的位置 | 高频度 |
| --- | --- | --- | --- |
| go-zero API/RPC 分层 | 理解网关、RPC 服务、配置、生成代码和 ServiceContext 依赖装配 | `apis/**`、`services/**`、`services/*/etc/*.yaml` | 高 |
| 微服务电商交易链路 | 面试中高频追问下单、库存、优惠券、支付和订单状态流转 | `apis/order`、`services/order`、`services/checkout`、`services/inventory`、`services/coupons`、`services/payment` | 高 |
| DTM Saga 与补偿 | 创建订单跨 checkout、coupon、order 服务，需要解释一致性边界 | `apis/order/internal/logic/createorderlogic.go` | 高 |
| Redis 原子操作与延时任务 | 库存预扣、结算幂等、订单/支付超时都依赖 Redis | `services/inventory/internal/decreaselua`、`services/checkout/internal/logic/preparecheckoutlogic.go`、`common/consts/biz/order.go` | 高 |
| Kafka 与 Outbox | 订单取消、支付成功、超时关闭需要可靠事件投递 | `common/outbox/dispatcher.go`、`common/mq`、`services/order/internal/**`、`services/payment/internal/**` | 高 |
| Eino Agent 与 Tool Calling | AI 客服不是简单聊天，而是基于工具调用编排电商业务能力 | `services/aiagent/internal/eino`、`services/aiagent/internal/tools` | 高 |
| SSE 流式响应 | AI 客服对外以 POST + SSE 方式返回 assistant、tool、confirmation 事件 | `apis/ai/internal/logic/chatlogic.go`、`apis/ai/ai.api` | 高 |
| 高风险操作确认与幂等恢复 | AI 发起下单、取消订单、删除购物车时必须避免误执行和重复执行 | `services/aiagent/internal/confirmation`、`dal/model/ai/confirmations`、`services/aiagent/internal/eino/checkpoint_store.go` | 高 |
| 上下文工程与记忆 | 多轮客服需要控制 token、保留关键事实并避免提示注入污染 | `services/aiagent/internal/memory`、`services/aiagent/internal/contextmanager`、`services/aiagent/internal/eino/memory_middleware.go` | 中高 |
| 可观测性 | 压测、排障和链路定位需要理解 Prometheus、Grafana、Jaeger、结构化日志 | `construct/observability`、各服务 `Telemetry` / `DevServer` 配置 | 中 |

## 2. 重点亮点与学习顺序

| 亮点标题 | 为什么重要 | 通用技术关键词 | 先看哪些文件 | 建议学习顺序 |
| --- | --- | --- | --- | --- |
| 微服务交易一致性 | 下单链路跨多个服务，最容易被问事务边界、补偿和幂等 | Saga、补偿事务、本地事务、幂等 | `apis/order/internal/logic/createorderlogic.go`、`services/order/internal/logic/createorderlogic.go`、`services/checkout/internal/logic/preparecheckoutlogic.go` | 1 |
| 库存与结算可靠性 | 电商后端的核心风险是超卖、重复提交和过期资源释放 | Redis Lua、预扣库存、请求指纹、延时任务 | `services/inventory/internal/decreaselua/decreaselua.lua`、`services/inventory/internal/logic/decreasepreinventorylogic.go`、`services/checkout/internal/logic/preparecheckoutlogic.go` | 2 |
| 可靠事件投递 | 订单取消、超时、支付成功后需要通知库存、优惠券、结算等服务 | Outbox、Kafka、重试、消费补偿 | `common/outbox/dispatcher.go`、`services/order/internal/logic/cancelorderlogic.go`、`services/payment/payment.go` | 3 |
| AI 工具安全执行链路 | AI 工具调用必须受权限、风险、确认、审计约束，否则会变成不可信业务入口 | Tool Registry、Execution Guard、审计、错误分类 | `services/aiagent/internal/tools/catalog.go`、`services/aiagent/internal/tools/executor.go`、`services/aiagent/internal/tools/retry.go` | 4 |
| 高风险确认与中断续跑 | AI 下单/取消订单不能模型说执行就执行，需要用户确认和恢复原工具调用 | Stateful interrupt、Checkpoint、CAS、状态机 | `services/aiagent/internal/tools/approval_manager.go`、`services/aiagent/internal/confirmation/manager.go`、`dal/model/ai/confirmations/aiconfirmationsmodel.go`、`services/aiagent/internal/eino/checkpoint_store.go` | 5 |
| Agent 分域编排 | 复杂电商意图被拆到商品、订单、购物车结算、优惠券、用户等子 Agent | Supervisor、SubAgent、Tool visibility、Prompt 分层 | `services/aiagent/internal/eino/agent.go`、`services/aiagent/internal/prompts/agent` | 6 |
| SSE 事件桥接 | 前端需要实时看到思考、工具执行、确认和最终回答，后端要处理心跳和超时 | SSE、server-streaming RPC、事件映射 | `apis/ai/internal/logic/chatlogic.go`、`services/aiagent/aiagent.proto`、`services/aiagent/internal/domain/message.go` | 7 |
| 记忆与 RAG 上下文治理 | 多轮对话需要在成本、准确性、安全之间做取舍 | MemoryProvider、滚动摘要、长期记忆、RAG 缓存 | `services/aiagent/internal/eino/memory_middleware.go`、`services/aiagent/internal/memory`、`services/aiagent/internal/contextmanager`、`services/aiagent/internal/rag` | 8 |

## 3. 必备知识点

- [ ] 能画出用户下单：API -> DTM Saga -> checkout/coupon/order -> payment -> timeout/outbox 的链路。
- [ ] 能解释结算预订单为什么要有请求指纹、Redis 5 分钟幂等 key、库存预扣和超时释放。
- [ ] 能解释库存 Redis Lua 如何同时完成幂等检查、库存校验、扣减和预扣标记。
- [ ] 能说明订单创建为什么先通过 Saga 锁定优惠券与结算状态，再创建订单。
- [ ] 能说明订单取消为什么使用本地事务写订单状态，同时写 outbox 事件，再异步释放库存、优惠券、结算资源。
- [ ] 能解释 AI 客服为什么拆成 `apis/ai` 网关和 `services/aiagent` 编排 RPC 服务。
- [ ] 能说明 Tool Registry、Tool metadata、Executor、业务 RPC handler 的职责分界。
- [ ] 能讲清楚 `user_id` 为什么只能来自登录态，而不能来自客户端、模型参数或会话 metadata。
- [ ] 能讲清楚高风险操作的 pending/approved/rejected/expired/executed/failed 状态机。
- [ ] 能说明 Checkpoint 为什么要 Redis 热缓存 + MySQL 持久化兜底。
- [ ] 能解释 SSE 事件中的 `assistant_delta`、`assistant_message`、`tool_progress`、`tool_result`、`confirmation_required`。
- [ ] 能说明滚动摘要 30 -> 10 + 20 的上下文治理思路，以及为什么记忆不能覆盖 system prompt 和安全规则。

## 4. 推荐阅读

| 主题 | 通用技术点 | 建议阅读位置 | 预计时间 | 读完能回答什么 |
| --- | --- | --- | --- | --- |
| 项目总体架构 | 微服务拆分、API/RPC 分层、基础设施 | `go.mod`、`apis/**/api`、`services/**/**.proto`、`construct/depend/docker-compose.yaml` | 40 分钟 | 这个系统有哪些服务、如何通信、依赖哪些中间件 |
| 下单 Saga | 分布式事务、补偿、错误映射 | `apis/order/internal/logic/createorderlogic.go` | 40 分钟 | 为什么下单不是单库事务，失败后如何回滚 |
| 结算与库存预扣 | 幂等、预扣、快照、过期释放 | `services/checkout/internal/logic/preparecheckoutlogic.go`、`services/inventory/internal/logic/decreasepreinventorylogic.go`、`services/inventory/internal/decreaselua/decreaselua.lua` | 50 分钟 | 如何避免重复预结算和库存超卖 |
| 订单取消与事件通知 | 本地事务、Outbox、Kafka | `services/order/internal/logic/cancelorderlogic.go`、`common/outbox/dispatcher.go`、`services/checkout/internal/consumer/cancel_order`、`services/coupons/internal/consumer/cancel_order` | 50 分钟 | 取消订单后如何释放下游资源 |
| 支付链路 | 支付单幂等、订单状态推进、支付成功事件 | `services/payment/internal/logic/createpaymentlogic.go`、`services/payment/payment.go`、`services/payment/internal/event/payment_success.go` | 40 分钟 | 为什么支付要先查订单，如何处理重复支付单和回调 |
| AI API 网关 | SSE、鉴权、请求大小、心跳、超时 | `apis/ai/ai.api`、`apis/ai/internal/logic/chatlogic.go` | 35 分钟 | 为什么 Grafana/JMeter 压测时要关注完整 SSE 连接耗时与首事件耗时 |
| AI Agent RPC | server-streaming RPC、事件协议 | `services/aiagent/aiagent.proto`、`services/aiagent/internal/logic/chatlogic.go` | 35 分钟 | 网关和编排服务之间如何流式传递事件 |
| Eino 编排 | Supervisor、SubAgent、AgentTool、Tool Calling | `services/aiagent/internal/eino/agent.go`、`services/aiagent/internal/prompts/agent` | 60 分钟 | 为什么要分域 Agent，如何限制子 Agent 可见工具 |
| 工具执行治理 | schema、metadata、超时、重试、审计 | `services/aiagent/internal/tools/catalog.go`、`services/aiagent/internal/tools/registry.go`、`services/aiagent/internal/tools/executor.go`、`services/aiagent/internal/tools/retry.go` | 60 分钟 | 模型如何安全调用真实电商 RPC |
| 确认状态机 | 高风险确认、Redis 锁、MySQL CAS | `services/aiagent/internal/tools/approval_manager.go`、`services/aiagent/internal/confirmation/manager.go`、`dal/model/ai/confirmations/aiconfirmationsmodel.go` | 60 分钟 | 用户重复点击确认会不会重复下单，为什么 |
| 上下文与记忆 | MemoryMiddleware、摘要、画像、长期事件 | `services/aiagent/internal/eino/memory_middleware.go`、`services/aiagent/internal/memory`、`services/aiagent/internal/contextmanager` | 60 分钟 | 多轮上下文如何进入模型，如何避免无限历史 |
| 知识库检索 | RAG 分类、检索、Redis Vector 缓存、sources | `services/aiagent/internal/rag`、`services/aiagent/internal/domain/rag.go` | 45 分钟 | 普通客服知识什么时候检索，失败时如何降级 |
| 数据模型 | AI 会话、消息、工具、确认、checkpoint、画像 | `dal/model/ai/**`、`construct/depend/sql/init.sql` | 50 分钟 | 每类数据落在哪张表，索引服务什么查询 |
| 测试证据 | 单元测试、集成测试、边界场景 | `services/aiagent/internal/**/*_test.go`、`apis/ai/internal/logic/*_test.go`、`test/rpc/**` | 45 分钟 | 项目有哪些可验证的边界，而不是只靠口头描述 |

## 5. 自学提醒

如果某个文件或原理看不懂，请继续追问 AI；本技能负责给学习路径与题目，不提供逐行讲解。建议每读完一个主题后，用“我能不能从用户请求讲到数据库变更和补偿事件”为标准自测。

## 6. 项目技术定位

后端 + AI 工程方向：项目主体是 Go/go-zero 电商微服务系统，在交易、库存、优惠券、支付、审计等后端能力之上，新增基于 Eino 的 AI 客服编排层，通过 SSE、Tool Calling、确认状态机、上下文记忆和 RAG，把自然语言请求安全映射到真实电商业务动作。

## 7. 核心原理解析

### 7.1 微服务交易一致性

问题 -> 电商下单涉及结算状态、优惠券锁定、订单创建等多个服务，如果只用单库事务无法覆盖跨服务失败。

机制 -> API 层使用 DTM Saga 编排步骤和补偿：有优惠券时先锁券，再锁定结算状态，最后创建订单；任一阶段失败，由对应补偿接口释放已占用资源。

在本项目中的落点 -> `apis/order/internal/logic/createorderlogic.go` 构造 Saga；`services/checkout/internal/logic/updatestatus2orderlogic.go`、`services/coupons/internal/logic/lockcouponlogic.go`、`services/order/internal/logic/createorderlogic.go` 承接各自本地事务。

### 7.2 库存预扣与幂等

问题 -> 用户重复提交结算或并发购买时，普通读写库存容易出现重复扣减或超卖。

机制 -> 结算服务先生成预订单，使用请求指纹 Redis key 去重；库存服务通过 Lua 脚本原子检查库存、扣减库存、设置预扣标记；失败后由结算服务同步回滚预扣，超时场景再通过延时任务释放。

在本项目中的落点 -> `services/checkout/internal/logic/preparecheckoutlogic.go`、`services/inventory/internal/decreaselua/decreaselua.lua`、`services/inventory/internal/logic/decreasepreinventorylogic.go`。

### 7.3 可靠事件与资源释放

问题 -> 订单取消、订单超时、支付超时、支付成功等事件会影响多个服务，直接同步调用容易被某个下游失败拖垮。

机制 -> 服务在本地事务内更新核心状态并写 outbox 消息，后台 dispatcher 认领待发送消息投递 Kafka，失败按指数退避重试，消费者再释放库存、优惠券或推进订单状态。

在本项目中的落点 -> `common/outbox/dispatcher.go`、`services/order/internal/logic/cancelorderlogic.go`、`services/order/internal/delaytask/order_timeout_scanner.go`、`services/payment/payment.go`。

### 7.4 AI 工具安全执行

问题 -> LLM 生成的 tool call 不能被直接信任，否则模型可能伪造 user_id、调用未授权工具或把失败总结成成功。

机制 -> 工具先注册到统一目录，metadata 定义风险等级、是否写操作、是否需要确认、超时、RPC 映射；执行时通过上下文注入可信用户身份，清理敏感参数，按错误类型决定是否有限重试，并将工具结果封装成结构化 envelope 和审计记录。

在本项目中的落点 -> `services/aiagent/internal/tools/catalog.go`、`services/aiagent/internal/tools/registry.go`、`services/aiagent/internal/tools/tool.go`、`services/aiagent/internal/tools/executor.go`、`services/aiagent/internal/audit/recorder.go`。

### 7.5 高风险确认与中断续跑

问题 -> AI 发起创建订单、取消订单、删除购物车等操作时，既要让用户二次确认，又要确认后恢复原来的工具调用上下文。

机制 -> 高风险工具在 Eino middleware 中触发确认事件，先写入 confirmation 记录和 checkpoint 信息，再中断执行；用户确认后，服务端通过 confirmation_id 找回 checkpoint/interrupt，执行状态从 pending 条件更新为 approved，再 Resume 原工具调用，完成后标记 executed 或 failed。

在本项目中的落点 -> `services/aiagent/internal/tools/approval_manager.go`、`services/aiagent/internal/eino/approval.go`、`services/aiagent/internal/confirmation/manager.go`、`services/aiagent/internal/logic/confirmactionlogic.go`、`services/aiagent/internal/eino/checkpoint_store.go`。

### 7.6 流式事件桥接

问题 -> AI 客服需要实时展示模型思考、工具运行、工具结果、确认卡片和最终回答，但内部是 Agent iterator 与 RPC stream。

机制 -> `services/aiagent` 将 Eino ADK iterator 事件转成领域事件，`apis/ai` 再把 RPC stream 转成 SSE。网关设置 `text/event-stream`、禁缓存、心跳、最大 5 分钟请求超时，并校验事件 JSON。

在本项目中的落点 -> `services/aiagent/internal/eino/agent.go`、`services/aiagent/internal/eino/a2ui.go`、`services/aiagent/aiagent.proto`、`apis/ai/internal/logic/chatlogic.go`。

## 8. 关键设计决策

| 备选 | 取舍 | 风险 | 验证 |
| --- | --- | --- | --- |
| AI 直接调用业务 RPC vs 通过工具执行层调用 | 选择工具执行层，集中参数校验、身份注入、风险控制、审计和错误封装 | 多一层适配代码，需要维护工具目录 | 看 `Tool Registry` 是否覆盖所有业务工具，执行失败是否统一落 `tool_result` |
| 高风险操作直接执行 vs 先确认后恢复 | 选择确认状态机 + checkpoint 恢复，避免误下单和重复取消 | checkpoint 丢失或确认并发会导致恢复失败 | 覆盖重复确认、过期确认、跨用户确认、Redis 故障降级到 MySQL CAS |
| 同步调用下游释放资源 vs Outbox + Kafka | 选择可靠事件投递，订单主状态和事件写入保持本地一致 | 消费延迟，需要补偿和重试 | 检查 outbox 状态、Kafka 消费日志、资源释放最终状态 |
| 结算时直接扣库存 vs 预扣库存 | 选择预扣库存，创建订单和支付前占用库存，超时释放 | 预扣资源可能滞留 | Redis ZSET 超时扫描与 ReturnPreInventory 流程验证 |
| 全量历史注入模型 vs 摘要 + 近期窗口 | 选择滚动摘要、最近消息、工具事实按需读取 | 摘要可能丢失细节 | 对比多轮会话中关键事实是否可由摘要或 tool_call_result 找回 |
| callback 直接推用户事件 vs iterator 统一消费 | 选择 ADK iterator 作为唯一用户可见事件转换入口 | 需要处理多类 Eino 事件 | 测试 tool_progress、tool_result、assistant_message、error 是否稳定输出 |
| Prompt 写死在代码 vs 文件化管理 | 选择 `internal/prompts` 集中管理 | 文件和代码引用需要一致 | 变更 prompt 后跑 Agent 路由和工具选择测试 |

## 9. 量化与验证

- AI SSE 首事件：PRD 目标是首个业务事件 3 秒内；建议用 JMeter JSR223 SSE 精确采样器分别统计首个 SSE 事件、首个 `assistant_delta`、首个 `tool_progress`、工具执行平均耗时。
- SSE 连接上限：单次连接最长 5 分钟；验证 `apis/ai/internal/logic/chatlogic.go` 超时事件和 `event: done` 是否按预期返回。
- 工具超时：查询工具默认 3 秒，写工具默认 5 秒；验证 `services/aiagent/internal/tools/executor.go` 超时后是否返回失败 envelope，而不是由模型总结成成功。
- 工具重试：查询工具默认最多重试 2 次，写操作不自动重试；验证 `services/aiagent/internal/tools/executor_retry_test.go`。
- 确认幂等：重复确认、过期确认、跨用户确认必须拒绝；验证 `services/aiagent/internal/logic/confirmactionlogic_test.go` 和 `dal/model/ai/confirmations/aiconfirmationsmodel_test.go`。
- 下单一致性：创建订单失败时 Saga 补偿 checkout/coupon/order；建议用集成测试和数据库状态检查验证。
- 库存可靠性：并发结算时 Redis Lua 不允许重复预扣同一用户同一预订单；建议构造并发请求验证库存值和预扣标记。
- Outbox 可靠性：Kafka 失败时消息进入 retry/failed 状态；建议单测 `common/outbox/dispatcher.go` 或通过 mock producer 验证重试退避。
- 可观测性：Prometheus `/targets` 应全部 UP，Grafana dashboard 至少能展示 `up`、Go runtime 和 process 指标；业务指标需真实请求触发后验证。
- 待测：真实生产级 TPS、P95/P99、模型 provider 并发限制、端到端工具成功率、RAG 命中率、摘要 token 节省比例。
