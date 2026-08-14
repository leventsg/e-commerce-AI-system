import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { UIMessage } from '../../../../frontend/src/types'
import { AgentChatWindow } from '../../../../frontend/src/components/agent/AgentChatWindow'

function message(overrides: Partial<UIMessage>): UIMessage {
  return {
    id: overrides.id || `msg_${overrides.timestamp || 1}`,
    type: overrides.type || 'assistant',
    content: overrides.content || '',
    timestamp: overrides.timestamp || 1,
    ...overrides,
  } as UIMessage
}

describe('AgentChatWindow', () => {
  it('renders one assistant group with thinking, tools, confirmations, then final output', async () => {
    const onConfirmAction = vi.fn()
    const user = userEvent.setup()
    render(
      <AgentChatWindow
        isStreaming={false}
        onConfirmAction={onConfirmAction}
        messages={[
          message({ id: 'user_1', type: 'user', content: '帮我取消订单', timestamp: 1 }),
          message({ id: 'thinking_1', type: 'thinking', content: '识别取消订单意图', timestamp: 2 }),
          message({ id: 'thinking_2', type: 'thinking', content: '检查是否高风险', timestamp: 3 }),
          message({ id: 'tool_1', type: 'tool-result', content: '订单可取消', timestamp: 4, toolName: 'order_cancel', toolStatus: 'failed', dataJson: '{"reason":"需要确认"}' }),
          message({ id: 'confirm_1', type: 'confirmation', content: '确认取消订单？', timestamp: 5, conversationId: 'conv_1', confirmationId: 'confirm_1', confirmationStatus: 'pending', expiresAt: Math.floor(Date.now() / 1000) + 60 }),
          message({ id: 'ai_1', type: 'assistant', content: '请确认后我再继续处理。', timestamp: 6 }),
        ]}
      />,
    )

    const group = screen.getByTestId('assistant-response-group')
    expect(within(group).getAllByTestId('assistant-avatar')).toHaveLength(1)

    const thinking = within(group).getByText('思考过程')
    const tools = within(group).getByText('工具调用执行过程')
    const confirmations = within(group).getByText('工具执行确认')
    const finalAnswer = within(group).getByText('请确认后我再继续处理。')

    expect(thinking.compareDocumentPosition(tools) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(tools.compareDocumentPosition(confirmations) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(confirmations.compareDocumentPosition(finalAnswer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    await user.click(within(group).getByRole('button', { name: /思考过程/ }))
    await user.click(within(group).getByRole('button', { name: /工具调用执行过程/ }))
    expect(within(group).getByText(/识别取消订单意图/)).toBeTruthy()
    expect(within(group).getByText(/检查是否高风险/)).toBeTruthy()
    expect(within(group).getByText('订单可取消')).toBeTruthy()

    await user.click(within(group).getByRole('button', { name: /确认执行/ }))
    expect(onConfirmAction).toHaveBeenCalledWith('conv_1', 'confirm_1', true)
  })

  it('keeps status panels open while the assistant response is still streaming', () => {
    render(
      <AgentChatWindow
        isStreaming
        messages={[
          message({ id: 'user_1', type: 'user', content: '帮我查订单并处理', timestamp: 1 }),
          message({ id: 'thinking_1', type: 'thinking', content: '先分析订单状态', timestamp: 2, streaming: false }),
          message({ id: 'tool_1', type: 'tool-result', content: '订单查询完成', timestamp: 3, toolName: 'order_list', toolStatus: 'success' }),
          message({ id: 'confirm_1', type: 'confirmation', content: '确认继续处理？', timestamp: 4, conversationId: 'conv_1', confirmationId: 'confirm_1', confirmationStatus: 'approved' }),
          message({ id: 'ai_1', type: 'assistant', content: '我继续为你处理', timestamp: 5, streaming: true }),
        ]}
      />,
    )

    const group = screen.getByTestId('assistant-response-group')
    expect(within(group).queryAllByText('已完成')).toHaveLength(0)
    expect(within(group).getByText('先分析订单状态')).toBeTruthy()
    expect(within(group).getByText('订单查询完成')).toBeTruthy()
    expect(within(group).getByText('确认继续处理？')).toBeTruthy()
  })
})
