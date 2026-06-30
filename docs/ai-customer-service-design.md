# AI 智能客服技术方案

## 1. 总体设计

AI 客服作为新的编排层接入现有电商系统，不侵入商品、库存、订单、优惠券、购物车等核心服务。

新增模块：

- `apis/ai`：对外 WebSocket 聊天入口。
- `services/aiagent`：AI Agent 服务，基于 Eino 负责模型接入、意图编排、工具调用、会话管理、确认管理和审计。
- 数据表：保存会话、消息、工具调用、确认记录和用户记忆。

调用链路：

用户 WebSocket  
-> `apis/ai`  
-> `services/aiagent`  
-> Eino Agent / Chain / Graph  
-> Eino ChatModel  
-> Eino Tool / ToolsNode  
-> 现有业务 RPC  
-> 返回结构化结果  
-> WebSocket 推送给用户

## 2. WebSocket 接口设计

接口：

`GET /douyin/ai/chat/ws?conversation_id=optional`

鉴权：

- 沿用现有认证中间件。
- 用户 ID 从请求上下文获取。
- 未登录用户不允许使用 AI 客服。

客户端发送用户消息：
```json
{
  "type": "user_message",
  "message_id": "client-msg-001",
  "content": "帮我查一下订单 202406300001",
  "metadata": {
    "source": "web"
  }
}
```

服务端返回 AI 消息：
```json
{
  "type": "assistant_message",
  "conversation_id": "conv_001",
  "message_id": "msg_001",
  "content": "我帮你查到该订单当前处于待支付状态。",
  "done": true
}
```


服务端返回工具结果：
```json
{
  "type": "tool_result",
  "tool": "order.get",
  "status": "success",
  "data": {}
}
```


服务端返回确认请求：
```json
{
  "type": "confirmation_required",
  "confirmation_id": "confirm_001",
  "action": "order.cancel",
  "summary": "确认取消订单 202406300001？",
  "expires_at": 1719730000
}
```

客户端确认操作：
```json
{
  "type": "confirm_action",
  "confirmation_id": "confirm_001",
  "approved": true
}
```

## 3. 核心模块
### 3.1 Eino 模型接入
AI Agent 使用 Eino 的 ChatModel 抽象接入模型，不在业务代码中自定义一套平行的 LLM Provider 接口。`services/aiagent` 只保留薄适配层，用于读取配置、创建 Eino ChatModel、统一错误降级和超时控制。

首期支持：
- OpenAI-compatible ChatModel。
- DeepSeek ChatModel。

配置项：
- Provider 名称。
- api key
- base url
- model
- timeout
- max tokens
- temperature

### 3.2 Eino Agent Orchestrator
职责：
- 将会话上下文转换为 Eino message。
- 构建系统提示词，约束模型只能调用已注册工具。
- 使用 Eino Agent / Chain / Graph 编排“模型推理 -> 工具调用 -> 结果总结”流程。
- 对流式输出进行事件转换，推送为 WebSocket `assistant_message`。
- 将 Eino callback 或本地包装器中的工具调用事件写入 `ai_tool_calls`。
- 在 Eino 执行工具前调用本地风险策略，拦截高风险工具并创建确认请求。

设计约束：
- Eino 负责模型、工具调用协议和编排流程。
- 本地代码负责用户身份、权限隔离、确认状态、审计、限流和业务 RPC 参数转换。
- 模型和 Eino 工具入参中的 `user_id` 不可信，执行前必须由本地 Execution Guard 覆盖。

### 3.3 Conversation Manager
职责：
- 创建会话。
- 恢复历史会话。
- 保存用户消息、AI 消息和工具调用摘要。
- 控制上下文长度。
- 维护用户长期偏好，例如常买分类、价格偏好
### 3.4 Intent Planner
职责：
- 判断用户意图。
- 生成工具调用计划。
- 判断是否缺少参数。
- 判断是否需要确认。
- 将自然语言请求转换为结构化工具调用。

意图类型：
- chat：闲聊。
- query：查询。
- recommend：商品推荐。
- action：自动化操作。
- confirm：用户确认。
- cancel：用户取消。

实现方式：
- 首期优先使用 Eino Tool Calling 让模型选择工具。
- 对明确中文意图保留规则兜底 Planner，模型不可用时仍可处理订单查询、取消订单、加购物车等明确请求。
- Planner 的输出必须经过 Tool Registry 和 Execution Guard 校验后才能执行。

### 3.5 Tool Registry
所有业务工具必须注册为 Eino Tool，并同步维护本地工具元数据白名单，模型不能调用未注册工具。
首期工具：
- product.search
- product.detail
- product.recommend
- inventory.get
- order.get
- order.list
- order.cancel
- checkout.prepare
- checkout.detail
- order.create
- cart.list
- cart.add
- cart.sub
- cart.delete 
- coupon.list
- coupon.detail
- coupon.claim
- coupon.my_list
- coupon.usage_list
- coupon.calculate

每个工具需要定义：
- 工具名称。
- 风险等级。
- 参数 schema。
- 是否需要确认。
- 超时时间。
- 对应 RPC 调用。
- 结果转换逻辑。
- Eino Tool schema。

### 3.6 Execution Guard / Engine
职责：
- 校验工具参数。
- 强制注入当前登录用户 ID。
- 屏蔽模型传入的 user_id。
- 调用现有 RPC 服务。
- 处理超时、错误和失败降级。
- 将 RPC 返回转换成 AI 可读结构。
- 写操作完成后记录审计日志。

Execution Guard 位于 Eino Tool 的业务处理函数内部或外层包装器中。任何 Eino 工具实际调用 RPC 前，都必须先经过该 Guard。

### 3.7 Confirmation Manager
高风险操作必须进入确认流程。
职责：
- 创建确认记录。
- 生成确认 ID。
- 保存待执行工具和参数。
- 设置确认过期时间。
- 用户确认后重新校验权限和状态。
- 防止过期确认、重复确认、跨用户确认。

状态：
- pending：待确认
- approved：已确认
- rejected：已拒绝
- expired：已过期
- executed：已执行
- failed：执行失败

## 4. 数据库设计
### 4.1 ai_conversations
| 字段 | 说明 |
|---|---|
| id | 会话 ID |
| user_id | 用户 ID |
| title | 会话标题 |
| status | 会话状态 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

### 4.2 ai_messages
| 字段 | 说明 |
|---|---|
| id | 消息 ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| role | user / assistant / tool |
| content | 消息内容 |
| metadata | 扩展信息 |
| created_at | 创建时间 |

### 4.3 ai_tool_calls
| 字段 | 说明 |
|---|---|
| id | 调用 ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| tool_name | 工具名称 |
| arguments | 工具参数 |
| result_summary | 结果摘要 |
| status | success / failed |
| error_message | 错误信息 |
| latency_ms | 耗时 |
| created_at | 创建时间 |

### 4.4 ai_confirmations
| 字段 | 说明 |
|---|---|
| id | 确认 ID |
| conversation_id | 会话 ID |
| user_id | 用户 ID |
| tool_name | 工具名称 |
| arguments | 待执行参数 |
| summary | 确认摘要 |
| status | 确认状态 |
| expires_at | 过期时间 |
| executed_at | 执行时间 |
| created_at | 创建时间 |

### 4.5 ai_user_memories
| 字段 | 说明 |
|---|---|
| id | 记忆 ID |
| user_id | 用户 ID |
| memory_type | 记忆类型 |
| content | 记忆内容 |
| confidence | 置信度 |
| created_at | 创建时间 |
| updated_at | 更新时间 |

## 5. 关键流程
### 5.1 商品推荐流程
1. 用户描述购买需求。
2. Eino Agent 结合系统提示词和工具 schema 生成商品推荐工具调用。
3. Execution Guard 校验并注入用户 ID。
4. 调用 product.recommend。
5. 推荐不足时调用 product.search。
6. Eino ChatModel 基于工具结果生成简短推荐理由。
7. 返回商品列表和推荐理由。
8. 用户要求加入购物车时调用 cart.add。

### 5.2 查询订单流程
1. 用户提供订单号或描述“最近订单”。
2. 有订单号时调用 order.get。
3. 无订单号时调用 order.list，并将结果交给 Eino ChatModel 根据用户描述筛选和总结。
4. 多个候选订单时让用户选择。
5. 返回订单状态、商品、金额、地址、支付状态。

### 5.3 取消订单流程
1. 用户提出取消订单。
2. 查询订单详情。
3. 校验订单属于当前用户。
4. 判断订单是否允许取消。
5. 创建确认请求。
6. 用户确认后调用 order.cancel。
7. 返回取消结果。
8. 记录审计日志。

### 5.4 创建订单流程
1. 用户表达购买意图。
2. AI 确认商品、数量、优惠券、地址、支付方式。
3. 调用 checkout.prepare 创建预结算。
4. 返回结算金额。
5. 创建订单确认请求。
6. 用户确认后调用 order.create。
7. 返回订单详情。

## 6. 测试方案
### 6.1 单元测试
- Intent Planner 意图识别。
- Eino Tool schema 注册和本地工具风险等级。
- Confirmation Manager 确认创建、确认、拒绝、过期。
- Execution Guard 用户 ID 注入和参数校验。
- Eino ChatModel 创建、超时和降级逻辑。
- Eino 工具调用事件到审计记录的转换。

### 6.2 集成测试
- WebSocket 建连成功。
- 未登录 WebSocket 被拒绝。
- 用户查询商品。
- 用户查询订单。
- 用户获取商品推荐。
- 用户添加购物车。
- 用户取消订单时必须先确认。
- 用户创建订单时必须先确认。

### 6.3 风控测试
- 模型传入伪造 user_id 时必须被覆盖。
- 用户不能查询他人订单。
- 过期确认不能执行。
- 重复确认不能重复执行。
- 工具调用失败时不能返回成功话术。

## 7. 实施建议
建议分阶段实现：
第一阶段：AI Agent 基础骨架  
- 新增 services/aiagent。
- 接入 Eino 依赖和 ChatModel 工厂。
- 实现 Eino Tool Registry 与本地工具元数据。
- 实现 Confirmation Manager。
- 实现基础单元测试。

第二阶段：WebSocket 聊天入口  
- 新增 apis/ai。
- 实现 WebSocket 建连、鉴权、消息收发。
- 接入 AI Agent 服务。

第三阶段：业务工具接入  
- 接入商品、库存、订单、购物车、优惠券、结算 RPC。
- 实现查询、推荐和低风险自动操作。

第四阶段：高风险操作和审计  
- 实现确认流程。
- 接入取消订单、创建订单。
- 接入审计日志。
- 完成风控测试。

第五阶段：增强能力  
- 会话记忆。
- 用户偏好。
- 运营配置。
- 模型切换。
- 限流和监控。
