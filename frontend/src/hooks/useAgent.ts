import { useCallback, useRef, useState } from 'react'
import type { UIMessage, ConversationSummary, StreamingState, TraceStep } from '@/types'
import { mockStreamAgentChat } from '@/services/api/mock'
import { TOOL_DISPLAY_NAMES } from '@/constants'

let idCounter = 0
function uid(p: string) { idCounter++; return `${p}_${Date.now()}_${idCounter}` }

export function useAgent() {
  const streamingCache = useRef<Map<string, StreamingState>>(new Map())
  const abortRef = useRef<AbortController | null>(null)
  const [activeId, setActiveId] = useState<string>(uid('conv'))
  const [messages, setMessages] = useState<UIMessage[]>([])
  const [sessions, setSessions] = useState<ConversationSummary[]>(() => [{
    id: activeId, title: '新会话', last_message_preview: '', updated_at: new Date().toISOString(), message_count: 0,
  }])
  const [isStreaming, setIsStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const sendMessage = useCallback(async (content: string) => {
    if (!content.trim() || isStreaming) return
    setError(null)

    const now = Date.now()

    // 添加用户消息
    const userMsg: UIMessage = { id: uid('user'), type: 'user', content, timestamp: now }
    const updated = [...messages, userMsg]
    setMessages(updated)

    // 更新流式缓存
    const cacheEntry: StreamingState = { conversationId: activeId, messages: updated, isStreaming: true }
    streamingCache.current.set(activeId, cacheEntry)
    setIsStreaming(true)

    const controller = new AbortController()
    abortRef.current = controller

    try {
      const traceBuffer: TraceStep[] = []
      let assistantContent = ''
      let assistantMsgId = ''

      for await (const event of mockStreamAgentChat({ message: content, conversation_id: activeId, stream: true })) {
        if (controller.signal.aborted) break
        const t = Date.now()
        const msgs = [...streamingCache.current.get(activeId)!.messages]

        switch (event.type) {
          case 'tool_progress': {
            const step: TraceStep = {
              action: event.tool || 'tool_call',
              tool_name: event.tool,
              status: 'running',
              iteration: traceBuffer.length,
              timestamp: new Date().toISOString(),
              params: {},
            }
            traceBuffer.push(step)

            // 内联工具调用气泡
            msgs.push({
              id: `tool_${event.tool_call_id || t}`,
              type: 'tool-result',
              content: event.content || '执行中...',
              timestamp: t,
              toolName: event.tool,
              toolStatus: 'running',
              toolCallId: event.tool_call_id,
              trace: [...traceBuffer],
            })
            break
          }
          case 'tool_result': {
            // 更新最后一个 tool_progress 步骤
            const last = traceBuffer[traceBuffer.length - 1]
            if (last) {
              last.status = event.status === 'success' ? 'success' : 'failed'
              last.result = event.data_json ? JSON.parse(event.data_json) : undefined
            }

            // 移除 running 状态的 tool 消息，替换为 result
            const runningIdx = msgs.findIndex(m => m.toolCallId === event.tool_call_id && m.toolStatus === 'running')
            if (runningIdx >= 0) {
              msgs[runningIdx] = {
                ...msgs[runningIdx],
                content: event.summary || event.content,
                toolStatus: event.status === 'success' ? 'success' : 'failed',
                dataJson: event.data_json,
                trace: [...traceBuffer],
              }
            } else {
              msgs.push({
                id: `tool_${event.tool_call_id || t}`,
                type: 'tool-result',
                content: event.summary || event.content,
                timestamp: t,
                toolName: event.tool,
                toolStatus: event.status === 'success' ? 'success' : 'failed',
                toolCallId: event.tool_call_id,
                dataJson: event.data_json,
                trace: [...traceBuffer],
              })
            }
            break
          }
          case 'confirmation_required': {
            msgs.push({
              id: `confirm_${event.confirmation_id || t}`,
              type: 'confirmation',
              content: event.content,
              timestamp: t,
              confirmationId: event.confirmation_id,
              expiresAt: event.expires_at,
            })
            break
          }
          case 'assistant_delta': {
            if (!assistantMsgId) {
              assistantMsgId = uid('ai')
              msgs.push({ id: assistantMsgId, type: 'assistant', content: event.content, timestamp: t, streaming: true, trace: [...traceBuffer] })
            } else {
              const idx = msgs.findIndex(m => m.id === assistantMsgId)
              if (idx >= 0) msgs[idx] = { ...msgs[idx], content: msgs[idx].content + event.content }
            }
            break
          }
          case 'assistant_message': {
            // 最终消息：替换流式消息
            const sIdx = msgs.findIndex(m => m.streaming)
            if (sIdx >= 0) {
              msgs[sIdx] = { ...msgs[sIdx], content: event.content, streaming: false, trace: [...traceBuffer] }
            } else {
              msgs.push({ id: event.message_id || uid('ai'), type: 'assistant', content: event.content, timestamp: t, trace: [...traceBuffer] })
            }
            break
          }
          case 'error': {
            msgs.push({ id: event.message_id, type: 'error', content: event.content, timestamp: t })
            break
          }
        }

        streamingCache.current.set(activeId, { conversationId: activeId, messages: msgs, isStreaming: true })
        setMessages([...msgs])
      }

      // 更新会话列表
      const lastMsg = streamingCache.current.get(activeId)?.messages.filter(m => m.type === 'assistant').pop()
      setSessions(prev => prev.map(s => s.id === activeId ? {
        ...s, last_message_preview: lastMsg?.content?.slice(0, 50) || content, updated_at: new Date().toISOString(),
        message_count: streamingCache.current.get(activeId)?.messages.length,
      } : s))
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      setError(errMsg)
      setMessages(prev => [...prev, { id: uid('err'), type: 'error', content: `AI 服务暂时不可用：${errMsg}`, timestamp: Date.now() }])
    } finally {
      setIsStreaming(false)
      const entry = streamingCache.current.get(activeId)
      if (entry) streamingCache.current.set(activeId, { ...entry, isStreaming: false })
    }
  }, [activeId, isStreaming, messages])

  const selectSession = useCallback((id: string) => {
    const cached = streamingCache.current.get(id)
    if (cached) {
      setMessages(cached.messages)
      setIsStreaming(cached.isStreaming)
    } else {
      setMessages([])
      setIsStreaming(false)
    }
    setActiveId(id)
  }, [])

  const newSession = useCallback(() => {
    const id = uid('conv')
    const newConv: ConversationSummary = { id, title: '新会话', updated_at: new Date().toISOString(), message_count: 0 }
    setSessions(prev => [newConv, ...prev])
    setActiveId(id)
    setMessages([])
    setError(null)
    streamingCache.current.set(id, { conversationId: id, messages: [], isStreaming: false })
  }, [])

  const stopGeneration = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  return { activeId, messages, sessions, isStreaming, error, sendMessage, selectSession, newSession, stopGeneration }
}
