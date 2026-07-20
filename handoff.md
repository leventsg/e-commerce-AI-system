# AI Customer Service Handoff

## 1. 当前任务与仓库状态

本仓库正在按 `docs/ai-customer-service-implementation-plan.md` 逐项实现 AI 智能客服。

当前分支：`feat/add_ai_agent_task11`

当前进度：

- Task 9（低风险写操作）已完成并进入 `main`。
- Task 10（Confirmation Manager）已完成并提交在当前分支历史中。
- 下一项是 Task 11：接入高风险 Eino Tool 和确认后的唯一一次业务执行。
- 写本交接前工作区是干净的；写完后预期只有 `handoff.md` 被修改。

相关提交：

```text
6f1288a 合并冲突
5e35bae 实现高风险执行确认管理
05001a6 Feat/add ai agent task9 (#21)
```

开始工作前必须依次阅读：

1. `AGENTS.md`
2. `docs/ai-customer-service-prd.md`
3. `docs/ai-customer-service-design.md`
4. `docs/ai-customer-service-implementation-plan.md`，重点是 Task 11

## 2. 已完成内容

### Task 9：低风险写操作

已实现三个不需要用户确认、但必须审计的 Eino Tool：

- `cart.add`
- `cart.sub`
- `coupon.claim`

核心文件：

- `services/aiagent/internal/tools/write_tools.go`
- `services/aiagent/internal/tools/write_tools_test.go`
- `services/aiagent/internal/tools/cart_tools.go`
- `services/aiagent/internal/tools/coupon_tools.go`
- `services/aiagent/internal/tools/executor.go`
- `services/aiagent/internal/audit/recorder.go`
- `services/aiagent/internal/audit/recorder_test.go`

已经落实的行为：

- 所有调用继续经过共享 `Executor` / Execution Guard。
- 模型参数中的身份字段会被递归清除，业务 RPC 只能收到认证上下文中的用户 ID。
- `cart.add` 的底层 RPC 每次只增加 1，因此工具层按 `quantity` 重复调用，并固定传 `Quantity: 0`。
- `cart.sub` 先用当前用户的 `CartItemList` 将 `cart_item_id` 解析为 `product_id`，再逐次调用减少 RPC。
- `cart.sub` 不允许减到 0；清零必须走后续需要确认的 `cart.delete`。
- 批量操作中途失败会返回 failed，并记录已完成数量，不会生成成功话术。
- 成功、业务失败、RPC 错误、超时都会写 `ai_tool_calls`。
- 写工具还会调用 audit RPC；审计失败不会伪装成业务写入未发生，也不会自动重试业务写入。

### Task 10：Confirmation Manager

已实现 Redis 短锁削峰 + MySQL CAS 最终幂等的混合方案。

核心文件：

- `services/aiagent/internal/confirmation/manager.go`
- `services/aiagent/internal/confirmation/manager_test.go`
- `services/aiagent/internal/confirmation/locker.go`
- `services/aiagent/internal/domain/confirmation.go`
- `dal/model/ai/confirmations/aiconfirmationsmodel.go`
- `dal/model/ai/confirmations/aiconfirmationsmodel_test.go`
- `services/aiagent/internal/config/config.go`
- `services/aiagent/internal/svc/servicecontext.go`
- `services/aiagent/etc/aiagent.yaml`
- `services/aiagent/etc/aiagent.prod.yaml`

Manager 已暴露：

- `Create`：创建 `pending` 确认。
- `Decide`：原子批准或拒绝。
- `MarkExecuted`：`approved -> executed`。
- `MarkFailed`：`approved -> failed`。

状态机：

```text
pending -> approved -> executed
                    -> failed
pending -> rejected
pending -> expired
```

幂等和锁行为：

- Redis key：`ai:confirmation:lock:<confirmation_id>`。
- 默认锁 TTL 为 5 秒，确认有效期默认 300 秒。
- Redis 锁竞争返回 `ErrConfirmationBusy`，并且不访问 MySQL。
- Redis 获取锁发生连接或超时错误时记录日志，降级到 MySQL CAS。
- MySQL 条件更新和 `RowsAffected` 是最终 winner 判定，Redis 不是事实来源。
- Redis 锁只覆盖确认状态变更，Manager 返回前释放，绝不能跨 Task 11 的业务 RPC 持有。
- 请求 context 已取消时，释放锁使用脱离取消且带 1 秒超时的 context。
- 释放锁失败只记录错误，依靠 TTL 回收，不覆盖已经成功的 MySQL 结果。

安全行为：

- `Create` 只接受 metadata 同时满足 high risk、require confirmation、write operation 的工具。
- `confirmation_id` 格式为 `confirm_<UUIDv7>`。
- 用户 ID 和 conversation ID 必须与数据库记录完全匹配。
- 过期、拒绝、已批准、已执行和失败记录都不能再次领取。
- 参数脱敏支持 snake_case、camelCase、kebab-case、大小写别名，以及 typed map/slice。
- 已覆盖 `user_id`、token、access/refresh token、session、auth/authorization、cookie、JWT 等认证字段。

Custom model 已增加：

- `FindOneUncached`
- `ResolvePending`
- `ExpirePending`
- `CompleteApproved`

没有修改数据库 schema、AiAgent proto 或 goctl 生成文件。

## 3. 已完成验证

Task 10 完成时新鲜执行并通过：

```bash
go test ./services/aiagent/internal/confirmation -run TestManager -count=1
go test -race ./services/aiagent/internal/confirmation ./dal/model/ai/confirmations ./common/utils/argx -count=1
go test ./services/aiagent/... ./dal/model/ai/confirmations ./common/utils/argx -count=1
go vet ./services/aiagent/... ./dal/model/ai/confirmations ./common/utils/argx
git diff --check
```

独立 code review 最终没有发现 Critical/Important 问题。

注意：这不是对整个仓库 `go test ./...` 的声明。Task 11 完成后先运行其聚焦测试和 `services/aiagent/...`；只有声称整个 AI 客服功能完成时，才按 `AGENTS.md` 跑全仓测试。

## 4. 当前卡点

没有技术阻塞，Task 11 尚未开始实现。

当前缺口是把 Confirmation Manager 接到真正的高风险执行链：

- 首次请求只能创建确认并返回 `confirmation_required`，不能调用业务 RPC。
- 用户确认后，只有 MySQL CAS 的唯一 winner 可以调用一次业务 RPC。
- 业务成功后调用 `MarkExecuted`；业务失败或超时后调用 `MarkFailed`。
- Task 10 的 Redis 锁在 Manager 返回前已经释放，Task 11 不应重新设计成长时间持锁。

计划中的高风险工具：

- `cart.delete` -> `Cart.DeleteCartItem`
- `order.create` -> `OrderService.CreateOrder`
- `order.cancel` -> `OrderService.CancelOrder`

额外要求：

- 创建订单前若没有 `pre_order_id`，先走 `checkout.prepare`，返回结算信息后再创建 `order.create` 确认。
- `order.create` 使用优惠券时，确认摘要必须展示 coupon ID、应付金额和商品数量。
- Task 11 的入口文件预计包括：
  - `services/aiagent/internal/tools/cart_tools.go`
  - `services/aiagent/internal/tools/order_tools.go`
  - `services/aiagent/internal/logic/confirmactionlogic.go`
  - `services/aiagent/internal/logic/confirmactionlogic_test.go`

## 5. 下一步计划

1. 先检查 Task 11 对应的现有生成 logic、proto、Tool Registry metadata 和 cart/order/checkout RPC 的真实请求响应结构。
2. 先写 `confirmactionlogic_test.go`，覆盖未确认不执行、批准后唯一执行、拒绝、过期、重复、跨用户、跨 conversation、业务失败和超时。
3. 实现高风险 Tool handler，但所有调用仍必须经过现有 Execution Guard；不要直接从 logic 绕过 Executor 调业务 RPC。
4. 首次高风险请求调用 `ConfirmationManager.Create`，返回结构化 `confirmation_required`。
5. 确认请求通过 `Decide(... Approved: true)` 领取唯一执行权；只有成功从 pending 转为 approved 的调用方继续执行。
6. 在 Redis 故障或锁过期、多请求同时进入时，依赖 MySQL CAS 保证只有一个 winner。
7. 业务 RPC 完成后标记 executed/failed，并确保失败绝不被总结为成功。
8. 为高风险写操作复用 Task 9 的 tool-call recorder 和 audit RPC，不另起旁路审计实现。
9. 更新设计文档和实施计划中的 Task 11 状态。
10. 至少运行：

```bash
go test ./services/aiagent/internal/logic -run TestConfirmAction -count=1
go test -race ./services/aiagent/internal/logic ./services/aiagent/internal/confirmation -count=1
go test ./services/aiagent/... -count=1
go vet ./services/aiagent/...
git diff --check
```

## 6. 绝对不要再踩的坑

1. 不要把 Redis 锁当作最终幂等保障。锁会超时、丢失、故障或同时放行；唯一执行权必须由 MySQL 条件更新决定。
2. 不要让 Redis 锁跨业务 RPC。长 RPC、超时或进程崩溃会造成无意义阻塞；Task 10 已明确只锁状态变更。
3. 不要在 Redis 故障时直接失败。既定策略是记录告警并降级到 MySQL CAS；只有锁明确被其他请求占用时返回 busy 且不查 MySQL。
4. 不要相信任何客户端、模型、tool 参数或 conversation metadata 中的 `user_id`。认证上下文是唯一可信来源。
5. 不要只删除字面量 `user_id`。身份和认证字段可能是 camel/kebab/snake 命名，也可能藏在 typed map/slice；必须复用 `common/utils/argx`。
6. 不要把数据库已提交但缓存清理失败误报成写入失败。go-zero cached model 可能返回非 nil `sql.Result` 和 cache error；当前 Create/CAS 路径已专门保留 committed 语义。
7. 不要使用缓存读取确认状态。确认决策必须 `FindOneUncached`，否则可能基于旧 pending 状态重复执行。
8. 不要在 CAS `RowsAffected == 0` 后仍调用业务 RPC。它代表已经有其他 winner 或状态已变化，必须停止并返回已处理状态。
9. 不要允许 approved 之外的状态进入 executed/failed，也不要允许 rejected/expired/failed/executed 再次领取。
10. 不要只校验 user ID 而忽略 conversation ID；两者都必须完全匹配。
11. 不要在部分批量写失败后生成成功话术，也不要因为审计失败自动重试已经发生的业务写入。
12. 不要手改 goctl 生成文件、现有业务 RPC 或数据库 schema；Task 11 是 AI 编排层接入。
13. 不要复活旧 `handoff.md` 中 Task 6/8 的冲突内容。该文件此前包含 `<<<<<<< HEAD` 冲突标记，已经在本次交接中整体替换。

## 7. 快速定位

- 实施计划：`docs/ai-customer-service-implementation-plan.md`，Task 11 从约第 780 行开始。
- Confirmation Manager：`services/aiagent/internal/confirmation/manager.go`
- Redis Locker：`services/aiagent/internal/confirmation/locker.go`
- MySQL CAS：`dal/model/ai/confirmations/aiconfirmationsmodel.go`
- ServiceContext 注入：`services/aiagent/internal/svc/servicecontext.go`
- 高风险 metadata：`services/aiagent/internal/tools/registry.go`
- Execution Guard：`services/aiagent/internal/tools/executor.go`
- 写操作审计：`services/aiagent/internal/audit/recorder.go`
- 参数脱敏：`common/utils/argx/sanitize.go`

新会话应从检查 Task 11 现有代码和 RPC 契约开始，不要重做 Task 9/10。
