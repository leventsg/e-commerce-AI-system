import { API_BASE } from '@/constants'
import type { AgentEvent, ClientMessage } from '@/types'
import { applyTokenRenewal, createAuthHeaders, type ApiResponse } from './client'

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
  const contentType = res.headers.get('Content-Type') || ''
  if (contentType.includes('application/json')) {
    const renewal = applyTokenRenewal(await res.json() as ApiResponse<unknown>)
    if (renewal) {
      yield* streamAgent(payload, renewal.access_token, signal)
      return
    }
    throw new Error('AI 服务返回非流式响应')
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
  const eventName = eventLine?.slice(7).trim()
  if (eventName === 'done') return 'done'

  const dataLines = lines
    .filter(line => line.startsWith('data:'))
    .map(line => line.slice(5).trimStart())
  if (dataLines.length === 0) return null

  try {
    return JSON.parse(dataLines.join('\n')) as AgentEvent
  } catch (error) {
    throw new Error(`AI 服务返回无效事件: ${error instanceof Error ? error.message : 'JSON parse failed'}`)
  }
}
