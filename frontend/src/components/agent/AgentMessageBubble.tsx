import { useState } from 'react'
import { Bot, User, Loader2, CheckCircle2, XCircle, AlertTriangle, ChevronDown, ChevronUp, Cpu } from 'lucide-react'
import type { UIMessage } from '@/types'
import { TOOL_DISPLAY_NAMES, TOOL_ICONS } from '@/constants'
import { MarkdownRenderer, CopyButton } from '@/components/common'
import { AgentThinkingBlock } from './AgentThinkingBlock'
import { ConfirmationCard } from './ConfirmationCard'

interface AgentMessageBubbleProps {
  message: UIMessage
  isLast?: boolean
  onConfirmAction?: (conversationId: string, confirmationId: string, approved: boolean) => Promise<void> | void
}

export function AgentMessageBubble({ message, onConfirmAction }: AgentMessageBubbleProps) {
  switch (message.type) {
    case 'user': return <UserBubble message={message} />
    case 'assistant': return <AssistantBubble message={message} />
    case 'thinking': return <ThinkingBubble message={message} />
    case 'tool-result': return <ToolBubble message={message} />
    case 'confirmation': return <ConfirmationCard message={message} onConfirm={(conversationId, id) => onConfirmAction?.(conversationId, id, true)} onReject={(conversationId, id) => onConfirmAction?.(conversationId, id, false)} />
    case 'error': return <ErrorBubble message={message} />
    default: return null
  }
}

function UserBubble({ message }: { message: UIMessage }) {
  return (
    <div className="flex justify-end animate-slide-up mb-6">
      <div className="flex items-start gap-3 max-w-[75%] flex-row-reverse">
        <div className="w-8 h-8 rounded-lg bg-orange-500/15 flex items-center justify-center shrink-0 mt-0.5">
          <User className="w-4 h-4 text-orange-400" />
        </div>
        <div className="bg-orange-500/8 border border-orange-500/15 rounded-2xl rounded-tr-md px-4 py-2.5">
          <p className="text-sm text-gray-200 whitespace-pre-wrap break-words">{message.content}</p>
        </div>
      </div>
    </div>
  )
}

function ThinkingBubble({ message }: { message: UIMessage }) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="flex animate-slide-up mb-3">
      <div className="flex items-start gap-3 max-w-[85%] ml-11">
        <div className="min-w-0 flex-1 rounded-xl border border-gray-800 bg-gray-900/50 overflow-hidden">
          <button
            onClick={() => setExpanded(!expanded)}
            className="w-full flex items-center gap-2 px-3 py-2 text-xs text-gray-400 hover:bg-gray-800/50 transition-colors"
          >
            {expanded ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
            <Cpu className="w-3.5 h-3.5 text-orange-400" />
            <span className="font-medium text-gray-300">模型思考</span>
            {message.streaming && <Loader2 className="w-3 h-3 text-orange-400 animate-spin ml-1" />}
          </button>
          {expanded && (
            <div className="border-t border-gray-800 px-3 py-2 text-xs text-gray-400 whitespace-pre-wrap break-words">
              {message.content}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function AssistantBubble({ message }: { message: UIMessage }) {
  const [expanded, setExpanded] = useState(false)
  const MAX_PREVIEW = 500
  const isLong = message.content.length > MAX_PREVIEW
  const displayContent = isLong && !expanded ? message.content.slice(0, MAX_PREVIEW) + '...' : message.content

  return (
    <div className="animate-slide-up mb-6">
      <div className="flex items-start gap-3 max-w-[85%]">
        <div className="w-8 h-8 rounded-lg bg-orange-500/15 flex items-center justify-center shrink-0 mt-0.5">
          <Bot className="w-4 h-4 text-orange-400" />
        </div>
        <div className="min-w-0 flex-1">
          {/* Trace block */}
          {message.trace && message.trace.length > 0 && (
            <AgentThinkingBlock trace={message.trace} isThinking={!!message.streaming} />
          )}

          {/* Content */}
          <div className="mt-2">
            <MarkdownRenderer content={displayContent} />
            {message.streaming && (
              <span className="inline-block w-1.5 h-4 bg-orange-500 ml-0.5 animate-pulse align-text-bottom rounded-sm" />
            )}
            {isLong && (
              <button onClick={() => setExpanded(!expanded)} className="text-xs text-orange-400 hover:text-orange-300 mt-1 flex items-center gap-1">
                {expanded ? <><ChevronUp className="w-3 h-3" /> 收起</> : <><ChevronDown className="w-3 h-3" /> 展开全部</>}
              </button>
            )}
          </div>

          {/* Copy button */}
          {!message.streaming && message.content && (
            <div className="flex items-center gap-2 mt-2 opacity-0 hover:opacity-100 transition-opacity">
              <CopyButton text={message.content} />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function ToolBubble({ message }: { message: UIMessage }) {
  const [showDetail, setShowDetail] = useState(false)
  const isSuccess = message.toolStatus === 'success'
  const isRunning = message.toolStatus === 'running'
  const isFailed = message.toolStatus === 'failed'

  return (
    <div className="flex animate-slide-up mb-3">
      <div className="flex items-start gap-3 max-w-[85%] ml-11">
        <button
          onClick={() => setShowDetail(!showDetail)}
          className={`flex items-center gap-2 px-3 py-2 rounded-xl border text-xs font-mono transition-all hover:scale-[1.02] ${
            isSuccess ? 'bg-emerald-500/5 border-emerald-500/20 text-emerald-400' :
            isFailed ? 'bg-red-500/5 border-red-500/20 text-red-400' :
            'bg-orange-500/5 border-orange-500/20 text-orange-400'
          }`}
        >
          {isRunning && <Loader2 className="w-3 h-3 animate-spin" />}
          {isSuccess && <CheckCircle2 className="w-3 h-3" />}
          {isFailed && <XCircle className="w-3 h-3" />}
          <span className="text-[10px]">{TOOL_ICONS[message.toolName || ''] || '🔧'}</span>
          <span>{TOOL_DISPLAY_NAMES[message.toolName || ''] || '工具调用'}</span>
          {isRunning && <span className="text-gray-600 animate-pulse-dot">执行中...</span>}
          {!isRunning && <span className="text-gray-500">· {message.content}</span>}
          {message.dataJson && (
            <span className="text-gray-600 ml-1">{showDetail ? '▲' : '▼'}</span>
          )}
        </button>

        {showDetail && message.dataJson && (
          <div className="mt-2 ml-11 p-3 rounded-xl bg-gray-900 border border-gray-800 animate-slide-up">
            <pre className="text-[10px] font-mono text-gray-400 whitespace-pre-wrap break-all max-h-40 overflow-y-auto">
              {JSON.stringify(JSON.parse(message.dataJson), null, 2)}
            </pre>
          </div>
        )}
      </div>
    </div>
  )
}

function ErrorBubble({ message }: { message: UIMessage }) {
  return (
    <div className="flex animate-slide-up mb-4">
      <div className="flex items-start gap-3 max-w-[85%] ml-11">
        <div className="flex items-center gap-2 px-3 py-2 rounded-xl bg-red-500/5 border border-red-500/20">
          <AlertTriangle className="w-4 h-4 text-red-400 shrink-0" />
          <span className="text-sm text-red-400">{message.content}</span>
        </div>
      </div>
    </div>
  )
}
