# 结算意图与幂等

## 行为

一次购买意图对应一个服务端生成的 `checkout_token`。同一 token 重复提交不再次预扣库存；用户再次购买相同商品时使用新意图、新 token，生成新的 `pre_order_id`。

商品、数量和优惠券仍生成 SHA-256 摘要，但只用于检查**同一 token 是否被修改参数**，不再作为幂等键。

## 数据库变更

手工执行 [checkout-token.sql](sql/checkout-token.sql)，仅向已有 `checkouts` 表增加 8 个字段、2 个唯一索引和 1 个排查索引。本次开发不连接数据库，不执行 SQL，不迁移或回填历史数据。

| 字段 | 用途 |
| --- | --- |
| checkout_token | crypto/rand 生成的 32 字节随机值，编码为 64 位十六进制；全表唯一 |
| checkout_intent_id | 调用方生成的领取请求 ID；与 user_id 组成唯一索引，避免领取重试产生新 token |
| checkout_request_hash | 首次消费时绑定规范化请求摘要 |
| checkout_request | 首次消费时保存商品及优惠券快照，供异常核对，不包含认证凭据 |
| prepare_state | 0 待提交、1 处理中、2 成功、3 失败、4 待核对 |
| token_expires_at | 未消费 token 的有效期，签发后 10 分钟 |
| prepare_started_at | 获得执行权的时间 |
| prepare_result | 成功或失败的原始响应，用于重放 |

签发时预分配 `pre_order_id`，金额为 0，原有 `status` 写入 4（PREPARING），`expire_time` 为 0。此时不是可下单的预订单。只有 `prepare_state=2` 的记录出现在结算列表、详情和订单/释放 RPC 的查询中。

按本次要求，不兼容旧记录：旧记录没有有效 token，新增字段默认值不会把它们识别为已成功的新版结算。不应在仍需处理旧预订单的环境中直接切换此版本。

`checkout_items` 的实际初始化结构使用 `(pre_order_id, product_id)` 联合主键，支持多商品订单；旧 `dal/model/checkout/checkout.sql` 明细表的单列主键定义与之不一致。本次字段 SQL 不改变明细表，部署时以 `construct/depend/sql/init.sql` 的明细表结构为准。

## API 调用

两个接口均沿用认证中间件，`user_id` 只来自登录态，调用方不可覆盖。

1. 用户开始一次新的购买，生成并保留 `intent_id`（例如 `crypto.randomUUID()`）。
2. `POST /douyin/checkout/token`，body 为 `{"intent_id":"<UUID>"}`。响应 envelope 的 `data` 包含 `checkout_token` 和 `expires_at`。
3. 持久保留返回的 token，再调用 `POST /douyin/checkout/prepare`：

```json
{
  "checkout_token": "<64位服务端令牌>",
  "coupon_id": "",
  "order_items": [{"product_id": 42, "quantity": 1}]
}
```

4. 重复点击、断线重试复用原 token。领取接口超时时复用原 `intent_id` 再领取，服务端返回原 token 和原过期时间。
5. 用户明确再次购买时生成新 `intent_id`。客户端不能在每次 HTTP 重试时自动创建新的 ID/token。修改商品、数量、优惠券也属于新意图，旧 token 不能修改参数。

`intent_id` 支持 1–64 位 ASCII 字母、数字、`_`、`-`。商品列表 1–100 项，商品 ID/数量为正整数，重复商品 ID 拒绝；商品顺序不影响参数摘要。普通前端 SDK 已增加 `checkoutApi.issueToken(intent_id)`，`checkoutApi.prepare` 强制要求 token；SDK 不暗中刷新 token。

| 业务码 | 含义 | 调用方行为 |
| --- | --- | --- |
| 0 | 成功，或重放已成功响应 | 使用返回的 pre_order_id；不再发起新结算 |
| 60015 | token 不存在或不属于当前用户 | 拒绝请求，不泄露所属用户 |
| 60016 | 尚未消费的 token 已过期 | 明确开始新意图后重新领取 |
| 60017 | 同 token 参数发生变化 | 恢复原参数或由用户明确发起新意图 |
| 60018 | 原请求处理中 | 稍后用原 token、原参数调用 prepare 查询结果 |
| 60019 | 结果待核对 | 不重新预扣、不自动领取新 token，人工核对 |

成功/失败结果在 token 过期后仍重放，不以缓存 TTL 放开执行。成功响应表示“该结算曾成功创建”；原预订单过期、取消或已下单后，重复 prepare 仍返回原创建结果，能否继续下单由原有订单状态/过期校验决定。

## 状态和异常

执行权只通过 `UPDATE ... WHERE user_id=? AND checkout_token=? AND prepare_state=0 AND token_expires_at > ?` 获取。并发失败方只读结果，不执行业务。首次请求摘要和快照在同一个 UPDATE 中绑定。

先查询商品及优惠券、校验金额，再注册现有 Redis 超时任务，然后预扣库存。注册任务失败时尚未预扣。库存成功后，主表金额/成功结果与全部明细在**同一个 MySQL 事务**提交。Redis 不参与幂等判定，删除或丢失 Redis key 不会恢复执行权。

获得执行权后，使用独立、最长 30 秒的上下文完成处理，客户端断开不会主动取消已启动的结算。处理中超过 35 秒再次查询时转为待核对，永不自动抢占或重跑；没有后续查询的滞留行可由排查 SQL 发现。

确定未执行业务的校验/读取失败保存失败结果；库存 RPC 超时、返回异常、COMMIT 结果不明保存待核对。提交结果不明时先读主表，若已成功则重放。不能在 COMMIT 结果不明时退还库存，也不能删除 token 后重扣。

这提供单个意图的持久化执行拦截，不承诺跨 MySQL/Redis/RPC 的 exactly-once。Redis 超时任务仍沿用既有服务，缓存整体丢失后的任务重建不属于本次改动。

### 待核对操作

本次实现采用人工核对，未增加后台自动恢复任务。`PREPARING` 记录不会被正常超时扫描器当作可用预订单释放，故待核对预扣需要运营处理，不能依赖正常超时释放。

```sql
SELECT pre_order_id, user_id, prepare_state, prepare_started_at,
       checkout_request, prepare_result
FROM checkouts
WHERE prepare_state = 4
   OR (prepare_state = 1 AND prepare_started_at <= UNIX_TIMESTAMP() - 35);
```

处理前确认原 worker 已停止。使用固定 `pre_order_id + user_id`、请求快照、checkout/inventory 日志及预扣记录核对真实结果：主表 `prepare_state=2` 表示事务成功，直接重放；确认只预扣未完成时通过已有 `ReturnPreInventory` RPC 归还，并检查业务状态码。归还确认成功或确认从未预扣后，才可经审计将意图标记为失败（`prepare_state=3`，`prepare_result` 填写非零业务码）。无法确认时保留 4 并继续核对。禁止将 1/2/3/4 改回 0，禁止删除记录后重跑原意图。

未消费、已消费记录当前均不自动删除；删除已消费记录会破坏领取请求的长期幂等映射，应在明确归档与重试窗口方案后另行处理。

## AI 集成

`checkout_prepare` 的模型 schema 仍只包含商品与优惠券，不暴露 token/intent_id/user_id。handler 从 Execution Guard/Eino 的可信 user ID、ConversationID、RunID、ToolCallID 构造领取请求 ID，调用 `IssueCheckoutToken` 后调用 `PrepareCheckout`。同次执行重复调用会取回同 token，新工具执行使用新意图。

继续沿用 Executor 的写操作审计，不开启写工具自动重试。60018、60019、PrepareCheckout 传输错误、缺失/残缺响应和调用取消均保守标记 `business_outcome=unknown`，不能归为成功或“未执行”。模型不得在收到 uncertain outcome 后换一次工具调用重新结算。创建正式订单仍须原有高风险确认。

## 测试与生成

测试源文件全部位于 `tests/checkout/`；`python tests/checkout/run.py` 使用临时 Go overlay 测试 internal 包，不在源码包落地测试文件。覆盖状态重放、参数冲突、用户隔离、过期、并发 CAS、不同 token 购买同商品、事务回滚、库存超时、COMMIT 回执丢失、API 身份透传、AI 稳定意图和 unknown 审计。

`go test ./tests/checkout` 默认不连接真实服务。显式设置 `CHECKOUT_TEST_ADDR`、`CHECKOUT_TEST_USER`、`CHECKOUT_TEST_PRODUCT` 后可运行 `TestLiveCheckoutIntentFlow`；该测试会真实创建两份库存预占，应仅在测试环境运行。本次不执行它。

生成命令（goctl v1.7.6；不会连接数据库）：

```text
goctl model mysql ddl --src dal/model/checkout/checkout.sql --dir dal/model/checkout
goctl api go --api apis/checkout/checkout.api --dir apis/checkout
# 在 services/checkout 目录执行
goctl rpc protoc checkout.proto --go_out=. --go-grpc_out=. --zrpc_out=. --client=true
```

API 生成后保留现有 ServiceContext 的公共认证中间件，不接入 goctl 新建的空白 middleware 模板；token handler 与现有 API 一样返回 `{code,msg,data}` envelope。

### 本次验证记录（2026-09-22）

- `python tests/checkout/run.py`：通过（包含 SQL 边界、checkout/API/AI handler 与 Executor 回归）。
- `go test ./services/checkout/... ./apis/checkout/... ./services/aiagent/... ./apis/ai/...`：通过。
- `npm.cmd run build`（frontend）：通过。
- `go test ./...`：已执行，失败限于现有 `test/rpc/**` 集成测试。认证、购物车、结算、库存、用户等本机 RPC 端口未启动（例如 10000、10003、10005、10011），连接被拒绝，部分既有测试在失败响应后触发 nil 指针 panic。未修改这些无关测试来掩盖失败。日志保存于 `.tmp/checkout-all-tests.log`。
- 新增真实结算集成测试默认跳过，未对用户数据库执行字段 SQL或数据写入；需用户执行字段 SQL 并启动测试环境后再验证真实跨服务流程。
