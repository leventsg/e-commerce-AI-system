<p align="center">
  <h1 align="center">e-commerce-AI-system</h1>
</p>

<p align="center">
  <strong>面向真实交易场景的 Go 微服务电商系统与多 Agent 智能客服</strong><br/>
  将「商品 / 订单 / 购物车 / 优惠券 / 支付 / 库存」等后端业务能力，与「RAG / Multi-Agent / Tool / Skill / Workflow」统一到一套可执行、可恢复、可评测的 AI 电商系统中。
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat-square&logo=go&logoColor=white" />
  <img src="https://img.shields.io/badge/go--zero-Microservices-00ADD8?style=flat-square" />
  <img src="https://img.shields.io/badge/Eino-Multi--Agent-purple?style=flat-square" />
  <img src="https://img.shields.io/badge/MySQL-OLTP-4479A1?style=flat-square&logo=mysql&logoColor=white" />
  <img src="https://img.shields.io/badge/Redis-Runtime-DC382D?style=flat-square&logo=redis&logoColor=white" />
  <img src="https://img.shields.io/badge/Kafka-Event%20Bus-231F20?style=flat-square&logo=apachekafka&logoColor=white" />
  <img src="https://img.shields.io/badge/Prometheus-Metrics-E6522C?style=flat-square&logo=prometheus&logoColor=white" />
  <img src="https://img.shields.io/badge/Grafana-Observability-F46800?style=flat-square&logo=grafana&logoColor=white" />
</p>

---

## 🚀 什么是 e-commerce-AI-system？

e-commerce-AI-system 是一个基于 Go 构建的电商微服务系统，并在传统商品、订单、购物车、优惠券、库存、结算与支付能力之上，增加了一个可调用真实业务系统的多 Agent 智能客服层 **NexusAgent**。

传统 AI 客服往往停留在：

```text
用户提问
   ↓
LLM
   ↓
文本回答
```

而 e-commerce-AI-system 希望进一步解决：

> **如何让 LLM 在真实业务系统中安全、可靠、可恢复地完成跨领域、多步骤任务。**

因此系统将传统电商后端与 Agent Runtime 结合，把：

**用户请求 → Supervisor 规划 → 子 Agent 协作 → Tool / Workflow 执行 → 风险确认 → 业务落库 → SSE 返回 → 自动化评测**

串成完整执行链路。

---

## ✨ 核心能力

### 🤖 NexusAgent：可执行多 Agent 智能客服

基于 Eino 构建 Supervisor + 多个领域 Agent：

```text
                         User
                          │
                          ▼
                    ┌────────────┐
                    │ Supervisor │
                    └──────┬─────┘
                           │
         ┌─────────────────┼──────────────────┐
         │                 │                  │
         ▼                 ▼                  ▼
   Product Agent      Order Agent       AfterSale Agent
         │                 │                  │
         ▼                 ▼                  ▼
  Product Tools      Order Tools        Skill / Workflow
         │                 │                  │
         └─────────────────┼──────────────────┘
                           ▼
                      Tool Gateway
                           │
                           ▼
                    Business Services
```

Supervisor 负责：

- 用户意图理解
- 跨领域任务拆解
- 子 Agent 路由
- AgentTool 调用
- 前序结果汇聚
- 任务重规划
- 最终结果生成

领域 Agent 负责领域内的决策与执行，例如商品、订单、购物车、优惠券与售后。

---

### 🧩 Tool Registry 与动态能力暴露

系统将 Agent 可调用能力统一注册到 Tool Registry：

```text
                    Tool Registry
                         │
            ┌────────────┴────────────┐
            │                         │
            ▼                         ▼
      Native Business Tool        MCP Tool
            │                         │
      order.query                 external.*
      cart.add                    file.*
      coupon.claim                knowledge.*
      inventory.query
```

根据：

- 当前 Agent
- 当前任务
- 用户权限
- Tool 风险等级
- TaskState

动态裁剪模型本轮可见 Tool 集合，避免一次性暴露全部工具造成 Tool Selection 噪声和 Prompt 膨胀。

当前统一编排 **20+ Native / MCP Tool**。

---

### 🧠 Skill / Workflow：控制关键业务执行过程

系统不将所有复杂业务都交给 LLM 自由规划。

对于需要动态判断、但存在稳定业务经验的场景，通过 Skill 固化：

- 场景 SOP
- 必要信息收集
- 前置检查
- 推荐执行顺序
- 可使用 Tool 边界
- 关键业务约束

例如：

```text
AfterSaleDiagnosis Skill
          │
          ├── 判断质量问题 / 尺码问题 / 主观退货
          ├── 查询订单真实状态
          ├── 获取售后政策
          └── 选择 Refund / Return / Exchange 路径
```

对于退款、取消订单、下单等确定性流程，则交给 Workflow：

```text
PreCheck
   ↓
Prepare
   ↓
Confirmation
   ↓
Commit
   ↓
Finalize
```

Skill 负责约束 Agent 的决策空间，Workflow 负责确定性状态推进，最终权限、确认、幂等与资源归属仍由 Tool Gateway 在代码层强制执行。

---

## 🧠 分层上下文与长期记忆

NexusAgent 不是简单将最近 N 条消息直接拼进 Prompt，而是设计：

```text
MemoryProvider
      │
      ▼
ContextBuilder
      │
      ├── Recent Messages
      ├── Session Summary
      ├── User Profile
      ├── Long-term Events
      ├── TaskState
      └── Tool Facts
```

### Context Projection

不同上下文消费者不共享完整 Context，而是按需投影：

```text
                    Global Context
                         │
                  ContextBuilder
                         │
        ┌────────────────┼─────────────────┐
        │                │                 │
        ▼                ▼                 ▼
   Supervisor        Sub Agent            RAG
    Context           Context           Context
        │                │                 │
 全局任务视角       领域最小上下文      最小检索上下文
```

外部 RAG API 仅注入完成检索所需的：

- 当前 Query
- 相关近期对话
- 当前任务实体
- 必要 Tool Facts
- 必要摘要

避免把完整 History、用户画像和内部执行轨迹直接传给检索服务。

同时基于滑动窗口 + 游标进行增量记忆巩固，将即将淘汰的原始消息压缩为 Session Summary，并同步更新用户画像与长期事件。

在当前评测口径下，平均上下文 Token 消耗降低约 **31%**。

---

## 🔍 RAG：知识问答与动态业务事实分离

NexusAgent 将知识型信息和动态业务事实分开处理：

```text
                     User Query
                         │
              ┌──────────┴──────────┐
              │                     │
              ▼                     ▼
        Knowledge Question      Business Fact
              │                     │
              ▼                     ▼
          RAG Retrieval          Native Tool
              │                     │
              └──────────┬──────────┘
                         ▼
                      Agent
```

适合 RAG 的内容：

- 售后政策
- 退款规则
- 优惠券规则
- 发票说明
- 商品知识
- 客服规范

必须实时通过业务 Tool 获取的内容：

- 当前库存
- 实时价格
- 订单状态
- 支付状态
- 优惠券可用状态
- 退款状态

对于“这个订单还能不能退？”这类问题，Agent 会同时调用：

```text
Order Tool → 获取真实订单状态
RAG API    → 获取售后规则
```

再完成联合判断。

---

## 🛡️ Tool Gateway：可信业务执行边界

LLM 只能提出“想做什么”，不能直接改变真实业务状态。

所有 Tool 调用统一经过 Tool Gateway：

```text
                   Model Tool Call
                         │
                         ▼
                  Schema Validation
                         │
                         ▼
              Trusted Identity Injection
                         │
                         ▼
                 Ownership Check
                         │
                         ▼
                  Risk Classification
                         │
                         ▼
                    Confirmation
                         │
                         ▼
                    Idempotency
                         │
                         ▼
                    Tool Execute
```

重点约束：

- `user_id` / token / permission 不允许由模型生成
- 用户输入的内部 ID 不作为可信身份来源
- 高风险写操作必须绑定确认快照
- Confirmation 单次有效，参数变化后原确认失效
- 写 Tool 使用独立业务幂等键，避免重复执行
- ToolCall ID 仅用于调用链关联，不承担业务幂等语义

当前评测样本中高风险越权执行率保持 **0%**。

---

## ⚙️ 高可靠 Agent Runtime

Agent Runtime 需要处理的不只是“模型成功返回”，还包括模型异常、Tool 异常、部分成功、未知状态与中断恢复。

### Tool Error Classification

```text
                    Tool Error
                        │
       ┌────────────────┼─────────────────┐
       │                │                 │
       ▼                ▼                 ▼
 Retryable Error   Business Error   Unknown Status
       │                │                 │
 bounded retry       stop            reconcile
       │                                  │
       └─────────────── idempotency ──────┘
```

系统区分：

- 可重试瞬时异常
- 不可重试业务异常
- 请求超时但执行结果未知

针对未知执行状态，不盲目重新执行写 Tool，而是优先通过业务状态查询或相同幂等键完成 reconciliation。

---

## 🔁 Saga 补偿与部分失败治理

多 Tool 任务可能出现：

```text
inventory.reserve     ✅
coupon.lock           ✅
order.create          ❌
```

系统不会简单把错误返回给 LLM 后结束，而是由 Workflow Runtime 根据预定义补偿关系执行 Saga：

```text
order.create failed
        │
        ▼
coupon.unlock
        │
        ▼
inventory.release
```

如果补偿本身失败：

```text
Compensation Failed
        ↓
PARTIAL_FAILED
        ↓
Human Handoff
```

补偿关系由代码 / Workflow Metadata 定义，不交给 LLM 自由决定。

---

## 🚦 Redis 分布式公平队列限流

LLM API 通常具有严格的并发 / RPM / TPM 限制，多实例 Agent 服务如果只使用本地 Semaphore，无法获得全局流量视图。

NexusAgent 基于 Redis 实现分布式公平队列，对进入模型调用链路的请求进行统一调度：

```text
 User A ── A1 A2 A3 A4 ...
 User B ── B1
 User C ── C1 C2
              │
              ▼
        Redis Fair Queue
              │
              ▼
       Global LLM Slots
              │
              ▼
          Model API
```

目标：

- 控制全局模型并发
- 吸收突发流量
- 避免单用户占满模型调用槽位
- 降低其他用户饥饿
- 支持队列等待超时与快速拒绝

---

## ⚡ Model Gateway：熔断与降级

所有模型请求统一经过 Model Gateway：

```text
Agent
  ↓
Fair Queue
  ↓
Circuit Breaker
  ↓
Primary Model
  │
  ├── Success
  │
  └── Failure
        ↓
      Retry
        ↓
   Circuit OPEN
        ↓
Fallback Model
        ↓
Capability Degradation
```

熔断器使用典型三态：

```text
CLOSED
  ↓ 连续失败达到阈值
OPEN
  ↓ 探针窗口
HALF-OPEN
  ├── 成功 → CLOSED
  └── 失败 → OPEN
```

降级策略包括：

- 主模型失败 → 备用模型
- 复杂生成能力不可用 → 降级为只读 / 确定性业务能力
- 核心模型全部不可用 → 明确返回降级状态或转人工

避免在模型持续异常时无限重试并进一步放大下游故障。

---

## ⏸️ Human-in-the-loop 与中断恢复

针对订单创建、取消、删除等高风险操作，引入 StatefulInterrupt + Checkpoint：

```text
Prepare Action
      ↓
StatefulInterrupt
      ↓
Persist Checkpoint
      ↓
User Confirm / Reject
      ↓
Resume
      ↓
Execute / Cancel
```

确认绑定具体操作快照，例如：

- 商品
- 数量
- 地址
- 优惠券
- 金额
- 目标订单

用户修改参数后，旧 Confirmation 自动失效，必须基于新参数重新确认。

---

## 🌊 SSE 流式交互

Agent 使用 SSE 输出运行过程：

```text
run_started
agent_transfer
tool_start
tool_end
assistant_delta
assistant_final
done
```

将：

- 用户可见回复
- Tool 执行进度
- Agent 转移事件
- 最终结果

以统一 Event Stream 返回前端。

内部 Trace 与用户对话消息分离：最终 assistant message 只在顶层 Agent Run 完成后持久化一次，避免子 Agent / 中间模型调用污染会话历史。

---

## 🛒 电商微服务后端

e-commerce-AI-system 的 AI 层并不是 Mock 业务系统，而是建立在真实微服务能力之上。

核心业务服务包括：

| Service | Responsibility |
| --- | --- |
| `auths` | 登录认证、Token 与身份校验 |
| `users` | 用户资料与收货地址 |
| `products` | 商品信息、SKU 与商品查询 |
| `inventory` | 库存查询、预占与释放 |
| `carts` | 购物车增删改查 |
| `coupons` | 优惠券领取、校验与使用 |
| `checkout` | 结算预览、金额计算与下单准备 |
| `order` | 订单创建、查询、取消与状态管理 |
| `payment` | 支付相关业务能力 |
| `audit` | 关键操作审计与事件记录 |

---

## 🏗️ 后端架构

```text
┌──────────────────────────────────────────────────────┐
│                     Clients                          │
│                                                      │
│         Web / App / AI Customer Service              │
└──────────────────────────┬───────────────────────────┘
                           │
                           ▼
┌──────────────────────────────────────────────────────┐
│                     API Layer                        │
│                                                      │
│      Auth API     Business API      Agent API        │
└───────────────┬───────────────┬───────────────┬──────┘
                │               │               │
                ▼               ▼               ▼
          ┌─────────┐     ┌──────────┐     ┌───────────┐
          │  Auth   │     │ Business │     │ NexusAgent│
          └─────────┘     │ Services │     └─────┬─────┘
                          └────┬─────┘           │
                               │                 │ Tool Calls
              ┌────────────────┼─────────────────┘
              │                │
              ▼                ▼
        MySQL / Redis      Kafka / Event Bus
              │
              ▼
       Prometheus / Jaeger
              │
              ▼
            Grafana
```

---

## 📦 微服务与基础设施

### Service Discovery

使用 Consul 完成服务注册与发现：

```text
Service Instance
      ↓ Register
    Consul
      ↑ Discover
API / RPC Client
```

### Redis

Redis 承担：

- 缓存
- Agent Runtime 状态
- 分布式公平队列
- 幂等状态
- 短期事件流
- 限流
- 分布式协调

### Kafka

Kafka 用于解耦在线请求与异步业务：

- 业务事件
- 审计事件
- 异步任务
- Agent 后台任务
- 记忆更新任务

---

## 📊 Agent Evaluation

Agent 系统不能只看“最终回答是否像对的”，还需要同时评估：

```text
Outcome
   +
Trajectory
   +
Final Business State
```

项目基于 **τ²-bench + 自建 Dataset** 构建自动化多轮评测体系。

当前评测集包含：

- **30 个任务**
- **60 次仿真执行**

覆盖：

- Tool Selection
- Tool Argument
- RAG + Tool 联合判断
- Multi-Agent 协作
- Context / TaskState
- Human-in-the-loop
- 权限与资源归属
- Tool Retry / Idempotency
- Unknown Execution Status
- Saga Compensation
- Prompt Injection

经 Prompt、Tool Schema 与 Context Retrieval 迭代：

```text
Pass¹  →  92%+
Pass³  →  99%+
```

测试集区分：

```text
Regression Suite
      ↓
基础 Tool / Schema / 权限 / Confirmation

Agent Benchmark
      ↓
多轮 / Multi-Agent / Context / RAG / Saga

Runtime Reliability Test
      ↓
模型故障 / Tool 故障 / 熔断 / 限流 / 并发
```

---

## 📈 可观测性

系统采用：

```text
Metrics     → Prometheus
Dashboard   → Grafana
Trace       → Jaeger
Log         → Structured Logging
```

重点关注：

### Agent Metrics

- Agent Run Duration
- TTFT
- Total Latency
- Agent Step Count
- Tool Calls / Run
- SubAgent Calls / Run
- Context Tokens

### Model Metrics

- Model Latency
- Model Error Rate
- 429 / 5xx
- Circuit Breaker State
- Fallback Rate
- Queue Wait Time

### Tool Metrics

- Tool Latency
- Tool Error Rate
- Retry Count
- Timeout Rate
- Idempotency Hit

### Business Metrics

- Task Success Rate
- Order Create Success
- Cancel Success
- Confirmation Reject Rate
- Human Handoff Rate

---

## 🧪 压测与可靠性测试

AI 客服压测不只关注传统 QPS，还重点关注：

```text
TTFT
Total Latency
SSE Completion Rate
Task Success Rate
Queue Wait Time
Model Error Rate
Tool Error Rate
```

压测分为：

```text
Mock LLM / Tool
      ↓
测试 Agent Runtime 自身上限

真实模型
      ↓
测试真实端到端容量

混合业务流量
      ↓
Chat / RAG / Tool / Multi-Agent / HITL

故障注入
      ↓
Model 503 / Tool Timeout / Partial Failure

公平队列测试
      ↓
验证多用户并发下无长期饥饿
```

---

## 🧱 技术栈

| Category | Technology |
| --- | --- |
| Language | Go |
| Microservice Framework | go-zero |
| AI Orchestration | Eino |
| Agent Architecture | Supervisor / AgentAsTool |
| Capability | Native Tool / MCP / Skill / Workflow |
| Knowledge | External RAG API |
| Database | MySQL |
| Cache / Runtime | Redis |
| Message Queue | Kafka |
| Search | Elasticsearch |
| Service Discovery | Consul |
| Streaming | SSE |
| Metrics | Prometheus |
| Dashboard | Grafana |
| Trace | Jaeger |
| Evaluation | τ²-bench / Custom Dataset |

---

## 🎯 为什么做 e-commerce-AI-system？

传统电商后端重点解决：

```text
用户请求
   ↓
固定 API
   ↓
业务逻辑
   ↓
数据库
```

传统 AI 客服则往往只解决：

```text
用户问题
   ↓
LLM / RAG
   ↓
文本回答
```

e-commerce-AI-system 希望把两者真正连接起来：

```text
                         e-commerce-AI-system
                     /             \
              Business System     NexusAgent
                    │                  │
           商品 / 订单 / 支付      Multi-Agent
           库存 / 优惠券 / 购物车   Context / RAG
                    │             Tool / Workflow
                    │             Guard / Runtime
                    └─────────┬────────┘
                              │
                        Executable AI
```

目标不是做一个“会聊天的客服 Bot”，而是探索：

> **如何让 Agent 在真实业务系统中具备可执行性，同时保持安全、一致性、可靠性和可评测性。**

---

## 🔧 可扩展性

系统将 Agent Runtime 与具体业务能力解耦。

| Extension | Description |
| --- | --- |
| Agent | 增加新的领域 Agent |
| Tool | 接入新的业务原子能力 |
| MCP | 接入外部标准化能力 |
| Skill | 扩展复杂场景 SOP |
| Workflow | 增加确定性业务流程 |
| Memory Provider | 替换或扩展长期记忆实现 |
| Context Projection | 针对不同 Agent / RAG 定制上下文 |
| Model Provider | 增加主模型 / Fallback 模型 |
| Evaluator | 扩展 Agent 评测维度 |

新增业务尽可能通过增加 Agent / Tool / Skill / Workflow 完成，而不是修改 Agent Runtime 核心。

---

## 🗺️ Roadmap

- [x] 电商基础微服务
- [x] 用户 / 商品 / 购物车 / 订单 / 优惠券 / 库存能力
- [x] Supervisor + 多领域 Agent
- [x] AgentAsTool 协作
- [x] Native / MCP Tool Registry
- [x] Dynamic Tool Exposure
- [x] Skill
- [x] ContextBuilder / MemoryProvider / TaskState
- [x] 外部 RAG API 接入
- [x] Tool Gateway
- [x] Human-in-the-loop
- [x] Checkpoint / Resume
- [x] Tool Retry / Idempotency
- [x] Saga Compensation
- [x] Redis 分布式公平队列
- [x] Model Circuit Breaker / Fallback
- [x] τ²-bench Agent Evaluation
- [x] Prometheus / Grafana / Trace
- [ ] Workflow
- [ ] Agent Runtime 故障注入平台
- [ ] Prompt / Skill / Model A/B Test
- [ ] 多模型动态路由

---

## 🤝 Contribution

项目仍在持续迭代中。

如果你对以下方向感兴趣：

- Go Microservices
- AI Agent
- Multi-Agent Orchestration
- Agent Runtime
- RAG
- Transactional Agent
- LLM Reliability
- Agent Evaluation

欢迎提交 Issue 或 Pull Request。

---
