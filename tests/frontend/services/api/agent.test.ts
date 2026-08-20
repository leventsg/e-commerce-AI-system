import { describe, expect, it, vi } from 'vitest'
import { streamAgentChat, listSessions, listMessages } from '../../../../frontend/src/services/api/agent'
import { setTokenRefreshHandler } from '../../../../frontend/src/services/api/client'

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

  it('joins multiple data lines in one SSE frame', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(streamFrom([
      'event: assistant_message\n',
      'data: {"type":"assistant_message",\n',
      'data: "conversation_id":"conv_1",\n',
      'data: "content":"你好","done":true}\n\n',
      'event: done\ndata: {"done":true}\n\n',
    ]), { status: 200 })))

    const events = []
    for await (const event of streamAgentChat({
      type: 'user_message',
      content: '你好',
      client_message_id: '018f6a08-7d4b-75d8-8f7a-7d4a2a4be001',
    }, 'access')) {
      events.push(event)
    }

    expect(events[0]).toMatchObject({
      type: 'assistant_message',
      conversation_id: 'conv_1',
      content: '你好',
    })
  })

  it('persists renewed tokens and retries SSE requests with the new access token', async () => {
    const refreshHandler = vi.fn()
    setTokenRefreshHandler(refreshHandler)
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        code: 10004,
        msg: '令牌续期成功',
        data: { access_token: 'new-access', refresh_token: 'new-refresh' },
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(streamFrom([
        'event: assistant_message\n',
        'data: {"type":"assistant_message","conversation_id":"conv_1","content":"你好","done":true}\n\n',
        'event: done\ndata: {"done":true}\n\n',
      ]), { status: 200, headers: { 'Content-Type': 'text/event-stream' } }))
    vi.stubGlobal('fetch', fetchMock)

    const events = []
    for await (const event of streamAgentChat({
      type: 'user_message',
      content: '你好',
      client_message_id: '018f6a08-7d4b-75d8-8f7a-7d4a2a4be001',
    }, 'old-access')) {
      events.push(event)
    }

    expect(refreshHandler).toHaveBeenCalledWith({ access_token: 'new-access', refresh_token: 'new-refresh' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[1][1].headers).toMatchObject({ 'Access-Token': 'new-access' })
    expect(events[0].content).toBe('你好')
  })
})

describe('agent history service', () => {
  it('listSessions builds pagination query and unwraps data', async () => {
    const payload = {
      code: 0,
      msg: 'ok',
      data: {
        total: 2,
        conversations: [
          { conversation_id: 'conv-1', title: '订单咨询', last_message_preview: '已送达', updated_at: '2026-08-20T15:06:37+08:00', message_count: 6 },
          { conversation_id: 'conv-2', title: '新会话', updated_at: '2026-08-20T11:00:00Z', message_count: 0 },
        ],
      },
    }
    const fetchMock = vi.fn(async () => new Response(JSON.stringify(payload), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listSessions('access-token', 2, 30)
    expect(result.total).toBe(2)
    expect(result.conversations[0].conversation_id).toBe('conv-1')
    expect(fetchMock).toHaveBeenCalledWith(
      '/douyin/ai/sessions?page=2&page_size=30',
      expect.objectContaining({
        headers: expect.objectContaining({ 'Access-Token': 'access-token' }),
      }),
    )
  })

  it('listMessages builds conversation query and passes through tool metadata', async () => {
    const payload = {
      code: 0,
      msg: 'ok',
      data: {
        conversation_id: 'conv-1',
        total: 1,
        messages: [{
          message_id: 'msg-3',
          role: 'tool',
          content: '查询结果',
          metadata: { tool_name: 'order_get', status: 'success', tool_call_id: 'call-1' },
          client_message_id: 'client-1',
          created_at: '2026-08-20T15:00:02+08:00',
        }],
      },
    }
    const fetchMock = vi.fn(async () => new Response(JSON.stringify(payload), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    const result = await listMessages('access-token', 'conv-1', 1, 50)
    expect(result.total).toBe(1)
    expect(result.messages[0].metadata?.tool_name).toBe('order_get')
    expect(fetchMock).toHaveBeenCalledWith(
      '/douyin/ai/sessions/messages?conversation_id=conv-1&page=1&page_size=50',
      expect.objectContaining({
        headers: expect.objectContaining({ 'Access-Token': 'access-token' }),
      }),
    )
  })

  it('listMessages encodes conversation id in the query string', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: { conversation_id: 'conv a/b', total: 0, messages: [] } }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await listMessages('access-token', 'conv a/b')
    expect(fetchMock).toHaveBeenCalledWith(
      '/douyin/ai/sessions/messages?conversation_id=conv%20a%2Fb&page=1&page_size=50',
      expect.anything(),
    )
  })
})
