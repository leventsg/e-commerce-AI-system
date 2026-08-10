/** 统一消息类型（对话流中渲染的原子单元） */
export interface UIMessage {
  id: string
  type: 'user' | 'assistant' | 'tool-result' | 'confirmation' | 'error' | 'system'
  content: string
  timestamp: number
  streaming?: boolean
  /** 工具调用相关 */
  toolName?: string
  toolStatus?: 'pending' | 'running' | 'success' | 'failed'
  toolCallId?: string
  dataJson?: string
  /** 确认卡片 */
  confirmationId?: string
  expiresAt?: number
  /** 工具调用链路步骤 */
  trace?: TraceStep[]
}

/** 单个工具调用步骤 */
export interface TraceStep {
  action: string
  tool_name?: string
  status: 'pending' | 'running' | 'success' | 'failed'
  iteration: number
  timestamp: string
  duration?: number
  params?: Record<string, unknown>
  result?: Record<string, unknown>
  error?: string
  subagent_name?: string
}

/** 会话列表摘要 */
export interface ConversationSummary {
  id: string
  title: string
  last_message_preview?: string
  updated_at: string
  message_count?: number
}

/** 流式状态缓存 */
export interface StreamingState {
  conversationId: string
  messages: UIMessage[]
  isStreaming: boolean
}
