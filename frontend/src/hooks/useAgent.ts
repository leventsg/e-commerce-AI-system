import { useCallback, useEffect, useRef, useState } from 'react'
import type { UIMessage, ConversationSummary, StreamingState, TraceStep } from '@/types'
import type { AgentEvent, ClientMessage, RAGSource } from '@/types'
import {
  streamAgentChat,
  streamConfirmAction,
  listSessions,
  listMessages,
  type SessionSummaryDTO,
  type HistoryMessageDTO,
} from '@/services/api/agent'
import { CONVERSATIONS_PAGE_SIZE, MESSAGES_PAGE_SIZE } from '@/constants'
import { useAuth } from '@/contexts'

let idCounter = 0
function uid(p: string) { idCounter++; return `${p}_${Date.now()}_${idCounter}` }

export function createClientMessageId() {
  const bytes = new Uint8Array(16)
  const random = globalThis.crypto?.getRandomValues?.bind(globalThis.crypto)
  if (random) {
    random(bytes)
  } else {
    for (let i = 0; i < bytes.length; i += 1) {
      bytes[i] = Math.floor(Math.random() * 256)
    }
  }

  let timestamp = Date.now()
  for (let i = 5; i >= 0; i -= 1) {
    bytes[i] = timestamp & 0xff
    timestamp = Math.floor(timestamp / 256)
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x70
  bytes[8] = (bytes[8] & 0x3f) | 0x80

  const hex = Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export function serverConversationId(id: string) {
  const trimmed = id.trim()
  if (!trimmed || trimmed.startsWith('local_conv_') || /^conv_\d+_\d+$/.test(trimmed)) return undefined
  return trimmed
}

export function createConfirmActionPayload(conversationId: string, confirmationId: string, approved: boolean): ClientMessage | null {
  const confirmedConversationId = serverConversationId(conversationId)
  const confirmedConfirmationId = confirmationId.trim()
  if (!confirmedConversationId || !confirmedConfirmationId) return null
  return {
    type: 'confirm_action',
    conversation_id: confirmedConversationId,
    confirmation_id: confirmedConfirmationId,
    approved,
  }
}

function eventToolCallId(event: AgentEvent) {
  if (event.tool_call_id) return event.tool_call_id
  if (event.message_id) return event.message_id
  if (event.data && typeof event.data === 'object' && 'tool_call_id' in event.data) {
    const toolCallId = (event.data as { tool_call_id?: unknown }).tool_call_id
    if (typeof toolCallId === 'string' && toolCallId.trim()) return toolCallId
  }
  return undefined
}

function eventSources(event: AgentEvent): RAGSource[] | undefined {
  if (event.sources && event.sources.length > 0) return event.sources
  if (event.data && typeof event.data === 'object' && 'sources' in event.data) {
    const sources = (event.data as { sources?: unknown }).sources
    if (Array.isArray(sources)) return sources as RAGSource[]
  }
  return undefined
}

export function finalizeStreamingMessages(messages: UIMessage[], assistantMessageId = '') {
  const streamingIdx = assistantMessageId
    ? messages.findIndex(message => message.id === assistantMessageId)
    : messages.findIndex(message => message.streaming)
  if (streamingIdx < 0 || !messages[streamingIdx].streaming) return messages
  return messages.map((message, index) => index === streamingIdx ? { ...message, streaming: false } : message)
}

function messageBelongsToConversation(message: UIMessage, conversationId = '') {
  return !conversationId || !message.conversationId || message.conversationId === conversationId
}

function lastStreamingThinkingIndex(messages: UIMessage[], conversationId = '') {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index]
    if (message.type === 'thinking' && message.streaming && messageBelongsToConversation(message, conversationId)) {
      return index
    }
  }
  return -1
}

export function finalizeThinkingMessages(messages: UIMessage[], conversationId = '') {
  if (!messages.some(message => message.type === 'thinking' && message.streaming && messageBelongsToConversation(message, conversationId))) return messages
  return messages.map(message => message.type === 'thinking' && message.streaming && messageBelongsToConversation(message, conversationId) ? { ...message, streaming: false } : message)
}

/** 后端会话 DTO 转前端会话摘要；id 统一为 conversation_id。 */
export function conversationSummaryFromDTO(dto: SessionSummaryDTO): ConversationSummary {
  return {
    id: dto.conversation_id,
    title: dto.title,
    last_message_preview: dto.last_message_preview,
    updated_at: dto.updated_at,
    message_count: dto.message_count,
  }
}

function historySources(metadata?: Record<string, unknown> | null): RAGSource[] | undefined {
  const sources = metadata && typeof metadata.sources === 'object' && metadata.sources !== null ? metadata.sources : undefined
  if (Array.isArray(sources) && sources.length > 0) return sources as RAGSource[]
  return undefined
}

/** 后端历史消息 DTO 转前端渲染消息；tool 消息还原工具名、状态和 data_json。 */
export function historyMessageToUIMessage(dto: HistoryMessageDTO): UIMessage {
  const timestamp = Date.parse(dto.created_at) || Date.now()
  const metadata = dto.metadata && typeof dto.metadata === 'object' ? dto.metadata : undefined

  if (dto.role === 'user') {
    return { id: `history_${dto.message_id}`, type: 'user', content: dto.content, timestamp }
  }
  if (dto.role === 'tool') {
    const toolName = typeof metadata?.tool_name === 'string' ? metadata.tool_name : undefined
    const toolCallId = typeof metadata?.tool_call_id === 'string' ? metadata.tool_call_id : undefined
    const dataJson = typeof metadata?.data_json === 'string' ? metadata.data_json : undefined
    const rawStatus = metadata?.status
    return {
      id: toolCallId ? `tool_${toolCallId}` : `history_${dto.message_id}`,
      type: 'tool-result',
      content: dto.content,
      timestamp,
      toolName,
      toolStatus: rawStatus === 'success' ? 'success' : rawStatus === 'failed' ? 'failed' : 'pending',
      toolCallId,
      dataJson,
    }
  }
  return {
    id: dto.message_id,
    type: 'assistant',
    content: dto.content,
    timestamp,
    sources: historySources(metadata),
  }
}

/** 合并历史消息与现有消息：按 id 去重，用户消息额外按内容去重，最终按时间正序。 */
export function mergeHistoryMessages(existing: UIMessage[], incoming: UIMessage[]): UIMessage[] {
  if (incoming.length === 0) return existing
  const ids = new Set(existing.map(message => message.id))
  const added: UIMessage[] = []
  for (const message of incoming) {
    if (ids.has(message.id)) continue
    if (message.type === 'user' && existing.some(m => m.type === 'user' && m.content === message.content)) continue
    ids.add(message.id)
    added.push(message)
  }
  if (added.length === 0) return existing
  return [...existing, ...added].sort((a, b) => a.timestamp - b.timestamp)
}

function localSession(id: string): ConversationSummary {
  return { id, title: '新会话', updated_at: new Date().toISOString(), message_count: 0 }
}

interface HistoryEntry {
  page: number
  hasMore: boolean
  loading: boolean
}

interface ActiveHistoryState {
  hasMore: boolean
  loading: boolean
}

export function applyAgentEvent(
  currentMessages: UIMessage[],
  traceBuffer: TraceStep[],
  event: AgentEvent,
  timestamp: number,
  assistantMessageId = '',
  conversationId = '',
): { messages: UIMessage[]; assistantMessageId: string } {
  const msgs = [...currentMessages]
  let nextAssistantMessageId = assistantMessageId
  const dataJson = event.data_json ?? (event.data === undefined ? undefined : JSON.stringify(event.data))
  const toolCallId = eventToolCallId(event)
  const eventConversationId = event.conversation_id?.trim() || conversationId

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
        id: `tool_${toolCallId || timestamp}`,
        type: 'tool-result',
        content: event.content || '执行中...',
        timestamp,
        toolName: event.tool,
        toolStatus: 'running',
        toolCallId,
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

      const runningIdx = msgs.findIndex(m => m.toolCallId === toolCallId && m.toolStatus === 'running')
      const content = event.summary || event.content || (event.status === 'success' ? '执行成功' : '执行失败')
      const toolStatus = event.status === 'success' ? 'success' : 'failed'
      if (runningIdx >= 0) {
        msgs[runningIdx] = { ...msgs[runningIdx], content, toolStatus, dataJson, trace: [...traceBuffer] }
      } else {
        msgs.push({
          id: `tool_${toolCallId || timestamp}`,
          type: 'tool-result',
          content,
          timestamp,
          toolName: event.tool,
          toolStatus,
          toolCallId,
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
        conversationId: event.conversation_id,
        confirmationId: event.confirmation_id,
        expiresAt: event.expires_at,
        confirmationStatus: 'pending',
      })
      break
    case 'assistant_thinking_delta': {
      if (conversationId && eventConversationId && eventConversationId !== conversationId) {
        break
      }
      const thinkingConversationId = eventConversationId || conversationId
      const existingIdx = lastStreamingThinkingIndex(msgs, thinkingConversationId)
      if (existingIdx >= 0) {
        msgs[existingIdx] = { ...msgs[existingIdx], content: msgs[existingIdx].content + (event.content || ''), streaming: true }
      } else {
        msgs.push({
          id: `thinking_${thinkingConversationId || 'local'}_${timestamp}`,
          type: 'thinking',
          content: event.content || '',
          timestamp,
          streaming: true,
          conversationId: thinkingConversationId || undefined,
          trace: [...traceBuffer],
        })
      }
      break
    }
    case 'assistant_delta':
      for (let i = 0; i < msgs.length; i += 1) {
        if (msgs[i].type === 'thinking' && msgs[i].streaming && messageBelongsToConversation(msgs[i], eventConversationId)) {
          msgs[i] = { ...msgs[i], streaming: false }
        }
      }
      if (!nextAssistantMessageId) {
        nextAssistantMessageId = `ai_${timestamp}`
        msgs.push({ id: nextAssistantMessageId, type: 'assistant', content: event.content || '', timestamp, streaming: true, trace: [...traceBuffer] })
      } else {
        const idx = msgs.findIndex(m => m.id === nextAssistantMessageId)
        if (idx >= 0) msgs[idx] = { ...msgs[idx], content: msgs[idx].content + (event.content || '') }
      }
      break
    case 'assistant_message': {
      const sIdx = nextAssistantMessageId ? msgs.findIndex(m => m.id === nextAssistantMessageId) : msgs.findIndex(m => m.type === 'assistant' && m.streaming)
      if (sIdx >= 0) {
        msgs[sIdx] = { ...msgs[sIdx], content: event.content || msgs[sIdx].content, sources: eventSources(event), streaming: false, trace: [...traceBuffer] }
      } else {
        msgs.push({ id: event.message_id || `ai_${timestamp}`, type: 'assistant', content: event.content || '', sources: eventSources(event), timestamp, trace: [...traceBuffer] })
      }
      break
    }
    case 'error':
      msgs.push({ id: event.message_id || `err_${timestamp}`, type: 'error', content: event.content || 'AI 服务暂时不可用', timestamp })
      break
  }

  return {
    messages: event.done && event.type !== 'assistant_delta' && event.type !== 'assistant_thinking_delta'
      ? finalizeThinkingMessages(finalizeStreamingMessages(msgs, nextAssistantMessageId), eventConversationId)
      : msgs,
    assistantMessageId: nextAssistantMessageId,
  }
}

export function useAgent() {
  const { token } = useAuth()
  const streamingCache = useRef<Map<string, StreamingState>>(new Map())
  const abortRef = useRef<AbortController | null>(null)
  const [activeId, setActiveId] = useState<string>(uid('local_conv'))
  const [messages, setMessages] = useState<UIMessage[]>([])
  const [sessions, setSessions] = useState<ConversationSummary[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [sessionsTotal, setSessionsTotal] = useState(0)
  const [sessionsLoading, setSessionsLoading] = useState(false)
  const [activeHistory, setActiveHistory] = useState<ActiveHistoryState>({ hasMore: false, loading: false })
  const activeIdRef = useRef(activeId)
  const isStreamingRef = useRef(isStreaming)
  const sessionsPageRef = useRef(0)
  const sessionsLoadingRef = useRef(false)
  const historyRef = useRef<Map<string, HistoryEntry>>(new Map())

  useEffect(() => { activeIdRef.current = activeId }, [activeId])
  useEffect(() => { isStreamingRef.current = isStreaming }, [isStreaming])

  const syncConversationId = useCallback((fromId: string, toId?: string) => {
    if (!toId || toId === fromId) return fromId
    const existing = streamingCache.current.get(fromId)
    if (existing) {
      streamingCache.current.delete(fromId)
      streamingCache.current.set(toId, { ...existing, conversationId: toId })
    }
    setSessions(prev => prev.map(s => s.id === fromId ? { ...s, id: toId } : s))
    activeIdRef.current = toId
    setActiveId(toId)
    return toId
  }, [])

  const loadMessages = useCallback(async (conversationId: string, page: number) => {
    if (!token || !serverConversationId(conversationId)) return
    const entry = historyRef.current.get(conversationId)
    if (page <= (entry?.page || 0) || entry?.loading) return
    const previous: HistoryEntry = entry || { page: 0, hasMore: true, loading: false }
    historyRef.current.set(conversationId, { ...previous, loading: true })
    if (conversationId === activeIdRef.current) {
      setActiveHistory({ hasMore: previous.hasMore, loading: true })
    }
    try {
      const result = await listMessages(token, conversationId, page, MESSAGES_PAGE_SIZE)
      const incoming = (result.messages || []).map(historyMessageToUIMessage)
      const current = streamingCache.current.get(conversationId)?.messages || []
      const merged = mergeHistoryMessages(current, incoming)
      streamingCache.current.set(conversationId, { conversationId, messages: merged, isStreaming: isStreamingRef.current })
      if (conversationId === activeIdRef.current) {
        setMessages(merged)
      }
      const hasMore = result.total > page * MESSAGES_PAGE_SIZE
      historyRef.current.set(conversationId, { page, hasMore, loading: false })
      if (conversationId === activeIdRef.current) {
        setActiveHistory({ hasMore, loading: false })
      }
    } catch {
      historyRef.current.set(conversationId, { page: previous.page, hasMore: previous.hasMore, loading: false })
      if (conversationId === activeIdRef.current) {
        setActiveHistory({ hasMore: previous.hasMore, loading: false })
      }
      setError('历史消息加载失败，请稍后重试')
    }
  }, [token])

  const loadSessions = useCallback(async (page: number) => {
    if (!token || sessionsLoadingRef.current || page <= sessionsPageRef.current) return
    sessionsLoadingRef.current = true
    setSessionsLoading(true)
    try {
      const result = await listSessions(token, page, CONVERSATIONS_PAGE_SIZE)
      const incoming = (result.conversations || []).map(conversationSummaryFromDTO)
      setSessions(prev => {
        const seen = new Set(prev.map(s => s.id))
        return [...prev, ...incoming.filter(s => !seen.has(s.id))]
      })
      setSessionsTotal(result.total)
      sessionsPageRef.current = page
      if (page === 1) {
        if (incoming.length > 0) {
          const first = incoming[0]
          activeIdRef.current = first.id
          setActiveId(first.id)
          loadMessages(first.id, 1)
        } else {
          // 无历史会话时保留一个本地"新会话"，保持可直接发消息的体验
          setSessions(prev => prev.length > 0 ? prev : [localSession(activeIdRef.current)])
        }
      }
    } catch {
      setError('历史会话加载失败，请稍后重试')
    } finally {
      sessionsLoadingRef.current = false
      setSessionsLoading(false)
    }
  }, [token, loadMessages])

  const loadMoreSessions = useCallback(() => {
    loadSessions(sessionsPageRef.current + 1)
  }, [loadSessions])

  const loadOlderMessages = useCallback((conversationId: string) => {
    if (isStreamingRef.current) return
    const entry = historyRef.current.get(conversationId)
    if (!entry) {
      loadMessages(conversationId, 1)
      return
    }
    if (!entry.hasMore || entry.loading) return
    loadMessages(conversationId, entry.page + 1)
  }, [loadMessages])

  // 页面刷新/登录后：先拉会话列表，再自动拉最近会话的第一页消息
  useEffect(() => {
    if (!token) return
    loadSessions(1)
  }, [token, loadSessions])

  const updateSessionSummary = useCallback((conversationId: string, fallback: string) => {
    const cached = streamingCache.current.get(conversationId)
    const lastMsg = cached?.messages.filter(m => m.type === 'assistant').pop()
    setSessions(prev => {
      const existing = prev.find(s => s.id === conversationId)
      const summary: ConversationSummary = {
        id: conversationId,
        title: existing?.title || '新会话',
        last_message_preview: lastMsg?.content?.slice(0, 50) || fallback,
        updated_at: new Date().toISOString(),
        message_count: cached?.messages.length,
      }
      return [summary, ...prev.filter(s => s.id !== conversationId)]
    })
  }, [])

  const sendMessage = useCallback(async (content: string) => {
    if (!content.trim() || isStreaming) return
    if (!token) {
      setError('请先登录')
      setMessages(prev => [...prev, { id: uid('err'), type: 'error', content: '请先登录后再使用 AI 客服', timestamp: Date.now() }])
      return
    }
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
    let conversationId = activeId
    let assistantMsgId = ''

    try {
      const traceBuffer: TraceStep[] = []
      const payload: ClientMessage = {
        type: 'user_message',
        conversation_id: serverConversationId(activeId),
        client_message_id: createClientMessageId(),
        content,
        metadata: { source: 'web' },
      }

      for await (const event of streamAgentChat(payload, token, controller.signal)) {
        if (controller.signal.aborted) break
        const t = Date.now()
        conversationId = syncConversationId(conversationId, event.conversation_id)
        const current = streamingCache.current.get(conversationId)?.messages || []
        const result = applyAgentEvent(current, traceBuffer, event, t, assistantMsgId, conversationId)
        assistantMsgId = result.assistantMessageId
        streamingCache.current.set(conversationId, { conversationId, messages: result.messages, isStreaming: true })
        setMessages([...result.messages])
      }

      updateSessionSummary(conversationId, content)
    } catch (e) {
      if (controller.signal.aborted) return
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      setError(errMsg)
      setMessages(prev => [...prev, { id: uid('err'), type: 'error', content: `AI 服务暂时不可用：${errMsg}`, timestamp: Date.now() }])
    } finally {
      setIsStreaming(false)
      const entry = streamingCache.current.get(conversationId)
      if (entry) {
        const finalizedMessages = finalizeThinkingMessages(finalizeStreamingMessages(entry.messages, assistantMsgId), conversationId)
        streamingCache.current.set(conversationId, { ...entry, messages: finalizedMessages, isStreaming: false })
        setMessages([...finalizedMessages])
      }
    }
  }, [activeId, isStreaming, messages, syncConversationId, token, updateSessionSummary])

  const confirmAction = useCallback(async (conversationId: string, confirmationId: string, approved: boolean) => {
    const payload = createConfirmActionPayload(conversationId, confirmationId, approved)
    if (!token || !payload || isStreaming) return
    const confirmedConversationId = payload.conversation_id!
    const confirmedConfirmationId = payload.confirmation_id!
    setError(null)
    setMessages(prev => {
      const next = prev.map(message => message.conversationId === confirmedConversationId && message.confirmationId === confirmedConfirmationId ? {
        ...message,
        confirmationStatus: approved ? 'approved' as const : 'rejected' as const,
      } : message)
      streamingCache.current.set(confirmedConversationId, { conversationId: confirmedConversationId, messages: next, isStreaming: true })
      return next
    })
    setIsStreaming(true)

    const controller = new AbortController()
    abortRef.current = controller
    let currentConversationId = confirmedConversationId
    let assistantMsgId = ''

    try {
      const traceBuffer: TraceStep[] = []
      for await (const event of streamConfirmAction(payload, token, controller.signal)) {
        if (controller.signal.aborted) break
        const t = Date.now()
        currentConversationId = syncConversationId(currentConversationId, event.conversation_id)
        const current = streamingCache.current.get(currentConversationId)?.messages || []
        const result = applyAgentEvent(current, traceBuffer, event, t, assistantMsgId, currentConversationId)
        assistantMsgId = result.assistantMessageId
        streamingCache.current.set(currentConversationId, { conversationId: currentConversationId, messages: result.messages, isStreaming: true })
        setMessages([...result.messages])
      }
      updateSessionSummary(currentConversationId, approved ? '已确认操作' : '已取消操作')
    } catch (e) {
      if (controller.signal.aborted) return
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      setError(errMsg)
      setMessages(prev => [...prev, { id: uid('err'), type: 'error', content: `确认服务暂时不可用：${errMsg}`, timestamp: Date.now() }])
    } finally {
      setIsStreaming(false)
      const entry = streamingCache.current.get(currentConversationId)
      if (entry) {
        const finalizedMessages = finalizeThinkingMessages(finalizeStreamingMessages(entry.messages, assistantMsgId), currentConversationId)
        streamingCache.current.set(currentConversationId, { ...entry, messages: finalizedMessages, isStreaming: false })
        setMessages([...finalizedMessages])
      }
    }
  }, [isStreaming, syncConversationId, token, updateSessionSummary])

  const selectSession = useCallback((id: string) => {
    activeIdRef.current = id
    setActiveId(id)
    const cached = streamingCache.current.get(id)
    if (cached) {
      setMessages(cached.messages)
      setIsStreaming(cached.isStreaming)
    } else {
      setMessages([])
      setIsStreaming(false)
    }
    const entry = historyRef.current.get(id)
    setActiveHistory({ hasMore: entry?.hasMore ?? false, loading: false })
    if (!entry && serverConversationId(id)) {
      loadMessages(id, 1)
    }
  }, [loadMessages])

  const newSession = useCallback(() => {
    const id = uid('local_conv')
    setSessions(prev => [localSession(id), ...prev])
    activeIdRef.current = id
    setActiveId(id)
    setMessages([])
    setError(null)
    setActiveHistory({ hasMore: false, loading: false })
    streamingCache.current.set(id, { conversationId: id, messages: [], isStreaming: false })
  }, [])

  const stopGeneration = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  const hasMoreSessions = sessionsTotal > sessions.length
  const hasOlderMessages = activeHistory.hasMore && serverConversationId(activeId) !== undefined

  return {
    activeId,
    messages,
    sessions,
    isStreaming,
    error,
    sessionsTotal,
    sessionsLoading,
    hasMoreSessions,
    loadMoreSessions,
    hasOlderMessages,
    loadingHistory: activeHistory.loading,
    loadOlderMessages,
    sendMessage,
    confirmAction,
    selectSession,
    newSession,
    stopGeneration,
  }
}
