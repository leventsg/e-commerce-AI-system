import { describe, expect, it } from 'vitest'
import type { TraceStep, UIMessage } from '../../../frontend/src/types'
import { applyAgentEvent, createClientMessageId, createConfirmActionPayload, finalizeStreamingMessages, serverConversationId } from '../../../frontend/src/hooks/useAgent'

describe('applyAgentEvent', () => {
  it('merges assistant deltas into one streaming assistant message', () => {
    const trace: TraceStep[] = []
    const first = applyAgentEvent([], trace, { type: 'assistant_delta', content: '你', done: false }, 1)
    const second = applyAgentEvent(first.messages, trace, { type: 'assistant_delta', content: '好', done: false }, 2, first.assistantMessageId)

    expect(second.messages).toHaveLength(1)
    expect(second.messages[0]).toMatchObject({ type: 'assistant', content: '你好', streaming: true })
  })

  it('keeps thinking deltas separate from final assistant content', () => {
    const trace: TraceStep[] = []
    const thinking = applyAgentEvent([], trace, { type: 'assistant_thinking_delta', conversation_id: 'conv-1', content: '我先分析一下', done: false }, 1, '', 'conv-1')
    const delta = applyAgentEvent(thinking.messages, trace, { type: 'assistant_delta', content: '最终', done: false }, 2, thinking.assistantMessageId)
    const final = applyAgentEvent(delta.messages, trace, { type: 'assistant_message', content: '最终回答', done: true }, 3, delta.assistantMessageId)

    expect(final.messages).toHaveLength(2)
    expect(final.messages[0]).toMatchObject({ type: 'thinking', content: '我先分析一下', streaming: false })
    expect(final.messages[1]).toMatchObject({ type: 'assistant', content: '最终回答', streaming: false })
  })

  it('merges message-id-less thinking deltas within the same conversation', () => {
    const trace: TraceStep[] = []
    const first = applyAgentEvent([], trace, {
      type: 'assistant_thinking_delta',
      conversation_id: 'conv-a',
      content: '先查库存',
      done: false,
    }, 1, '', 'conv-a')
    const second = applyAgentEvent(first.messages, trace, {
      type: 'assistant_thinking_delta',
      conversation_id: 'conv-a',
      content: '，再看价格',
      done: false,
    }, 2, first.assistantMessageId, 'conv-a')

    expect(second.messages).toHaveLength(1)
    expect(second.messages[0]).toMatchObject({
      type: 'thinking',
      conversationId: 'conv-a',
      content: '先查库存，再看价格',
      streaming: true,
    })
  })

  it('does not merge thinking deltas across conversations', () => {
    const trace: TraceStep[] = []
    const convA = applyAgentEvent([{
      id: 'thinking_conv-a_1',
      type: 'thinking',
      conversationId: 'conv-a',
      content: 'A 正在分析',
      timestamp: 1,
      streaming: true,
    }], trace, {
      type: 'assistant_thinking_delta',
      conversation_id: 'conv-a',
      content: ' A 后续',
      done: false,
    }, 2, '', 'conv-a')
    const convB = applyAgentEvent([{
      id: 'thinking_conv-b_1',
      type: 'thinking',
      conversationId: 'conv-b',
      content: 'B 正在分析',
      timestamp: 1,
      streaming: true,
    }], trace, {
      type: 'assistant_thinking_delta',
      conversation_id: 'conv-a',
      content: ' A 后续',
      done: false,
    }, 2, '', 'conv-b')

    expect(convA.messages[0]).toMatchObject({ conversationId: 'conv-a', content: 'A 正在分析 A 后续' })
    expect(convB.messages[0]).toMatchObject({ conversationId: 'conv-b', content: 'B 正在分析' })
    expect(convB.messages).toHaveLength(1)
  })

  it('closes only the current conversation thinking message when assistant delta starts', () => {
    const trace: TraceStep[] = []
    const result = applyAgentEvent([{
      id: 'thinking_conv-a_1',
      type: 'thinking',
      conversationId: 'conv-a',
      content: 'A thinking',
      timestamp: 1,
      streaming: true,
    }, {
      id: 'thinking_conv-b_1',
      type: 'thinking',
      conversationId: 'conv-b',
      content: 'B thinking',
      timestamp: 1,
      streaming: true,
    }], trace, { type: 'assistant_delta', conversation_id: 'conv-a', content: '最终', done: false }, 2, '', 'conv-a')

    expect(result.messages.find(message => message.conversationId === 'conv-a')?.streaming).toBe(false)
    expect(result.messages.find(message => message.conversationId === 'conv-b')?.streaming).toBe(true)
    expect(result.messages.find(message => message.type === 'assistant')?.content).toBe('最终')
  })

  it('clears assistant streaming marker when a done event arrives', () => {
    const trace: TraceStep[] = []
    const delta = applyAgentEvent([], trace, { type: 'assistant_delta', content: '正在查询', done: false }, 1)
    const done = applyAgentEvent(delta.messages, trace, {
      type: 'tool_result',
      message_id: 'msg_tool_1',
      tool: 'order_list',
      status: 'success',
      summary: '查询完成',
      data: { total: 1 },
      done: true,
    }, 2, delta.assistantMessageId)

    expect(done.messages.find(message => message.id === delta.assistantMessageId)?.streaming).toBe(false)
  })

  it('finalizes assistant streaming marker when the SSE stream closes without a final message event', () => {
    const trace: TraceStep[] = []
    const delta = applyAgentEvent([], trace, { type: 'assistant_delta', content: '处理中', done: false }, 1)
    const finalized = finalizeStreamingMessages(delta.messages, delta.assistantMessageId)

    expect(finalized.find(message => message.id === delta.assistantMessageId)?.streaming).toBe(false)
  })

  it('marks a running tool as failed when tool_result status is not success', () => {
    const trace: TraceStep[] = []
    const progress = applyAgentEvent([], trace, {
      type: 'tool_progress',
      message_id: 'msg_tool_1',
      tool: 'order_list',
      content: '正在查询订单',
      done: false,
    }, 1)
    const result = applyAgentEvent(progress.messages, trace, {
      type: 'tool_result',
      message_id: 'msg_tool_1',
      tool: 'order_list',
      status: 'failed',
      summary: '查询失败',
      data: { reason: 'rpc timeout' },
      done: false,
    }, 2)

    const toolMessage = result.messages[0]
    expect(result.messages).toHaveLength(1)
    expect(toolMessage.toolCallId).toBe('msg_tool_1')
    expect(toolMessage.toolStatus).toBe('failed')
    expect(toolMessage.dataJson).toBe(JSON.stringify({ reason: 'rpc timeout' }))
  })

  it('creates confirmation messages from confirmation_required events', () => {
    const trace: TraceStep[] = []
    const result = applyAgentEvent([] as UIMessage[], trace, {
      type: 'confirmation_required',
      conversation_id: 'conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001',
      confirmation_id: 'confirm_1',
      content: '确认取消订单？',
      expires_at: 1719730000,
      done: true,
    }, 1)

    expect(result.messages[0]).toMatchObject({
      type: 'confirmation',
      conversationId: 'conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001',
      confirmationId: 'confirm_1',
      expiresAt: 1719730000,
    })
  })

  it('omits local-only conversation ids from new user message payloads', () => {
    expect(serverConversationId('local_conv_1786351344009_2')).toBeUndefined()
    expect(serverConversationId('conv_1786351344009_2')).toBeUndefined()
    expect(serverConversationId('conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001')).toBe('conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001')
  })

  it('generates UUIDv7 client message ids', () => {
    const id = createClientMessageId()

    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(id.startsWith('client_msg_')).toBe(false)
  })

  it('builds confirm_action payload from backend conversation and confirmation ids', () => {
    expect(createConfirmActionPayload('conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001', 'confirm_1', true)).toEqual({
      type: 'confirm_action',
      conversation_id: 'conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001',
      confirmation_id: 'confirm_1',
      approved: true,
    })
    expect(createConfirmActionPayload('local_conv_1', 'confirm_1', false)).toBeNull()
  })
})
