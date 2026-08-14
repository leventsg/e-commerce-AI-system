import { useState, useRef, useEffect, type KeyboardEvent } from 'react'
import { SendHorizontal, Square } from 'lucide-react'

interface AgentChatInputProps {
  onSend: (message: string) => void
  onCancel?: () => void
  isStreaming?: boolean
  placeholder?: string
}

export function AgentChatInput({ onSend, onCancel, isStreaming = false, placeholder = '输入消息... (Enter 发送, Shift+Enter 换行)' }: AgentChatInputProps) {
  const [input, setInput] = useState('')
  const [isComposing, setIsComposing] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const handleSend = () => {
    if (!input.trim() || isStreaming) return
    onSend(input.trim())
    setInput('')
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey && !isComposing) { e.preventDefault(); handleSend() }
  }

  useEffect(() => { textareaRef.current?.focus() }, [])

  return (
    <div className="shrink-0 border-t border-gray-800 px-4 py-3 bg-gray-950">
      <div className="max-w-3xl mx-auto flex items-end gap-2 bg-gray-900 border border-gray-800 rounded-2xl px-4 py-2.5 focus-within:border-orange-500/30 transition-colors">
        <textarea
          ref={textareaRef}
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          onCompositionStart={() => setIsComposing(true)}
          onCompositionEnd={() => setIsComposing(false)}
          placeholder={placeholder}
          rows={1}
          className="flex-1 bg-transparent text-sm text-gray-200 placeholder-gray-500 resize-none outline-none max-h-32 py-0.5"
        />
        {isStreaming ? (
          <button onClick={onCancel} className="w-8 h-8 rounded-xl bg-red-500/15 hover:bg-red-500/25 border border-red-500/20 flex items-center justify-center shrink-0 transition-colors">
            <Square className="w-4 h-4 text-red-400" />
          </button>
        ) : (
          <button onClick={handleSend} disabled={!input.trim()} className="w-8 h-8 rounded-xl bg-orange-500/15 hover:bg-orange-500/25 border border-orange-500/20 flex items-center justify-center shrink-0 transition-colors disabled:opacity-40">
            <SendHorizontal className="w-4 h-4 text-orange-400" />
          </button>
        )}
      </div>
      <p className="text-[10px] text-gray-600 text-center mt-2">AI 生成内容仅供参考 · 高风险操作需二次确认</p>
    </div>
  )
}
