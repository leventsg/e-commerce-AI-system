# 通用前端设计规范

> 基于 CookAgent 项目的生产实践提炼。适用于 AI Agent 控制台、数据看板、管理后台等企业级前端项目。

---

## 1. 状态管理分层

### 三层划分

```
Global State (Context)   ← 跨路由、跨组件持久化（Auth / Theme / 会话）
Page State (useState)    ← 单页面生命周期（表单、筛选、分页）
UI State (useState/ref)  ← 瞬态交互（Modal 开关、Tooltip、动画）
```

### 选型决策树

```
需要跨路由共享？
  ├── 是 → Context（如 AuthContext、ThemeContext）
  │         └── 跨页面频繁交互 → 考虑 Zustand
  └── 否 → 组件内部 useState
            └── 深层 prop drilling → 提升到页面级 state 或拆分为 Context
```

### 反模式

```tsx
// ❌ 把所有东西塞进一个 Context
const AppContext = createContext({
  user, theme, conversations, messages, sidebarOpen, modalState, ...
})

// ✅ 按正交域拆分
<AuthProvider>
  <ThemeProvider>
    <ConversationProvider>
      <AgentProvider>
        {children}
      </AgentProvider>
    </ConversationProvider>
  </ThemeProvider>
</AuthProvider>
```

### Context 值稳定化

```tsx
// ✅ 用 useMemo 包裹 context value，避免不必要的 re-render
export function ConversationProvider({ children }: PropsWithChildren) {
  const { token } = useAuth()
  const conversation = useConversation(token)
  const value = useMemo(() => conversation, [conversation])
  return <ConversationContext.Provider value={value}>{children}</ConversationContext.Provider>
}
```

---

## 2. API 层设计

### 三层抽象模型

```
Layer 1: HTTP Client   — 认证、错误处理、基础 fetch 封装
Layer 2: Service       — 业务 API（REST / SSE / WebSocket）
Layer 3: Hook          — 状态消费（缓存、取消、重试、乐观更新）
```

### Layer 1 — HTTP Client

```ts
// client.ts — 项目唯一的 fetch 封装入口
const API_BASE = '/api/v1'

export function createAuthHeaders(token?: string): HeadersInit {
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export async function apiGet<T>(path: string, token?: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, { headers: createAuthHeaders(token) })
  if (!res.ok) throw await parseError(res)
  return res.json()
}

// 401 统一处理：dispatch 自定义事件，由顶层组件监听
async function parseError(res: Response): Promise<Error> {
  if (res.status === 401) {
    window.dispatchEvent(new CustomEvent('auth-unauthorized'))
    return new UnauthorizedError()
  }
  return new ApiError(await res.json())
}
```

### Layer 2 — Service（含 SSE 流式）

```ts
// agent.ts — Async Generator 模式处理 SSE
export async function* streamAgentChat(
  req: AgentChatRequest, token?: string, signal?: AbortSignal
): AsyncGenerator<SSEEvent> {
  const res = await fetch(`${API_BASE}/agent/chat`, {
    method: 'POST',
    headers: { ...createJsonHeaders(token), Accept: 'text/event-stream' },
    body: JSON.stringify(req),
    signal,
  })
  const reader = res.body!.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const lines = buffer.split('\n')
    buffer = lines.pop() || ''
    for (const line of lines) {
      if (line.startsWith('data: ')) {
        const event = JSON.parse(line.slice(6))
        yield event
        if (event.type === 'done') return
      }
    }
  }
}
```

**核心设计**：Async Generator 将字节流解析与业务逻辑完全解耦。API 层只负责 `yield Event`，Hook 层决定 `事件 → 状态`。

### Layer 3 — Hook

```ts
// useAgent.ts — 流式状态 + 线程安全取消
export function useAgent(token?: string) {
  const cache = useRef<Map<string, StreamingState>>(new Map())  // 后台流式缓存
  const abortRef = useRef<AbortController | null>(null)
  const [activeId, setActiveId] = useState<string>()
  const [messages, setMessages] = useState<Message[]>([])
  const [isStreaming, setIsStreaming] = useState(false)

  const sendMessage = useCallback(async (content: string) => {
    const controller = new AbortController()
    abortRef.current = controller
    setIsStreaming(true)

    for await (const event of streamAgentChat(req, token, controller.signal)) {
      const state = cache.current.get(sessionId)
      applyEvent(state, event)  // 更新缓存
      if (activeId === sessionId) setMessages([...state.messages])  // 仅当前会话更新 UI
    }
    setIsStreaming(false)
  }, [token, activeId])

  const stopGeneration = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  return { messages, isStreaming, sendMessage, stopGeneration }
}
```

### API 层设计原则

1. **单一入口**：所有 HTTP 调用统一经过 `client.ts`，不做裸 `fetch`
2. **统一错误**：401 全局处理，业务错误类型化（`ApiError`, `UnauthorizedError`）
3. **流式解耦**：SSE 用 Async Generator 隔离解析逻辑
4. **线程安全**：`AbortController` + `useRef` 防止竞态

---

## 3. 组件设计

### 目录分组策略

```
components/
├── [domain]/           ← 按业务域分组（不是按类型）
│   ├── Feature.tsx
│   ├── FeatureDetail.tsx
│   └── index.ts       ← barrel export
├── common/             ← 跨域共享（Button, Modal, CopyButton...）
├── layout/             ← 框架壳（Sidebar, Header, Layout...）
└── index.ts            ← 顶层 barrel
```

**铁律**：永远不创建 `components/buttons/`、`components/modals/` 这种按类型分的目录。

### 组件拆分粒度

```
什么时候拆组件？
  ├── 同一文件超过 300 行
  ├── 有独立的状态逻辑（useState / useEffect）
  ├── 被 2 个以上父组件复用
  └── 有明确的视觉边界（卡片、面板、区块）

什么时候不该拆？
  └── "为了整洁"而拆 → 导致 props 地狱
```

### 组合优于配置

```tsx
// ❌ 配置式：props 膨胀
<ChatWindow mode="agent" showToolbar showSidebar showTimeline enableScroll />

// ✅ 组合式：父组件决定子组件
<ChatWindow>
  <AgentMessageList />
  <ToolSelector />
  <AgentChatInput />
</ChatWindow>
```

### 双模式处理

当系统需要两种模式（如标准 Chat vs Agent Chat），用 **容器组件 + prop 路由**，而非路由分支：

```tsx
// ✅ 容器统一入口
function ChatView({ mode }: { mode: 'chat' | 'agent' }) {
  if (mode === 'agent') return <AgentChatWindow /> /* + <AgentChatInput /> */
  return <ChatWindow /> /* + <ChatInput /> */
}
```

好处：切换模式不卸载容器，保持 Sidebar / 会话列表等外围状态不丢失。

---

## 4. 路由设计

### 路由结构

```
/                     → 重定向到 /chat
/chat/:id?            → 标准聊天
/agent/:id?           → Agent 聊天
/dashboard            → 数据看板
/settings             → 设置页面
/login                → 登录（非受保护）
```

### 路由守卫

```tsx
function RequireAuth({ children }: PropsWithChildren) {
  const { isAuthenticated } = useAuth()
  const location = useLocation()

  useEffect(() => {
    const handler = () => { /* 退出登录时触发 */ }
    window.addEventListener('auth-unauthorized', handler)
    return () => window.removeEventListener('auth-unauthorized', handler)
  }, [])

  if (!isAuthenticated) {
    return <Navigate to="/login" state={{ from: location.pathname }} replace />
  }
  return children
}
```

**要点**：
- `state.from` 记录来源路径，登录后自动跳回
- `auth-unauthorized` 事件由 HTTP client 层触发，路由层监听——解耦

---

## 5. UI 设计规范

### 色彩系统

```
主题色（Primary）
  用途：Logo、主按钮、链接、强调
  选色：1 个鲜明色（CookAgent 用 orange-500），避免多色混杂

辅助色（Accent）
  用途：Agent/高级功能（如 indigo-500）、图表

语义色
  成功: green-500   | 错误: red-500
  警告: amber-500   | 信息: blue-500

中性色（Dark Mode）
  背景: gray-950     → 全屏底色
  卡片: gray-900/800 → 内容容器
  分割: gray-700     → 边框、分割线
  文字: gray-100/300/500 → 主要/次要/辅助
```

### 间距系统

| Token | 值 | 用途 |
|---|---|---|
| `p-2` / `gap-2` | 8px | 紧凑型组件内部间距 |
| `p-4` / `gap-4` | 16px | 卡片内边距 |
| `p-6` | 24px | 页面级 section padding |
| `space-y-6` | 24px | 消息/列表项之间的间隔 |

### 卡片

```css
/* 标准卡片（dark mode） */
.card {
  @apply bg-gray-900/50 backdrop-blur-sm border border-gray-700/50 rounded-xl p-6
         hover:bg-gray-800/50 transition-colors duration-200;
}
```

### 圆角

| 尺寸 | 值 | 用途 |
|---|---|---|
| `rounded-lg` | 8px | 按钮、输入框、标签 |
| `rounded-xl` | 12px | 卡片、消息气泡 |
| `rounded-2xl` | 16px | Modal、大型面板 |

### 字体

- **UI 文本**：系统默认栈（`system-ui, -apple-system, sans-serif`）——不引入额外字体文件
- **代码/数据**：`font-mono`（等宽字体）
- **不推荐**：Google Fonts 加载（增加首屏时间，除非品牌必须）

### 动画

| 场景 | 实现 | 时长 |
|---|---|---|
| 元素出现 | `animate-fade-in` (opacity 0→1) | 200ms |
| 面板展开 | `transition-all` (max-height + opacity) | 200ms |
| Loading | `animate-spin` | 持续 |
| 骨架屏 | `animate-pulse` | 持续 |
| 侧边栏 | `transition-transform` | 300ms |

**原则**：
1. 优先 CSS `transition`，必要时才用 `@keyframes`
2. 动画时长 150-300ms，过长让用户觉得"慢"
3. 流式文字不要用动画——React diff 天然无闪烁

---

## 6. SSE 流式数据范式

### 完整模板

```ts
// 1. API 层: Async Generator
async function* streamAPI(req: Req, signal: AbortSignal): AsyncGenerator<Event> {
  const res = await fetch(url, { signal, ... })
  const reader = res.body!.getReader()
  // ... yield parsed events
}

// 2. Hook 层: useRef cache + 条件渲染
function useStream() {
  const cache = useRef<Map<string, State>>(new Map())
  const abortRef = useRef<AbortController | null>(null)
  const [activeId, setActiveId] = useState<string>()
  const [state, setState] = useState<State>()

  const start = async (req: Req) => {
    const ctrl = new AbortController()
    abortRef.current = ctrl
    for await (const event of streamAPI(req, ctrl.signal)) {
      apply(cache.current.get(id)!, event)     // 更新缓存
      if (activeId === id) setState({...cache}) // 条件渲染
    }
  }

  const stop = () => abortRef.current?.abort()
  return { state, start, stop }
}
```

### 关键决策

| 决策 | 理由 |
|---|---|
| `useRef` 存 cache | 不触发 re-render，后台静默更新 |
| `Map` 而非 `{}` | 支持动态 key（session ID），有 `size` 和迭代器 |
| 条件更新 UI | 切换会话时后台流不中断，切回时秒恢复 |
| `AbortController` | 标准 Web API，比 `axios.CancelToken` 简洁 |
| `useCallback` 包裹 start/stop | 避免 Context value 频繁变化 |

---

## 7. 工程规范

### 目录结构

```
src/
├── components/          # UI 组件（按域分组）
│   ├── [domain]/        #   业务域组件
│   ├── common/          #   跨域共享
│   ├── layout/          #   框架组件
│   └── index.ts         #   barrel export
├── contexts/            # React Context
├── hooks/               # 自定义 Hook
├── services/api/        # API 调用层
├── types/               # TypeScript 类型
├── pages/               # 路由页面
├── utils/               # 纯函数工具
└── constants/           # 常量
```

### 命名规范

| 类型 | 规范 | 示例 |
|---|---|---|
| 组件文件 | PascalCase | `AgentChatWindow.tsx` |
| Hook 文件 | camelCase, `use` 前缀 | `useAgent.ts` |
| 类型文件 | 小写, 业务域名 | `chat.ts`, `api.ts` |
| API 函数 | camelCase, 动词开头 | `streamAgentChat`, `listSessions` |
| 类型定义 | PascalCase | `SSEEvent`, `ConversationSummary` |
| 常量 | SCREAMING_SNAKE_CASE | `API_BASE`, `PAGE_SIZE` |
| Handler 函数 | `handle` + 事件名 | `handleSend`, `handleKeyDown` |
| 回调 props | `on` + 事件名 | `onSend`, `onSelect` |

### Barrel Export 模式

```ts
// contexts/index.ts
export { AuthProvider, useAuth } from './AuthContext'
export { ThemeProvider, useTheme } from './ThemeContext'
export type { AuthContextValue, ThemeContextValue } from './types'
```

- **必须加 `type` 关键字** 用于类型导出（`verbatimModuleSyntax`）
- **只导出外部需要的**，内部实现细节不暴露

### TypeScript 严格度

```json
{
  "compilerOptions": {
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "verbatimModuleSyntax": true
  }
}
```

- `strict: true` — 总开关
- `noUnusedLocals` / `noUnusedParameters` — 防止死代码
- `verbatimModuleSyntax` — 强制 `import type` 用于类型导入

### 依赖管理

```
生产依赖（dependencies）
  ├── react, react-dom            # 框架
  ├── react-router-dom            # 路由
  ├── lucide-react                # 图标（一致性 > 自绘 SVG）
  └── recharts                    # 图表（看板页）

开发依赖（devDependencies）
  ├── vite                        # 构建
  ├── typescript                  # 类型
  ├── tailwindcss                 # 样式
  ├── @tailwindcss/vite           # Vite 插件
  └── eslint                      # 代码检查
```

**原则**：能用原生 API 解决的（fetch, AbortController, ReadableStream），不引入第三方库。

---

## 8. 交互模式速查

| 模式 | 实现要点 |
|---|---|
| **路由守卫** | `<RequireAuth>` + `location.state.from` |
| **流式取消** | `AbortController` + `useRef` |
| **后台流式** | `useRef<Map>` cache + 条件 setState |
| **行内编辑** | `<input>` 覆盖 `<span>`, `onBlur` 保存, IME composing 处理 |
| **两段确认** | `showConfirm ? <ConfirmButtons> : <Trigger>` |
| **分页加载** | `hasMore` + `loadMore` + `IntersectionObserver` |
| **暗色模式** | `class` 策略 + `localStorage` + `prefers-color-scheme` |
| **401 处理** | `CustomEvent('auth-unauthorized')` + 顶层监听 |
| **Mobile 适配** | `fixed` 侧边栏 + backdrop overlay + `translate-x` |
| **空状态** | 品牌 Logo + 功能卡片 + 快捷提问词条 |

---

## 附录：技术选型参考

| 需求 | 推荐 | 不推荐 |
|---|---|---|
| 状态管理（简单） | React Context + useReducer | Redux |
| 状态管理（复杂） | Zustand | MobX |
| 表单 | React Hook Form | Formik |
| 图表 | Recharts | Chart.js |
| 图标 | Lucide React | Font Awesome |
| HTTP | 原生 fetch | Axios（除非需要拦截器） |
| 动画 | CSS transition + tailwind | Framer Motion（简单场景） |
