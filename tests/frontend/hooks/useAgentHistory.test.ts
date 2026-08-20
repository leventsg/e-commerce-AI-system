import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useAgent } from '../../../frontend/src/hooks/useAgent'
import {
  listMessages,
  listSessions,
  type HistoryMessageDTO,
  type SessionSummaryDTO,
} from '../../../frontend/src/services/api/agent'

vi.mock('../../../frontend/src/contexts', () => ({
  useAuth: () => ({ token: 'access-token' }),
}))

vi.mock('../../../frontend/src/services/api/agent', async (importOriginal) => {
  const mod = await importOriginal<typeof import('../../../frontend/src/services/api/agent')>()
  return {
    ...mod,
    listSessions: vi.fn(),
    listMessages: vi.fn(),
    streamAgentChat: vi.fn(),
    streamConfirmAction: vi.fn(),
  }
})

function sessionDTO(id: string, title = '会话'): SessionSummaryDTO {
  return {
    conversation_id: id,
    title,
    last_message_preview: '预览',
    updated_at: '2026-08-20T15:00:00+08:00',
    message_count: 1,
  }
}

function messageDTO(id: string, role: 'user' | 'assistant', content: string, seq = 0): HistoryMessageDTO {
  const created = new Date(Date.UTC(2026, 7, 20, 0, 0, seq))
  return { message_id: id, role, content, created_at: created.toISOString() }
}

describe('useAgent history loading', () => {
  beforeEach(() => {
    vi.mocked(listSessions).mockReset()
    vi.mocked(listMessages).mockReset()
  })

  it('loads sessions and the first conversation messages on mount', async () => {
    vi.mocked(listSessions).mockResolvedValueOnce({
      total: 1,
      conversations: [sessionDTO('conv-1', '订单咨询')],
    })
    vi.mocked(listMessages).mockResolvedValueOnce({
      conversation_id: 'conv-1',
      total: 2,
      messages: [messageDTO('msg-1', 'user', '查订单', 1), messageDTO('msg-2', 'assistant', '已找到', 2)],
    })

    const { result } = renderHook(() => useAgent())

    await waitFor(() => expect(vi.mocked(listSessions)).toHaveBeenCalledWith('access-token', 1, 30))
    await waitFor(() => expect(vi.mocked(listMessages)).toHaveBeenCalledWith('access-token', 'conv-1', 1, 50))
    await waitFor(() => expect(result.current.sessions).toHaveLength(1))
    expect(result.current.activeId).toBe('conv-1')
    expect(result.current.messages).toHaveLength(2)
    expect(result.current.messages[0]).toMatchObject({ id: 'history_msg-1', type: 'user', content: '查订单' })
  })

  it('keeps a local new session placeholder when there is no history', async () => {
    vi.mocked(listSessions).mockResolvedValueOnce({ total: 0, conversations: [] })

    const { result } = renderHook(() => useAgent())

    await waitFor(() => expect(result.current.sessions).toHaveLength(1))
    expect(result.current.sessions[0].title).toBe('新会话')
    expect(result.current.sessions[0].id).toMatch(/^local_conv_/)
    expect(vi.mocked(listMessages)).not.toHaveBeenCalled()
  })

  it('loadMoreSessions appends the next page of sessions', async () => {
    vi.mocked(listSessions)
      .mockResolvedValueOnce({ total: 2, conversations: [sessionDTO('conv-1')] })
      .mockResolvedValueOnce({ total: 2, conversations: [sessionDTO('conv-2')] })
    vi.mocked(listMessages).mockResolvedValue({ conversation_id: 'conv-1', total: 0, messages: [] })

    const { result } = renderHook(() => useAgent())

    await waitFor(() => expect(result.current.sessions).toHaveLength(1))
    expect(result.current.hasMoreSessions).toBe(true)
    act(() => { result.current.loadMoreSessions() })
    await waitFor(() => expect(result.current.sessions).toHaveLength(2))
    expect(result.current.sessions[1].id).toBe('conv-2')
    expect(vi.mocked(listSessions)).toHaveBeenLastCalledWith('access-token', 2, 30)
  })

  it('loadOlderMessages fetches older pages and prepends them', async () => {
    vi.mocked(listSessions).mockResolvedValueOnce({ total: 1, conversations: [sessionDTO('conv-1')] })
    const page1 = Array.from({ length: 50 }, (_, i) => messageDTO(`msg-${i + 1}`, i % 2 === 0 ? 'user' : 'assistant', `内容${i + 1}`, i + 1))
    vi.mocked(listMessages)
      .mockResolvedValueOnce({ conversation_id: 'conv-1', total: 60, messages: page1 })
      .mockResolvedValueOnce({ conversation_id: 'conv-1', total: 60, messages: [messageDTO('msg-0', 'user', '最早的消息', 0)] })

    const { result } = renderHook(() => useAgent())

    await waitFor(() => expect(result.current.messages).toHaveLength(50))
    expect(result.current.hasOlderMessages).toBe(true)
    act(() => { result.current.loadOlderMessages('conv-1') })
    await waitFor(() => expect(vi.mocked(listMessages)).toHaveBeenLastCalledWith('access-token', 'conv-1', 2, 50))
    await waitFor(() => expect(result.current.messages).toHaveLength(51))
    expect(result.current.messages[0]).toMatchObject({ id: 'history_msg-0', content: '最早的消息' })
  })

  it('selectSession loads messages for an uncached server conversation', async () => {
    vi.mocked(listSessions).mockResolvedValueOnce({
      total: 2,
      conversations: [sessionDTO('conv-1'), sessionDTO('conv-2')],
    })
    vi.mocked(listMessages)
      .mockResolvedValueOnce({ conversation_id: 'conv-1', total: 1, messages: [messageDTO('msg-1', 'user', 'hello', 1)] })
      .mockResolvedValueOnce({ conversation_id: 'conv-2', total: 1, messages: [messageDTO('msg-2', 'assistant', 'hi', 2)] })

    const { result } = renderHook(() => useAgent())

    await waitFor(() => expect(result.current.sessions).toHaveLength(2))
    act(() => { result.current.selectSession('conv-2') })
    await waitFor(() => expect(result.current.activeId).toBe('conv-2'))
    await waitFor(() => expect(result.current.messages).toHaveLength(1))
    expect(result.current.messages[0]).toMatchObject({ content: 'hi' })
  })
})
