import { describe, expect, it, vi } from 'vitest'
import { streamAgentChat } from '../../../../frontend/src/services/api/agent'
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
