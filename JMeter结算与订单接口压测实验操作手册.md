# JMeter 结算与订单接口压测实验操作手册

适用项目：e-commerce-AI-system  
编写日期：2026-09-22  
接口依据：当前工作区 API、RPC、鉴权中间件、库存 Lua 和数据库定义。  
交付性质：实验操作手册；尚未向运行中的服务发起压测，不包含实测性能结论。本文中的并发数、SLO 和库存量均为实验起点，必须结合实际机器调整。

## 1. 实验目标与执行路线

回答四个问题：

1. 库存充足时，结算和创建订单分别能稳定承受多少业务成功请求/秒？
2. 大量用户同时购买同一商品时，是否超卖、重复建单或丢失库存？
3. 取消、超时和失败补偿后，订单、结算、Redis 库存能否最终一致？
4. 性能上限来自 API、RPC、鉴权、DTM、Redis、MySQL、消息消费还是压测机？

首次执行顺序：环境检查 → 准备用户与库存 → 单用户冒烟 → 结算基线 → 下单链路 → 阶梯加压 → 有限库存抢购 → 幂等实验 → 取消与超时恢复 → 分析报告。

本项目未发现独立 `seckill` HTTP 路由。本文使用现有结算、下单接口构造热点商品抢购，不能据此宣称系统已经具备活动资格、限购、开售时间等完整秒杀能力。支付接口不在本实验范围内。

## 2. 已核对的接口和关键实现

### 2.1 接口清单

下面端口来自开发 YAML；经统一网关部署时，改成实际域名、协议和端口，保留正确路由。

| 用途 | 方法与路径 | 开发端口 | 请求内容 / 关联字段 |
|---|---|---:|---|
| 登录、准备令牌 | POST `/douyin/user/login` | 8001 | `email`、`password` |
| 添加本人收货地址 | POST `/douyin/user/address` | 8001 | 收件人和地址信息 |
| 预结算，主测 | POST `/douyin/checkout/prepare` | 8005 | `order_items`、可选 `coupon_id` |
| 创建订单，主测 | POST `/douyin/order/create` | 8004 | `pre_order_id`、`address_id`、`payment_method` |
| 取消订单，扩展 | POST `/douyin/order/cancel` | 8004 | `order_id`、`cancel_reason` |
| 订单详情，扩展 | GET `/douyin/order/detail` | 8004 | `order_id`，见下方 GET 注意事项 |
| 订单列表，扩展 | GET `/douyin/order/list` | 8004 | `page`、`page_size`、可选 `statuses` |
| 结算详情，扩展 | GET `/douyin/checkout/detail` | 8005 | `pre_order_id` |

只测对外路由，不用 `/douyin/order/internal/create` 替代正常下单，否则绕过的链路会使容量结果失真。

**GET 参数注意事项**：当前相关请求结构使用 `json` 标签，没有 `form` 标签；go-zero v1.7.6 的表单解析读取 `form` 标签。因此不能假设 `?order_id=...` 已绑定成功。读接口先单独冒烟核对目标订单 ID；当前代码可尝试带 `Content-Type: application/json` 的 GET JSON Body，JMeter 使用 HttpClient4。网关若不支持 GET Body，先单独修复、验证参数绑定后再纳入读接口压测；本文主实验不依赖 GET。不要将参数错误的快速响应当成查询能力。

### 2.2 鉴权不是 Bearer 模式

业务请求必须同时发送：

```http
Content-Type: application/json
Access-Token: <access_token>
Cookie: Refresh-Token=<refresh_token>
```

依据：`common/consts/biz/auth.go`、`common/middleware/auth.go`。令牌绑定客户端 IP：登录与压测应走相同入口和出口 IP；分布式压测每台执行机使用自己准备的会话。不要靠随意变化 `X-Real-IP` 模拟不同用户。

`user_id` 由认证上下文决定，正常请求不传该字段。每个 `address_id` 必须属于对应登录用户。访问令牌常量有效期为 2 小时，实际运行以签发结果为准；长稳测试需要提前安排续期策略。认证刷新响应表示需要客户端重发，不表示原业务请求已经执行成功。

### 2.3 影响实验设计的事实

| 实现事实 | 对实验的影响 |
|---|---|
| 结算指纹由用户、优惠券、排序后的商品及数量组成，Redis 有效期 300 秒 | 同一用户不断提交相同购物篮，会大量得到 `60008`，不是新结算吞吐量 |
| 预结算调用库存 Redis Lua 预扣，再查商品、写结算记录 | 压测需要真实商品、正确 Redis 库存和数据库记录 |
| Redis 预库存键为 `inventory:product:<product_id>` | 仅改 MySQL 库存不能保证压测生效；缺失 Redis 键在 Lua 中按 0 处理 |
| 预结算 10 分钟过期，订单 30 分钟过期 | 预生成订单数据必须及时使用；等待释放要覆盖异步处理时间 |
| 创建订单经 DTM Saga，等待结果，再按预订单查订单 | 创建接口响应时间包含 Saga 及后续查询；需观察 DTM 和补偿 |
| 当前创建 API 实际固定传 ALIPAY | 基线传 `payment_method: 2`；不能用此实验比较支付方式性能 |
| `orders.pre_order_id` 有唯一索引 | 重复提交应核对每个预订单最终最多一个订单，不能只数成功响应 |
| 当前 `checkout_items` DDL 主键仅含 `pre_order_id` | 多商品结算需先确认实际迁移；本手册基线使用单商品，避免主键冲突干扰 |

结算的 Redis Lua 错误在上层存在映射成 `60010` 的路径，因此“库存不足”消息不一定证明只是售罄，必须结合日志和库存确认。

### 2.4 业务码口径

| 业务码 | 当前代码含义 | 如何统计 |
|---|---|---|
| `0` | 正常成功 | 还需检查返回 ID、归属及最终落库 |
| `60008` | 重复预结算 | 幂等实验的预期拒绝；普通容量实验属于异常数据/失败 |
| `100001` | 库存服务库存不足 | 售罄实验单列为预期拒绝；充足库存实验为失败 |
| `60010` | 结算库存失败映射，且与另一码有重号 | 不自动加入售罄白名单，先查原因 |
| `11003` | 订单已存在 | 重复下单可能遇到，但 Saga 可能先返回状态错误 |
| `500` | 通用服务错误 | 技术失败 |

HTTP 200 不等于业务成功。正常响应按 `code/msg/data` 包装进行验证；鉴权失败可能只有 `code/msg`。首次冒烟必须保存真实响应，确认部署版本与本手册一致。

## 3. 实验环境准备

### 3.1 记录环境基线

执行前填表，优化前后保持一致：

| 项目 | 本次记录 |
|---|---|
| Git commit / 是否有本地代码改动 | 待填 |
| API、RPC 实例数与各自 CPU / 内存 | 待填 |
| Go 版本、构建参数、GOMAXPROCS | 待填 |
| MySQL / Redis / Kafka / DTM 版本 | 待填 |
| 数据库连接池、RPC/API 超时、限流配置 | 待填 |
| 网关、TLS、网络距离、容器资源限制 | 待填 |
| 压测机 CPU、内存、JDK、JMeter 版本、出口 IP | 待填 |
| 用户数、商品数、库存、历史订单量、冷热缓存 | 待填 |
| 监控采样频率、服务日志级别 | 待填 |

正式容量实验尽量让压测机与服务分开。若只能同机运行，将结果标注为“本机综合资源条件下的实验结果”。

### 3.2 启动依赖与服务

项目依赖定义在 `construct/depend/docker-compose.yaml`，观测组件定义在 `construct/observability/docker-compose.yaml`。以下命令在仓库根目录、专用实验环境执行：

```powershell
docker compose -f construct/depend/docker-compose.yaml up -d
docker compose -f construct/depend/docker-compose.yaml ps
docker compose -f construct/observability/docker-compose.yaml up -d
```

至少启动 auths、users、product、inventory、checkout、order RPC，相关 API，以及 Consul、MySQL、Redis、Kafka、DTM；启用优惠券场景时确认 coupons RPC 可用。订单 API 构建 coupons RPC target，基础环境也应保持其正常注册。不要把“容器已启动”等同于业务依赖就绪。

根目录 `docker-compose.yml` 使用已有服务镜像/可执行文件；需按项目部署流程先构建准备。也可在独立终端用当前 YAML 启动 Go 服务，例如：

```powershell
go run ./services/checkout -f services/checkout/etc/checkout.yaml
# 另一个终端
go run ./apis/checkout -f apis/checkout/etc/checkout-api.yaml
# 订单 RPC 与 API 同理，其他依赖服务也要启动
```

开发 YAML 与 Docker 映射的 MySQL/Redis 地址可能不同，逐项对齐实际配置。执行以下检查：

```powershell
Test-NetConnection 127.0.0.1 -Port 8001
Test-NetConnection 127.0.0.1 -Port 8005
Test-NetConnection 127.0.0.1 -Port 8004
```

端口检查仅代表可连接；还要确认 Consul 健康注册、库存 Lua 已加载、DTM 可回调各 RPC、Kafka 消费者和超时扫描器正在工作。

### 3.3 安装 JMeter

从 [Apache JMeter 官方下载页](https://jmeter.apache.org/download_jmeter.cgi) 下载正式版本，按该版本要求安装兼容 JDK，记录版本；下面以安装到 `D:\tools\apache-jmeter` 为例，不要求目录带固定版本号。

```powershell
java -version
$JMeter = 'D:\tools\apache-jmeter\bin\jmeter.bat'
& $JMeter -v
```

GUI 只用于构建和少量调试，正式压测使用 CLI，关闭 View Results Tree 等重型监听器。参见 [官方最佳实践](https://jmeter.apache.org/usermanual/best-practices.html)。

### 3.4 文件布局

本文是交付的手册；以下 `.jmx`、CSV 和配置文件由操作者按后续步骤创建，并非已经随手册生成。所有实验文件放在根目录 `tests/` 下：

```text
tests/performance/jmeter/
  data/
    accounts.csv           # 已有实验账号
    sessions.csv           # 已登录会话、商品和本人地址
    preorders.csv          # 创建订单单接口实验的短期数据
  plans/
    checkout-only.jmx
    checkout-order.jmx
    order-only.jmx
    flash-sale.jmx
    duplicate-order.jmx
  conf/load.properties
  results/<实验批次>/
    samples.jtl
    jmeter.log
    html/
    environment.md
    reconciliation.md
```

```powershell
New-Item -ItemType Directory -Force -Path tests/performance/jmeter/data,tests/performance/jmeter/plans,tests/performance/jmeter/conf,tests/performance/jmeter/results
```

令牌和测试账号文件只留本地，不提交真实密码和令牌；正式 JTL 不保存请求头/响应全文。

## 4. 测试数据：先解决“压到了什么”

### 4.1 账号、商品与地址

准备真实有效的专用测试账号，不绕过邮箱注册规则伪造用户。规模较大时使用已有数据装载流程或实验库快照。每个用户至少有一个本人地址；商品必须在商品服务可查询，并在库存服务可用。

首次可通过登录后调用 `POST /douyin/user/address` 准备地址，请求示例：

```json
{
  "recipient_name": "压测用户",
  "phone_number": "13800000000",
  "province": "浙江省",
  "city": "杭州市",
  "detailed_address": "实验环境测试地址",
  "is_default": true
}
```

提取响应的 `data.data.address_id`，冒烟核对实际响应层级后写入账号文件。手机号、地址均使用专用测试数据。

`accounts.csv` 示例（示例 ID 要替换）：

```csv
user_id,email,password,address_id,product_id
10001,load001@example.test,REPLACE_TEST_PASSWORD,101,90001
10002,load002@example.test,REPLACE_TEST_PASSWORD,102,90001
```

### 4.2 小批量生成会话 CSV

在将实际执行压测的机器上运行；请求量大时分批生成，并核对首批令牌有效期。脚本仅登录，不注册账号或创建订单。

```powershell
$loginUrl = 'http://127.0.0.1:8001/douyin/user/login'
$accountsPath = Join-Path $PWD 'tests/performance/jmeter/data/accounts.csv'
$sessionsPath = Join-Path $PWD 'tests/performance/jmeter/data/sessions.csv'
$sessionRows = foreach ($account in (Import-Csv -LiteralPath $accountsPath)) {
    $body = @{ email = $account.email; password = $account.password } | ConvertTo-Json -Compress
    $resp = Invoke-RestMethod -Method Post -Uri $loginUrl -ContentType 'application/json' -Body $body
    if ($null -eq $resp.code -or $resp.code -ne 0 -or
        -not $resp.data.access_token -or -not $resp.data.refresh_token) {
        throw "登录失败，用户 $($account.user_id)，请检查响应协议与业务码"
    }
    [pscustomobject]@{
        user_id = $account.user_id
        access_token = $resp.data.access_token
        refresh_token = $resp.data.refresh_token
        address_id = $account.address_id
        product_id = $account.product_id
        quantity = 1
    }
}
$sessionRows | Export-Csv -LiteralPath $sessionsPath -NoTypeInformation -Encoding UTF8
```

输出列为：`user_id,access_token,refresh_token,address_id,product_id,quantity`。JMeter 明确填写变量名并忽略 CSV 首行，避免 PowerShell UTF-8 BOM 干扰首列名。

### 4.3 用户/请求数据预算

主容量实验以**每次迭代读取一个新的用户与购物篮组合**为原则，不循环读取相同 CSV。

例如目标 100 次新结算/秒，持续 10 分钟，需要至少 60,000 条有效、不重复指纹的数据，再为预热和波动留余量。100 个线程不意味着只需 100 个用户。热点商品、数量固定为 1 时，最直接的方式是一条请求对应一个新用户。

账号少时可以用多个有效商品组合扩大数据池，但这会改变热点分布，必须与同商品秒杀实验分开。不要仅改变请求头、伪造 nonce 或随机 `user_id` 来规避指纹，它们不会改变这里的结算判重键。

### 4.4 库存基线

- 充足库存实验：库存覆盖整个实验可能成功预占的总件数，留余量；未下单预结算也占库存。
- 有限库存实验：专用商品设为 S=100，N=500 个不同用户，各买 1 件，同一抢购波次内不取消、不补货、不等待到期。
- 记录实验前 MySQL `inventory.total/sold` 和 Redis `inventory:product:<ID>`，通过现有初始化/补货流程使它们处于可解释的一致状态。
- 不直接在有活跃预订单时 `SET` 库存或清空 Redis；这样会抹掉在途预占关系。

库存 Redis 预占与 MySQL 实扣分属不同阶段；本实验不执行支付，不能要求“创建 N 个订单后 MySQL sold 必须增加 N”。用业务单号追踪预占、取消和释放。

## 5. 先做一条完整业务冒烟

取一行会话，先在 PowerShell 验证接口。该操作会真实预占库存并创建实验订单：

```powershell
$u = Import-Csv tests/performance/jmeter/data/sessions.csv | Select-Object -First 1
$headers = @{
    'Access-Token' = $u.access_token
    'Cookie' = "Refresh-Token=$($u.refresh_token)"
}
$prepareBody = @{
    order_items = @(@{ product_id = [int]$u.product_id; quantity = 1 })
} | ConvertTo-Json -Depth 5 -Compress
$prepared = Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8005/douyin/checkout/prepare' -Headers $headers -ContentType 'application/json' -Body $prepareBody
if ($null -eq $prepared.code -or $prepared.code -ne 0 -or -not $prepared.data.pre_order_id) {
    throw '预结算未成功，停止下单，检查 prepared 响应'
}
$createBody = @{
    pre_order_id = $prepared.data.pre_order_id
    address_id = [int]$u.address_id
    payment_method = 2
} | ConvertTo-Json -Compress
$created = Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8004/douyin/order/create' -Headers $headers -ContentType 'application/json' -Body $createBody
if ($null -eq $created.code -or $created.code -ne 0 -or -not $created.data.order.order_id) {
    throw '创建订单未确认成功，记录 pre_order_id 并核对 Saga 与数据库'
}
$created.data.order | Select-Object order_id,pre_order_id,user_id,order_status,payment_status
```

核对订单归属、商品、数量和金额；保存真实成功响应的脱敏样本。移除或标记已经使用的 CSV 行，后续压测换新数据。HTTP 超时但服务可能已落库时，不马上用新预订单重试，先查这次预订单的最终状态。

## 6. JMeter 测试计划搭建

### 6.1 公共配置

打开 `jmeter.bat`，新建 Test Plan，保存到 `tests/performance/jmeter/plans/checkout-order.jmx`。

添加 User Defined Variables：

| 变量 | 值 |
|---|---|
| protocol | `${__P(protocol,http)}` |
| host | `${__P(host,127.0.0.1)}` |
| checkout_port | `${__P(checkout_port,8005)}` |
| order_port | `${__P(order_port,8004)}` |

添加 HTTP Request Defaults：协议 `${protocol}`、服务器 `${host}`、Implementation=HttpClient4、Connect Timeout=3000ms、Response Timeout=10000ms。使用 KeepAlive，关闭自动重定向和嵌入资源下载。10 秒是客户端观察上限，不代表服务端超时；单独记录网关/API/RPC/DTM 实际超时。

添加 Thread Group：

| 设置 | 冒烟值 | 正式参数 |
|---|---|---|
| Threads | 1 | `${__P(threads,10)}` |
| Ramp-up | 1 | `${__P(rampup,10)}` 秒 |
| Loop Count | 1 | `${__P(loops,-1)}`，-1 表示持续循环 |
| Specify Thread lifetime / Scheduler | 开启 | 开启 |
| Duration | 60 | `${__P(duration,120)}` 秒 |
| Sampler error 后动作 | Start Next Thread Loop | Start Next Thread Loop |

这里错误后跳到下一迭代，让结算失败时不继续下单。售罄实验也保留这个行为。持续时间到达和 CSV 耗尽都可能提前结束实验，检查最终样本量。

在线程组下添加 CSV Data Set Config：

| 字段 | 值 |
|---|---|
| Filename | `${__P(data_file)}`，运行时传绝对路径 |
| Encoding | UTF-8 |
| Variable Names | `user_id,access_token,refresh_token,address_id,product_id,quantity` |
| Ignore first line | True |
| Delimiter | `,` |
| Allow quoted data | True |
| Recycle on EOF | False |
| Stop thread on EOF | True |
| Sharing mode | All threads |

CSV 每次线程组迭代读新行。“All threads”仅在当前执行机共享读取位置，多机仍需分别切分数据。机制详见 [CSV Data Set Config](https://jmeter.apache.org/usermanual/component_reference.html#CSV_Data_Set_Config)。

在线程组下添加 HTTP Header Manager：

```text
Content-Type = application/json
Access-Token = ${access_token}
Cookie = Refresh-Token=${refresh_token}
```

本方案每次迭代切换用户，显式设置 Cookie 即可，不再额外让 Cookie Manager 保存上一个用户的会话。不要在日志或结果文件里保存这些头。

### 6.2 计划树

```text
Test Plan
├─ User Defined Variables
├─ HTTP Request Defaults
└─ Thread Group
   ├─ CSV Data Set Config
   ├─ HTTP Header Manager
   └─ Transaction Controller: TX_checkout_order
      ├─ HTTP Request: 01_checkout_prepare
      │  ├─ JSON Extractor: pre_order_id
      │  ├─ JSON Extractor: expire_time
      │  └─ JSR223 Assertion: 检查结算成功及提取值
      └─ If Controller: 结算成功才进入
         └─ HTTP Request: 02_order_create
            ├─ JSON Extractor: order_id
            └─ JSR223 Assertion: 检查创建成功与归属
```

Transaction Controller 取消 `Generate parent sample`，单独生成事务统计样本；正常基线不勾选把 timer/pre/post processor 耗时计入事务。报告按 `01_`、`02_` 和 `TX_` 分开，不能用包含事务样本的 Total 当 API QPS。

若结算失败会跳出当前迭代，事务样本未必能完整覆盖所有尝试；业务漏斗分母以 `01_checkout_prepare` 请求数为准，不能只靠 `TX_` 数量推算全链路成功率。

### 6.3 预结算 HTTP Request

Method=POST，Port=`${checkout_port}`，Path=`/douyin/checkout/prepare`。Body Data：

```json
{"order_items":[{"product_id":${product_id},"quantity":${quantity}}]}
```

添加两个 JSON Extractor：

| Variable | JSON Path | Match No. | Default |
|---|---|---:|---|
| pre_order_id | `$.data.pre_order_id` | 1 | `NOT_FOUND` |
| expire_time | `$.data.expire_time` | 1 | `0` |

默认值每次覆盖，避免失败后复用上一轮 ID。添加 JSR223 Assertion，Language=groovy，启用脚本编译缓存：

```groovy
import groovy.json.JsonSlurper

vars.put('biz_code', 'NON_JSON')
vars.put('biz_outcome', 'technical_failure')
vars.put('prepare_ok', 'false')
vars.put('order_id', '')
try {
    def body = new JsonSlurper().parseText(prev.getResponseDataAsString())
    String code = body.code == null ? 'MISSING' : body.code.toString()
    vars.put('biz_code', code)
    boolean ok = prev.isSuccessful() && code == '0' &&
        vars.get('pre_order_id') != null &&
        vars.get('pre_order_id') != 'NOT_FOUND' &&
        (body.data?.expire_time ?: 0).toString().toLong() > System.currentTimeMillis() / 1000L
    if (ok) {
        vars.put('prepare_ok', 'true')
        vars.put('biz_outcome', 'business_success')
    } else {
        if (code == '100001') vars.put('biz_outcome', 'stock_rejected')
        if (code == '60008') vars.put('biz_outcome', 'duplicate_rejected')
        AssertionResult.setFailure(true)
        AssertionResult.setFailureMessage('prepare business failure, code=' + code)
    }
} catch (Exception e) {
    AssertionResult.setFailure(true)
    AssertionResult.setFailureMessage('prepare invalid JSON or schema')
}
```

脚本不把 `stock_rejected` 改成成功；它在有限库存报告里单独归类。这样能保留原始业务失败率，避免美化成功率。

### 6.4 创建订单 HTTP Request

If Controller 的 Condition=`${prepare_ok}`，勾选 Interpret Condition as Variable Expression。内部请求 Method=POST，Port=`${order_port}`，Path=`/douyin/order/create`：

```json
{"pre_order_id":"${pre_order_id}","address_id":${address_id},"payment_method":2}
```

添加 JSON Extractor：Variable=`order_id`，Path=`$.data.order.order_id`，Match=1，Default=`NOT_FOUND`。

添加 JSR223 Assertion，Groovy：

```groovy
import groovy.json.JsonSlurper

vars.put('biz_code', 'NON_JSON')
vars.put('biz_outcome', 'technical_failure')
try {
    def body = new JsonSlurper().parseText(prev.getResponseDataAsString())
    String code = body.code == null ? 'MISSING' : body.code.toString()
    vars.put('biz_code', code)
    def order = body.data?.order
    boolean ok = prev.isSuccessful() && code == '0' && order?.order_id &&
        order.pre_order_id?.toString() == vars.get('pre_order_id') &&
        order.user_id?.toString() == vars.get('user_id')
    if (ok) {
        vars.put('biz_outcome', 'business_success')
    } else {
        vars.put('biz_outcome', 'business_failure')
        AssertionResult.setFailure(true)
        AssertionResult.setFailureMessage('create business failure or wrong owner, code=' + code)
    }
} catch (Exception e) {
    AssertionResult.setFailure(true)
    AssertionResult.setFailureMessage('create invalid JSON or schema')
}
```

JSR223 脚本通过 `vars.get()` 读取变量，不把 `${变量}` 直接写入脚本源码。1 用户调试时临时加 View Results Tree，确认成功响应、提取变量、断言和失败跳转，然后禁用该监听器。

### 6.5 结算单接口计划

另存为 `checkout-only.jmx`，移除下单请求、If Controller 和事务控制器，保留结算请求及断言。它测的是库存预占、商品读取和结算落库，不是纯计算接口。

## 7. 实验矩阵与负载设置

所有轮次先冒烟，使用不同数据批次；正式结果至少重复 3 轮。下面阶梯是起点，不能在低阶已明显过载时继续盲目加压。

| 编号 | 实验 | 线程 / 时间示例 | 数据要求 | 主要观察 |
|---|---|---|---|---|
| E0 | 冒烟 | 1 线程 × 1 次 | 全新用户与预订单 | 鉴权、协议、落库 |
| E1 | 结算基线与阶梯 | 10/25/50/100/200；每级 5 分钟 | 足够多新指纹，库存充足 | 新结算 TPS、P95/P99 |
| E2 | 结算→下单 | 同 E1，先低并发 | 每次迭代新指纹 | 完整链路成功率、DTM |
| E3 | 仅创建订单 | 10/25/50/100，每批 1～3 分钟 | 已准备、未使用、未过期预订单 | 创建接口容量 |
| E4 | 同商品抢购 | 100/200/500 线程，单波次各 1 次 | N 个用户，S=100 件库存 | 成功占位≤S、售罄、库存无负数 |
| E5 | 重复结算/下单 | 20/50/100 同步请求 | 同一用户、同一购物篮或预订单 | 最多一个有效业务结果 |
| E6 | 取消与超时恢复 | 20～100 笔受控订单 | 保存所有业务 ID | 只释放一次、最终一致 |
| E7 | 稳定性 | 稳定容量的约 60%～70%，先 60 分钟 | 足量数据、续期与库存预算 | 泄漏、积压、周期抖动 |

容量级别分别运行，预热至少 1～2 分钟且使用独立数据，预热完成后再保存正式统计。所有报告区分升压段、稳定段和排空段；需要稳定段 P95 时按 JTL 时间筛选后计算，不能平均多个 P95。

### 7.1 建议验收目标

在没有产品 SLO 时，可暂定：库存充足下结算 P95≤500ms、创建订单 P95≤1500ms，技术错误率≤0.1%、业务成功率≥99%。这些是**待确认的实验目标，不是项目已达成的指标**。

正确性必须满足：无超卖、无跨用户数据、同一预订单无重复订单、取消或超时无重复释放、补偿最终收敛。有限库存场景不要求 99% 下单成功，必须分别报告售罄率与技术错误率。

停止当前级别的建议条件：持续 30 秒技术错误率>5%、P99 超出客户端超时预算、大量连接耗尽或队列持续增长、发现负库存/重复订单/错误归属。停止发压后继续观察在途事务，不能立即清库。

### 7.2 线程数不是 QPS

基础 Thread Group 是闭环并发：响应越慢，单位时间发送越少。稳态近似 `并发数≈TPS×(平均响应时间+思考时间)`，但不能用线程数直接宣称 QPS，更不能忽略服务变慢导致的到达率下降。

需要固定速率实验时，可在目标 HTTP Sampler 下添加 Constant Throughput Timer，目标单位为 samples/min，例如 100 请求/秒填 6000，选所有活动线程共享口径。定时器只放目标请求下；全链路实验若放在父级，会同时限速多个请求。它只会延迟，不保证过载时仍达到目标速率。报告同时记录目标和实际到达率，硬性到达率测试另用经验证的开放负载方案。参见 [Constant Throughput Timer](https://jmeter.apache.org/usermanual/component_reference.html#Constant_Throughput_Timer)。

## 8. 仅创建订单实验：预订单短期数据池

直接重复使用一个 `pre_order_id` 测到的是判重/状态检查。E3 应按以下方式进行：

1. 新建低并发数据准备计划，仅执行预结算，每个用户与购物篮只用一次。
2. 在结算请求下启用 Simple Data Writer，输出 CSV 到 `results/<批次>/prepare-seed.jtl`；在该计划启动前配置 `sample_variables=user_id,pre_order_id,expire_time`，不要同时运行正式压测。
3. 依据 JTL 的 `success=true`、非空 ID 与原 `sessions.csv` 的唯一 `user_id` 联合导出 `preorders.csv`，列顺序如下。不要把 token 加到通用 JTL。
4. 启动订单实验前检查所有预订单剩余有效期覆盖 ramp-up、执行时间与安全余量；例如至少还剩 5 分钟、订单实验不超过 3 分钟。数据准备耗时过长时拆成小批，不能一次准备几个小时的数据。
5. 下单期间不再发起结算；CSV EOF 不循环，使用后作废。

```csv
user_id,access_token,refresh_token,address_id,pre_order_id,expire_time
10001,REPLACE_ACCESS,REPLACE_REFRESH,101,REPLACE_PREORDER,REPLACE_UNIX_SECONDS
```

可用以下 PowerShell 合并（文件路径替换为实际批次，保留本地敏感文件）：

```powershell
$sessionByUser = @{}
Import-Csv tests/performance/jmeter/data/sessions.csv | ForEach-Object {
    if ($sessionByUser.ContainsKey($_.user_id)) { throw '订单种子实验要求 sessions 用户唯一' }
    $sessionByUser[$_.user_id] = $_
}
$minimumExpiry = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() + 300
$preorderRows = foreach ($seed in (Import-Csv 'tests/performance/jmeter/results/REPLACE_BATCH/prepare-seed.jtl')) {
    if ($seed.label -ne '01_checkout_prepare' -or $seed.success -ne 'true') { continue }
    if (-not $seed.pre_order_id -or $seed.pre_order_id -eq 'NOT_FOUND') { continue }
    if ([long]$seed.expire_time -le $minimumExpiry) { throw '预订单剩余有效期不足，请重做小批数据' }
    $s = $sessionByUser[$seed.user_id]
    if ($null -eq $s) { throw '种子找不到对应会话' }
    [pscustomobject]@{
        user_id=$seed.user_id; access_token=$s.access_token; refresh_token=$s.refresh_token
        address_id=$s.address_id; pre_order_id=$seed.pre_order_id; expire_time=$seed.expire_time
    }
}
$preorderRows | Export-Csv tests/performance/jmeter/data/preorders.csv -NoTypeInformation -Encoding UTF8
```

另存 `order-only.jmx`：CSV 列改为上述列，只留 `02_order_create` 及订单断言，移除结算请求和 If Controller。先用 1 条记录冒烟，再换未使用数据压测。

E3 仍包含 API 鉴权、DTM、结算状态转换和 RPC，它是“创建订单 HTTP 接口实验”，不是只测订单 RPC 的 INSERT 性能。

## 9. 热点秒杀与幂等实验

### 9.1 有限库存同步抢购 E4

1. 从 `checkout-order.jmx` 另存 `flash-sale.jmx`，准备 N 行不同用户、同商品、quantity=1 的会话，记录初始可售库存 S。
2. Threads=N，Loop Count=1，Ramp-up=5 秒；Duration 足够覆盖集合和请求，如 120 秒。
3. 仅在 `01_checkout_prepare` 下添加 Synchronizing Timer：Number of Simulated Users to Group by=N，Timeout=30000ms。
4. 移除思考时间和吞吐量定时器；确保 CSV 至少 N 行，避免集合人数不足。
5. 同步释放后结算成功者立即下单；售罄者保留业务失败记录并跳过下单。
6. 压测结束立即记录 Redis、结算和订单状态；在 10 分钟预结算过期前完成首次核对。

Synchronizing Timer 仅在当前 JVM 内集合；超时会释放已到达线程，不保证刚好 N 个同时发出。用 JTL 时间戳检查真实发出时间窗。多机总线程为每机线程之和，分别配置组大小和数据。参见 [Synchronizing Timer](https://jmeter.apache.org/usermanual/component_reference.html#Synchronizing_Timer)。

判定：窗口内不补货、不取消、不发生到期释放时，成功占用库存的总件数≤S；成功订单去重数量≤成功结算去重数量；Redis 不为负。HTTP 超时后落库的记录也必须纳入核对。满库存被正确消费且没有技术失败时才预期恰好 S 件；不能只按请求成功率判定超卖。

不要把返回 `60010` 的所有请求视为正常售罄。当前代码可能将库存 RPC 技术错误转换到这一业务码，应关联服务日志与 Redis 状态。

### 9.2 重复预结算 E5-A

使用专用新用户；20 个线程共享同一个合法会话、相同商品、相同数量，Loop=1，在结算请求下添加同步定时器。本实验明确允许重复数据，不使用主容量 CSV 的“不重复”规则。

观察：一个请求实际创建结算，其余可能返回 `60008`。核对同用户该购物篮在该波次只产生一次库存预占和一个有效预订单。把初次执行失败但判重键残留、导致后续只拒绝的情况单独记录，不能因为“没有重复单”就认定正确。

### 9.3 同一预订单并发下单 E5-B

先为一个用户创建一个有效预订单。从 `order-only.jmx` 复制出 `duplicate-order.jmx`，用同一会话、地址和预订单做 20/50/100 线程同步请求；每轮使用新的预订单。

不能要求所有请求 `code=0`，也不能强行要求所有拒绝都为 `11003`，因为 Saga 可能先拒绝结算状态变更。必须同时核对：

- `orders` 按 `pre_order_id` 分组只出现一个订单。
- 成功响应中的所有 `order_id` 指向同一实际订单。
- 结算状态与订单状态一致，失败 Saga 没有误补偿成功请求。
- 无重复扣减、重复归还、孤立订单地址和明细。

另做“首个请求已经成功后顺序重放”和“请求超时后用同一业务 ID 核对”的实验，区分并发冲突、重复提交和未知提交结果。

## 10. 取消、超时及恢复实验

### 10.1 取消订单

使用已经成功创建的未支付订单；HTTP POST `/douyin/order/cancel`：

```json
{"order_id":"${order_id}","cancel_reason":"jmeter实验清理"}
```

检查 `code=0`、返回订单 ID 正确，然后等待后台处理，核对订单取消、结算状态变化、库存回补。20 个线程并发取消同一订单时，最终只释放一次；重复取消被拒绝属于预期行为。

取消测试使用独立计划和记录了所有者会话的订单 CSV，不能拿别的用户 token 取消。普通下单容量实验不要默认每轮跟一个取消，否则会额外加入大量写入、消息与库存回补，改变负载模型。

### 10.2 超时释放

| 用例 | 操作 | 观察时间 |
|---|---|---|
| 预结算未创建订单 | 创建 20 笔预结算后停止业务请求 | 超过各自 `expire_time`，再观察扫描与消费收敛 |
| 创建订单未支付 | 创建 20 笔订单后不支付、不取消 | 超过 30 分钟订单期限，再观察收敛 |
| 消费积压恢复 | 专用环境临时暂停消费者，再恢复 | 记录停顿窗口、积压峰值、追平耗时 |

超时扫描间隔常量为 1 秒，但不能推导为“1 秒内保证归还”：还取决于批次、重试、Outbox 和 Kafka 消费。先每隔 10 秒观察状态，至队列排空；超过约定收敛预算仍有孤立预占就记录失败。不要为缩短实验临时修改全局 TTL 后把结果与默认配置混为一谈。

结束负载后至少覆盖本轮最长未完成订单的期限和恢复窗口。若开启真实支付另立实验，不能使用本手册的“全部未支付应最终恢复”假设。

## 11. CLI 执行与 HTML 报告

在 `tests/performance/jmeter/conf/load.properties` 创建以下配置：

```properties
jmeter.save.saveservice.output_format=csv
jmeter.save.saveservice.print_field_names=true
jmeter.save.saveservice.timestamp_format=ms
jmeter.save.saveservice.time=true
jmeter.save.saveservice.label=true
jmeter.save.saveservice.response_code=true
jmeter.save.saveservice.response_message=true
jmeter.save.saveservice.thread_name=true
jmeter.save.saveservice.data_type=true
jmeter.save.saveservice.successful=true
jmeter.save.saveservice.assertion_results_failure_message=true
jmeter.save.saveservice.bytes=true
jmeter.save.saveservice.sent_bytes=true
jmeter.save.saveservice.thread_counts=true
jmeter.save.saveservice.url=true
jmeter.save.saveservice.latency=true
jmeter.save.saveservice.connect_time=true
jmeter.save.saveservice.idle_time=true
jmeter.save.saveservice.response_data=false
jmeter.save.saveservice.samplerData=false
jmeter.save.saveservice.requestHeaders=false
jmeter.save.saveservice.responseHeaders=false
sample_variables=user_id,pre_order_id,order_id,biz_code,biz_outcome
jmeter.reportgenerator.overall_granularity=10000
```

在仓库根目录运行一轮独立实验。下面参数只是 50 线程、5 分钟示例，必须先保证 CSV/库存预算：

```powershell
$JMeter = 'D:\tools\apache-jmeter\bin\jmeter.bat'
$batch = Get-Date -Format 'yyyyMMdd-HHmmss'
$resultDir = Join-Path $PWD "tests/performance/jmeter/results/$batch"
$plan = Join-Path $PWD 'tests/performance/jmeter/plans/checkout-order.jmx'
$data = Join-Path $PWD 'tests/performance/jmeter/data/sessions.csv'
$config = Join-Path $PWD 'tests/performance/jmeter/conf/load.properties'
New-Item -ItemType Directory -Path $resultDir | Out-Null
$jmeterArgs = @(
    '-n', '-t', $plan,
    '-q', $config,
    '-l', (Join-Path $resultDir 'samples.jtl'),
    '-j', (Join-Path $resultDir 'jmeter.log'),
    '-Jprotocol=http', '-Jhost=127.0.0.1',
    '-Jcheckout_port=8005', '-Jorder_port=8004',
    '-Jthreads=50', '-Jrampup=30', '-Jduration=300', '-Jloops=-1',
    "-Jdata_file=$data"
)
& $JMeter @jmeterArgs
if ($LASTEXITCODE -ne 0) { throw 'JMeter 运行异常，检查 jmeter.log' }
```

完成后离线生成报告，避免生成报表占用发压资源：

```powershell
& $JMeter -g (Join-Path $resultDir 'samples.jtl') -q $config -o (Join-Path $resultDir 'html')
if ($LASTEXITCODE -ne 0) { throw '报告生成失败，检查字段与输出目录' }
Start-Process (Join-Path $resultDir 'html/index.html')
```

报告输出目录必须是新的或空目录；每轮新建批次，保留原始 JTL。正常进程退出不代表断言通过，需实际检查 `success` 和业务分类。Dashboard 配置参见 [官方报告说明](https://jmeter.apache.org/usermanual/generating-dashboard.html)。

停止发压优先使用同一安装目录的 `shutdown.cmd` 发起优雅停止，等待在途采样完成。失控时才用 `stoptest.cmd`，并将这一轮标记为中断轮次，继续核对服务端在途事务。

分布式运行时每台节点的线程数都会生效，例如 3 台各 100 线程即总计 300；每台机器分配不重叠 CSV、相同版本 JMX/JMeter/JDK，令牌按该机器的出口准备。汇总保留时间戳，不平均分位数，不把单机同步屏障当作全局同时开抢。

## 12. 服务端监控与瓶颈定位

观测栈配置包含 Prometheus（宿主 9091）、Grafana（3000）、Jaeger（16686）。结算/订单 API 开发配置暴露指标端口 8105/8104，默认指标路径需按实际配置检查；容器部署还要确认指标端口映射及采集地址。

不要因为打开了 Grafana 就认为数据库和 Redis 已受监控：当前 Compose 中未包含完整 MySQL/Redis exporter，缺失时需补采集或用对应管理命令记录。

| 层级 | 至少记录 | 常见瓶颈证据 |
|---|---|---|
| 压测机 | CPU、JVM GC/堆、网卡、连接、实际发出速率 | 服务端空闲但发压机 CPU 满、实际 QPS 上不去 |
| API/鉴权 | 路由 QPS、P95/P99、业务码、鉴权 RPC 耗时 | 登录失败占大头、鉴权延迟占主要比例 |
| Go RPC | CPU、RSS、goroutine、GC、连接池、调用错误 | CPU 饱和或 goroutine 持续增长 |
| Redis | 操作延迟、CPU、内存、连接、慢日志、拒绝连接 | 热点键/Lua 排队，预库存未初始化 |
| MySQL | TPS、连接、慢查询、锁等待、死锁、磁盘 I/O | 热点行锁、连接池等待、索引/写入瓶颈 |
| DTM | Saga 耗时、失败/补偿、未结束事务 | HTTP 超时但事务仍在执行或补偿 |
| Kafka/Outbox | 消费 lag、未发送记录、重试、最老积压年龄 | 取消/过期后库存迟迟不恢复 |

Docker 部署可周期记录 `docker stats --no-stream`。MySQL 用 `SHOW FULL PROCESSLIST`、`SHOW ENGINE INNODB STATUS` 和已启用的慢查询/Performance Schema；Redis 用 `INFO stats`、`INFO clients`、`INFO memory`、`SLOWLOG GET 20`。压测期间不要持续使用 Redis `MONITOR`。

指标名以实际 `/metrics` 为准，先找请求计数和时延 histogram，再写 PromQL，不凭空假设项目导出了某个指标。Prometheus、压测机和服务机时间同步；CPU、P99、错误率、锁等待与 lag 放在同一时间轴比较。

## 13. 数据正确性核对

### 13.1 核对范围

每一轮保存用户集合、商品 ID、起止时间、成功与失败请求的预订单/订单 ID。SQL 中示例 `10001,10002` 替换成该轮专用用户；正式大批量核对可把业务 ID 清单导入专用实验辅助表再 JOIN。跨轮复用用户时，仅按用户过滤不够，必须结合业务 ID 集合。

下面 SQL 依照当前 DDL 编写，实际部署分库时到对应库查询并离线关联。只读核对在发压结束后执行，不把大扫描混入性能采样窗口。

```sql
-- 同一预订单重复订单：预期 0 行
SELECT pre_order_id, COUNT(*) AS n
FROM orders
WHERE user_id IN (10001, 10002)
GROUP BY pre_order_id
HAVING COUNT(*) > 1;

-- 订单状态数量；枚举以 services/order/order.proto 为准，DDL 注释存在差异
SELECT order_status, payment_status, COUNT(*) AS n
FROM orders
WHERE user_id IN (10001, 10002)
GROUP BY order_status, payment_status;

-- 结算预占/确认/取消/过期数量
SELECT status, COUNT(*) AS n
FROM checkouts
WHERE user_id IN (10001, 10002)
GROUP BY status;

-- 同库部署时：订单的预订单不存在、归属不同或已创建订单却未确认
SELECT o.order_id, o.pre_order_id, o.user_id, c.status
FROM orders o LEFT JOIN checkouts c ON c.pre_order_id = o.pre_order_id
WHERE o.user_id IN (10001, 10002)
  AND (c.pre_order_id IS NULL OR c.user_id <> o.user_id
       OR (o.order_status IN (1, 2) AND c.status <> 1));

-- 下单缺明细或缺地址快照：正常有效订单应为 0 行
SELECT o.order_id
FROM orders o
WHERE o.user_id IN (10001, 10002)
  AND (NOT EXISTS (SELECT 1 FROM order_items i WHERE i.order_id = o.order_id)
       OR NOT EXISTS (SELECT 1 FROM order_addresses a WHERE a.order_id = o.order_id));

-- 实验商品库存基线/终态
SELECT product_id, total, sold FROM inventory WHERE product_id = 90001;

-- 订单 Outbox 堆积概况；共享库时需再限定本实验 message_key
SELECT status, COUNT(*) AS n, MIN(created_at) AS oldest
FROM order_outbox_messages
GROUP BY status;
```

只在连接的实验 Redis 实例执行只读观察，密码使用现有凭证管理方式，不放进报告：

```text
GET inventory:product:90001
ZCARD checkout:timeout:zset
ZCARD order:timeout:zset
```

ZSET 未到期任务存在是正常现象，不能把 ZCARD>0 当作故障；还应查截止当前时间的到期成员、是否持续积压，并关联日志。不要认为 Redis 中 `inventory:deduct:lock:*` 有 TTL 就会自动回补库存，真正释放依赖业务处理。

### 13.2 数量与金额守恒

专用商品、无外部流量、无补货、无支付、无取消/过期释放的短窗口中：

```text
初始 Redis 可售量 S = 当前可售量 R + 本窗口仍有效的预占总件数 H
有效预占/订单占用件数不得超过 S
```

H 按预订单去重计件，不能把“结算一份 + 对应订单一份”算两次。请求失败或超时后若预占已产生，也必须纳入 H 和异常待处理集合。全部未支付业务已取消/到期且补偿收敛后，专用商品的可售量应恢复到实验前基线，且没有多归还。

金额按整数“分”核对：结算原价=商品单价×数量，订单优惠前金额、优惠额、应付金额相互一致。不从字符串展示金额推断已支付。

## 14. 结果分析与实验报告模板

### 14.1 必须拆开的指标

- 请求吞吐量：目标 HTTP Sampler 的请求数÷统计窗口秒数。
- 业务成功吞吐量：业务断言通过的目标请求数÷同一窗口秒数。
- 售罄拒绝率：已验证为正常售罄的请求数÷尝试数。
- 技术错误率：网络、超时、5xx、服务异常等请求数÷尝试数；不将模糊 `60010` 直接归入正常售罄。
- 下单漏斗：发起结算数 → 结算成功去重数 → 发起创建数 → 成功订单去重数 → 最终有效落库数。
- 时延：分别报告成功请求 P50/P90/P95/P99 和全部请求分位数；快速拒绝过多会拉低全部请求分位数。

链路 TPS 与单接口 QPS 分别汇报。一个链路包含结算和创建两次 HTTP 请求，再加事务样本后，JMeter Total 更不能当成“订单 TPS”。

快速查看 JTL 分类（小中型文件可用，超大 JTL 用流式统计工具）：

```powershell
$samples = Import-Csv (Join-Path $resultDir 'samples.jtl')
$httpSamples = $samples | Where-Object { $_.label -in @('01_checkout_prepare','02_order_create') }
$httpSamples | Group-Object label,biz_code,biz_outcome,success | Select-Object Count,Name
$httpSamples | Where-Object { $_.label -eq '02_order_create' -and $_.success -eq 'true' } |
    Select-Object -ExpandProperty order_id -Unique | Measure-Object
```

### 14.2 报告填表

```text
实验批次：
代码版本 / 本地修改：
环境与资源：
场景：E1/E2/E3/E4/E5/E6/E7
数据规模 / 商品数 / 初始库存：
并发 / ramp-up / 预热 / 正式稳定窗口：
目标与实际请求速率：
请求总数 / 新结算去重数 / 成功订单去重数：
结算成功 TPS / 创建成功 TPS / 链路成功 TPS：
成功请求 P50 / P95 / P99：
全部请求 P95 / P99：
业务成功率 / 售罄率 / 技术错误率：
错误码 Top 5：
API / RPC / MySQL / Redis / DTM / 压测机资源峰值：
Kafka / Outbox 峰值积压与恢复时间：
库存、重复订单、金额和归属核对结果：
异常样例 pre_order_id / order_id（不含令牌）：
瓶颈判断及支撑指标：
本轮改动变量：
3 次重复实验差异：
原始 JTL / HTML / 监控截图 / SQL 核对附件：
```

容量结论写法：“在【机器/实例/数据量/入口】条件下，连续【时间】稳定保持【业务成功 TPS】，成功请求 P95【数值】，技术错误率【数值】，库存和订单核对【结果】。”没有实测值时保留空白，不能把计划线程数写成已实现的高并发成绩。

每次优化只改变一个主要因素：连接池、索引、缓存、实例数或超时等；保持负载、数据和缓存条件可比。服务实例翻倍而 DTM/Redis/数据库没变时，不假定吞吐必然翻倍。

## 15. 常见问题排查表

| 现象 | 优先检查 |
|---|---|
| HTTP 200 但全是鉴权失败 | `Access-Token`、`Refresh-Token` Cookie、登录与压测出口 IP |
| 每个用户只有第一条结算成功 | 300 秒指纹判重、CSV 是否循环、用户是否过少 |
| 一开始就 `100001` | Redis 商品库存键、商品 ID、实际 Redis 实例/库 |
| 大量 `60010` | 不只查库存，检查库存 RPC、Redis Lua/连接异常 |
| 创建订单参数错误/过期 | `pre_order_id` 提取、地址归属、种子有效期、错误后是否仍下单 |
| 创建接口超时但有订单落库 | DTM WaitResult、API/RPC 超时、Saga 最终状态 |
| GET 查询全失败或总返回默认页 | 当前 `json`/`form` 标签及网关 GET Body 支持 |
| QPS 很高但新增订单很少 | 将判重/售罄当成功、统计了事务样本、指纹复用 |
| 发压过程中线程数逐渐减少 | CSV EOF、线程停止、持续时间设置 |
| 同步集合一直等/波次人数不足 | Timer 人数大于实际线程、CSV 不够、超时设置 |
| P95 很好但成功率低 | 快速失败样本拉低时延，拆开成功/失败分布 |
| 停压后库存没恢复 | 10/30 分钟期限、扫描任务、Outbox、Kafka、重复/丢失释放 |
| 加线程无收益，服务端空闲 | 压测机 CPU/GC、端口与网络、监听器、请求 pacing |

## 16. 实验收尾

1. 停止发压，保存 JTL、配置、环境、监控与业务 ID 清单。
2. 等待在途 Saga 完成并核对取消/超时恢复；异常记录保留到定位结束。
3. 按实验业务 ID 清单取消未支付订单，或等待正常过期；记录失败项，不全表删除。
4. 需要重复实验时恢复专用环境快照，或按已有初始化流程重建测试数据与一致库存；确认没有仍会回调旧业务单的后台任务。
5. 不执行 `FLUSHALL`、不直接重置正在使用的库存键、不以清库方式掩盖补偿失败。
6. 复核文件不含可用密码/令牌后再归档报告。

## 17. 核对依据

仓库依据：

- `apis/checkout/checkout.api`、`apis/order/order.api`、`apis/user/user.api`：接口和参数。
- `apis/checkout/internal/logic/checkoutlogic.go`：认证用户与结算 RPC 映射。
- `apis/order/internal/logic/createorderlogic.go`：DTM Saga、ALIPAY、返回详情。
- `common/middleware/auth.go`、`common/middleware/clientip.go`、`common/consts/biz/auth.go`：令牌、Cookie、客户端 IP。
- `services/checkout/internal/logic/preparecheckoutlogic.go`：300 秒指纹、库存预扣和结算写入。
- `services/inventory/internal/logic/decreasepreinventorylogic.go`、`services/inventory/internal/decreaselua/decreaselua.lua`：库存键、Lua 与错误码。
- `services/order/internal/logic/createorderlogic.go`、`services/order/order.proto`：订单数据、状态枚举。
- `common/consts/biz/order.go`：超时与扫描常量。
- `dal/model/checkout/checkout.sql`、`dal/model/order/order.sql`、`dal/model/inventory/inventory.sql`：对账字段和唯一约束。
- `construct/depend/docker-compose.yaml`、`construct/observability/docker-compose.yaml`：基础设施与观测入口。

工具设置依据：本手册链接的 Apache JMeter 官方最佳实践、组件手册与 Dashboard 文档。实际 UI 名称随版本可能变化，执行时记录所用版本，并用单用户冒烟验证计划。
