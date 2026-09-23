# Checkout token implementation plan

**Goal:** 同次结算重试只执行一次；相同商品的新购买意图可以再次结算。

**Architecture:** 在现有 checkouts 表保存服务端签发的 token、领取请求 ID、参数快照、执行状态和结果。MySQL 条件更新决定唯一执行者，Redis 只承担现有超时调度。API 与 AI 均先领取 token，再提交结算。

**Tech Stack:** Go、go-zero API/RPC/model、MySQL、Redis、sqlmock。

**Spec:** 用户已确认的 checkout_token 设计；本文件记录字段方案与执行边界。

## Constraints and design

- 只提供 ALTER SQL，不连接或修改用户数据库，不回填历史数据。
- checkouts 增加 checkout_token、checkout_intent_id、checkout_request_hash、checkout_request、prepare_state、token_expires_at、prepare_started_at、prepare_result。
- token 使用 crypto/rand 生成 32 字节随机值；领取请求 ID 由调用方生成，重试领取复用同 ID，按 user_id + checkout_intent_id 唯一。
- prepare_state: 0 issued、1 processing、2 succeeded、3 failed、4 unknown。未生成有效预订单时 status=4（PREPARING）；只有 prepare_state=2 的记录可查询或下单。
- 首次消费 token 的有效期 10 分钟；执行期限 30 秒。成功或失败的结果长期保留重放，过期只限制首次消费。超时的 processing 只转 unknown，永不自动接管执行。
- 绑定规范化后的商品列表和优惠券；同 token 不同参数拒绝。商品 ID 重复项拒绝，避免库存 Lua 对重复 key 的问题。
- 先查询商品和优惠金额，再预扣库存。库存结果不明或数据库提交结果不明时保留 unknown，禁止删除记录或重新扣库存。人工核对使用请求快照和固定 pre_order_id。
- 成功结果与明细在同一事务写入；重放先返回同一结果，预订单能否继续下单由现有订单状态和过期检查决定。
- AI 使用可信 RunID + ToolCallID + ConversationID 派生领取请求 ID；不接受模型提供 token。写工具依然不自动重试，沿用原审计链路。
- 所有新增测试在 tests/ 下；通过 Go overlay 测试 internal 包，源码目录不落测试文件。

## Tasks

- [x] 1. 添加协议回归测试并观察失败；扩展 checkout.proto、checkout.api、checkout.sql，生成代码并提供人工 ALTER SQL。
- [x] 2. 添加持久化 token 领取、查找、CAS 消费、失败/unknown 和成功事务方法；SQL 边界测试覆盖用户隔离和状态条件。
- [x] 3. 替换 PrepareCheckout 商品指纹锁，接入 token 状态机；测试成功重放、并发拒绝、新意图、参数冲突、过期、失败和不明结果。
- [x] 4. 接入 API token 领取和参数透传、前端 SDK、AI 稳定执行标识；测试 AI 同次重试复用、新调用隔离及不信任模型输入。
- [x] 5. 更新 API 和 AI 设计/实施文档；运行 overlay 回归、checkout/API 编译测试、AI 测试和 go test ./...，报告实际限制。

## 验证结果

新增回归、checkout/API/AI 测试和前端构建通过。全仓 `go test ./...` 已运行但未通过：失败位于既有 `test/rpc/**`，本机 RPC 服务连接被拒绝，部分旧测试随后 nil panic。数据库字段 SQL 未执行，真实结算链路待用户部署测试环境后验证。详见 `docs/checkout-token-design.md` 的验证记录。

## Verification commands

```powershell
go test ./tests/checkout
python tests/checkout/run.py
go test ./services/checkout/... ./apis/checkout/...
go test ./services/aiagent/...
go test ./apis/ai/...
go test ./...
```
