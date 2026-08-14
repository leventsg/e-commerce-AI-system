import type { AgentEvent, AgentChatRequest } from '@/types'
import { TOOL_DISPLAY_NAMES, HIGH_RISK_TOOLS } from '@/constants'

let mockCounter = 0
function mid(prefix: string) { mockCounter++; return `${prefix}_${Date.now()}_${mockCounter}` }

/**
 * 模拟 SSE 流式响应 — 接入真实后端时替换为 fetch + ReadableStream。
 * 完全按照后端 AgentEvent 字段生成。
 */
export async function* mockStreamAgentChat(req: AgentChatRequest): AsyncGenerator<AgentEvent> {
  const { message } = req
  const convId = req.conversation_id || mid('conv')

  // 判断用户意图 → 决定调用哪些工具
  const toolSteps: { tool: string; params: Record<string, unknown>; result: Record<string, unknown>; summary: string; agent: string }[] = []

  if (message.includes('订单')) {
    toolSteps.push({
      tool: 'order_list', params: { page: 1, page_size: 10 }, agent: 'order_agent',
      result: { total: 3, orders: [{ order_id: 'ord_001', status: '待支付', amount: 29900, items: 2 }, { order_id: 'ord_002', status: '已完成', amount: 89900, items: 1 }] },
      summary: '找到 3 个订单',
    })
  } else if (message.includes('购物车')) {
    toolSteps.push({
      tool: 'cart_list', params: { page: 1, page_size: 20 }, agent: 'cart_checkout_agent',
      result: { total: 2, items: [{ cart_item_id: 1, product_id: 12, product_name: '无线耳机', quantity: 2, price: 29900 }] },
      summary: '购物车中有 2 件商品',
    })
  } else if (message.includes('优惠券')) {
    toolSteps.push({
      tool: 'coupon_list', params: { page: 1, page_size: 10 }, agent: 'coupon_agent',
      result: { total: 4, coupons: [{ id: 'cpn_001', name: '满199减50', type: '满减', min_amount: 19900, value: 5000 }] },
      summary: '找到 4 张可用优惠券',
    })
  } else {
    toolSteps.push({
      tool: 'product_search', params: { keyword: message, page: 1, page_size: 10 }, agent: 'product_agent',
      result: { total: 5, items: [{ id: 1, name: '无线蓝牙耳机', price: 29900, stock: 128 }, { id: 2, name: '智能手表', price: 89900, stock: 56 }] },
      summary: '找到 5 件相关商品',
    })
  }

  const isHighRisk = HIGH_RISK_TOOLS.some(t => message.includes(t)) || message.includes('取消') || message.includes('删除订单')

  // 逐个执行工具
  for (let i = 0; i < toolSteps.length; i++) {
    const step = toolSteps[i]
    const callId = mid('call')

    // tool_progress
    yield {
      type: 'tool_progress', conversation_id: convId, message_id: mid('msg'),
      tool_call_id: callId, tool: step.tool, content: `正在调用 ${TOOL_DISPLAY_NAMES[step.tool] || step.tool}...`, done: false,
    }

    // 模拟延迟
    await new Promise(r => setTimeout(r, 300 + Math.random() * 800))

    // tool_result
    yield {
      type: 'tool_result', conversation_id: convId, message_id: mid('msg'),
      tool_call_id: callId, tool: step.tool, status: 'success',
      content: step.summary, summary: step.summary,
      data_json: JSON.stringify(step.result), done: false,
    }
  }

  // 高风险确认
  if (isHighRisk) {
    yield {
      type: 'confirmation_required', conversation_id: convId, message_id: mid('msg'),
      confirmation_id: mid('cfm'), content: `确认执行：${message}？此操作不可撤销。`,
      expires_at: Math.floor(Date.now() / 1000) + 300, done: false,
    }
  }

  // 流式 AI 回复
  const response = isHighRisk
    ? '已收到您的请求。由于这是高风险操作，需要您确认后才能执行。请查看上方的确认卡片，点击"确认"继续。'
    : toolSteps.length === 0
      ? '你好！我是 go-mall AI 助手。我可以帮你搜索商品、管理购物车、查询订单、查看优惠券等。请问有什么可以帮你的？'
      : `根据查询结果，${toolSteps.map(s => s.summary).join('；')}。如需查看详情或进行其他操作，请随时告诉我。`

  const words = response.split('')
  for (let i = 0; i < words.length; i += 2) {
    const chunk = words.slice(i, i + 2).join('')
    yield {
      type: 'assistant_delta', conversation_id: convId, message_id: mid('ai'),
      content: chunk, done: false,
    }
    await new Promise(r => setTimeout(r, 20))
  }

  // 最终消息
  yield {
    type: 'assistant_message', conversation_id: convId, message_id: mid('ai'),
    content: response, done: true,
  }
}
