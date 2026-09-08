# AI 客服接口 JMeter 压测计划

## 1. 压测目标

本计划只压测 AI 客服相关接口，不单独压测商品、购物车、订单、优惠券等普通电商 HTTP API。电商后端服务仍作为 AI 工具调用的下游依赖参与链路观测。

压测目标：

- 验证 `POST /douyin/ai/chat` SSE 流式接口在并发会话下的建连成功率、首事件耗时、完整响应耗时、错误率和连接稳定性。
- 验证 AI 客服业务查询链路：AI 网关 -> aiagent RPC -> Eino Agent -> Tool -> 电商 RPC -> SSE 返回。
- 验证低风险写操作链路：添加购物车、领取优惠券、更新用户资料、地址操作等是否稳定，并记录审计。
- 验证高风险确认链路：创建订单、取消订单、删除购物车商品等必须先返回 `confirmation_required`，确认后才能执行。
- 验证历史会话接口 `GET /douyin/ai/sessions` 和 `GET /douyin/ai/sessions/messages` 的分页、延迟和用户隔离。
- 验证模型不可用、工具超时、下游 RPC 失败时不会伪造成成功回复。

## 2. 压测接口范围

| 接口 | 方法 | 默认端口 | 说明 |
|---|---|---:|---|
| `/douyin/ai/chat` | POST | 8007 | SSE 聊天、工具调用、确认提交统一入口 |
| `/douyin/ai/sessions` | GET | 8007 | 当前用户历史会话列表 |
| `/douyin/ai/sessions/messages` | GET | 8007 | 指定会话历史消息 |

依赖服务只观测不直接压测：`services/aiagent`、product、inventory、order、checkout、cart、coupon、user、audit RPC、MySQL、Redis、Kafka、Consul、LLM Provider、RAG 服务。

## 3. 前置条件

你已经获取 token，因此 JMeter 不再配置登录取 token 步骤。

压测前需要准备：

- `access_token`：填入 JMeter 变量 `access_token`。
- `refresh_token`：如果接口鉴权要求 refresh cookie，也填入 JMeter 变量 `refresh_token`。
- AI API 地址：默认 `http://localhost:8007`。
- 测试数据 CSV：

| 文件 | 字段 | 用途 |
|---|---|---|
| `ai_prompts.csv` | `scenario,prompt` | AI 多场景自然语言请求 |
| `orders.csv` | `order_id,cancelable` | 订单查询、取消订单确认 |
| `products.csv` | `product_id` | 商品查询、加购 |
| `coupons.csv` | `coupon_id` | 优惠券查询、领取 |
| `conversations.csv` | `conversation_id` | 历史会话查询，可由前置压测生成 |

建议测试数据与真实用户数据隔离，尤其是取消订单、创建订单、删除购物车等会改变业务状态的场景。

## 4. JMeter 全局配置

### 4.1 创建 Test Plan

1. 打开 JMeter。
2. `File` -> `New`。
3. 右键 `Test Plan` -> `Add` -> `Config Element` -> `User Defined Variables`。
4. 添加变量：

| Name | Value 示例 |
|---|---|
| `protocol` | `http` |
| `ai_host` | `localhost` |
| `ai_port` | `8007` |
| `access_token` | 你已经获取的 access token |
| `refresh_token` | 你已经获取的 refresh token，没有则留空 |

### 4.2 配置 Cookie

1. 右键 `Test Plan` -> `Add` -> `Config Element` -> `HTTP Cookie Manager`。
2. 如果每个线程要模拟独立会话，勾选 `Clear cookies each iteration`。
3. 如果要模拟同一会话多轮对话，不勾选。
4. 如果需要 refresh cookie，在线程下添加 `JSR223 PreProcessor`：

```groovy
import org.apache.jmeter.protocol.http.control.Cookie

def refresh = vars.get('refresh_token')
if (refresh) {
    def cm = sampler.getCookieManager()
    cm.add(new Cookie('Refresh-Token', refresh, vars.get('ai_host'), '/', false, 0))
}
```

### 4.3 配置 Header

右键 Thread Group -> `Add` -> `Config Element` -> `HTTP Header Manager`。

AI SSE 请求添加：

| Name | Value |
|---|---|
| `Content-Type` | `application/json` |
| `Accept` | `text/event-stream` |
| `Access-Token` | `${access_token}` |

历史会话 GET 请求添加：

| Name | Value |
|---|---|
| `Access-Token` | `${access_token}` |

## 5. AI SSE 压测方式

JMeter 原生 `HTTP Request` 可以压 `POST /douyin/ai/chat`，但它通常把整个 SSE 流结束时间作为一次请求耗时。AI 接口需要拆两个关键指标：

- `Latency`：首个 SSE 事件耗时，即从请求发出到收到第一个 `event:` 或 `data:`。
- `Elapsed`：完整响应耗时，即从请求发出到最终 `assistant_message`、`error` 或连接关闭。

建议：

- 调试阶段先用普通 `HTTP Request`，确认 body、token、断言正确。
- 正式压测使用 `JSR223 Sampler` 读取 SSE 行，并把首事件耗时写入 `Latency`。

## 6. Thread Group 设计

### 6.1 AI 普通聊天

目标：验证不调用业务工具时的模型响应能力。

JMeter 步骤：

1. 新建 `Thread Group - AI Chat Basic`。
2. 线程数从 5 开始，Ramp-up 60 秒，持续 5 分钟。
3. 添加 `HTTP Request`：
   - Method: `POST`
   - Server Name: `${ai_host}`
   - Port: `${ai_port}`
   - Path: `/douyin/ai/chat`
   - Body Data:

```json
{
  "type": "user_message",
  "conversation_id": "conv-${__UUID()}",
  "client_message_id": "jm-basic-${__time()}-${__threadNum}",
  "content": "你好，请简单介绍一下你能帮我做什么",
  "metadata": {
    "source": "jmeter",
    "scenario": "ai_chat_basic"
  }
}
```

4. 添加 `Response Assertion`：
   - 包含 `event:`。
   - 包含 `assistant_message` 或 `error`。
   - 不允许空响应。
5. 添加 `Duration Assertion`：
   - 建议阈值 30000 ms，可按模型性能调整。

### 6.2 AI 业务查询

目标：验证 AI 工具查询链路，不直接调用普通电商 API。

Prompt 示例：

| 场景 | Prompt |
|---|---|
| 商品查询 | `帮我查一下商品 ${product_id} 的详情和库存` |
| 商品推荐 | `推荐几款适合学生党的数码产品，价格不要太高` |
| 订单查询 | `帮我查一下订单 ${order_id} 的状态` |
| 购物车查询 | `看看我的购物车里有什么` |
| 优惠券查询 | `我有哪些可用优惠券` |
| 地址查询 | `帮我看一下我的默认收货地址` |

JMeter 步骤：

1. 新建 `Thread Group - AI Tool Query`。
2. 添加 `CSV Data Set Config`：
   - `products.csv`
   - `orders.csv`
   - `ai_prompts.csv`
3. 添加 `HTTP Request` 或 `JSR223 Sampler` 请求 `/douyin/ai/chat`。
4. Body 示例：

```json
{
  "type": "user_message",
  "conversation_id": "conv-${__UUID()}",
  "client_message_id": "jm-query-${__time()}-${__threadNum}",
  "content": "帮我查一下商品 ${product_id} 的详情和库存",
  "metadata": {
    "source": "jmeter",
    "scenario": "ai_tool_query"
  }
}
```

5. 添加断言：
   - 包含 `tool_progress` 或 `tool_result`。
   - 最终包含 `assistant_message` 或明确的 `error`。
   - 不允许最终回答把失败工具描述为成功。
6. 服务端复核：
   - `ai_tool_calls` 中有对应 tool name、status、latency。
   - 查询类工具目标超时为 3 秒。

### 6.3 AI 低风险写操作

目标：验证 AI 发起低风险写操作时的吞吐、延迟、审计和失败处理。

Prompt 示例：

- `把商品 ${product_id} 加入购物车`
- `帮我领取优惠券 ${coupon_id}`
- `把我的用户名改成 JMeterUser${__threadNum}`
- `新增一个测试收货地址，收件人张三，手机号 13800000000，广东省深圳市南山区测试路 1 号`

JMeter 步骤：

1. 新建 `Thread Group - AI Low Risk Write`。
2. 添加 `CSV Data Set Config`：`products.csv`、`coupons.csv`。
3. 添加 `/douyin/ai/chat` 请求。
4. Body 示例：

```json
{
  "type": "user_message",
  "conversation_id": "conv-${__UUID()}",
  "client_message_id": "jm-low-write-${__time()}-${__threadNum}",
  "content": "把商品 ${product_id} 加入购物车",
  "metadata": {
    "source": "jmeter",
    "scenario": "ai_low_risk_write"
  }
}
```

5. 添加断言：
   - 成功时包含 `tool_result`。
   - 失败时包含明确失败信息。
   - 不应返回 `confirmation_required`，除非当前工具元数据已被调整为高风险。
6. 服务端复核：
   - 写类工具目标超时为 5 秒。
   - 写成功必须有 audit 或 AI tool call 记录。

### 6.4 AI 高风险确认

目标：验证高风险操作必须先确认，确认后才能执行，重复确认和越权确认不能执行。

#### 第一步：触发确认

JMeter 请求：

```json
{
  "type": "user_message",
  "conversation_id": "${conversation_id}",
  "client_message_id": "jm-confirm-start-${__time()}-${__threadNum}",
  "content": "帮我取消订单 ${order_id}",
  "metadata": {
    "source": "jmeter",
    "scenario": "ai_high_risk_confirm"
  }
}
```

JMeter 步骤：

1. 新建 `Thread Group - AI High Risk Confirm`。
2. 添加 `JSR223 PreProcessor` 固定会话 ID：

```groovy
if (!vars.get('conversation_id')) {
    vars.put('conversation_id', 'conv-' + UUID.randomUUID().toString())
}
```

3. 添加 `HTTP Request - trigger confirmation`：
   - Method: `POST`
   - Path: `/douyin/ai/chat`
4. 添加 `Response Assertion`：
   - 必须包含 `confirmation_required`。
   - 不应包含订单已取消、订单已创建等最终执行成功文本。
5. 添加 `Regular Expression Extractor`：
   - Name: `confirmation_id`
   - Regular Expression: `"confirmation_id"\s*:\s*"([^"]+)"`
   - Template: `$1$`
   - Match No.: `1`

#### 第二步：批准确认

JMeter 请求：

```json
{
  "type": "confirm_action",
  "conversation_id": "${conversation_id}",
  "confirmation_id": "${confirmation_id}",
  "approved": true
}
```

JMeter 步骤：

1. 添加 `If Controller`：
   - Condition: `${__groovy(vars.get('confirmation_id') != null && vars.get('confirmation_id').length() > 0)}`
2. 在 If Controller 下添加 `HTTP Request - approve confirmation`。
3. 添加断言：
   - 包含 `tool_result`、`assistant_message` 或明确 `error`。
   - 业务执行结果必须和服务端订单状态一致。
4. 添加重复确认请求：
   - 再次发送同一个 `confirmation_id`。
   - 断言必须被拒绝，不能重复执行业务 RPC。

#### 第三步：拒绝确认

另建低比例线程或单独测试：

```json
{
  "type": "confirm_action",
  "conversation_id": "${conversation_id}",
  "confirmation_id": "${confirmation_id}",
  "approved": false
}
```

断言：

- 不调用下游业务 RPC。
- 返回拒绝或取消结果。
- `ai_confirmations` 状态变为 `rejected`。

### 6.5 AI 历史会话

目标：验证历史会话列表和消息列表在并发查询下的性能。

JMeter 步骤：

1. 新建 `Thread Group - AI History Query`。
2. 添加 `HTTP Request - list sessions`：
   - Method: `GET`
   - Path: `/douyin/ai/sessions`
   - Parameters: `page=1`、`page_size=20`
3. 添加 `JSON Extractor`：
   - `conversation_id`: `$.data.conversations[0].conversation_id`
   - 如果响应不是 envelope，则使用 `$.conversations[0].conversation_id`。
4. 添加 `HTTP Request - list messages`：
   - Method: `GET`
   - Path: `/douyin/ai/sessions/messages`
   - Parameters: `conversation_id=${conversation_id}`、`page=1`、`page_size=50`
5. 添加断言：
   - HTTP 200。
   - 只返回当前 token 用户自己的会话。
   - 响应中存在 `conversation_id`、`messages` 或空列表。

## 7. JSR223 SSE 精确采样器

正式压测建议用该 Sampler 替代普通 HTTP Request，以记录首事件耗时。

添加位置：Thread Group -> `Add` -> `Sampler` -> `JSR223 Sampler`，Language 选择 `groovy`。

```groovy
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.time.Duration

def conv = vars.get('conversation_id')
if (!conv) {
    conv = 'conv-' + UUID.randomUUID().toString()
    vars.put('conversation_id', conv)
}

def prompt = vars.get('prompt') ?: '你好，请帮我查询一下我的购物车'
def body = """
{
  "type": "user_message",
  "conversation_id": "${conv}",
  "client_message_id": "jm-sse-${System.currentTimeMillis()}-${ctx.getThreadNum()}",
  "content": "${prompt}",
  "metadata": {"source":"jmeter","scenario":"sse_jsr223"}
}
"""

def client = HttpClient.newBuilder()
    .connectTimeout(Duration.ofSeconds(5))
    .build()

def builder = HttpRequest.newBuilder()
    .uri(URI.create("${vars.get('protocol')}://${vars.get('ai_host')}:${vars.get('ai_port')}/douyin/ai/chat"))
    .timeout(Duration.ofSeconds(305))
    .header("Content-Type", "application/json")
    .header("Accept", "text/event-stream")
    .header("Access-Token", vars.get('access_token'))
    .POST(HttpRequest.BodyPublishers.ofString(body, java.nio.charset.StandardCharsets.UTF_8))

def refresh = vars.get('refresh_token')
if (refresh) {
    builder.header("Cookie", "Refresh-Token=" + refresh)
}

def req = builder.build()
def result = prev
def start = System.nanoTime()
def firstEventMs = -1L
def responseText = new StringBuilder()
def eventCount = 0

try {
    def resp = client.send(req, HttpResponse.BodyHandlers.ofInputStream())
    result.setResponseCode(String.valueOf(resp.statusCode()))

    resp.body().withReader("UTF-8") { reader ->
        String line
        while ((line = reader.readLine()) != null) {
            responseText.append(line).append('\n')
            if (line.startsWith('event:') || line.startsWith('data:')) {
                eventCount++
                if (firstEventMs < 0) {
                    firstEventMs = ((System.nanoTime() - start) / 1_000_000L) as long
                    result.setLatency(firstEventMs)
                }
            }
            if (line.startsWith('event: done')) {
                break
            }
        }
    }

    def totalMs = ((System.nanoTime() - start) / 1_000_000L) as long
    def text = responseText.toString()
    def ok = resp.statusCode() == 200 && eventCount > 0
    result.setSuccessful(ok)
    result.setResponseMessage("first_event_ms=${firstEventMs}, total_ms=${totalMs}, event_count=${eventCount}")
    result.setResponseData(text.take(4096), "UTF-8")
} catch (Throwable t) {
    result.setSuccessful(false)
    result.setResponseMessage(t.toString())
    result.setResponseData(responseText.toString().take(4096), "UTF-8")
}
```

## 8. AI 专用混合流量模型

| 场景 | 占比 | Thread Group |
|---|---:|---|
| 普通聊天 | 25% | `AI Chat Basic` |
| 商品/订单/优惠券/地址查询 | 35% | `AI Tool Query` |
| 低风险写操作 | 15% | `AI Low Risk Write` |
| 高风险确认触发 | 10% | `AI High Risk Confirm` |
| 历史会话查询 | 15% | `AI History Query` |

如果 LLM Provider 有 QPS 或并发限制，优先按模型限制设置 AI 总并发，不要只按后端机器 CPU 设置。

## 9. 压测阶段

| 阶段 | 线程数 | Ramp-up | 持续时间 | 目标 |
|---|---:|---:|---:|---|
| Smoke | 3-5 | 30s | 5m | 验证 token、SSE、断言、CSV |
| Baseline | 10 | 2m | 10m | 获取低压基线 |
| Load | 30 | 5m | 30m | 验证目标负载 |
| Stress | 30 -> 100 | 每级 5m | 30-60m | 找到 AI 链路拐点 |
| Spike | 10 -> 80 -> 10 | 1m 上升 | 15m | 验证突增连接 |
| Soak | 30 | 10m | 2h | 验证长时间稳定性 |
| Failure | 10-20 | 2m | 15m | 关闭 LLM/RAG/Redis/Kafka 任一依赖验证降级 |

## 10. 通过标准

| 指标 | 目标 |
|---|---|
| `/douyin/ai/chat` HTTP 200 比例 | >= 99% |
| SSE 有事件返回比例 | >= 99% |
| 首个业务事件 P95 | <= 2s |
| 普通聊天完整响应 P95 | <= 30s，按模型能力可调整 |
| 工具查询完整响应 P95 | <= 45s，且单个查询工具 <= 3s |
| 写操作完整响应 P95 | <= 60s，且单个写工具 <= 5s |
| 历史会话查询 P95 | <= 800ms |
| 高风险操作 | RPC 执行前必须返回 `confirmation_required` |
| 重复确认 | 必须拒绝，不能重复执行业务 RPC |
| 工具失败 | 最终回答不能描述为成功 |
| 审计记录 | 写操作必须有 `ai_tool_calls` 和 audit 记录 |

## 11. 监控项

压测期间同时观察：

- JMeter：TPS、P90/P95/P99、错误率、Latency、Elapsed、活跃线程、连接异常。
- ai-api 指标端口：默认 8107。
- aiagent.rpc 指标端口：默认 11009。
- MySQL：`ai_messages`、`ai_tool_calls`、`ai_confirmations` 写入速度、慢 SQL、锁等待、连接数。
- Redis：confirmation lock、限流、缓存命中率、慢命令、连接数。
- Kafka：`ai-memory-updates` 生产情况、consumer lag。
- LLM Provider：QPS、限流、首 token 时间、总响应时间、错误率、token 成本。
- RAG 服务：retrieve/embedding 延迟、错误率、缓存命中。
- 下游 RPC：product、inventory、order、checkout、cart、coupon、user、audit 的超时和错误。

## 12. 命令行执行

GUI 调试完成后，用非 GUI 模式执行。

Windows：

```powershell
.\jmeter.bat -n `
  -t test\jmeter\ai-api-load.jmx `
  -l test\results\ai-api-load.jtl `
  -e `
  -o test\results\ai-api-load-report
```

Linux/macOS：

```bash
jmeter -n \
  -t test/jmeter/ai-api-load.jmx \
  -l test/results/ai-api-load.jtl \
  -e \
  -o test/results/ai-api-load-report
```

建议按日期和阶段保存：

```text
test/results/ai-2026-08-30-smoke/
test/results/ai-2026-08-30-load-30vu/
test/results/ai-2026-08-30-stress-100vu/
```

## 13. JMeter 测试计划结构

```text
Test Plan
|-- User Defined Variables
|-- HTTP Cookie Manager
|-- Thread Group - AI Chat Basic
|-- Thread Group - AI Tool Query
|-- Thread Group - AI Low Risk Write
|-- Thread Group - AI High Risk Confirm
|-- Thread Group - AI History Query
|-- Summary Report
|-- Backend Listener
`-- tearDown Thread Group - optional status check
```

正式压测注意：

- 不要启用 `View Results Tree`。
- SSE 响应内容很大时，只保存前 4KB 或只保存事件统计。
- 高风险确认场景使用专用订单数据，避免误取消正常订单。
- 每轮压测前记录 token 对应用户，压测后检查该用户的会话、工具调用和确认记录。

## 14. 报告模板

```text
压测名称：
压测时间：
代码版本/镜像版本：
JMeter 版本：
AI API 地址：
Token 用户：
线程数/Ramp-up/持续时间：
场景比例：

核心结果：
- 总请求数：
- TPS：
- HTTP 200 比例：
- SSE 有事件返回比例：
- Latency P90/P95/P99：
- Elapsed P90/P95/P99：
- 错误率：
- 首事件 P95：
- 完整响应 P95：
- confirmation_required 数量：
- tool_result 成功/失败数量：

服务端观测：
- ai-api CPU/内存：
- aiagent CPU/内存：
- MySQL：
- Redis：
- Kafka：
- LLM Provider：
- RAG：
- 下游 RPC：

问题样例：
- 请求 body：
- 响应事件：
- trace/log：

结论：
- 是否达标：
- 最大稳定 AI 并发：
- 系统瓶颈：
- 优化建议：
```
