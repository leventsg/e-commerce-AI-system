# AI 智能客服交接文档

更新时间：2026-07-20

当前分支：`feat/add_ai_agent_task12`

当前基线：`e28059f Feat/add ai agent task11 (#23)`，与 `main` / `origin/main` 一致

## 1. 我们在做什么

仓库正在按 `docs/ai-customer-service-implementation-plan.md` 逐任务实现 AI 智能客服。

已经完成 Task 1-11。当前准备开始：

```text
Task 12: 新增 apis/ai WebSocket API
```

目标入口：

```text
GET /douyin/ai/chat/ws?conversation_id=optional
```

职责边界：

- `apis/ai` 负责认证后的 WebSocket 协议、连接和事件转发。
- `services/aiagent` 负责会话、Planner、Eino 编排、工具执行、确认和审计。
- 用户 ID 只能来自认证上下文，WebSocket payload 中不能接受或信任 `user_id`。
- 现有 product、inventory、order、checkout、cart、coupon、users、audit 服务仍是业务事实来源。

新会话开始前必须依次阅读：

1. `AGENTS.md`
2. `docs/ai-customer-service-prd.md`
3. `docs/ai-customer-service-design.md`
4. `docs/ai-customer-service-implementation-plan.md`，重点 Task 12 和 Task 13

## 2. 已经完成了什么

### 2.1 Task 11：高风险 Eino Tool

PR #23 已合入 `main`，实现三个高风险工具：

- `cart.delete`
- `order.create`
- `order.cancel`

完整流程：

```text
Eino 首次调用
-> 校验可信执行上下文和参数
-> 只创建 pending confirmation
-> 返回 confirmation_required
-> 用户调用 ConfirmAction
-> MySQL CAS 的唯一 winner 获得 approved
-> 通过共享 Executor / Execution Guard 调业务 RPC
-> 成功标记 executed，失败标记 failed
```

关键文件：

- `services/aiagent/internal/tools/high_risk_tools.go`
- `services/aiagent/internal/tools/high_risk_tools_test.go`
- `services/aiagent/internal/logic/confirmactionlogic.go`
- `services/aiagent/internal/logic/confirmactionlogic_test.go`
- `services/aiagent/internal/svc/servicecontext.go`

已经落实的安全语义：

- 普通 Eino 调用绝不直接执行高风险 RPC。
- 确认记录持久化的是脱敏参数；确认执行不接受客户端重新提交业务参数。
- `user_id` 在 RPC 前由认证上下文强制注入。
- 拒绝、过期、重复、跨用户、跨会话、锁竞争均不能执行 RPC。
- Redis 只保护状态读取/更新，不跨业务 RPC 持锁；最终幂等由 MySQL CAS 保证。
- RPC 已执行但审计失败时返回 failed，并标记 `BusinessExecuted=true`；确认仍进入 `executed`，防止用户重试造成重复写入。
- RPC 成功但 confirmation 终态保存失败时保留真实业务结果，并追加明确的 error event，不能伪装成业务未执行。

### 2.2 下单和预结算契约

Tool schema 已与真实 RPC 对齐：

- `checkout.prepare` 必填 `order_items[]`；每项含 `product_id`、`quantity`，`coupon_id` 可选。
- `order.create` 必填 `pre_order_id`、`address_id`、`payment_method`，`coupon_id` 可选。
- `payment_method`：1 微信，2 支付宝。
- 没有 `pre_order_id` 时必须先调用 `checkout.prepare`，不能直接创建订单确认。
- 创建 `order.create` 确认前会用可信用户查询 checkout detail，确认预订单 owner、金额和商品数量。
- 使用优惠券时基于 checkout 商品快照调用 `coupon.calculate`；优惠券不可用或计算失败时不创建确认，摘要展示与该优惠券一致的应付金额。

### 2.3 Execution Guard、审计和共享 helper

- 所有工具继续走 `services/aiagent/internal/tools/executor.go`。
- 写操作 RPC 成功但 `ai_tool_calls` 或 audit 持久化失败时，不再报告完整成功。
- `domain.AgentEvent.BusinessExecuted` 只用于区分“业务已执行但审计失败”和“业务未执行”。
- handler map 的共享合并函数已改为：

```go
func mergeHandlers(destination, source map[string]HandlerFunc)
```

它位于 `executor.go`，查询、低风险写和高风险工具共同使用。不要再恢复带查询限定的 `registerQueryHandlers`。

### 2.4 库存高并发死锁修复

Task 11 开发期间发现并修复了库存真实扣减死锁，修复也包含在 PR #23。

旧问题：

```text
SELECT COUNT(*) ... FOR UPDATE
-> 不存在记录产生 gap lock
-> 并发 INSERT inventory_lock
-> MySQL 1213 deadlock
```

当前实现：

- `TryLockOrder` 直接向 `inventory_lock(order_id, user_id)` INSERT，通过唯一索引原子抢占执行权。
- MySQL 1062 表示同一请求已执行，按幂等成功返回；1213、1205和普通错误继续上抛。
- 抢占记录与库存扣减在同一事务内，后续失败时幂等记录一起回滚，允许安全重试。
- 多商品库存行按 `product_id` 固定顺序 `FOR UPDATE`，避免交叉锁顺序。

验证场景已覆盖：

- 单商品 100 并发扣减。
- 双商品相反输入顺序并发扣减。
- 库存不足导致事务回滚后，使用相同 `pre_order_id` 可以重试成功。
- 成功后重复相同幂等键不会再次扣减。

关键文件：

- `dal/model/inventory/inventorymodel.go`
- `dal/model/inventory/inventorymodel_test.go`
- `test/rpc/inventory/inventory_test.go`

## 3. 当前状态和卡点

### 3.1 工作区

写交接前工作区是干净的。写完后应只有：

```text
M handoff.md
```

不要把旧 Task 11 的未提交状态带入新会话；PR #23 已经合并。

### 3.2 Task 12 尚未开始

目前没有技术阻塞，但 WebSocket API 代码尚不存在：

- 仓库中没有 `apis/ai` 目录。
- `services/aiagent/internal/logic/chatlogic.go` 仍是 goctl 生成的 TODO。
- `ConfirmActionLogic` 已完成，可供 WebSocket 的 `confirm_action` 消息调用。

因此当前不是“修 Task 11”，而是从 Task 12 的 API 契约和网关骨架开始。

### 3.3 测试环境现状

Task 11 和库存相关聚焦测试曾通过：

```bash
go test ./services/aiagent/... -count=1
go test ./dal/model/inventory/... ./services/inventory/... -count=1
go test ./test/rpc/inventory \
  -run 'TestInventoryService_(HighConcurrency|MultiProductLockOrder|IdempotencyClaimRollsBackWithFailedDeduction)$' \
  -count=3
git diff --check
```

不要声称当前 `go test ./...` 全绿。最后一次全仓执行仍有与 AI Task 11 无关的旧集成测试失败：

- auth 固定用户/token 状态不满足。
- checkout/order/coupon 测试使用不存在或已失效的固定数据。
- product 测试依赖失效的七牛账号、Elasticsearch、错误的本地 MySQL 凭据和缺失图片。
- users 部分测试依赖不存在的用户或前置记录。

这些失败不是库存死锁回归；`test/rpc/inventory` 已通过。Task 12 应先跑 `apis/ai/...` 和 `services/aiagent/...` 聚焦测试，最终仍要如实报告全仓测试结果。

## 4. 下一步计划

1. 阅读现有 API 网关的认证中间件用法，优先参考 `apis/carts`、`apis/order` 等模块的 `WithClientMiddleware` 和 `WrapperAuthMiddleware`。
2. 核对 `services/aiagent/aiagent.proto` 的 `Chat` / `ConfirmAction` 请求响应，禁止手改生成文件。
3. 新增 `apis/ai/ai.api`，定义：

```text
GET /douyin/ai/chat/ws?conversation_id=optional
```

4. 按仓库 go-zero 生成惯例创建 `apis/ai` 骨架；生成后只在项目允许的自定义文件中实现逻辑。
5. WebSocket 客户端输入只允许：

- `user_message`
- `confirm_action`

6. 服务端事件只允许：

- `assistant_message`
- `tool_result`
- `confirmation_required`
- `error`

7. 从 HTTP 认证上下文获取用户 ID，再构造 AiAgent RPC 请求；忽略或拒绝 payload 中任何身份字段。
8. 为未登录拒绝、普通消息、确认消息、非法类型、断连和 RPC 错误先写测试。
9. Task 12 完成后再进入 Task 13 WebSocket 集成测试，不要提前混做限流和 Task 14 审计扩展。
10. 最低验证：

```bash
go test ./apis/ai/... -count=1
go test ./services/aiagent/... -count=1
git diff --check
```

## 5. 绝对不要再踩的坑

1. 不要信任 WebSocket query、payload、metadata、模型参数中的 `user_id`；认证上下文是唯一可信来源。
2. 不要让高风险 Eino Tool 直接调用业务 RPC；首次调用只能创建 confirmation。
3. 不要在确认时接受客户端重新提交 tool name 或 arguments；必须执行数据库确认记录中的内容。
4. 不要把 Redis 锁当最终幂等保障，也不要让 Redis 锁跨业务 RPC；MySQL CAS 才决定唯一 winner。
5. 不要在 CAS `RowsAffected == 0` 后继续执行 RPC。
6. 不要把“业务已经执行但审计失败”报告成完整成功，也不要自动重试已发生的写操作。
7. 不要把“确认终态保存失败”误报成业务 RPC 失败；返回真实结果并追加 error event。
8. 不要用 checkout 的旧金额给另一个 coupon 做确认摘要；有 coupon 时必须用相同商品快照调用 `coupon.calculate`。
9. 不要恢复 `SELECT ... FOR UPDATE` 后 INSERT 的库存幂等模式；不存在记录会产生 gap lock 并在高并发下死锁。
10. 不要吞掉所有库存 INSERT 错误；只有 MySQL 1062 是幂等命中，1213/1205必须上抛。
11. 不要把 handler 合并 helper 命名成查询专用；统一使用 `mergeHandlers`。
12. 不要手改 goctl 生成文件。先改 `.api` / `.proto` 源文件，再按项目惯例生成。
13. 不要顺手修复全仓旧 RPC fixture、七牛、ES 或用户数据问题；除非任务明确授权，保持 Task 12 改动范围小。
14. 不要用旧交接中的状态判断进度；以当前分支、实施计划勾选和实际代码为准。

## 6. 快速定位

- Task 12 计划：`docs/ai-customer-service-implementation-plan.md` 约第 837 行。
- AI RPC 契约：`services/aiagent/aiagent.proto`。
- Chat TODO：`services/aiagent/internal/logic/chatlogic.go`。
- ConfirmAction：`services/aiagent/internal/logic/confirmactionlogic.go`。
- 高风险工具：`services/aiagent/internal/tools/high_risk_tools.go`。
- Tool Registry：`services/aiagent/internal/tools/registry.go`。
- Execution Guard：`services/aiagent/internal/tools/executor.go`。
- Confirmation Manager：`services/aiagent/internal/confirmation/manager.go`。
- ServiceContext：`services/aiagent/internal/svc/servicecontext.go`。
- API 认证参考：`apis/carts`、`apis/order`。

新会话的第一件事应是检查 Task 12 的真实认证上下文和 go-zero API 生成模式，然后为 `apis/ai` 制定并执行测试先行的实现方案。不要重做已合并的 Task 11。
