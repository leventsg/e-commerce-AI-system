# AI 客服 API 接口文档

本文档描述 AI 客服当前实现的两层接口：

- **对外接口**：`apis/ai` 提供的 WebSocket API，供前端和测试客户端使用。
- **内部接口**：`services/aiagent` 提供的 gRPC API，仅供服务间调用。

外部客户端不得直接调用内部 gRPC，也不得提交或覆盖用户 ID。

## 1. 对外 WebSocket API

### 1.1 建立连接

```text
GET /douyin/ai/chat?conversation_id={optional}
```

开发环境默认地址：

```text
ws://localhost:8007/douyin/ai/chat
```

恢复已有会话：

```text
ws://localhost:8007/douyin/ai/chat?conversation_id=conv_xxx
```

| 项目 | 值 |
| --- | --- |
| 协议 | WebSocket over HTTP GET |
| 消息格式 | UTF-8 JSON 文本帧 |
| 最大消息大小 | 64 KiB |
| 服务端单次写超时 | 10 秒 |
| `conversation_id` | 可选；不传时由 Chat RPC 创建新会话 |

连接建立后，同一连接会保存最新的 `conversation_id`。服务端事件返回非空会话 ID 时，后续消息和确认操作自动使用该 ID。

### 1.2 鉴权

WebSocket 握手复用现有认证中间件，必须同时携带：

| 位置 | 名称 | 必填 | 说明 |
| --- | --- | --- | --- |
| Header | `Access-Token` | 是 | 访问令牌 |
| Cookie | `Refresh-Token` | 是 | 刷新令牌 |

用户 ID 只从认证结果中取得。客户端消息不得包含 `user_id`；一旦提交，服务端返回 `error` 事件且不调用 AiAgent RPC。

认证失败发生在 WebSocket Upgrade 之前，响应沿用现有认证 API 的 JSON 业务错误格式，连接不会升级。访问令牌过期且刷新成功时，中间件返回令牌刷新响应，客户端需要使用新令牌重新建连。

浏览器原生 `WebSocket` API 不能设置自定义 `Access-Token` Header。当前可使用支持自定义 Header 的客户端、同源反向代理或服务端 WebSocket 客户端进行联调。

使用 `wscat` 建连示例：

```bash
wscat \
  -H "Access-Token: <access-token>" \
  -H "Cookie: Refresh-Token=<refresh-token>" \
  -c "ws://localhost:8007/douyin/ai/chat"
```

### 1.3 客户端消息：`user_message`

发送一条用户聊天消息。

```json
{
  "type": "user_message",
  "content": "帮我查一下订单 202406300001",
  "metadata": {
    "source": "web"
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | 固定为 `user_message` |
| `content` | string | 是 | 用户输入，去除空白后不能为空 |
| `metadata.source` | string | 否 | 消息来源，缺省为 `web` |

前端不需要生成或提交 `message_id`。用户消息的数据库 ID 由后端生成；旧客户端提交的 `message_id` 会被忽略。

### 1.4 客户端消息：`confirm_action`

批准或拒绝高风险操作。确认参数由服务端持久化，客户端不能在此消息中重新提交工具参数。

批准：

```json
{
  "type": "confirm_action",
  "confirmation_id": "confirm_xxx",
  "approved": true
}
```

拒绝：

```json
{
  "type": "confirm_action",
  "confirmation_id": "confirm_xxx",
  "approved": false
}
```

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `type` | string | 是 | 固定为 `confirm_action` |
| `confirmation_id` | string | 是 | `confirmation_required` 事件返回的确认 ID |
| `approved` | boolean | 是 | `true` 批准，`false` 拒绝；必须显式提交 |

确认操作要求当前连接已经具有非空 `conversation_id`。过期、已拒绝、已执行、执行失败、跨用户、跨会话或重复确认均不会再次执行高风险 RPC。

## 2. 服务端 WebSocket 事件

服务端可能对一条客户端消息依次返回多个事件。例如低风险写操作通常先返回 `tool_result`，再返回 `assistant_message`。

### 2.1 公共字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `type` | string | 事件类型 |
| `conversation_id` | string | 会话 ID；可能省略 |
| `message_id` | string | 服务端消息 ID；可能省略 |
| `content` | string | 用户可读文本；可能省略 |
| `tool` | string | 工具名称；工具事件使用 |
| `status` | string | 工具或确认状态 |
| `data` | JSON | 结构化工具结果；为空时省略 |
| `confirmation_id` | string | 确认 ID |
| `action` | string | 待确认工具名称 |
| `summary` | string | 确认摘要 |
| `expires_at` | int64 | Unix 秒级过期时间 |
| `done` | boolean | 当前事件是否结束 |

### 2.2 `assistant_message`

AI 回复或操作结果摘要。

```json
{
  "type": "assistant_message",
  "conversation_id": "conv_xxx",
  "message_id": "msg_xxx",
  "content": "我帮你查到该订单当前处于待支付状态。",
  "done": true
}
```

### 2.3 `tool_result`

查询或写操作的结构化结果。`status` 通常为 `success` 或 `failed`。

```json
{
  "type": "tool_result",
  "conversation_id": "conv_xxx",
  "message_id": "msg_xxx",
  "tool": "order.get",
  "status": "success",
  "data": {
    "order_id": "202406300001",
    "status": "pending_payment"
  },
  "done": true
}
```

工具失败时仍使用 `tool_result`，但 `status` 为 `failed`。客户端不得根据 `content` 猜测成功状态，应以 `type`、`status` 和 `data` 为准。

### 2.4 `confirmation_required`

高风险工具首次调用只创建确认记录，不执行业务写 RPC。

```json
{
  "type": "confirmation_required",
  "conversation_id": "conv_xxx",
  "message_id": "msg_xxx",
  "tool": "order.cancel",
  "status": "pending",
  "confirmation_id": "confirm_xxx",
  "action": "order.cancel",
  "summary": "确认取消订单 202406300001？",
  "expires_at": 1719730000,
  "done": true
}
```

当前必须确认的操作：

- `cart.delete`
- `order.create`
- `order.cancel`

### 2.5 `error`

协议校验、RPC 调用、事件转换或持久化失败。

```json
{
  "type": "error",
  "conversation_id": "conv_xxx",
  "content": "消息格式无效",
  "done": true
}
```

业务 RPC 已执行但消息持久化失败时，服务端保留原业务事件并追加：

```json
{
  "type": "error",
  "conversation_id": "conv_xxx",
  "content": "业务结果已产生，但消息保存失败，请勿重复操作",
  "status": "failed",
  "data": {
    "business_executed": true
  },
  "done": true
}
```

收到 `business_executed=true` 后不得自动重试写操作。

### 2.6 常见协议错误

| 场景 | 返回内容 |
| --- | --- |
| 非法 JSON | `消息格式无效` |
| 非文本帧 | `仅支持 JSON 文本消息` |
| 未知 `type` | `不支持的消息类型` |
| `user_message` 缺少字段 | `content 为必填字段` |
| `confirm_action` 缺少字段 | `conversation_id、confirmation_id 和 approved 为必填字段` |
| payload 包含 `user_id` | `客户端不得提交 user_id` |
| AiAgent Chat 不可用 | `AI 服务暂时不可用，请稍后重试` |
| ConfirmAction 不可用 | `确认服务暂时不可用，请稍后重试` |

## 3. 完整确认流程示例

1. 客户端发送：

```json
{
  "type": "user_message",
  "content": "取消订单 202406300001"
}
```

2. 服务端返回 `confirmation_required`：

```json
{
  "type": "confirmation_required",
  "conversation_id": "conv_xxx",
  "confirmation_id": "confirm_xxx",
  "action": "order.cancel",
  "summary": "确认取消订单 202406300001？",
  "expires_at": 1719730000,
  "done": true
}
```

3. 客户端批准：

```json
{
  "type": "confirm_action",
  "confirmation_id": "confirm_xxx",
  "approved": true
}
```

4. 服务端返回实际执行结果：

```json
{
  "type": "tool_result",
  "conversation_id": "conv_xxx",
  "tool": "order.cancel",
  "status": "success",
  "data": {
    "order_id": "202406300001"
  },
  "done": true
}
```

## 4. 内部 AiAgent gRPC API

> 本章接口仅供 `apis/ai` 等受信任服务调用，不对浏览器或外部客户端开放。

服务：

```protobuf
service AiAgent {
  rpc Chat(ChatRequest) returns (ChatResponse);
  rpc ConfirmAction(ConfirmActionRequest) returns (ConfirmActionResponse);
}
```

开发环境通过 Consul 服务名 `aiagent.rpc` 发现，RPC 服务默认监听 `10009`。

### 4.1 `AiAgent.Chat`

请求：

```protobuf
message ChatRequest {
  uint32 user_id = 1;
  string conversation_id = 2;
  string message_id = 3;
  string content = 4;
  string source = 5;
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `user_id` | 是 | 由 API 网关从认证上下文注入，必须大于 0 |
| `conversation_id` | 否 | 空值创建新会话；非空值必须属于当前用户 |
| `message_id` | 否 | 仅供受信任内部调用方兼容使用；`apis/ai` 始终传空值并由后端生成 |
| `content` | 是 | 用户消息，不能为空 |
| `source` | 否 | 缺省为 `web` |

处理流程：

```text
Conversation Manager
-> Intent Planner
-> Eino Runner 或受控 Tool
-> 批量持久化 AgentEvent
-> ChatResponse
```

工具执行前，服务端会清除 Planner arguments 中的 `user_id`，再注入 `ChatRequest.user_id`。高风险策略同时由 Tool Registry metadata 兜底，不能仅依赖 Planner 的确认标志。

### 4.2 `AiAgent.ConfirmAction`

请求：

```protobuf
message ConfirmActionRequest {
  uint32 user_id = 1;
  string conversation_id = 2;
  string confirmation_id = 3;
  bool approved = 4;
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `user_id` | 是 | 可信调用方注入的认证用户 ID |
| `conversation_id` | 是 | 确认所属会话 ID |
| `confirmation_id` | 是 | 待决策确认 ID |
| `approved` | 是 | 批准或拒绝；proto3 中 `false` 同时表示拒绝 |

批准后仅由确认状态 CAS 获胜者取得一次性执行权。工具名称和参数从服务端确认记录读取，不接受调用方重新提交工具参数。

### 4.3 gRPC 响应

`ChatResponse` 和 `ConfirmActionResponse` 结构一致：

```protobuf
message ChatResponse {
  int32 status_code = 1;
  string status_msg = 2;
  repeated AgentEvent events = 3;
}
```

`AgentEvent`：

```protobuf
message AgentEvent {
  string type = 1;
  string conversation_id = 2;
  string message_id = 3;
  string content = 4;
  string tool = 5;
  string status = 6;
  string data_json = 7;
  string confirmation_id = 8;
  string action = 9;
  string summary = 10;
  int64 expires_at = 11;
  bool done = 12;
}
```

`data_json` 必须是合法 JSON 字符串。WebSocket 网关验证后将其转换为外部事件的结构化 `data` 字段；非法 JSON 不会透传，而是转换为 `error` 事件。

## 5. WebSocket 与 gRPC 字段映射

| WebSocket 输入 | gRPC 请求 | 规则 |
| --- | --- | --- |
| 认证上下文 | `user_id` | 服务端注入，payload 不可提交 |
| 查询参数 `conversation_id` | `conversation_id` | 同一连接会随响应事件更新 |
| `content` | `ChatRequest.content` | 原样传递 |
| `metadata.source` | `ChatRequest.source` | 缺省为 `web` |
| `confirmation_id` | `ConfirmActionRequest.confirmation_id` | 原样传递 |
| `approved` | `ConfirmActionRequest.approved` | 必须显式提交 |
| 服务端可信客户端 IP | gRPC metadata `x-client-ip` | 用于工具审计，不接受 payload 覆盖 |

| gRPC 事件 | WebSocket 事件 |
| --- | --- |
| `type` | `type` |
| `conversation_id` | `conversation_id` |
| `message_id` | `message_id` |
| `content` | `content` |
| `tool` | `tool` |
| `status` | `status` |
| 合法 `data_json` | 结构化 `data` |
| `confirmation_id` | `confirmation_id` |
| `action` | `action` |
| `summary` | `summary` |
| `expires_at` | `expires_at` |
| `done` | `done` |

## 6. 当前限制

- RPC 当前返回事件数组，WebSocket 网关收到完整 RPC 响应后再逐条推送，不是 token 级实时流式 RPC。
- 用户级聊天限流和工具级限流属于 Task 15，当前尚未完成。
- WebSocket 未登录、连续对话和高风险确认的完整集成验收属于 Task 13/16，仍需执行。
- 浏览器原生 WebSocket 的自定义 Header 限制尚未通过 Cookie access token 或专用握手协议解决。
- 客户端应自行保存服务端返回的 `conversation_id`，用于断线后恢复会话。
