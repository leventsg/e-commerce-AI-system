import { describe, expect, it } from 'vitest'
import type { UIMessage } from '../../../frontend/src/types'
import {
  conversationSummaryFromDTO,
  historyMessageToUIMessage,
  mergeHistoryMessages,
} from '../../../frontend/src/hooks/useAgent'

describe('conversationSummaryFromDTO', () => {
  it('maps conversation_id to id and keeps summary fields', () => {
    expect(conversationSummaryFromDTO({
      conversation_id: 'conv-1',
      title: '订单咨询',
      last_message_preview: '已送达',
      updated_at: '2026-08-20T15:06:37+08:00',
      message_count: 6,
    })).toEqual({
      id: 'conv-1',
      title: '订单咨询',
      last_message_preview: '已送达',
      updated_at: '2026-08-20T15:06:37+08:00',
      message_count: 6,
    })
  })
})

describe('historyMessageToUIMessage', () => {
  it('maps user role to a user message', () => {
    const message = historyMessageToUIMessage({
      message_id: 'msg-1',
      role: 'user',
      content: '查一下订单',
      created_at: '2026-08-20T15:00:00+08:00',
    })
    expect(message).toMatchObject({ id: 'history_msg-1', type: 'user', content: '查一下订单' })
    expect(message.timestamp).toBe(Date.parse('2026-08-20T15:00:00+08:00'))
  })

  it('maps assistant role and restores RAG sources from metadata', () => {
    const message = historyMessageToUIMessage({
      message_id: 'msg-2',
      role: 'assistant',
      content: '已找到订单',
      metadata: { sources: [{ document_id: 'doc-1', title: '帮助文档', chunks: [] }] },
      created_at: '2026-08-20T15:00:01+08:00',
    })
    expect(message).toMatchObject({
      id: 'msg-2',
      type: 'assistant',
      sources: [{ document_id: 'doc-1', title: '帮助文档', chunks: [] }],
    })
  })

  it('maps tool role with tool name, status and data_json from metadata', () => {
    const message = historyMessageToUIMessage({
      message_id: 'msg-3',
      role: 'tool',
      content: '查询结果',
      metadata: {
        tool_name: 'order_get',
        status: 'success',
        tool_call_id: 'call-1',
        data_json: '{"total":1}',
      },
      client_message_id: 'client-1',
      created_at: '2026-08-20T15:00:02+08:00',
    })
    expect(message).toMatchObject({
      id: 'tool_call-1',
      type: 'tool-result',
      content: '查询结果',
      toolName: 'order_get',
      toolStatus: 'success',
      toolCallId: 'call-1',
      dataJson: '{"total":1}',
    })
  })
})

describe('mergeHistoryMessages', () => {
  it('dedupes by id and keeps chronological order', () => {
    const existing: UIMessage[] = [
      { id: 'msg-1', type: 'user', content: '你好', timestamp: 1 },
      { id: 'msg-2', type: 'assistant', content: '你好，有什么可以帮你', timestamp: 2 },
    ]
    const incoming: UIMessage[] = [
      { id: 'msg-2', type: 'assistant', content: '你好，有什么可以帮你', timestamp: 2 },
      { id: 'msg-3', type: 'user', content: '查订单', timestamp: 3 },
    ]
    const merged = mergeHistoryMessages(existing, incoming)
    expect(merged.map(m => m.id)).toEqual(['msg-1', 'msg-2', 'msg-3'])
  })

  it('prepends older pages before existing messages', () => {
    const existing: UIMessage[] = [
      { id: 'msg-51', type: 'user', content: '新问题', timestamp: 100 },
    ]
    const older: UIMessage[] = [
      { id: 'msg-1', type: 'user', content: '第一个问题', timestamp: 1 },
      { id: 'msg-2', type: 'assistant', content: '第一个回答', timestamp: 2 },
    ]
    const merged = mergeHistoryMessages(existing, older)
    expect(merged.map(m => m.id)).toEqual(['msg-1', 'msg-2', 'msg-51'])
  })

  it('skips duplicate user messages by content', () => {
    const existing: UIMessage[] = [
      { id: 'history_msg-1', type: 'user', content: '查订单', timestamp: 1 },
    ]
    const incoming: UIMessage[] = [
      { id: 'msg-1', type: 'user', content: '查订单', timestamp: 1 },
    ]
    expect(mergeHistoryMessages(existing, incoming)).toHaveLength(1)
  })

  it('returns existing messages unchanged when incoming is empty', () => {
    const existing: UIMessage[] = [{ id: 'msg-1', type: 'assistant', content: 'ok', timestamp: 1 }]
    expect(mergeHistoryMessages(existing, [])).toEqual(existing)
  })
})
