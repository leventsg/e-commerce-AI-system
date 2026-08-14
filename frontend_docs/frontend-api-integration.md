# 前端 API 接入确认文档

> 本文档基于当前仓库代码确认前端需要接入的 API、接口用途、接入方式和页面展示规则。当前前端代码位于 `frontend/`，设计规范位于 `frontend_docs/`。

## 1. 接入结论

前端首期应优先完成 AI 客服控制台闭环：

1. 登录获取 `access_token` 和 `refresh_token`。
2. 所有受保护接口携带 `Access-Token` Header 和 `Refresh-Token` Cookie。
3. 用 `POST /douyin/ai/chat` 建立 SSE 流式聊天请求，替换当前 `mockStreamAgentChat`。
4. 在聊天流里展示用户消息、AI 增量回复、工具调用进度、工具结果、确认卡片和错误。
5. 业务 REST 接口先作为工具结果详情和后续商城页面的数据源，不应由前端绕过 AI 确认策略直接执行高风险 AI 操作。

当前代码事实源：

- `apis/ai/ai.api` 定义 `post /douyin/ai/chat`。
- `apis/ai/internal/logic/chatlogic.go` 实现 `text/event-stream`。
- `apis/ai/internal/types/protocol.go` 定义客户端消息和服务端事件。
- `services/aiagent/aiagent.proto` 定义内部 Chat / ConfirmAction 流式 RPC。
- `frontend/src/hooks/useAgent.ts` 当前使用 `frontend/src/services/api/mock.ts`，需要替换为真实 SSE service。

注意：`docs/ai-customer-service-api.md` 当前描述了 WebSocket 版本，和代码实现不一致。前端接入以代码中的 SSE `POST /douyin/ai/chat` 为准，后续应同步更新该后端文档。

## 2. 前端 API 层结构

按现有 `frontend_docs/frontend-design-spec.md`，API 层保持三层：

| 层级 | 文件建议 | 职责 |
| --- | --- | --- |
| HTTP Client | `frontend/src/services/api/client.ts` | 统一 base URL、认证 Header、响应包装解析、401/认证刷新处理 |
| Service | `frontend/src/services/api/auth.ts`、`agent.ts`、`mall.ts` | 按业务封装登录、AI SSE、商品/购物车/订单/优惠券/结算/支付接口 |
| Hook | `frontend/src/hooks/useAgent.ts`、`useAuth` 相关 hook | 管理页面状态、取消请求、流式事件落 UI、错误提示 |

当前 `API_BASE = '/api/v1'` 与后端真实路由 `/douyin/**` 不匹配。建议改为：

```ts
export const API_BASE = import.meta.env.VITE_API_BASE || ''
```

开发环境使用 Vite proxy 将 `/douyin` 转发到后端 API 网关。当前 `frontend/vite.config.ts` 只代理 `/api`，接入时应改为代理 `/douyin`，或由后端网关统一暴露 `/api/v1 -> /douyin` 映射。本文档后续示例按直接请求 `/douyin/**` 编写。

## 3. 通用认证接入

### 3.1 登录接口

| 项 | 内容 |
| --- | --- |
| 方法 | `POST` |
| 路径 | `/douyin/user/login` |
| 请求体 | `{ "email": string, "password": string }` |
| 响应数据 | `{ access_token: string, refresh_token: string }` |
| 页面用途 | 登录页提交账号密码，成功后进入 AI 控制台 |

普通 JSON API 成功响应会被 go-zero `JsonBaseResponseCtx` 包装：

```json
{
  "code": 0,
  "msg": "ok",
  "data": {
    "access_token": "access-token",
    "refresh_token": "refresh-token"
  }
}
```

前端 `LoginPage` 当前使用用户名 mock 登录。接入后字段需要调整为 `email/password`，成功后：

- `access_token` 存入 `localStorage` 或内存态，用于 `Access-Token` Header。
- `refresh_token` 写入 Cookie，名称必须为 `Refresh-Token`，用于后端认证中间件读取。
- 可同时调用 `/douyin/user/info` 获取 `user_name/email/avatar` 展示在 `TopBar` 和 `Sidebar`。

### 3.2 受保护接口认证

后端认证中间件要求：

| 位置 | 名称 | 必填 | 说明 |
| --- | --- | --- | --- |
| Header | `Access-Token` | 是 | 登录接口返回的访问令牌 |
| Cookie | `Refresh-Token` | 是 | 登录接口返回的刷新令牌 |

前端请求示例：

```ts
await fetch('/douyin/product/list?page=1&size=10', {
  headers: { 'Access-Token': accessToken },
  credentials: 'include',
})
```

认证失败时后端通常仍返回 HTTP 200，但业务响应 `code !== 0`。前端不能只判断 HTTP status，需要统一解析业务码。

访问令牌过期时，后端会返回刷新令牌响应，响应形态为：

```json
{
  "code": 10004,
  "msg": "令牌续期成功",
  "data": {
    "access_token": "new-access-token",
    "refresh_token": "new-refresh-token"
  }
}
```

前端处理策略：

1. 如果 `code === 0`，返回 `data`。
2. 如果是 token renewed 业务码，更新本地 token 和 Cookie，然后重试原请求一次。
3. 如果认证失败或刷新失败，清空登录态并触发 `auth-unauthorized`，跳转 `/login`。
4. 写操作不得在网络错误或 `business_executed=true` 场景下自动重试。

## 4. AI 客服 SSE 接口

### 4.1 用户消息

| 项 | 内容 |
| --- | --- |
| 方法 | `POST` |
| 路径 | `/douyin/ai/chat` |
| Content-Type | `application/json` |
| Accept | `text/event-stream` |
| 响应 | SSE 事件流 |
| 超时 | 后端单次请求最长 5 分钟，空闲每 10 秒 `: ping` |

请求体：

```json
{
  "type": "user_message",
  "conversation_id": "conv_xxx",
  "client_message_id": "client_msg_1730000000000_1",
  "content": "查看我的订单",
  "metadata": {
    "source": "web"
  }
}
```

字段说明：

| 字段 | 必填 | 前端来源 |
| --- | --- | --- |
| `type` | 是 | 固定为 `user_message` |
| `conversation_id` | 否 | 当前会话 ID；新会话可为空 |
| `client_message_id` | 是 | 前端生成，用于用户消息幂等 |
| `content` | 是 | 输入框内容，trim 后不能为空 |
| `metadata.source` | 否 | 固定传 `web` |

安全要求：

- 不允许发送 `user_id`。
- `client_message_id` 必须每条用户消息唯一；用户手动重试同一条消息时可复用同一 ID，避免后端重复执行工具。

### 4.2 确认操作

高风险操作由服务端先返回 `confirmation_required`，前端展示确认卡片。用户点击后再次调用同一个 SSE 接口：

```json
{
  "type": "confirm_action",
  "conversation_id": "conv_xxx",
  "confirmation_id": "confirm_xxx",
  "approved": true
}
```

拒绝：

```json
{
  "type": "confirm_action",
  "conversation_id": "conv_xxx",
  "confirmation_id": "confirm_xxx",
  "approved": false
}
```

字段说明：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `type` | 是 | 固定为 `confirm_action` |
| `conversation_id` | 是 | 当前会话 ID |
| `confirmation_id` | 是 | `confirmation_required` 返回的确认 ID |
| `approved` | 是 | `true` 批准，`false` 拒绝 |

前端只提交确认 ID 和是否批准，不提交工具参数。过期、重复、跨用户、跨会话确认由后端拒绝，前端按 `error` 或失败 `tool_result` 展示。

### 4.3 服务端事件

`/douyin/ai/chat` 返回标准 SSE：

```text
event: assistant_delta
id: msg_xxx
data: {"type":"assistant_delta","conversation_id":"conv_xxx","message_id":"msg_xxx","content":"您好","done":false}
```

前端需要解析：

| 事件类型 | 用途 | 页面展示 |
| --- | --- | --- |
| `assistant_delta` | AI 流式片段 | 追加到当前 assistant 气泡，展示光标 |
| `assistant_message` | AI 最终回复或非流式回复 | 结束当前 assistant 气泡；若本轮已有 delta，不重复追加同文案 |
| `tool_progress` | 工具开始执行 | 展示运行中的工具胶囊和思考链路 |
| `tool_result` | 工具执行结果 | 更新工具状态和摘要，可展开结构化数据 |
| `confirmation_required` | 高风险确认请求 | 展示确认卡片，包含摘要、过期倒计时、确认/取消按钮 |
| `error` | 协议、认证、RPC 或业务错误 | 展示错误气泡，停止本轮流式状态 |
| `done` | SSE 结束标记 | 关闭当前请求，`isStreaming=false` |

服务端事件字段：

```ts
interface ServerEvent {
  type: 'assistant_message' | 'assistant_delta' | 'tool_progress' | 'tool_result' | 'confirmation_required' | 'error'
  conversation_id?: string
  message_id?: string
  content?: string
  tool?: string
  status?: string
  data?: unknown
  confirmation_id?: string
  action?: string
  summary?: string
  expires_at?: number
  done: boolean
}
```

当前前端类型 `AgentEvent` 使用 `data_json` 字符串，而 API 网关实际输出 `data` JSON。接入时应将前端类型改为 `data?: unknown`，或在 service 层兼容 `data_json` 和 `data`。

### 4.4 SSE Service 设计

建议新增 `frontend/src/services/api/agent.ts`：

```ts
export async function* streamAgentChat(
  payload: ClientMessage,
  token: string,
  signal?: AbortSignal,
): AsyncGenerator<ServerEvent> {
  const res = await fetch('/douyin/ai/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      'Access-Token': token,
    },
    credentials: 'include',
    body: JSON.stringify(payload),
    signal,
  })

  if (!res.ok || !res.body) {
    throw new Error(`AI 服务连接失败: ${res.status}`)
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { value, done } = await reader.read()
    if (done) return
    buffer += decoder.decode(value, { stream: true })

    const chunks = buffer.split('\n\n')
    buffer = chunks.pop() || ''

    for (const chunk of chunks) {
      if (chunk.startsWith(':')) continue
      const eventLine = chunk.split('\n').find(line => line.startsWith('event: '))
      const dataLine = chunk.split('\n').find(line => line.startsWith('data: '))
      if (!dataLine) continue

      const eventName = eventLine?.slice(7).trim()
      if (eventName === 'done') return

      yield JSON.parse(dataLine.slice(6)) as ServerEvent
    }
  }
}
```

Hook 侧保留当前 `AbortController`：

- 发送新消息时创建 controller。
- 点击停止按钮时 `abort()`。
- abort 后只停止前端消费；后端是否继续执行由服务端超时/上下文取消决定。
- 若事件包含新的 `conversation_id`，立即更新当前会话 ID 和会话列表。

## 5. 商城业务 REST 接口清单

这些接口服务页面详情展示、后续商城页面和 AI 工具结果二次查看。所有接口均使用 `Access-Token` Header + `Refresh-Token` Cookie。

### 5.1 用户与地址

| 接口 | 用途 | 前端展示 |
| --- | --- | --- |
| `POST /douyin/user/login` | 登录 | `LoginPage` 登录表单 |
| `POST /douyin/user/register` | 注册 | 可后续增加注册入口 |
| `POST /douyin/user/logout` | 登出 | `Sidebar` 退出按钮 |
| `GET /douyin/user/info` | 当前用户信息 | `TopBar` 头像/用户名、Sidebar 底部用户 |
| `PUT /douyin/user/update` | 更新用户资料 | 后续设置页 |
| `GET /douyin/user/address/list` | 地址列表 | 创建订单确认前选择地址 |
| `GET /douyin/user/address?address_id=1` | 地址详情 | 订单详情、地址管理 |
| `POST /douyin/user/address` | 添加地址 | 后续地址管理 |
| `PUT /douyin/user/address` | 更新地址 | 后续地址管理 |
| `DELETE /douyin/user/address` | 删除地址 | 后续地址管理 |

### 5.2 商品

| 接口 | 用途 | 前端展示 |
| --- | --- | --- |
| `GET /douyin/product/list?page=1&size=10` | 商品列表 | 商品搜索/推荐结果卡片、工具结果展开 |
| `GET /douyin/product?id=1` | 商品详情 | 商品详情抽屉或详情页 |

展示规则：

- 价格按后端分值或字符串来源统一格式化为人民币。
- 商品卡片展示图片、名称、价格、库存、销量、分类。
- 从 AI `product_search` / `product_recommend` 工具结果跳转详情时使用 `product_id/id` 调用详情接口补全。

### 5.3 购物车

| 接口 | 用途 | 前端展示 |
| --- | --- | --- |
| `GET /douyin/carts/list` | 购物车列表 | “查看购物车”工具结果、后续购物车页 |
| `POST /douyin/carts/add` | 添加商品到购物车 | 低风险操作结果气泡 |
| `POST /douyin/carts/sub` | 减少商品数量 | 低风险操作结果气泡 |
| `DELETE /douyin/carts/delete` | 删除商品 | 普通页面可直接确认后调用；AI 聊天内必须走 `confirmation_required` |

AI 聊天内涉及 `cart_delete` 时，不由前端直接调用 `/douyin/carts/delete`。前端只发送自然语言消息或确认 ID，让 `services/aiagent` 经过 Execution Guard 和审计执行。

### 5.4 优惠券

| 接口 | 用途 | 前端展示 |
| --- | --- | --- |
| `GET /douyin/coupon/list?page=1&size=10&type=1` | 可领取优惠券列表 | 优惠券工具结果卡片 |
| `GET /douyin/coupon/detail?coupon_id=xxx` | 优惠券详情 | 优惠券详情抽屉 |
| `POST /douyin/coupon/claim` | 领取优惠券 | 低风险操作结果气泡 |
| `GET /douyin/coupon/my/list?page=1&size=10` | 我的优惠券 | “我的优惠券”列表 |
| `GET /douyin/coupon/my/usage?page=1&size=10` | 优惠券使用记录 | 优惠券历史 |
| `GET /douyin/coupon/calculate` | 优惠试算 | 结算确认摘要 |

注意：`coupon/calculate` 的 `.api` 定义为 GET 且包含数组参数 `items`。浏览器接入前需要确认后端实际解析格式；若现有 httpx 无法稳定解析数组对象，建议后端改为 POST JSON 或提供前端可调用的明确 query 编码规范。

### 5.5 结算与订单

| 接口 | 用途 | 前端展示 |
| --- | --- | --- |
| `POST /douyin/checkout/prepare` | 创建预结算 | AI 下单前预结算、后续结算页 |
| `GET /douyin/checkout/list?page=1&page_size=10` | 结算列表 | 后续结算列表 |
| `GET /douyin/checkout/detail?pre_order_id=xxx` | 结算详情 | 创建订单前确认金额和商品 |
| `POST /douyin/order/create` | 创建订单 | AI 聊天内必须走确认卡片 |
| `POST /douyin/order/cancel` | 取消订单 | AI 聊天内必须走确认卡片 |
| `GET /douyin/order/detail?order_id=xxx` | 订单详情 | 订单详情卡片/抽屉 |
| `GET /douyin/order/list?page=1&page_size=10` | 订单列表 | “查看我的订单”工具结果、订单页 |

高风险规则：

- `order_create`、`order_cancel` 不允许前端在 AI 聊天内直接调用业务 REST。
- 用户必须先看到 `confirmation_required` 卡片，再通过 `confirm_action` 批准。
- 如果服务端返回失败 `tool_result`，前端必须按失败展示，不能根据 AI 文案推断成功。

### 5.6 支付

| 接口 | 用途 | 前端展示 |
| --- | --- | --- |
| `POST /douyin/payment/create` | 创建支付单 | 后续订单支付入口 |
| `GET /douyin/payment/list?page=1&page_size=10&method=1` | 支付记录 | 后续支付记录页 |

支付当前不在 AI 客服首期高风险确认实现范围内。若后续加入 AI 支付能力，必须先扩展后端确认策略和审计规则，再开放前端入口。

## 6. AI 工具与页面展示映射

当前前端 `TOOL_DISPLAY_NAMES` 已覆盖 20 个 AI 工具。工具结果展示应按 `event.tool` 选择展示形态：

| 工具 | 来源能力 | 展示形态 |
| --- | --- | --- |
| `product_search` | 商品搜索 | 商品列表卡片，支持查看详情、加入购物车 |
| `product_detail` | 商品详情 | 商品详情卡片，展示图片、价格、库存、分类 |
| `product_recommend` | 商品推荐 | 推荐商品列表，展示推荐理由 |
| `inventory_get` | 库存查询 | 库存状态标签 |
| `order_get` | 订单详情 | 订单状态卡片，展示金额、商品、地址 |
| `order_list` | 订单列表 | 订单列表摘要，可展开详情 |
| `order_create` | 创建订单 | 确认卡片后展示订单结果 |
| `order_cancel` | 取消订单 | 确认卡片后展示取消结果 |
| `checkout_prepare` | 预结算 | 预结算摘要，展示金额和可选支付方式 |
| `checkout_detail` | 结算详情 | 结算详情卡片 |
| `cart_list` | 购物车列表 | 购物车商品列表和合计 |
| `cart_add` | 添加购物车 | 成功/失败结果胶囊，可刷新购物车 |
| `cart_sub` | 减少购物车 | 成功/失败结果胶囊，可刷新购物车 |
| `cart_delete` | 删除购物车 | 确认卡片后展示删除结果 |
| `coupon_list` | 优惠券列表 | 优惠券列表卡片 |
| `coupon_detail` | 优惠券详情 | 优惠券详情卡片 |
| `coupon_claim` | 领取优惠券 | 成功/失败结果胶囊 |
| `coupon_my_list` | 我的优惠券 | 我的券列表 |
| `coupon_usage_list` | 使用记录 | 使用记录列表 |
| `coupon_calculate` | 优惠试算 | 金额试算摘要 |

首期可以继续用 `ToolBubble` 的可展开 JSON 作为兜底；建议后续在 `AgentMessageBubble` 内增加 `ToolResultRenderer`，对商品、订单、购物车、优惠券做结构化卡片。

## 7. 页面接入计划

### 7.1 `LoginPage`

当前状态：mock 登录，字段是 `username/password`。

接入要求：

- 改为 `email/password`。
- 调用 `POST /douyin/user/login`。
- 保存 `Access-Token` 和 `Refresh-Token`。
- 登录成功后调用 `GET /douyin/user/info`，将用户展示名写入 AuthContext。
- 失败时展示后端 `msg`，保留当前红色错误块样式。

### 7.2 `AuthContext`

当前状态：只保存 `token/username`。

接入要求：

- 增加 `refreshToken` 或 Cookie 写入工具。
- 统一监听 `auth-unauthorized` 并清空 token、用户名和 Cookie。
- 提供 `setTokens` 和 `refreshTokens` 能力给 API client 使用。
- 不在任何前端状态中保存或提交 `user_id` 作为业务可信来源。

### 7.3 `useAgent`

当前状态：使用 `mockStreamAgentChat`。

接入要求：

- 用真实 `streamAgentChat` 替换 mock。
- 发送用户消息时生成 `client_message_id`。
- 事件 `conversation_id` 非空时同步当前会话 ID。
- `assistant_delta` 追加到同一个 streaming assistant 消息。
- `assistant_message` 用于结束或补齐最终消息，避免和 delta 重复。
- `tool_progress` 创建 running 工具消息和 trace step。
- `tool_result` 更新对应工具消息，按 `status` 展示成功/失败。
- `confirmation_required` 创建确认消息，确认卡片按钮调用 `confirm_action`。
- `error` 创建错误消息并结束本轮流式。

### 7.4 `ConfirmationCard`

当前状态：只有静态按钮，没有事件回调。

接入要求：

- 增加 `onConfirm(confirmationId)` 和 `onReject(confirmationId)`。
- 按 `expires_at` 倒计时，过期后禁用按钮。
- 点击后进入 loading 状态，防重复点击。
- 确认或拒绝的服务端事件继续追加到当前会话。
- 拒绝后展示取消状态，不调用业务 REST。

### 7.5 `Sidebar`

当前状态：会话列表只在前端内存中维护。

接入要求：

- 当前后端尚未暴露“会话列表/历史消息”外部 REST，因此首期保持本地会话列表。
- `conversation_id` 由 AI SSE 返回后更新本地会话。
- 刷新页面后历史会话无法恢复，这是当前外部 API 缺口，应记录为后端待补接口：`GET /douyin/ai/conversations`、`GET /douyin/ai/conversations/{id}/messages`、重命名和删除会话接口。

## 8. 缺口与需要后端确认的点

1. `docs/ai-customer-service-api.md` 写 WebSocket，但代码实现是 SSE。需要后端文档同步为 SSE。
2. `apis/ai/ai.api` 仍是 go-zero `post /chat` 普通声明，但 handler 手动写 SSE，这一点可以保留。
3. AI 外部 API 暂无会话列表、历史消息、删除会话、重命名会话接口，Sidebar 只能本地维护。
4. `coupon/calculate` 使用 GET 接收数组对象，前端 query 编码方式需要后端确认。
5. 浏览器无法手动设置 Cookie 的 HttpOnly 属性。若要求更安全的 refresh token，建议后端登录时通过 `Set-Cookie` 写 `Refresh-Token`。
6. 当前 Vite proxy 只代理 `/api`，需要改为 `/douyin` 或由网关提供 `/api/v1` 映射。
7. 当前前端 `AgentEvent` 字段 `data_json` 与 API 网关输出 `data` 不一致，接入时需要修正。

## 9. 验证清单

完成接入后至少验证：

1. 未登录访问 `/agent` 会跳转 `/login`。
2. 登录成功后请求 Header 包含 `Access-Token`，请求 Cookie 包含 `Refresh-Token`。
3. 普通提问能收到 `assistant_delta` 和 `assistant_message`，页面无重复最终回复。
4. “查看购物车/查看订单/优惠券”等问题能展示 `tool_progress` 和 `tool_result`。
5. `tool_result.status=failed` 时展示失败态，不总结为成功。
6. “取消订单/创建订单/删除购物车”先展示 `confirmation_required`，点击确认后才继续执行。
7. 确认卡片过期后按钮禁用，重复点击不会重复发送。
8. SSE 请求可被停止按钮取消，UI 恢复可输入状态。
9. token 过期刷新成功后能重试普通 JSON API；刷新失败会退出登录。
10. `business_executed=true` 的错误不会被自动重试。

## 10. 推荐实施顺序

1. 修正前端 API base 和 Vite proxy。
2. 完成 `client.ts` 的业务响应包装、认证 Header、refresh token 处理。
3. 新增 `auth.ts`，让 `LoginPage` 接真实登录和用户信息。
4. 新增 `agent.ts` SSE parser，用真实 `streamAgentChat` 替换 mock。
5. 改造 `useAgent` 的事件归并和确认动作。
6. 改造 `ConfirmationCard` 交互。
7. 新增 `mall.ts` 封装商品、购物车、订单、优惠券、结算、支付接口。
8. 在工具结果展示中逐步加入结构化业务卡片。
9. 补齐前端单元测试或集成测试，重点覆盖 SSE parser、确认流程和认证刷新。
