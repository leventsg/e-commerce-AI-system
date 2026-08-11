import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { AlertTriangle, Bot, CheckCircle2, ChevronDown, ChevronRight, Cpu, Loader2, ShieldAlert, Wrench, XCircle } from 'lucide-react'
import type { UIMessage } from '@/types'
import { TOOL_DISPLAY_NAMES, TOOL_ICONS } from '@/constants'
import { MarkdownRenderer, CopyButton } from '@/components/common'
import { ConfirmationCard } from './ConfirmationCard'
import type { ChatRenderItem } from './chatRenderItems'

interface AssistantResponseGroupProps {
  item: Extract<ChatRenderItem, { kind: 'assistant-group' }>
  onConfirmAction?: (conversationId: string, confirmationId: string, approved: boolean) => Promise<void> | void
}

export function AssistantResponseGroup({ item, onConfirmAction }: AssistantResponseGroupProps) {
  const finalAnswer = item.assistantMessages[item.assistantMessages.length - 1]

  return (
    <div className="animate-slide-up mb-6" data-testid="assistant-response-group">
      <div className="flex items-start gap-3 max-w-[85%]">
        <div className="w-8 h-8 rounded-lg bg-orange-500/15 flex items-center justify-center shrink-0 mt-0.5" data-testid="assistant-avatar">
          <Bot className="w-4 h-4 text-orange-400" />
        </div>
        <div className="min-w-0 flex-1 space-y-2">
          {item.thinkingText && (
            <StatusPanel title="思考过程" icon={<Cpu className="w-3.5 h-3.5 text-orange-400" />} isRunning={item.isStreaming || item.messages.some(message => message.type === 'thinking' && message.streaming)}>
              <div className="text-xs text-gray-400 whitespace-pre-wrap break-words leading-relaxed">{item.thinkingText}</div>
            </StatusPanel>
          )}

          {item.toolMessages.length > 0 && (
            <StatusPanel title="工具调用执行过程" icon={<Wrench className="w-3.5 h-3.5 text-orange-400" />} isRunning={item.isStreaming || item.toolMessages.some(message => message.toolStatus === 'running')}>
              <div className="space-y-1.5">
                {item.toolMessages.map(message => <ToolStatusRow key={message.toolCallId || message.id} message={message} />)}
              </div>
            </StatusPanel>
          )}

          {item.confirmationMessages.length > 0 && (
            <StatusPanel title="工具执行确认" icon={<ShieldAlert className="w-3.5 h-3.5 text-amber-400" />} isRunning={item.isStreaming || item.confirmationMessages.some(message => message.confirmationStatus === 'pending')}>
              <div className="space-y-2">
                {item.confirmationMessages.map(message => (
                  <ConfirmationCard
                    key={message.confirmationId || message.id}
                    message={message}
                    embedded
                    onConfirm={(conversationId, id) => onConfirmAction?.(conversationId, id, true)}
                    onReject={(conversationId, id) => onConfirmAction?.(conversationId, id, false)}
                  />
                ))}
              </div>
            </StatusPanel>
          )}

          {item.errorMessages.map(message => <ErrorInline key={message.id} message={message} />)}

          {finalAnswer && <AssistantAnswer message={finalAnswer} />}
        </div>
      </div>
    </div>
  )
}

function StatusPanel({ title, icon, isRunning, children }: { title: string; icon: ReactNode; isRunning: boolean; children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(isRunning)
  const [touched, setTouched] = useState(false)

  useEffect(() => {
    if (!touched) setIsOpen(isRunning)
  }, [isRunning, touched])

  return (
    <div className="rounded-xl border border-gray-800 bg-gray-900/55 overflow-hidden">
      <button
        type="button"
        onClick={() => { setTouched(true); setIsOpen(open => !open) }}
        className="w-full flex items-center gap-2 px-3 py-2 text-xs text-gray-400 hover:bg-gray-800/55 transition-colors"
      >
        {isOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        {icon}
        <span className="font-medium text-gray-300">{title}</span>
        {isRunning ? <Loader2 className="w-3 h-3 text-orange-400 animate-spin ml-1" /> : <span className="ml-auto text-[10px] text-gray-600">已完成</span>}
      </button>
      {isOpen && (
        <div className="border-t border-gray-800 px-3 py-2 animate-fade-in">
          {children}
        </div>
      )}
    </div>
  )
}

function ToolStatusRow({ message }: { message: UIMessage }) {
  const [showDetail, setShowDetail] = useState(false)
  const isSuccess = message.toolStatus === 'success'
  const isFailed = message.toolStatus === 'failed'
  const isRunning = message.toolStatus === 'running'

  return (
    <div className={`rounded-lg border px-3 py-2 ${
      isSuccess ? 'border-emerald-500/20 bg-emerald-500/5' :
      isFailed ? 'border-red-500/20 bg-red-500/5' :
      'border-orange-500/20 bg-orange-500/5'
    }`}>
      <button type="button" onClick={() => setShowDetail(open => !open)} className="w-full flex items-center gap-2 text-left">
        {isRunning && <Loader2 className="w-3.5 h-3.5 text-orange-400 animate-spin shrink-0" />}
        {isSuccess && <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400 shrink-0" />}
        {isFailed && <XCircle className="w-3.5 h-3.5 text-red-400 shrink-0" />}
        <span className="text-[10px] shrink-0">{TOOL_ICONS[message.toolName || ''] || '🔧'}</span>
        <span className="text-xs font-medium text-gray-300 shrink-0">{TOOL_DISPLAY_NAMES[message.toolName || ''] || '工具调用'}</span>
        <span className={`text-xs min-w-0 flex-1 truncate ${isFailed ? 'text-red-300' : isSuccess ? 'text-emerald-300' : 'text-orange-300'}`}>{message.content}</span>
        {message.dataJson && (showDetail ? <ChevronDown className="w-3 h-3 text-gray-500 shrink-0" /> : <ChevronRight className="w-3 h-3 text-gray-500 shrink-0" />)}
      </button>
      {showDetail && message.dataJson && (
        <pre className="mt-2 max-h-40 overflow-y-auto rounded-lg bg-gray-950/70 p-2 text-[10px] text-gray-400 whitespace-pre-wrap break-all font-mono">
          {formatJson(message.dataJson)}
        </pre>
      )}
    </div>
  )
}

function AssistantAnswer({ message }: { message: UIMessage }) {
  const [expanded, setExpanded] = useState(false)
  const MAX_PREVIEW = 500
  const isLong = message.content.length > MAX_PREVIEW
  const displayContent = isLong && !expanded ? `${message.content.slice(0, MAX_PREVIEW)}...` : message.content

  return (
    <div className="pt-1">
      <MarkdownRenderer content={displayContent} />
      {message.streaming && (
        <span className="inline-block w-1.5 h-4 bg-orange-500 ml-0.5 animate-pulse align-text-bottom rounded-sm" />
      )}
      {isLong && (
        <button type="button" onClick={() => setExpanded(!expanded)} className="text-xs text-orange-400 hover:text-orange-300 mt-1 flex items-center gap-1">
          {expanded ? <><ChevronDown className="w-3 h-3" /> 收起</> : <><ChevronRight className="w-3 h-3" /> 展开全部</>}
        </button>
      )}
      {!message.streaming && message.content && (
        <div className="flex items-center gap-2 mt-2 opacity-0 hover:opacity-100 transition-opacity">
          <CopyButton text={message.content} />
        </div>
      )}
    </div>
  )
}

function ErrorInline({ message }: { message: UIMessage }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2 rounded-xl bg-red-500/5 border border-red-500/20">
      <AlertTriangle className="w-4 h-4 text-red-400 shrink-0" />
      <span className="text-sm text-red-400">{message.content}</span>
    </div>
  )
}

function formatJson(value: string) {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}
