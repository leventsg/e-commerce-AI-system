import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ConfirmationCard } from '../../../../frontend/src/components/agent/ConfirmationCard'
import type { UIMessage } from '../../../../frontend/src/types'

function confirmationMessage(overrides: Partial<UIMessage> = {}): UIMessage {
  return {
    id: 'confirm_msg',
    type: 'confirmation',
    content: '确认取消订单？',
    timestamp: Date.now(),
    conversationId: 'conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001',
    confirmationId: 'confirm_1',
    expiresAt: Math.floor(Date.now() / 1000) + 60,
    confirmationStatus: 'pending',
    ...overrides,
  }
}

describe('ConfirmationCard', () => {
  it('calls confirm and reject callbacks with the bound conversation and confirmation ids', async () => {
    const user = userEvent.setup()
    const onConfirm = vi.fn()
    const onReject = vi.fn()

    render(<ConfirmationCard message={confirmationMessage()} onConfirm={onConfirm} onReject={onReject} />)

    await user.click(screen.getByText('确认执行'))
    expect(onConfirm).toHaveBeenCalledWith('conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001', 'confirm_1')

    await user.click(screen.getByText('取消'))
    expect(onReject).toHaveBeenCalledWith('conv_018f6a08-7d4b-75d8-8f7a-7d4a2a4be001', 'confirm_1')
  })

  it('disables action buttons after expiration', () => {
    render(<ConfirmationCard message={confirmationMessage({ expiresAt: Math.floor(Date.now() / 1000) - 1 })} />)

    expect((screen.getByText('确认执行').closest('button') as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByText('取消').closest('button') as HTMLButtonElement).disabled).toBe(true)
  })
})
