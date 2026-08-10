/** 后端 AgentEvent（与 domain.AgentEvent 对应） */
export interface AgentEvent {
  type: 'assistant_message' | 'assistant_delta' | 'tool_result' | 'tool_progress' | 'confirmation_required' | 'error'
  conversation_id: string
  message_id: string
  tool_call_id?: string
  content: string
  tool?: string
  status?: string
  data_json?: string
  confirmation_id?: string
  summary?: string
  expires_at?: number
  done: boolean
  business_executed?: boolean
}

/** SSE 流式请求 */
export interface AgentChatRequest {
  message: string
  conversation_id?: string
  stream?: boolean
}
