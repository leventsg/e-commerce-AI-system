import { useRef, useEffect, useState, useCallback } from 'react'
import { Bot, FileText, Search, ShoppingCart, Ticket, Package, X } from 'lucide-react'
import type { RAGSource, UIMessage } from '@/types'
import { AgentMessageBubble } from './AgentMessageBubble'
import { AssistantResponseGroup } from './AssistantResponseGroup'
import { buildChatRenderItems } from './chatRenderItems'

interface AgentChatWindowProps {
  messages: UIMessage[]
  isStreaming: boolean
  onSuggestionClick?: (text: string) => void
  onConfirmAction?: (conversationId: string, confirmationId: string, approved: boolean) => Promise<void> | void
  hasOlderMessages?: boolean
  loadingHistory?: boolean
  onLoadOlder?: () => void
}

const SUGGESTIONS = [
  { icon: <Search className="w-5 h-5 text-orange-400" />, text: '搜索蓝牙耳机' },
  { icon: <ShoppingCart className="w-5 h-5 text-orange-400" />, text: '查看购物车' },
  { icon: <Package className="w-5 h-5 text-orange-400" />, text: '查看我的订单' },
  { icon: <Ticket className="w-5 h-5 text-orange-400" />, text: '有什么优惠券' },
]

function scrollToBottom(el: HTMLDivElement | null) {
  if (!el || typeof el.scrollTo !== 'function') return
  el.scrollTo({ top: el.scrollHeight, behavior: 'smooth' })
}

export function AgentChatWindow({
  messages,
  isStreaming,
  onSuggestionClick,
  onConfirmAction,
  hasOlderMessages = false,
  loadingHistory = false,
  onLoadOlder,
}: AgentChatWindowProps) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [nearBottom, setNearBottom] = useState(true)
  const [userScrolling, setUserScrolling] = useState(false)
  const [sourcePanel, setSourcePanel] = useState<RAGSource[] | null>(null)
  const prevLen = useRef(messages.length)
  const prevScrollHeightRef = useRef(0)
  const wasLoadingHistoryRef = useRef(false)

  const checkNearBottom = useCallback(() => {
    const el = scrollRef.current; if (!el) return true
    return el.scrollHeight - el.scrollTop - el.clientHeight < 100
  }, [])

  const handleScroll = useCallback(() => {
    if (!userScrolling) { setUserScrolling(true); setTimeout(() => setUserScrolling(false), 1000) }
    setNearBottom(checkNearBottom())
    const el = scrollRef.current
    if (el && el.scrollTop < 80 && hasOlderMessages && !loadingHistory && !isStreaming) {
      onLoadOlder?.()
    }
  }, [userScrolling, checkNearBottom, hasOlderMessages, loadingHistory, isStreaming, onLoadOlder])

  // Force scroll on new user message
  useEffect(() => {
    if (messages.length > prevLen.current && messages[messages.length - 1]?.type === 'user') {
      scrollToBottom(scrollRef.current)
    }
    prevLen.current = messages.length
  }, [messages])

  // Auto-scroll when near bottom
  useEffect(() => {
    if (nearBottom && !userScrolling) {
      scrollToBottom(scrollRef.current)
    }
  }, [messages, nearBottom, userScrolling])

  // 记录加载历史前的滚动高度，加载完成后恢复滚动位置，避免内容跳动
  useEffect(() => {
    if (loadingHistory) {
      prevScrollHeightRef.current = scrollRef.current?.scrollHeight || 0
      wasLoadingHistoryRef.current = true
    }
  }, [loadingHistory])

  useEffect(() => {
    const el = scrollRef.current
    if (el && wasLoadingHistoryRef.current && !loadingHistory) {
      const diff = el.scrollHeight - prevScrollHeightRef.current
      if (diff > 0) {
        el.scrollTop += diff
      }
      wasLoadingHistoryRef.current = false
    }
  }, [messages, loadingHistory])

  useEffect(() => {
    const el = scrollRef.current; if (!el) return
    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [handleScroll])

  const isEmpty = messages.length === 0
  const renderItems = buildChatRenderItems(messages)

  return (
    <div ref={scrollRef} className={`flex-1 overflow-y-auto px-4 md:px-6 py-4 bg-gradient-to-b from-gray-950 to-gray-900/50 ${isEmpty ? 'overflow-y-hidden' : ''}`}>
      {isEmpty ? (
        <EmptyState onSuggestionClick={onSuggestionClick} />
      ) : (
        <div className="max-w-3xl mx-auto w-full">
          {loadingHistory && (
            <div className="py-2 text-center text-xs text-gray-600">加载历史消息中...</div>
          )}
          {renderItems.map((item, i) => (
            item.kind === 'user'
              ? <AgentMessageBubble key={item.message.id} message={item.message} isLast={i === renderItems.length - 1} onConfirmAction={onConfirmAction} />
              : <AssistantResponseGroup key={item.id} item={item} onConfirmAction={onConfirmAction} onOpenSources={setSourcePanel} />
          ))}
        </div>
      )}
      <div className="h-4" />
      {sourcePanel && sourcePanel.length > 0 && (
        <SourcePanel sources={sourcePanel} onClose={() => setSourcePanel(null)} />
      )}
    </div>
  )
}

function SourcePanel({ sources, onClose }: { sources: RAGSource[]; onClose: () => void }) {
  return (
    <div className="fixed inset-y-0 right-0 z-50 w-[360px] max-w-full border-l border-gray-800 bg-gray-950/95 backdrop-blur shadow-2xl flex flex-col">
      <div className="flex items-center gap-2 border-b border-gray-800 px-4 py-3">
        <FileText className="w-4 h-4 text-orange-400 shrink-0" />
        <span className="text-sm font-medium text-gray-200">参考来源（{sources.length}篇）</span>
        <button
          type="button"
          onClick={onClose}
          className="ml-auto p-1 rounded-lg text-gray-400 hover:text-gray-200 hover:bg-gray-800 transition-colors"
          aria-label="关闭参考来源"
        >
          <X className="w-4 h-4" />
        </button>
      </div>
      <div className="flex-1 overflow-y-auto px-4 py-3 space-y-4">
        {sources.map(source => (
          <div key={source.document_id} className="rounded-xl border border-gray-800 bg-gray-900/60 p-3">
            <div className="text-xs font-medium text-gray-300 mb-2">{source.title}</div>
            <div className="space-y-2">
              {source.chunks.map(chunk => (
                <button
                  key={chunk.chunk_id}
                  type="button"
                  onClick={() => {
                    if (source.document_url) window.open(source.document_url, '_blank')
                  }}
                  className="block w-full text-left rounded-lg border border-gray-800 bg-gray-950/60 px-3 py-2 text-xs text-gray-400 hover:border-orange-500/30 hover:text-gray-300 transition-colors"
                >
                  <span className="line-clamp-4 whitespace-pre-wrap break-words leading-relaxed">{chunk.content}</span>
                  <span className="mt-1 block text-[10px] text-orange-400/80">查看完整文档 →</span>
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
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
