# 蓝松松｜AI 应用工程师简历内容与证据审计

## 一句话岗位定位

稳妥版：Go 后端基础扎实、具备真实业务 Agent 落地经验的 2027 届 AI 应用工程师。

进取版：能够贯通 Eino 多 Agent 编排、Tool/Skill/MCP 能力扩展、上下文记忆、安全确认与评估回归的 AI 应用工程师。

进取版需要在面试中展示或讲清：Skill 定义与加载方式、MCP 接入边界、评估集结构、Redis 记忆缓存策略及典型失败案例。

## 简历顶部摘要

深圳大学计算机技术硕士（2027 届），具备 Go 微服务与 LLM Agent 应用落地经验。基于 Eino 构建面向真实电商业务的多 Agent 智能客服，覆盖工具调用、分层记忆、Skill/MCP 扩展、安全确认与评估回归；有 Golang 后端实习经历，能够完成从模型编排到业务 RPC、缓存、消息队列和可观测性的端到端交付。

## 教育背景

- 深圳大学｜计算机技术｜硕士｜2024.09—2027.07｜GPA：3.69/4.0｜二等学业奖学金 2 次
- 西南科技大学｜软件工程｜本科｜2020.09—2024.07｜GPA：3.43/5.0

## 项目经历成稿

### go-mall 智能电商 Agent｜AI 应用工程｜2025.12—至今

技术栈：Go、go-zero、Eino ADK、SSE、MySQL、Redis、Kafka、DTM、Elasticsearch、Gorse、Docker、Prometheus、Grafana、Jaeger

项目简介：面向商品、库存、订单、购物车、结算和优惠券等真实电商业务构建可执行的智能客服 Agent，通过自然语言完成查询、推荐、低风险写操作与高风险确认。

- 基于 Eino ADK 构建 Supervisor 与 5 个领域 Agent，通过 AgentTool、ToolsNode 和领域工具白名单完成任务拆解、路由与协作，SSE 持续输出思考、工具进度、工具结果和最终回答事件。
- 设计统一能力链路，管理核心 20 个业务 Tool 与 2 个记忆能力 Tool；将本地 Tool、场景化 Skill 和 MCP 外部能力纳入统一的参数校验、超时、权限与审计策略，模型不直接接触业务 RPC 身份字段。
- 实现 `MemoryProvider + MemoryMiddleware` 分层上下文，组合近期消息、滚动摘要、Redis 短期记忆缓存、长期事件、用户画像与工具事实；通过 Kafka 异步更新画像和长期记忆，局部组件异常时降级而不阻塞基础对话。
- 对创建订单、取消订单等高风险操作接入 StatefulInterrupt、Redis 短锁、MySQL CAS 与 Checkpoint 恢复；从认证上下文注入用户身份，拦截越权、过期和重复执行，并记录真实工具结果与写操作审计。
- 建立覆盖 Agent 路由、工具选择与参数、安全规则、记忆召回和回答质量的评估集与回归流程；底层电商链路使用 Redis Lua 防超卖、DTM Saga、Kafka 异步事件及 Elasticsearch/Gorse 检索推荐提供可执行场景。

## 实习经历成稿

### 深圳小鹅网络技术有限公司｜Golang 后端开发实习生｜2025.09—2025.12

- 负责 BI 数据平台与直播中控台迭代，统一年报多端数据入口，通过 Goroutine 并发拉取与按需懒加载，将核心接口响应由 800ms 降至 350ms。
- 设计多级数据聚合方案，优先调用下游接口并对缺失指标实时下钻 Doris 补全；引入结果集缓存降低 Doris 查询频率 40%，支撑高并发下的数据秒级呈现。
- 采用字段预检与分页拉取构建数据导出链路，通过 Redis 队列、后台协程和 Kubernetes 分发实现多实例任务调度；结合数据库游标、分批处理与 Kafka 回传保障大数据量导出稳定执行。

## 专业技能成稿

- AI 工程：Eino ADK、Multi-Agent、Tool Calling、Context Engineering、Agent Skill、MCP、Agent Eval、Prompt Engineering
- Go 后端：Go、go-zero、Gin、gRPC、Protobuf、RESTful API、SSE、GORM
- 数据与中间件：MySQL、Redis、Kafka、Elasticsearch、Gorse、DTM、Doris、Consul
- 工程实践：Docker、Kubernetes、Linux、Git、Prometheus、Grafana、Jaeger、EFK

## HR 开场白

### Boss 直聘 / 微信短版

您好，我是深圳大学计算机技术专业 2027 届硕士，目标 AI 应用工程师。基于 Go、go-zero 与 Eino 开发过面向真实电商业务的多 Agent 智能客服，覆盖 Tool/Skill/MCP、分层记忆、安全确认和评估回归；另有小鹅通 Golang 后端实习经历。希望有机会进一步交流岗位需求。

### 完整但简洁版

您好，我是深圳大学计算机技术专业 2027 届硕士蓝松松，求职方向是 AI 应用工程师。我既有 Golang 后端工程基础，也完整实践过 Agent 应用落地：在 go-mall 项目中使用 Eino ADK 构建 Supervisor 与 5 个领域 Agent，统一接入业务 Tool、Skill 和 MCP 能力，并实现分层记忆、高风险确认、Checkpoint 恢复、审计与评估回归。此前在深圳小鹅网络负责 BI 数据平台与直播中控台后端迭代，做过并发优化、缓存治理和异步数据导出。期待了解贵团队的 Agent 或 LLM 应用工程岗位。

## 45—60 秒自我介绍

面试官您好，我叫蓝松松，是深圳大学计算机技术专业 2027 届硕士。本科和研究生阶段主要积累软件工程、机器学习与后端系统能力。我的优势是能把 AI Agent 和真实业务工程结合起来：在 go-mall 中，我基于 Eino ADK 构建了 Supervisor 与 5 个领域 Agent，把商品、订单、购物车、优惠券等能力封装为统一 Tool，并继续扩展 Skill、MCP、分层记忆、安全确认和评估回归。项目底层是 Go 微服务电商系统，涉及 Redis、Kafka、DTM 和可观测性。此前我在深圳小鹅网络做 Golang 后端实习，通过并发拉取和懒加载将核心接口从 800ms 优化到 350ms，也参与了缓存治理和异步导出链路建设。我希望应聘 AI 应用工程师，继续做可用、可控、可评估的 Agent 系统。

## 强主张证据审计

| 建议写法 | 事实证据 | 个人边界 | 风险说明 |
|---|---|---|---|
| 构建 Supervisor 与 5 个领域 Agent | `services/aiagent/internal/eino/agent.go` 定义 supervisor、product、order、cart_checkout、coupon、general Agent | 使用“构建”，不使用“主导” | 面试需解释 Supervisor 只调度、SubAgent 绑定领域工具的边界 |
| 管理 20 个业务 Tool 与 2 个记忆能力 Tool | `services/aiagent/internal/domain/tool.go` 定义 20 个业务工具及 `search_user_memory`、`get_tool_call_result` | 数量来自当前仓库 | MCP 动态工具不计入 22 个核心工具，避免口径混乱 |
| 分层上下文与滚动摘要 | `MemoryProvider`、`MemoryMiddleware`、`SummaryManager` 及 30→10+20 测试 | 当前仓库可核验 | Redis 短期记忆缓存为用户明确给定的已实现设定，需准备缓存键、TTL 与回源策略 |
| 高风险确认与恢复 | StatefulInterrupt、Redis 短锁、MySQL CAS、Redis/MySQL Checkpoint Store | 当前仓库可核验 | 不宣称分布式事务覆盖 Agent 状态；最终幂等由确认状态机负责 |
| Tool/Skill/MCP 统一能力链路 | Tool Catalog → Registry → Eino adapter → Executor → Handler → RPC 可核验；Skill/MCP 为用户给定设定 | 不写外部平台名称或工具数量 | 面试需展示一个 Skill 和一个 MCP server 的接入示例 |
| 建立 Agent 评估集与回归流程 | 用户明确给定的已实现设定 | 不写准确率、得分或样本量 | 需准备数据集来源、评分维度、失败案例与回归门槛 |
| 接口 800ms→350ms | 旧简历中的实习数据 | 保留原始口径 | 面试需说明压测环境、接口范围和并发/懒加载贡献 |
| Doris 查询频率降低 40% | 旧简历中的实习数据 | 保留原始口径 | 面试需说明统计周期、缓存命中与数据一致性策略 |

## 证据补强清单

1. 准备一个 Agent Skill 的目录结构、触发条件、输入输出和失败处理示例。
2. 准备一个 MCP server 的连接、工具发现、权限隔离、超时与审计流程图。
3. 准备 Agent Eval 的样本分类、评分规则、基线模型和一次失败回归案例。
4. 准备 Redis 短期记忆缓存的 key、TTL、用户隔离、缓存未命中与降级策略。
5. 准备实习指标的测试口径：800ms→350ms 和 Doris 查询频率降低 40% 的观测方式。

## 高频追问

- 为什么采用 Supervisor + SubAgent，而不是单 Agent 绑定全部工具？
- 如何保证模型输出的 `user_id` 不会造成越权？
- 动态上下文、会话摘要、长期事件和工具事实如何避免重复注入？
- Redis Checkpoint 丢失或锁服务异常时，为什么不会重复执行订单操作？
- Skill、MCP Tool 和本地业务 Tool 如何进入同一权限与审计链路？
- Agent 评估如何覆盖工具参数正确性、安全规则和最终回答质量？
- Redis Lua、DTM Saga 与 Kafka 分别解决交易链路中的什么问题？
