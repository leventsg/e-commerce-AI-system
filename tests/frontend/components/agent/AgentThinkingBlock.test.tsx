import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AgentThinkingBlock } from '../../../../frontend/src/components/agent/AgentThinkingBlock'

describe('AgentThinkingBlock attribution', () => {
  it('does not guess an agent for shared query tools', () => {
    render(<AgentThinkingBlock isThinking trace={[{
      action: '查询订单', tool_name: 'order_get', status: 'running', iteration: 1, timestamp: '',
    }]} />)
    expect(screen.getByText('订单详情')).toBeTruthy()
    expect(screen.queryByText(/^via /)).toBeNull()
  })

  it('uses the reported agent for a shared query', () => {
    render(<AgentThinkingBlock isThinking trace={[{
      action: '查询订单', tool_name: 'order_get', status: 'running', iteration: 1, timestamp: '',
      subagent_name: 'after_sales_agent',
    }]} />)
    expect(screen.getByText('via 售后服务 Agent')).toBeTruthy()
  })

  it('identifies the unique owner of order creation when no agent is reported', () => {
    render(<AgentThinkingBlock isThinking trace={[{
      action: '创建订单', tool_name: 'order_create', status: 'running', iteration: 1, timestamp: '',
    }]} />)
    expect(screen.getByText('via 订单履约 Agent')).toBeTruthy()
  })
})
