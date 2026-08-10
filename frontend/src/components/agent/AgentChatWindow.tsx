import { useRef, useEffect, useState, useCallback } from 'react'
import { Bot, Search, ShoppingCart, Ticket, Package } from 'lucide-react'
import type { UIMessage } from '@/types'
import { AgentMessageBubble } from './AgentMessageBubble'

interface AgentChatWindowProps {
  messages: UIMessage[]
  isStreaming: boolean
  onSuggestionClick?: (text: string) => void
  onConfirmAction?: (conversationId: string, confirmationId: string, approved: boolean) => Promise<void> | void
}

const SUGGESTIONS = [
  { icon: <Search className="w-5 h-5 text-orange-400" />, text: '搜索蓝牙耳机' },
  { icon: <ShoppingCart className="w-5 h-5 text-orange-400" />, text: '查看购物车' },
  { icon: <Package className="w-5 h-5 text-orange-400" />, text: '查看我的订单' },
  { icon: <Ticket className="w-5 h-5 text-orange-400" />, text: '有什么优惠券' },
]

export function AgentChatWindow({ messages, isStreaming: _isStreaming, onSuggestionClick, onConfirmAction }: AgentChatWindowProps) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [nearBottom, setNearBottom] = useState(true)
  const [userScrolling, setUserScrolling] = useState(false)
  const prevLen = useRef(messages.length)

  const checkNearBottom = useCallback(() => {
    const el = scrollRef.current; if (!el) return true
    return el.scrollHeight - el.scrollTop - el.clientHeight < 100
  }, [])

  const handleScroll = useCallback(() => {
    if (!userScrolling) { setUserScrolling(true); setTimeout(() => setUserScrolling(false), 1000) }
    setNearBottom(checkNearBottom())
  }, [userScrolling, checkNearBottom])

  // Force scroll on new user message
  useEffect(() => {
    if (messages.length > prevLen.current && messages[messages.length - 1]?.type === 'user') {
      scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
    }
    prevLen.current = messages.length
  }, [messages])

  // Auto-scroll when near bottom
  useEffect(() => {
    if (nearBottom && !userScrolling) {
      scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
    }
  }, [messages, nearBottom, userScrolling])

  useEffect(() => {
    const el = scrollRef.current; if (!el) return
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [handleScroll])

  const isEmpty = messages.length === 0

  return (
    <div ref={scrollRef} className={`flex-1 overflow-y-auto px-4 md:px-6 py-4 bg-gradient-to-b from-gray-950 to-gray-900/50 ${isEmpty ? 'overflow-y-hidden' : ''}`}>
      {isEmpty ? (
        <EmptyState onSuggestionClick={onSuggestionClick} />
      ) : (
        <div className="max-w-3xl mx-auto w-full">
          {messages.map((msg, i) => (
            <AgentMessageBubble key={msg.id} message={msg} isLast={i === messages.length - 1} onConfirmAction={onConfirmAction} />
          ))}
        </div>
      )}
      <div className="h-4" />
    </div>
  )
}

function EmptyState({ onSuggestionClick }: { onSuggestionClick?: (text: string) => void }) {
  return (
    <div className="h-full flex flex-col items-center justify-center px-6 py-12">
      <div className="w-20 h-20 rounded-2xl bg-orange-500/10 border border-orange-500/20 flex items-center justify-center mb-6">
        <Bot className="w-10 h-10 text-orange-400" />
      </div>
      <h2 className="text-2xl font-bold text-gray-100 mb-2">go-mall AI 助手</h2>
      <p className="text-gray-400 text-center max-w-md mb-8">
        Hello，我是你的智能客服。我可以帮你搜索商品、管理购物车、查询订单、领取优惠券等。
      </p>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 max-w-lg w-full">
        {SUGGESTIONS.map((s, i) => (
          <button
            key={i}
            onClick={() => onSuggestionClick?.(s.text)}
            className="flex items-center gap-3 p-3 rounded-xl bg-gray-900/60 border border-gray-800 hover:border-orange-500/30 hover:bg-gray-900 transition-all text-left group"
          >
            <div className="p-2 rounded-lg bg-orange-500/10 group-hover:bg-orange-500/20 transition-colors">{s.icon}</div>
            <span className="text-sm text-gray-300">{s.text}</span>
          </button>
        ))}
      </div>
    </div>
  )
}
