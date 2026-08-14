export type ClientEventType = 'user_message' | 'confirm_action'
export type ServerEventType = 'assistant_message' | 'assistant_delta' | 'assistant_thinking_delta' | 'tool_result' | 'tool_progress' | 'confirmation_required' | 'error'

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

/** 旧 mock 类型兼容；真实后端使用 ClientMessage。 */
export interface AgentChatRequest {
  message: string
  conversation_id?: string
  stream?: boolean
}
