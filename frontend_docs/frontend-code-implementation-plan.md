# 前端 API 接入代码实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `frontend/` 当前 mock 版 AI 控制台接入真实 go-mall 后端 API，完成登录认证、SSE 聊天流、高风险确认、工具结果展示和商城 REST service 封装。

**Architecture:** 前端保持现有 React + Context + Hook 结构。API 层拆为统一 HTTP client、领域 service 和页面 hook：`client.ts` 处理认证、业务响应包装和 token 续期，`auth.ts` / `agent.ts` / `mall.ts` 分别封装登录、AI SSE 和商城 REST，`useAgent.ts` 负责把服务端事件归并为 UI 消息。首期不新增完整商城页面，也不实现后端尚未暴露的 AI 会话历史管理接口。

**Tech Stack:** React 18、TypeScript、Vite 6、Tailwind CSS 4、lucide-react、Vitest、jsdom、Testing Library、Fetch API、ReadableStream SSE。

---

## 1. 实施边界

本计划只实现 `frontend_docs/frontend-api-integration.md` 中的前端代码接入，不修改后端服务。

必须遵守：

- AI 聊天以当前代码事实为准：`POST /douyin/ai/chat`，响应 `text/event-stream`。
- 普通 JSON API 响应按 `{ code, msg, data }` 解析；成功码为 `0`，token 续期码为 `10004`。
- 认证 Header 使用 `Access-Token`，Cookie 使用 `Refresh-Token`。
- 前端不得提交 `user_id`。
- AI 聊天内高风险操作只走 `confirmation_required -> confirm_action`，不直接调用业务 REST。
- 所有测试文件放在 workspace 根目录的 `tests/` 下。

暂不实现：

- AI 会话历史列表、历史消息恢复、会话重命名、会话删除，因为后端尚无外部 REST。
- 商品/订单/购物车/优惠券结构化卡片的完整视觉重做；首期仍使用工具结果 JSON 展开兜底。
- HttpOnly refresh token；首期由前端写普通 Cookie，后续需后端 `Set-Cookie` 支持。

## 2. 文件规划

新增：

- `frontend/src/services/api/auth.ts`：登录、登出、用户信息 service。
- `frontend/src/services/api/agent.ts`：SSE parser、用户消息流、确认操作流。
- `frontend/src/services/api/mall.ts`：商城 REST service 封装。
- `tests/frontend/services/api/client.test.ts`：HTTP client 单测。
- `tests/frontend/services/api/agent.test.ts`：SSE parser 单测。
- `tests/frontend/hooks/agentEvents.test.ts`：Agent 事件归并单测。
- `tests/frontend/setup.ts`：Vitest 测试环境初始化。

修改：

- `frontend/package.json`：新增 test script 和测试依赖。
- `frontend/vite.config.ts`：新增 Vitest 配置和 `/douyin` dev proxy。
- `frontend/src/constants/index.ts`：修正 `API_BASE`，增加 refresh token storage key。
- `frontend/src/types/api.ts`：对齐 API 网关的 ClientMessage / ServerEvent 类型。
- `frontend/src/types/chat.ts`：补充确认状态、工具 data JSON 存储字段。
- `frontend/src/services/api/client.ts`：统一请求封装、业务响应解析、认证失败事件。
- `frontend/src/services/api/index.ts`：导出真实 service。
- `frontend/src/contexts/AuthContext.tsx`：真实登录态、token/Cookie 管理。
- `frontend/src/pages/Login.tsx`：接入真实登录表单。
- `frontend/src/hooks/useAgent.ts`：接入真实 SSE、确认操作和事件归并。
- `frontend/src/components/agent/AgentMessageBubble.tsx`：透传确认回调。
- `frontend/src/components/agent/ConfirmationCard.tsx`：确认/拒绝交互、loading、过期禁用。
- `frontend/src/App.tsx`：把确认回调传给聊天窗口。

## 3. 详细任务

### Task 1: 安装测试框架并建立测试入口

**Files:**

- Modify: `frontend/package.json`
- Modify: `frontend/vite.config.ts`
- Create: `tests/frontend/setup.ts`

- [ ] **Step 1: 安装依赖**

Run:

```bash
cd frontend
npm install -D vitest jsdom @testing-library/react @testing-library/user-event
```

Expected: `package.json` 和 `package-lock.json` 新增对应 devDependencies。

- [ ] **Step 2: 增加测试脚本**

在 `frontend/package.json` 的 `scripts` 中增加：

```json
"test": "vitest run"
```

- [ ] **Step 3: 配置 Vitest**

在 `frontend/vite.config.ts` 增加 test 配置：

```ts
test: {
  environment: 'jsdom',
  setupFiles: ['../tests/frontend/setup.ts'],
  globals: true,
}
```

同时保留 React、Tailwind 和 alias 配置。

- [ ] **Step 4: 创建测试 setup**

创建 `tests/frontend/setup.ts`：

```ts
import { afterEach, vi } from 'vitest'

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  document.cookie = 'Refresh-Token=; Max-Age=0; path=/'
})
```

- [ ] **Step 5: 运行空测试检查**

Run:

```bash
cd frontend
npm run test -- --passWithNoTests
```

Expected: Vitest 可启动，无测试时通过。

### Task 2: 统一 API 常量和 HTTP Client

**Files:**

- Modify: `frontend/src/constants/index.ts`
- Modify: `frontend/src/services/api/client.ts`
- Test: `tests/frontend/services/api/client.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `tests/frontend/services/api/client.test.ts`，覆盖：

```ts
import { describe, expect, it, vi } from 'vitest'
import { apiGet, apiPost, ApiError, setTokenRefreshHandler } from '../../../frontend/src/services/api/client'

describe('api client', () => {
  it('unwraps successful business responses', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      code: 0,
      msg: 'ok',
      data: { name: 'Alice' },
    }), { status: 200 })))

    await expect(apiGet<{ name: string }>('/douyin/user/info', 'access')).resolves.toEqual({ name: 'Alice' })
  })

  it('uses Access-Token header and include credentials', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await apiPost('/douyin/carts/add', { product_id: 1 }, 'access')

    expect(fetchMock).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({
      credentials: 'include',
      headers: expect.objectContaining({ 'Access-Token': 'access' }),
    }))
  })

  it('updates tokens and retries once when backend returns token renewed code', async () => {
    const refreshHandler = vi.fn()
    setTokenRefreshHandler(refreshHandler)
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        code: 10004,
        msg: '令牌续期成功',
        data: { access_token: 'new-access', refresh_token: 'new-refresh' },
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 0, msg: 'ok', data: { ok: true } }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiGet('/douyin/user/info', 'old-access')).resolves.toEqual({ ok: true })
    expect(refreshHandler).toHaveBeenCalledWith({ access_token: 'new-access', refresh_token: 'new-refresh' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('dispatches auth-unauthorized on business auth failure', async () => {
    const listener = vi.fn()
    window.addEventListener('auth-unauthorized', listener)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 10007, msg: '令牌无效' }), { status: 200 })))

    await expect(apiGet('/douyin/user/info', 'bad-token')).rejects.toBeInstanceOf(ApiError)
    expect(listener).toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
cd frontend
npm run test -- ../tests/frontend/services/api/client.test.ts
```

Expected: FAIL，原因是 `setTokenRefreshHandler`、业务响应解包或 `Access-Token` 尚未实现。

- [ ] **Step 3: 修改常量**

在 `frontend/src/constants/index.ts`：

```ts
export const API_BASE = import.meta.env.VITE_API_BASE || ''

export const STORAGE_KEYS = {
  TOKEN: 'go-mall-token',
  REFRESH_TOKEN: 'go-mall-refresh-token',
  USERNAME: 'go-mall-username',
  USER: 'go-mall-user',
  THEME: 'go-mall-theme',
} as const
```

- [ ] **Step 4: 实现 HTTP client**

将 `frontend/src/services/api/client.ts` 改为：

```ts
import { API_BASE } from '@/constants'

export const SUCCESS_CODE = 0
export const TOKEN_RENEWED_CODE = 10004

export interface ApiResponse<T> {
  code: number
  msg: string
  data?: T
}

export interface AuthTokens {
  access_token: string
  refresh_token: string
}

export class ApiError extends Error {
  code: number
  status: number
  data?: unknown

  constructor(message: string, code: number, status: number, data?: unknown) {
    super(message)
    this.code = code
    this.status = status
    this.data = data
  }
}

let tokenRefreshHandler: ((tokens: AuthTokens) => void) | null = null

export function setTokenRefreshHandler(handler: ((tokens: AuthTokens) => void) | null) {
  tokenRefreshHandler = handler
}

export function createAuthHeaders(token?: string | null): Record<string, string> {
  return token ? { 'Access-Token': token } : {}
}

export async function apiGet<T>(path: string, token?: string | null): Promise<T> {
  return apiRequest<T>(path, { method: 'GET' }, token)
}

export async function apiPost<T, D = unknown>(path: string, data: D, token?: string | null): Promise<T> {
  return apiRequest<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  }, token)
}

export async function apiPut<T, D = unknown>(path: string, data: D, token?: string | null): Promise<T> {
  return apiRequest<T>(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  }, token)
}

export async function apiDelete<T, D = unknown>(path: string, data?: D, token?: string | null): Promise<T> {
  return apiRequest<T>(path, {
    method: 'DELETE',
    headers: data ? { 'Content-Type': 'application/json' } : undefined,
    body: data ? JSON.stringify(data) : undefined,
  }, token)
}

export async function apiRequest<T>(
  path: string,
  init: RequestInit = {},
  token?: string | null,
  hasRetried = false,
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: 'include',
    headers: {
      ...init.headers,
      ...createAuthHeaders(token),
    },
  })

  const payload = await parseJson<ApiResponse<T> | ApiResponse<AuthTokens>>(res)
  if (!res.ok) {
    throw new ApiError(payload?.msg || `HTTP ${res.status}`, payload?.code ?? -1, res.status, payload?.data)
  }

  if (payload.code === TOKEN_RENEWED_CODE && !hasRetried) {
    const tokens = payload.data as AuthTokens | undefined
    if (tokens?.access_token && tokens?.refresh_token) {
      tokenRefreshHandler?.(tokens)
      return apiRequest<T>(path, init, tokens.access_token, true)
    }
  }

  if (payload.code !== SUCCESS_CODE) {
    if (isAuthCode(payload.code)) {
      window.dispatchEvent(new CustomEvent('auth-unauthorized'))
    }
    throw new ApiError(payload.msg || '请求失败', payload.code, res.status, payload.data)
  }

  return payload.data as T
}

async function parseJson<T>(res: Response): Promise<T> {
  const text = await res.text()
  if (!text) return { code: SUCCESS_CODE, msg: 'ok' } as T
  return JSON.parse(text) as T
}

function isAuthCode(code: number) {
  return code >= 10000 && code < 10010 && code !== TOKEN_RENEWED_CODE
}
```

- [ ] **Step 5: 运行测试确认通过**

Run:

```bash
cd frontend
npm run test -- ../tests/frontend/services/api/client.test.ts
```

Expected: PASS。

### Task 3: 接入 Auth service 与 AuthContext

**Files:**

- Create: `frontend/src/services/api/auth.ts`
- Modify: `frontend/src/services/api/index.ts`
- Modify: `frontend/src/contexts/AuthContext.tsx`
- Modify: `frontend/src/pages/Login.tsx`

- [ ] **Step 1: 新增 auth service**

创建 `frontend/src/services/api/auth.ts`：

```ts
import { apiGet, apiPost, type AuthTokens } from './client'

export interface LoginRequest {
  email: string
  password: string
}

export interface UserInfo {
  user_id: number
  logout_at: string
  created_at: string
  update_at: string
  email: string
  user_name: string
  avatar: string
}

export function login(req: LoginRequest) {
  return apiPost<AuthTokens, LoginRequest>('/douyin/user/login', req)
}

export function logout(token?: string | null) {
  return apiPost<Record<string, never>, Record<string, never>>('/douyin/user/logout', {}, token)
}

export function getUserInfo(token?: string | null) {
  return apiGet<UserInfo>('/douyin/user/info', token)
}
```

- [ ] **Step 2: 改造 AuthContext**

`AuthContext` 对外提供：

```ts
interface AuthUser {
  user_id?: number
  email?: string
  user_name?: string
  avatar?: string
}

interface AuthState {
  token: string | null
  refreshToken: string | null
  user: AuthUser | null
  username: string | null
  isAuthenticated: boolean
  loginWithPassword: (email: string, password: string) => Promise<void>
  setTokens: (tokens: AuthTokens) => void
  logout: () => void
}
```

实现要求：

- 初始化时从 `localStorage` 读取 token、refresh token 和 user。
- `setTokens` 写入 `localStorage`，并写入 `document.cookie = "Refresh-Token=<value>; path=/"`。
- 调用 `setTokenRefreshHandler(setTokens)`，让 API client 可更新 token。
- `loginWithPassword` 调用 `login`，再调用 `getUserInfo`，最后写 user。
- `logout` 调用清理逻辑：清空 localStorage、清除 Cookie、dispatch `auth-unauthorized`。
- 监听 `auth-unauthorized` 时只做本地清理，不重复 dispatch，避免循环。

- [ ] **Step 3: 改造 LoginPage**

要求：

- 表单字段从 `username/password` 改为 `email/password`。
- placeholder 使用 `you@example.com`。
- submit 调用 `loginWithPassword(email.trim(), password)`。
- 成功后 `navigate('/agent', { replace: true })`。
- catch 中展示 `ApiError.message` 或 `登录失败，请重试`。

- [ ] **Step 4: 更新 service barrel**

在 `frontend/src/services/api/index.ts` 导出：

```ts
export * from './client'
export * from './auth'
```

- [ ] **Step 5: 运行构建检查**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS。

### Task 4: 实现 AI SSE service 和 parser 测试

**Files:**

- Modify: `frontend/src/types/api.ts`
- Create: `frontend/src/services/api/agent.ts`
- Modify: `frontend/src/services/api/index.ts`
- Test: `tests/frontend/services/api/agent.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `tests/frontend/services/api/agent.test.ts`，覆盖：

```ts
import { describe, expect, it, vi } from 'vitest'
import { streamAgentChat } from '../../../frontend/src/services/api/agent'

function streamFrom(chunks: string[]) {
  const encoder = new TextEncoder()
  return new ReadableStream({
    start(controller) {
      chunks.forEach(chunk => controller.enqueue(encoder.encode(chunk)))
      controller.close()
    },
  })
}

describe('agent SSE service', () => {
  it('parses SSE events and ignores heartbeat comments', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(streamFrom([
      ': ping\n\n',
      'event: assistant_delta\nid: msg_1\ndata: {"type":"assistant_delta","conversation_id":"conv_1","message_id":"msg_1","content":"你","done":false}\n\n',
      'event: tool_result\ndata: {"type":"tool_result","conversation_id":"conv_1","tool":"order_list","status":"success","data":{"total":1},"done":false}\n\n',
      'event: done\ndata: {"done":true}\n\n',
    ]), { status: 200, headers: { 'Content-Type': 'text/event-stream' } })))

    const events = []
    for await (const event of streamAgentChat({
      type: 'user_message',
      content: '查看订单',
      client_message_id: 'client_1',
      metadata: { source: 'web' },
    }, 'access')) {
      events.push(event)
    }

    expect(events).toHaveLength(2)
    expect(events[0].type).toBe('assistant_delta')
    expect(events[1].data).toEqual({ total: 1 })
  })

  it('keeps buffering until a split SSE frame is complete', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(streamFrom([
      'event: confirmation_required\ndata: {"type":"confirmation_required","confirmation_id":"confirm_1"',
      ',"conversation_id":"conv_1","content":"确认取消订单？","expires_at":1719730000,"done":true}\n\n',
      'event: done\ndata: {"done":true}\n\n',
    ]), { status: 200 })))

    const events = []
    for await (const event of streamAgentChat({
      type: 'user_message',
      content: '取消订单',
      client_message_id: 'client_2',
    }, 'access')) {
      events.push(event)
    }

    expect(events[0].type).toBe('confirmation_required')
    expect(events[0].confirmation_id).toBe('confirm_1')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
cd frontend
npm run test -- ../tests/frontend/services/api/agent.test.ts
```

Expected: FAIL，原因是 `agent.ts` 尚未实现。

- [ ] **Step 3: 更新 API 类型**

`frontend/src/types/api.ts` 改为：

```ts
export type ClientEventType = 'user_message' | 'confirm_action'
export type ServerEventType = 'assistant_message' | 'assistant_delta' | 'tool_result' | 'tool_progress' | 'confirmation_required' | 'error'

export interface ClientMetadata {
  source?: string
}

export interface ClientMessage {
  type: ClientEventType
  conversation_id?: string
  content?: string
  client_message_id?: string
  metadata?: ClientMetadata
  confirmation_id?: string
  approved?: boolean
}

export interface AgentEvent {
  type: ServerEventType
  conversation_id?: string
  message_id?: string
  tool_call_id?: string
  content?: string
  tool?: string
  status?: string
  data?: unknown
  data_json?: string
  confirmation_id?: string
  action?: string
  summary?: string
  expires_at?: number
  done: boolean
  business_executed?: boolean
}
```

- [ ] **Step 4: 实现 `agent.ts`**

创建 `frontend/src/services/api/agent.ts`：

```ts
import { API_BASE } from '@/constants'
import type { AgentEvent, ClientMessage } from '@/types'
import { createAuthHeaders } from './client'

export function streamAgentChat(payload: ClientMessage, token: string, signal?: AbortSignal) {
  return streamAgent(payload, token, signal)
}

export function streamConfirmAction(payload: ClientMessage, token: string, signal?: AbortSignal) {
  return streamAgent(payload, token, signal)
}

async function* streamAgent(payload: ClientMessage, token: string, signal?: AbortSignal): AsyncGenerator<AgentEvent> {
  const res = await fetch(`${API_BASE}/douyin/ai/chat`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      ...createAuthHeaders(token),
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
    const { done, value } = await reader.read()
    if (done) return
    buffer += decoder.decode(value, { stream: true })

    const frames = buffer.split('\n\n')
    buffer = frames.pop() || ''

    for (const frame of frames) {
      const event = parseSSEFrame(frame)
      if (event === 'done') return
      if (event) yield event
    }
  }
}

export function parseSSEFrame(frame: string): AgentEvent | 'done' | null {
  const lines = frame.split('\n').map(line => line.trimEnd())
  if (lines.every(line => line === '' || line.startsWith(':'))) return null

  const eventLine = lines.find(line => line.startsWith('event: '))
  const dataLine = lines.find(line => line.startsWith('data: '))
  if (!dataLine) return null

  const eventName = eventLine?.slice(7).trim()
  if (eventName === 'done') return 'done'

  try {
    return JSON.parse(dataLine.slice(6)) as AgentEvent
  } catch (error) {
    throw new Error(`AI 服务返回无效事件: ${error instanceof Error ? error.message : 'JSON parse failed'}`)
  }
}
```

- [ ] **Step 5: 更新 barrel 并运行测试**

在 `frontend/src/services/api/index.ts` 增加：

```ts
export * from './agent'
```

Run:

```bash
cd frontend
npm run test -- ../tests/frontend/services/api/agent.test.ts
```

Expected: PASS。

### Task 5: 抽出 Agent 事件归并并改造 useAgent

**Files:**

- Modify: `frontend/src/types/chat.ts`
- Modify: `frontend/src/hooks/useAgent.ts`
- Test: `tests/frontend/hooks/agentEvents.test.ts`

- [ ] **Step 1: 写失败测试**

创建 `tests/frontend/hooks/agentEvents.test.ts`：

```ts
import { describe, expect, it } from 'vitest'
import type { TraceStep, UIMessage } from '../../../frontend/src/types'
import { applyAgentEvent } from '../../../frontend/src/hooks/useAgent'

describe('applyAgentEvent', () => {
  it('merges assistant deltas into one streaming assistant message', () => {
    const trace: TraceStep[] = []
    const first = applyAgentEvent([], trace, { type: 'assistant_delta', content: '你', done: false }, 1)
    const second = applyAgentEvent(first.messages, trace, { type: 'assistant_delta', content: '好', done: false }, 2, first.assistantMessageId)

    expect(second.messages).toHaveLength(1)
    expect(second.messages[0]).toMatchObject({ type: 'assistant', content: '你好', streaming: true })
  })

  it('marks a running tool as failed when tool_result status is not success', () => {
    const trace: TraceStep[] = []
    const progress = applyAgentEvent([], trace, {
      type: 'tool_progress',
      tool_call_id: 'call_1',
      tool: 'order_list',
      content: '正在查询订单',
      done: false,
    }, 1)
    const result = applyAgentEvent(progress.messages, trace, {
      type: 'tool_result',
      tool_call_id: 'call_1',
      tool: 'order_list',
      status: 'failed',
      summary: '查询失败',
      data: { reason: 'rpc timeout' },
      done: false,
    }, 2)

    const toolMessage = result.messages[0]
    expect(toolMessage.toolStatus).toBe('failed')
    expect(toolMessage.dataJson).toBe(JSON.stringify({ reason: 'rpc timeout' }))
  })

  it('creates confirmation messages from confirmation_required events', () => {
    const trace: TraceStep[] = []
    const result = applyAgentEvent([] as UIMessage[], trace, {
      type: 'confirmation_required',
      confirmation_id: 'confirm_1',
      content: '确认取消订单？',
      expires_at: 1719730000,
      done: true,
    }, 1)

    expect(result.messages[0]).toMatchObject({
      type: 'confirmation',
      confirmationId: 'confirm_1',
      expiresAt: 1719730000,
    })
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
cd frontend
npm run test -- ../tests/frontend/hooks/agentEvents.test.ts
```

Expected: FAIL，原因是 `applyAgentEvent` 尚未导出。

- [ ] **Step 3: 更新 `UIMessage`**

在 `frontend/src/types/chat.ts` 的 `UIMessage` 增加：

```ts
confirmationStatus?: 'pending' | 'approved' | 'rejected' | 'expired'
```

- [ ] **Step 4: 实现并导出事件归并函数**

在 `frontend/src/hooks/useAgent.ts` 导出：

```ts
export function applyAgentEvent(
  currentMessages: UIMessage[],
  traceBuffer: TraceStep[],
  event: AgentEvent,
  timestamp: number,
  assistantMessageId = '',
): { messages: UIMessage[]; assistantMessageId: string } {
  const msgs = [...currentMessages]
  let nextAssistantMessageId = assistantMessageId
  const dataJson = event.data_json ?? (event.data === undefined ? undefined : JSON.stringify(event.data))

  switch (event.type) {
    case 'tool_progress': {
      const step: TraceStep = {
        action: event.tool || 'tool_call',
        tool_name: event.tool,
        status: 'running',
        iteration: traceBuffer.length,
        timestamp: new Date(timestamp).toISOString(),
        params: {},
      }
      traceBuffer.push(step)
      msgs.push({
        id: `tool_${event.tool_call_id || timestamp}`,
        type: 'tool-result',
        content: event.content || '执行中...',
        timestamp,
        toolName: event.tool,
        toolStatus: 'running',
        toolCallId: event.tool_call_id,
        trace: [...traceBuffer],
      })
      break
    }
    case 'tool_result': {
      const last = traceBuffer[traceBuffer.length - 1]
      if (last) {
        last.status = event.status === 'success' ? 'success' : 'failed'
        last.result = event.data && typeof event.data === 'object' ? event.data as Record<string, unknown> : undefined
      }
      const runningIdx = msgs.findIndex(m => m.toolCallId === event.tool_call_id && m.toolStatus === 'running')
      const content = event.summary || event.content || (event.status === 'success' ? '执行成功' : '执行失败')
      if (runningIdx >= 0) {
        msgs[runningIdx] = { ...msgs[runningIdx], content, toolStatus: event.status === 'success' ? 'success' : 'failed', dataJson, trace: [...traceBuffer] }
      } else {
        msgs.push({
          id: `tool_${event.tool_call_id || timestamp}`,
          type: 'tool-result',
          content,
          timestamp,
          toolName: event.tool,
          toolStatus: event.status === 'success' ? 'success' : 'failed',
          toolCallId: event.tool_call_id,
          dataJson,
          trace: [...traceBuffer],
        })
      }
      break
    }
    case 'confirmation_required':
      msgs.push({
        id: `confirm_${event.confirmation_id || timestamp}`,
        type: 'confirmation',
        content: event.summary || event.content || '请确认是否执行该操作',
        timestamp,
        confirmationId: event.confirmation_id,
        expiresAt: event.expires_at,
        confirmationStatus: 'pending',
      })
      break
    case 'assistant_delta':
      if (!nextAssistantMessageId) {
        nextAssistantMessageId = `ai_${timestamp}`
        msgs.push({ id: nextAssistantMessageId, type: 'assistant', content: event.content || '', timestamp, streaming: true, trace: [...traceBuffer] })
      } else {
        const idx = msgs.findIndex(m => m.id === nextAssistantMessageId)
        if (idx >= 0) msgs[idx] = { ...msgs[idx], content: msgs[idx].content + (event.content || '') }
      }
      break
    case 'assistant_message': {
      const sIdx = nextAssistantMessageId ? msgs.findIndex(m => m.id === nextAssistantMessageId) : msgs.findIndex(m => m.streaming)
      if (sIdx >= 0) {
        msgs[sIdx] = { ...msgs[sIdx], content: event.content || msgs[sIdx].content, streaming: false, trace: [...traceBuffer] }
      } else {
        msgs.push({ id: event.message_id || `ai_${timestamp}`, type: 'assistant', content: event.content || '', timestamp, trace: [...traceBuffer] })
      }
      break
    }
    case 'error':
      msgs.push({ id: event.message_id || `err_${timestamp}`, type: 'error', content: event.content || 'AI 服务暂时不可用', timestamp })
      break
  }

  return { messages: msgs, assistantMessageId: nextAssistantMessageId }
}
```

- [ ] **Step 5: 改造 `useAgent` 主流程**

要求：

- `useAgent` 调用 `const { token } = useAuth()`。
- 用户消息请求使用：

```ts
{
  type: 'user_message',
  conversation_id: activeId,
  client_message_id: uid('client_msg'),
  content,
  metadata: { source: 'web' },
}
```

- 遍历 `streamAgentChat(payload, token, controller.signal)`。
- 每个事件调用 `applyAgentEvent`。
- 如果事件返回 `conversation_id` 与当前本地临时 ID 不同，更新 activeId、cache key 和 sessions。
- catch 中如果不是 abort，追加错误消息。

- [ ] **Step 6: 增加确认 action**

`useAgent` 返回：

```ts
confirmAction: (confirmationId: string, approved: boolean) => Promise<void>
```

它调用 `streamConfirmAction({ type: 'confirm_action', conversation_id: activeId, confirmation_id: confirmationId, approved }, token, controller.signal)`，并把返回事件继续通过 `applyAgentEvent` 归并进当前会话。

- [ ] **Step 7: 运行事件归并测试**

Run:

```bash
cd frontend
npm run test -- ../tests/frontend/hooks/agentEvents.test.ts
```

Expected: PASS。

### Task 6: 接通确认卡片和组件回调

**Files:**

- Modify: `frontend/src/components/agent/ConfirmationCard.tsx`
- Modify: `frontend/src/components/agent/AgentMessageBubble.tsx`
- Modify: `frontend/src/components/agent/AgentChatWindow.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 1: 修改确认卡片 props**

`ConfirmationCardProps` 改为：

```ts
interface ConfirmationCardProps {
  message: UIMessage
  onConfirm?: (confirmationId: string) => Promise<void> | void
  onReject?: (confirmationId: string) => Promise<void> | void
}
```

- [ ] **Step 2: 增加倒计时和 loading**

实现要求：

- 用 `useEffect` 每秒刷新剩余秒数。
- `remaining <= 0` 或无 `confirmationId` 时禁用按钮。
- 点击确认/取消后设置本地 loading，完成后停止 loading。
- pending 状态按钮可点；approved/rejected/expired 状态不可重复点击。

- [ ] **Step 3: 透传回调**

- `AgentMessageBubble` 增加 `onConfirmAction?: (confirmationId: string, approved: boolean) => Promise<void> | void`。
- confirmation 类型调用：

```tsx
<ConfirmationCard
  message={message}
  onConfirm={id => onConfirmAction?.(id, true)}
  onReject={id => onConfirmAction?.(id, false)}
/>
```

- `AgentChatWindow` 增加同名 prop 并传给 `AgentMessageBubble`。
- `App.tsx` 从 `useAgent()` 取 `confirmAction`，传给 `AgentChatWindow`。

- [ ] **Step 4: 构建检查**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS。

### Task 7: 新增商城 REST service

**Files:**

- Create: `frontend/src/services/api/mall.ts`
- Modify: `frontend/src/services/api/index.ts`

- [ ] **Step 1: 新增类型和方法**

创建 `frontend/src/services/api/mall.ts`，至少导出：

```ts
import { apiDelete, apiGet, apiPost, apiPut } from './client'

export interface PageParams {
  page?: number
  page_size?: number
  size?: number
}

export const productApi = {
  list: (params: { page?: number; size?: number }, token?: string | null) =>
    apiGet(`/douyin/product/list?${toQuery(params)}`, token),
  detail: (id: number, token?: string | null) =>
    apiGet(`/douyin/product?${toQuery({ id })}`, token),
}

export const cartApi = {
  list: (token?: string | null) => apiGet('/douyin/carts/list', token),
  add: (product_id: number, token?: string | null) => apiPost('/douyin/carts/add', { product_id }, token),
  sub: (product_id: number, token?: string | null) => apiPost('/douyin/carts/sub', { product_id }, token),
  delete: (product_id: number, token?: string | null) => apiDelete('/douyin/carts/delete', { product_id }, token),
}

export const couponApi = {
  list: (params: { page?: number; size?: number; type?: number }, token?: string | null) =>
    apiGet(`/douyin/coupon/list?${toQuery(params)}`, token),
  detail: (coupon_id: string, token?: string | null) =>
    apiGet(`/douyin/coupon/detail?${toQuery({ coupon_id })}`, token),
  claim: (coupon_id: string, token?: string | null) =>
    apiPost('/douyin/coupon/claim', { coupon_id }, token),
  myList: (params: { page?: number; size?: number }, token?: string | null) =>
    apiGet(`/douyin/coupon/my/list?${toQuery(params)}`, token),
  usage: (params: { page?: number; size?: number }, token?: string | null) =>
    apiGet(`/douyin/coupon/my/usage?${toQuery(params)}`, token),
  calculate: (params: { coupon_id: string; items: Array<{ product_id: number; quantity: number }> }, token?: string | null) =>
    apiGet(`/douyin/coupon/calculate?${toQuery(params as unknown as Record<string, unknown>)}`, token),
}

export const checkoutApi = {
  prepare: (data: { coupon_id?: string; order_items: Array<{ product_id: number; quantity: number }> }, token?: string | null) =>
    apiPost('/douyin/checkout/prepare', data, token),
  list: (params: { page?: number; page_size?: number }, token?: string | null) =>
    apiGet(`/douyin/checkout/list?${toQuery(params)}`, token),
  detail: (pre_order_id: string, token?: string | null) =>
    apiGet(`/douyin/checkout/detail?${toQuery({ pre_order_id })}`, token),
}

export const orderApi = {
  create: (data: { pre_order_id: string; coupon_id?: string; address_id: number; payment_method: number }, token?: string | null) =>
    apiPost('/douyin/order/create', data, token),
  cancel: (data: { order_id: string; cancel_reason?: string }, token?: string | null) =>
    apiPost('/douyin/order/cancel', data, token),
  detail: (order_id: string, token?: string | null) =>
    apiGet(`/douyin/order/detail?${toQuery({ order_id })}`, token),
  list: (params: { statuses?: number[]; page?: number; page_size?: number }, token?: string | null) =>
    apiGet(`/douyin/order/list?${toQuery(params)}`, token),
}

export const paymentApi = {
  create: (data: { order_id: string; payment_method: number }, token?: string | null) =>
    apiPost('/douyin/payment/create', data, token),
  list: (params: { method?: number; page?: number; page_size?: number }, token?: string | null) =>
    apiGet(`/douyin/payment/list?${toQuery(params)}`, token),
}

export const addressApi = {
  list: (token?: string | null) => apiGet('/douyin/user/address/list', token),
  detail: (address_id: number, token?: string | null) => apiGet(`/douyin/user/address?${toQuery({ address_id })}`, token),
  add: (data: Record<string, unknown>, token?: string | null) => apiPost('/douyin/user/address', data, token),
  update: (data: Record<string, unknown>, token?: string | null) => apiPut('/douyin/user/address', data, token),
  delete: (address_id: number, token?: string | null) => apiDelete('/douyin/user/address', { address_id }, token),
}

function toQuery(params: Record<string, unknown>) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    if (Array.isArray(value)) {
      value.forEach(item => search.append(key, typeof item === 'object' ? JSON.stringify(item) : String(item)))
      return
    }
    search.set(key, String(value))
  })
  return search.toString()
}
```

在 `couponApi.calculate` 上方增加注释：

```ts
// 后端当前将 coupon/calculate 定义为 GET + items[]；若 httpx 无法稳定解析对象数组，应改为 POST JSON。
```

- [ ] **Step 2: 更新 barrel**

`frontend/src/services/api/index.ts` 增加：

```ts
export * from './mall'
```

- [ ] **Step 3: 构建检查**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS。

### Task 8: 更新 Vite proxy 和最终验证

**Files:**

- Modify: `frontend/vite.config.ts`

- [ ] **Step 1: 调整 proxy**

将 dev server proxy 改为：

```ts
server: {
  proxy: {
    '/douyin': {
      target: 'http://localhost:8007',
      changeOrigin: true,
    },
  },
},
```

保留 `VITE_API_BASE` 覆盖能力；如使用聚合网关，只需设置 `VITE_API_BASE`。

- [ ] **Step 2: 全量前端测试**

Run:

```bash
cd frontend
npm run test
```

Expected: PASS。

- [ ] **Step 3: Lint**

Run:

```bash
cd frontend
npm run lint
```

Expected: PASS。若现有 lint 配置对测试文件路径未覆盖，调整 ESLint 配置只做必要范围变更。

- [ ] **Step 4: Build**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS。

- [ ] **Step 5: 手工联调**

启动前端：

```bash
cd frontend
npm run dev
```

手工检查：

- 未登录访问 `/agent` 跳转 `/login`。
- 登录成功后进入 `/agent`，TopBar/Sidebar 显示真实用户。
- 输入“查看我的订单”能展示工具调用和 AI 回复。
- 输入“取消订单 xxx”先显示确认卡片。
- 点击确认/取消后继续追加服务端返回事件。
- 点击停止按钮后输入框恢复可用。

## 4. 验收标准

- `frontend_docs/frontend-code-implementation-plan.md` 与 `frontend_docs/frontend-api-integration.md` 协议一致。
- 前端不再依赖 `mockStreamAgentChat` 完成主聊天链路。
- 登录接口使用 `email/password`，并保存 `Access-Token` / `Refresh-Token`。
- AI SSE 能解析 heartbeat、done、split chunk 和六类业务事件。
- 高风险确认通过 `confirm_action` 执行，按钮防重复并支持过期禁用。
- 工具失败按失败态展示，不根据 AI 文案推断成功。
- 商城 REST service 已封装但不绕过 AI 高风险确认策略。
- `npm run test`、`npm run lint`、`npm run build` 完成验证；若依赖下载或后端联调不可用，记录具体失败命令和原因。

## 5. 已知风险与后续事项

- `docs/ai-customer-service-api.md` 仍描述 WebSocket，需后续同步为 SSE。
- `coupon/calculate` GET 数组参数解析方式需要后端确认。
- 前端写普通 Cookie 无法提供 HttpOnly 安全属性，建议后端登录成功后改为 `Set-Cookie`。
- 缺少 AI 会话历史外部接口，Sidebar 刷新后无法恢复历史会话。
