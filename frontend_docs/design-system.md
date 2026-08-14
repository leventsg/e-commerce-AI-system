# CookAgent Design System

> 基于 CookAgent 项目（React 19 + Tailwind CSS 4 + Dark Mode）的实际样式提取。
> 本规范可跨项目复用，适用于 AI Agent 控制台、数据看板、管理后台等企业级产品。

---

## 1. 页面布局规范

### 整体框架

```
┌──────────────────────────────────────────────────────┐
│  Sidebar (fixed / overlay)  │  Main Content (flex-1) │
│  w-64 (256px)               │  min-h-screen          │
│  bg-gray-950 border-r       │  flex flex-col         │
└──────────────────────────────────────────────────────┘
```

### 布局实现

```css
/* 根布局 */
.page-layout {
  @apply flex h-screen overflow-hidden bg-gray-950;
}

/* 侧边栏 */
.sidebar {
  @apply w-64 shrink-0 border-r border-gray-800/80 bg-gray-950
         flex flex-col overflow-y-auto;
}

/* 主内容区 */
.main-content {
  @apply flex-1 flex flex-col min-w-0 overflow-hidden;
}
```

### 响应式策略

| 断点 | 侧边栏 | 内容区 |
|---|---|---|
| Desktop (`lg+`) | `w-64` 固定 | `flex-1` |
| Mobile (`<lg`) | `fixed inset-y-0 left-0 z-40` + backdrop | `w-full` |

```css
/* Mobile 侧边栏 */
.sidebar-mobile {
  @apply fixed inset-y-0 left-0 z-40 w-64 transform transition-transform duration-300;
}
.sidebar-mobile.open  { @apply translate-x-0; }
.sidebar-mobile.closed { @apply -translate-x-full; }

/* Backdrop */
.sidebar-backdrop {
  @apply fixed inset-0 z-30 bg-black/50 backdrop-blur-sm;
}
```

### 内容区内部布局

```css
/* 消息/列表区: 可滚动 */
.scroll-area {
  @apply flex-1 overflow-y-auto px-4 md:px-6 py-4;
}

/* 输入区: 固定底部 */
.input-bar {
  @apply shrink-0 border-t border-gray-800/80 px-4 py-3;
}
```

### 最大内容宽度

| 场景 | 宽度 |
|---|---|
| 聊天消息列表 | `max-w-3xl mx-auto` |
| 表单（登录/注册） | `max-w-md mx-auto` |
| 数据看板 | `max-w-7xl mx-auto` |

---

## 2. 色彩规范

### 基础色板

```
Dark Mode（默认）
  页面背景: bg-gray-950  (#030712)
  卡片背景: bg-gray-900/60 — bg-gray-800/50
  Hover 态: bg-gray-800
  边框:     border-gray-800/80 — border-gray-700/50
  分割线:   border-gray-800

  主文字:   text-gray-100  (#f3f4f6)
  次文字:   text-gray-300  (#d1d5db)
  辅助文字: text-gray-400  (#9ca3af)
  禁用文字: text-gray-500  (#6b7280)

Light Mode
  页面背景: bg-gradient-to-b from-white to-gray-50
  卡片背景: bg-white
  Hover 态: bg-gray-50
  边框:     border-gray-200

  主文字:   text-gray-900
  次文字:   text-gray-700
  辅助文字: text-gray-500
```

### 语义色

```css
/* Primary — 品牌主色 */
--color-primary: #f97316;  /* orange-500 */
--color-primary-hover: #ea580c;  /* orange-600 */

/* Accent — Agent/高级功能 */
--color-accent: #6366f1;   /* indigo-500 */

/* Success */
--color-success: #10b981;  /* emerald-500 */
--color-success-bg: rgba(16, 185, 129, 0.1);

/* Error */
--color-error: #ef4444;    /* red-500 */
--color-error-bg: rgba(239, 68, 68, 0.1);

/* Warning */
--color-warning: #f59e0b;  /* amber-500 */
--color-warning-bg: rgba(245, 158, 11, 0.1);

/* Info */
--color-info: #3b82f6;     /* blue-500 */
--color-info-bg: rgba(59, 130, 246, 0.1);
```

### 图表色板

```css
/* 8 色调色板，用于图表和数据可视化 */
--chart-1: #f97316;  /* orange  */
--chart-2: #3b82f6;  /* blue    */
--chart-3: #10b981;  /* emerald */
--chart-4: #8b5cf6;  /* violet  */
--chart-5: #ec4899;  /* pink    */
--chart-6: #eab308;  /* yellow  */
--chart-7: #6366f1;  /* indigo  */
--chart-8: #14b8a6;  /* teal    */
```

### 渐变

```css
/* 页面背景渐变 */
.bg-page-gradient {
  @apply bg-gradient-to-b from-white to-gray-50
         dark:from-gray-900 dark:to-gray-950;
}

/* 登录页渐变 */
.bg-login-gradient {
  @apply bg-gradient-to-br from-orange-50 via-white to-amber-50
         dark:from-gray-900 dark:via-gray-950 dark:to-gray-900;
}

/* 统计卡片渐变 */
.bg-stat-gradient-orange {
  @apply bg-gradient-to-br from-orange-50 to-orange-100
         dark:from-orange-900/20 dark:to-orange-800/20;
}
```

---

## 3. Typography 规范

### 字体族

```css
/* 不使用 Google Fonts — 零额外网络请求 */
body {
  font-family: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
}

/* 等宽字体 — 代码 / 数据 / 时间戳 */
.font-mono {
  font-family: ui-monospace, 'Cascadia Code', 'Fira Code', monospace;
}
```

### 字号阶梯

| Token | Class | 尺寸 | 用途 |
|---|---|---|---|
| `text-xs` | `0.75rem / 1rem` | 辅助文字、标签、时间戳 |
| `text-sm` | `0.875rem / 1.25rem` | **正文（默认）** |
| `text-base` | `1rem / 1.5rem` | 强调段落、输入框 |
| `text-lg` | `1.125rem / 1.75rem` | 小标题 |
| `text-xl` | `1.25rem / 1.75rem` | Modal 标题 |
| `text-2xl` | `1.5rem / 2rem` | 页面标题 |
| `text-3xl` | `1.875rem / 2.25rem` | Logo / 品牌 |

### 字重

| Token | 用途 |
|---|---|
| `font-normal (400)` | 正文 |
| `font-medium (500)` | 标签、按钮文字、卡片标题 |
| `font-semibold (600)` | 页面标题、Modal 标题 |
| `font-bold (700)` | Logo、Hero 标题 |

### 文字颜色层级

```
Heading   → text-gray-900 dark:text-gray-100  (font-semibold)
Body      → text-gray-700 dark:text-gray-300  (font-normal)
Secondary → text-gray-500 dark:text-gray-400  (text-sm)
Disabled  → text-gray-400 dark:text-gray-500  (text-sm)
Placeholder → placeholder-gray-400 dark:placeholder-gray-500
```

---

## 4. Button 规范

### 层级定义

```css
/* Primary — 主操作（每页面仅一个） */
.btn-primary {
  @apply px-4 py-2 rounded-lg font-medium text-sm
         bg-orange-500 text-white
         hover:bg-orange-600 active:bg-orange-700
         transition-colors duration-150
         disabled:opacity-50 disabled:cursor-not-allowed;
}

/* Secondary — 次要操作 */
.btn-secondary {
  @apply px-4 py-2 rounded-lg font-medium text-sm
         bg-gray-800 text-gray-200 border border-gray-700/50
         hover:bg-gray-700 hover:border-gray-600
         transition-colors duration-150
         disabled:opacity-50;
}

/* Ghost — 无背景按钮（Toolbar、Action） */
.btn-ghost {
  @apply p-2 rounded-lg
         text-gray-400 hover:text-gray-200 hover:bg-gray-800
         transition-colors duration-150;
}

/* Danger — 删除等危险操作 */
.btn-danger {
  @apply px-3 py-1.5 rounded-lg text-sm
         text-red-400 hover:text-red-300 hover:bg-red-900/30
         transition-colors duration-150;
}
```

### 尺寸

| Size | Class | 用途 |
|---|---|---|
| `sm` | `px-3 py-1.5 text-sm` | 表格操作、标签操作 |
| `md` | `px-4 py-2 text-sm` | **标准按钮** |
| `lg` | `px-6 py-3 text-base` | 登录/注册提交 |
| `icon` | `p-2` (方形) | Icon-only 按钮 |

### 状态

```css
/* Loading */
.btn-loading {
  @apply cursor-wait opacity-80;
}

/* Active / Pressed */
.btn-active {
  @apply scale-[0.98];
}

/* Focus ring */
.btn:focus-visible {
  @apply outline-none ring-2 ring-orange-500 ring-offset-2
         ring-offset-gray-950;
}
```

### Icon Button 模式

```tsx
// 标准图标按钮
<button className="p-2 rounded-lg text-gray-400 hover:text-gray-200 hover:bg-gray-800 transition-colors">
  <SendHorizontal className="w-5 h-5" />
</button>

// 取消/停止按钮（流式中断）
<button className="p-2 rounded-lg bg-red-500 text-white hover:bg-red-600 transition-colors">
  <Square className="w-4 h-4" />
</button>
```

---

## 5. Card 规范

### 标准卡片

```css
.card {
  @apply bg-white dark:bg-gray-900/60
         border border-gray-200/70 dark:border-gray-800/80
         rounded-2xl p-6
         shadow-sm
         transition-all duration-200;
}

.card:hover {
  @apply shadow-md -translate-y-0.5;
}
```

### 变体

```css
/* Glassmorphism（工具选择面板、下拉菜单） */
.card-glass {
  @apply bg-gray-800/50 backdrop-blur-sm
         border border-gray-700/50
         rounded-xl;
}

/* 统计卡片 */
.card-stat {
  @apply bg-gradient-to-br from-orange-50 to-orange-100
         dark:from-orange-900/20 dark:to-orange-800/20
         border border-orange-200 dark:border-orange-800
         rounded-2xl p-4 shadow-sm;
}

/* 消息气泡 — 用户 */
.card-message-user {
  @apply bg-orange-500/10 border border-orange-500/20
         rounded-2xl rounded-br-md px-4 py-2.5;
}

/* 消息气泡 — AI */
.card-message-ai {
  @apply bg-gray-800/50 border border-gray-700/30
         rounded-2xl rounded-bl-md px-4 py-2.5;
}
```

### 圆角规范

| 元素 | Class | 场景 |
|---|---|---|
| 按钮、输入框、标签 | `rounded-lg` (8px) | 小尺寸控件 |
| 卡片、消息气泡、面板 | `rounded-xl` (12px) | 内容容器 |
| Modal、大型面板 | `rounded-2xl` (16px) | 覆盖层 |

---

## 6. Modal / Dialog 规范

### 结构

```tsx
function Modal({ open, onClose, title, children }) {
  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center px-4 py-8">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Content */}
      <div className={`
        relative w-full max-w-lg
        bg-white dark:bg-gray-900
        rounded-2xl shadow-2xl
        border border-gray-200/70 dark:border-gray-800/70
        overflow-hidden
      `}>
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4
                        border-b border-gray-200/80 dark:border-gray-800/80">
          <h3 className="text-xl font-semibold">{title}</h3>
          <button onClick={onClose} className="p-1 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800">
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Body */}
        <div className="px-6 py-4 overflow-y-auto">
          {children}
        </div>
      </div>
    </div>,
    document.body
  )
}
```

### 宽度预设

| Token | Class | 场景 |
|---|---|---|
| `sm` | `max-w-sm` | 确认对话框 |
| `md` | `max-w-md` | 简单表单 |
| `lg` | `max-w-lg` | **标准 Modal** |
| `xl` | `max-w-xl` | 宽表单 |
| `2xl` | `max-w-2xl` | 详情展示 |
| `4xl` | `max-w-4xl` | 复杂编辑器 |

### 交互

| 行为 | 实现 |
|---|---|
| 关闭 | ESC 键 + 点击 Backdrop + X 按钮 |
| 打开时 | `document.body.style.overflow = 'hidden'` |
| 动画 | `backdrop-blur-sm` + `shadow-2xl` |

---

## 7. 表单规范

### 输入框

```css
.input {
  @apply w-full rounded-lg
         border border-gray-200 dark:border-gray-800
         bg-white dark:bg-gray-950
         text-gray-900 dark:text-gray-100
         placeholder-gray-400 dark:placeholder-gray-500
         px-3 py-2 text-sm
         focus:outline-none focus:ring-2 focus:ring-orange-500
         transition-colors duration-150;
}

.input-error {
  @apply border-red-500 focus:ring-red-500;
}
```

### 标签

```css
.label {
  @apply block text-sm font-medium
         text-gray-700 dark:text-gray-200
         mb-1;
}
```

### 表单布局

```css
.form-stack {
  @apply space-y-4;  /* 字段间隔 16px */
}

.form-group {
  /* label + input 自然堆叠 */
}
```

### 错误状态

```css
.form-error {
  @apply text-sm text-red-600 dark:text-red-400
         bg-red-50 dark:bg-red-900/30
         border border-red-200 dark:border-red-800
         rounded-lg px-3 py-2 mt-1;
}
```

### Textarea（Agent 输入框）

```css
.textarea-agent {
  @apply w-full resize-none
         bg-transparent
         text-sm text-gray-100
         placeholder-gray-500
         outline-none
         min-h-[40px] max-h-[200px]
         px-3 py-2;
}
```

### Switch / Toggle

```css
.toggle {
  @apply relative w-10 h-5 rounded-full
         bg-gray-700 transition-colors duration-200
         peer-checked:bg-orange-500;
}

.toggle-thumb {
  @apply absolute top-0.5 left-0.5 w-4 h-4 rounded-full
         bg-white transition-transform duration-200
         peer-checked:translate-x-5;
}
```

---

## 8. 状态展示规范

### Status Badge

```css
.status-badge {
  @apply inline-flex items-center gap-1.5
         px-2.5 py-1 rounded-full text-xs font-medium;
}

.status-success { @apply bg-emerald-500/10 text-emerald-400 border border-emerald-500/20; }
.status-warning { @apply bg-amber-500/10 text-amber-400 border border-amber-500/20; }
.status-error   { @apply bg-red-500/10 text-red-400 border border-red-500/20; }
.status-info    { @apply bg-blue-500/10 text-blue-400 border border-blue-500/20; }
.status-neutral { @apply bg-gray-500/10 text-gray-400 border border-gray-500/20; }
```

### 进度指示

```css
/* Spinner — 加载中 */
.spinner {
  @apply animate-spin text-orange-500 w-5 h-5;
}

/* Pulse — 骨架屏 */
.skeleton {
  @apply animate-pulse bg-gray-800 rounded-lg;
}

/* 脉冲点 — 在线/活跃 */
.pulse-dot {
  @apply w-2 h-2 rounded-full bg-emerald-500 animate-pulse;
}
```

### 颜色语义对照

| 含义 | 颜色 | 典型场景 |
|---|---|---|
| 成功 / 正常 | `emerald-500` | 工具执行成功、指标正常、操作完成 |
| 警告 / 需关注 | `amber-500` | 指标低于阈值、工具调用中、待处理 |
| 错误 / 失败 | `red-500` | 工具执行失败、认证失败、网络错误 |
| 信息 / 中性 | `blue-500` | 工具结果、提示信息、数据展示 |
| 进行中 | `orange-500` + `animate-spin` | 流式输出、Agent 思考、数据加载 |

---

## 9. Loading / Error / Empty 状态设计

### Loading

```tsx
// 全屏 Loading
function PageLoading() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-950">
      <Loader2 className="w-8 h-8 text-orange-500 animate-spin" />
    </div>
  )
}

// 内联 Loading（消息流中）
function LoadingIndicator() {
  return (
    <div className="flex items-center gap-3 px-4 py-6 animate-fade-in">
      <div className="w-8 h-8 rounded-full bg-orange-500/20 flex items-center justify-center">
        <Loader2 className="w-4 h-4 text-orange-500 animate-spin" />
      </div>
      <div className="flex gap-1">
        <span className="w-2 h-2 bg-orange-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
        <span className="w-2 h-2 bg-orange-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
        <span className="w-2 h-2 bg-orange-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
      </div>
    </div>
  )
}

// 按钮 Loading
function ButtonLoading({ label }: { label: string }) {
  return (
    <button disabled className="btn-primary cursor-wait opacity-80">
      <Loader2 className="w-4 h-4 animate-spin inline mr-2" />
      {label}
    </button>
  )
}
```

### Error

```tsx
// 内联错误提示
function InlineError({ message }: { message: string }) {
  return (
    <div className="flex items-start gap-3 px-4 py-3 mx-4
                    bg-red-500/10 border border-red-500/20
                    rounded-xl text-sm text-red-400">
      <AlertCircle className="w-5 h-5 shrink-0 mt-0.5" />
      <span>{message}</span>
    </div>
  )
}

// 表单错误
function FormError({ errors }: { errors: string | string[] }) {
  return (
    <div className="text-sm text-red-600 dark:text-red-400
                    bg-red-50 dark:bg-red-900/30
                    border border-red-200 dark:border-red-800
                    rounded-lg px-3 py-2">
      {Array.isArray(errors) ? (
        <ul className="list-disc ml-5 space-y-1">
          {errors.map((e, i) => <li key={i}>{e}</li>)}
        </ul>
      ) : (
        <p>{errors}</p>
      )}
    </div>
  )
}
```

### Empty

```tsx
// 空状态标准模板
function EmptyState({
  icon,           // ReactNode: 品牌图标
  title,          // string: 标题
  description,    // string: 描述文案
  suggestions,    // { icon, text }[]: 快捷提问 / 操作入口
  onSuggestionClick,
}) {
  return (
    <div className="h-full flex flex-col items-center justify-center px-6 py-12">
      {/* Logo */}
      <div className="w-20 h-20 rounded-2xl bg-orange-500/10
                      flex items-center justify-center mb-6">
        {icon}
      </div>

      {/* Text */}
      <h2 className="text-2xl font-bold text-gray-100 mb-2">{title}</h2>
      <p className="text-gray-400 text-center max-w-md mb-8">{description}</p>

      {/* Feature Cards */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4 max-w-2xl w-full mb-8">
        {suggestions.map((s, i) => (
          <button
            key={i}
            onClick={() => onSuggestionClick?.(s.text)}
            className="flex items-center gap-3 p-4 rounded-xl
                       bg-gray-800/50 border border-gray-700/30
                       hover:border-orange-500/30 hover:bg-gray-800
                       transition-all text-left group"
          >
            <div className="p-2 rounded-lg bg-orange-500/10
                            group-hover:bg-orange-500/20 transition-colors">
              {s.icon}
            </div>
            <span className="text-sm text-gray-300">{s.text}</span>
          </button>
        ))}
      </div>
    </div>
  )
}
```

### 状态展示决策树

```
数据状态
├── Loading (首次)      → 全屏 Spinner / 骨架屏
├── Loading (增量)      → 消息流底部 Spinner + 跳跃点动画
├── Error (可恢复)      → 内联 Error + Retry 按钮
├── Error (不可恢复)    → 全屏 Error + 返回首页
├── Empty (无数据)      → Logo + 标题 + 描述 + 快捷操作
├── Empty (搜索无结果)  → "No results" + 修改搜索建议
└── Success             → 正常渲染
```

---

## 10. 动画交互规范

### 过渡时长

| 类型 | 时长 | 用途 |
|---|---|---|
| 微交互 | `150ms` | 按钮 hover、颜色切换 |
| 标准 | `200ms` | 面板展开、元素出现/消失 |
| 强调 | `300ms` | 侧边栏滑入、Modal 出现 |
| 持续 | `infinite` | Spinner、脉冲、骨架屏 |

### CSS 动画类

```css
/* 淡入 */
@keyframes fade-in {
  from { opacity: 0; }
  to   { opacity: 1; }
}
.animate-fade-in {
  animation: fade-in 0.2s ease-out;
}

/* 滑入（从下方） */
@keyframes slide-up {
  from { opacity: 0; transform: translateY(8px); }
  to   { opacity: 1; transform: translateY(0); }
}
.animate-slide-up {
  animation: slide-up 0.3s ease-out;
}

/* 跳跃点（Loading 动画） */
.animate-bounce {
  animation: bounce 1s infinite;
}

/* 旋转 */
.animate-spin {
  animation: spin 1s linear infinite;
}

/* 脉冲 */
.animate-pulse {
  animation: pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite;
}
```

### Hover 效果

```css
/* 卡片 hover — 轻微上浮 + 阴影增强 */
.card-interactive {
  @apply transition-all duration-200;
}
.card-interactive:hover {
  @apply shadow-md -translate-y-0.5;
}

/* 列表项 hover — 背景色变化 */
.list-item {
  @apply transition-colors duration-150 cursor-pointer;
}
.list-item:hover {
  @apply bg-gray-800;
}

/* 工具芯片 hover — 边框高亮 */
.tool-chip {
  @apply border border-gray-700/50 transition-colors duration-150;
}
.tool-chip:hover {
  @apply border-orange-500/30 bg-orange-500/5;
}
```

### 展开/折叠

```css
/* Thinking Block — max-height 动画 */
.thinking-block {
  @apply overflow-hidden transition-all duration-200;
}
.thinking-block.open {
  @apply max-h-[2000px] opacity-100;
}
.thinking-block.closed {
  @apply max-h-0 opacity-0;
}
```

### 侧边栏滑入（Mobile）

```css
.sidebar {
  @apply fixed inset-y-0 left-0 z-40 w-64
         transform transition-transform duration-300 ease-in-out;
}
.sidebar.closed { @apply -translate-x-full; }
.sidebar.open   { @apply translate-x-0; }

.sidebar-backdrop {
  @apply fixed inset-0 z-30 bg-black/50 backdrop-blur-sm
         transition-opacity duration-300;
}
```

### 流式文字

**不做任何动画**。React 的 Virtual DOM diff 天然保证：
- 新字符追加不闪烁
- 光标用 `animate-pulse` 的 `<span>` 模拟
- Markdown 渲染在流式结束后统一执行

```tsx
// 流式光标
{isStreaming && (
  <span className="inline-block w-1.5 h-4 bg-orange-500 ml-0.5
                   animate-pulse align-text-bottom rounded-sm" />
)}
```

### 动画原则

1. **优先 CSS `transition`**，需要关键帧时才用 `@keyframes`
2. **时长 150-300ms**，不做超过 500ms 的动画
3. **流式内容不用动画** — React diff 天然无闪烁
4. **`prefers-reduced-motion`** 尊重系统设置
5. **GPU 加速属性**：`transform`、`opacity`（避免 `width`/`height` 动画）

---

## 附录：Tailwind Config 参考

```js
// tailwind.config.ts
export default {
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        primary: {
          50: '#fff7ed',
          500: '#f97316',
          600: '#ea580c',
        },
      },
      borderRadius: {
        'xl': '0.75rem',   // 12px
        '2xl': '1rem',     // 16px
      },
      animation: {
        'fade-in': 'fade-in 0.2s ease-out',
        'slide-up': 'slide-up 0.3s ease-out',
        'pulse-dot': 'pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
      keyframes: {
        'fade-in': {
          from: { opacity: '0' },
          to: { opacity: '1' },
        },
        'slide-up': {
          from: { opacity: '0', transform: 'translateY(8px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
      },
    },
  },
}
```
