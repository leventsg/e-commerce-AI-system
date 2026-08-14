import { describe, expect, it } from 'vitest'
import type { UIMessage } from '../../../../frontend/src/types'
import { buildChatRenderItems } from '../../../../frontend/src/components/agent/chatRenderItems'

function message(overrides: Partial<UIMessage>): UIMessage {
  return {
    id: overrides.id || `msg_${overrides.timestamp || 1}`,
    type: overrides.type || 'assistant',
    content: overrides.content || '',
    timestamp: overrides.timestamp || 1,
    ...overrides,
  } as UIMessage
}

describe('buildChatRenderItems', () => {
  it('groups assistant-side messages after each user message', () => {
    const items = buildChatRenderItems([
      message({ id: 'user_1', type: 'user', content: '查订单', timestamp: 1 }),
      message({ id: 'thinking_1', type: 'thinking', content: '先理解用户问题', timestamp: 2 }),
      message({ id: 'tool_1', type: 'tool-result', content: '订单查询完成', timestamp: 3, toolName: 'order_list', toolStatus: 'success' }),
      message({ id: 'confirm_1', type: 'confirmation', content: '确认取消订单？', timestamp: 4, conversationId: 'conv_1', confirmationId: 'cfm_1' }),
      message({ id: 'ai_1', type: 'assistant', content: '这是你的订单', timestamp: 5 }),
      message({ id: 'user_2', type: 'user', content: '谢谢', timestamp: 6 }),
      message({ id: 'ai_2', type: 'assistant', content: '不客气', timestamp: 7 }),
    ])

    expect(items).toHaveLength(4)
    expect(items[0]).toMatchObject({ kind: 'user', message: { id: 'user_1' } })
    expect(items[1]).toMatchObject({ kind: 'assistant-group' })
    expect(items[1].kind === 'assistant-group' ? items[1].messages.map(item => item.id) : []).toEqual(['thinking_1', 'tool_1', 'confirm_1', 'ai_1'])
    expect(items[2]).toMatchObject({ kind: 'user', message: { id: 'user_2' } })
    expect(items[3].kind === 'assistant-group' ? items[3].messages.map(item => item.id) : []).toEqual(['ai_2'])
  })

  it('keeps final assistant output after thinking, tools, and confirmations inside a group', () => {
    const items = buildChatRenderItems([
      message({ id: 'user_1', type: 'user', content: '取消订单', timestamp: 1 }),
      message({ id: 'ai_1', type: 'assistant', content: '最终回答', timestamp: 8 }),
      message({ id: 'tool_old', type: 'tool-result', content: '先执行', timestamp: 4, toolName: 'order_cancel', toolStatus: 'running', toolCallId: 'call_1' }),
      message({ id: 'thinking_2', type: 'thinking', content: '再检查状态', timestamp: 3 }),
      message({ id: 'confirm_1', type: 'confirmation', content: '确认取消？', timestamp: 6, conversationId: 'conv_1', confirmationId: 'cfm_1' }),
      message({ id: 'thinking_1', type: 'thinking', content: '判断风险', timestamp: 2 }),
      message({ id: 'tool_new', type: 'tool-result', content: '执行完成', timestamp: 5, toolName: 'order_cancel', toolStatus: 'success', toolCallId: 'call_1' }),
      message({ id: 'confirm_2', type: 'confirmation', content: '再次确认？', timestamp: 7, conversationId: 'conv_1', confirmationId: 'cfm_2' }),
    ])

    const group = items.find(item => item.kind === 'assistant-group')
    expect(group?.kind).toBe('assistant-group')
    if (group?.kind !== 'assistant-group') return

    expect(group.thinkingText).toBe('判断风险\n再检查状态')
    expect(group.toolMessages.map(item => item.id)).toEqual(['tool_new'])
    expect(group.confirmationMessages.map(item => item.confirmationId)).toEqual(['cfm_1', 'cfm_2'])
    expect(group.assistantMessages.map(item => item.id)).toEqual(['ai_1'])
    expect(group.isStreaming).toBe(false)
  })

  it('marks a group as streaming when any grouped message is streaming or running', () => {
    const items = buildChatRenderItems([
      message({ id: 'user_1', type: 'user', content: '查购物车', timestamp: 1 }),
      message({ id: 'thinking_1', type: 'thinking', content: '分析购物车意图', timestamp: 2, streaming: true }),
      message({ id: 'tool_1', type: 'tool-result', content: '执行中', timestamp: 3, toolName: 'cart_list', toolStatus: 'running' }),
    ])

    const group = items[1]
    expect(group.kind).toBe('assistant-group')
    expect(group.kind === 'assistant-group' ? group.isStreaming : false).toBe(true)
  })
})
