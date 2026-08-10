import { useEffect, useState } from 'react'
import { AlertTriangle, Clock, Check, X } from 'lucide-react'
import type { UIMessage } from '@/types'

interface ConfirmationCardProps {
  message: UIMessage
  onConfirm?: (conversationId: string, confirmationId: string) => Promise<void> | void
  onReject?: (conversationId: string, confirmationId: string) => Promise<void> | void
}

export function ConfirmationCard({ message, onConfirm, onReject }: ConfirmationCardProps) {
  const [now, setNow] = useState(() => Date.now())
  const [loading, setLoading] = useState<'confirm' | 'reject' | null>(null)
  const remaining = message.expiresAt ? Math.max(0, Math.floor((message.expiresAt * 1000 - now) / 1000)) : 0
  const isExpired = !!message.expiresAt && remaining <= 0
  const isFinal = message.confirmationStatus === 'approved' || message.confirmationStatus === 'rejected' || message.confirmationStatus === 'expired'
  const disabled = !message.conversationId || !message.confirmationId || isExpired || isFinal || loading !== null

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])

  const handleConfirm = async () => {
    if (disabled || !message.conversationId || !message.confirmationId) return
    setLoading('confirm')
    try {
      await onConfirm?.(message.conversationId, message.confirmationId)
    } finally {
      setLoading(null)
    }
  }

  const handleReject = async () => {
    if (disabled || !message.conversationId || !message.confirmationId) return
    setLoading('reject')
    try {
      await onReject?.(message.conversationId, message.confirmationId)
    } finally {
      setLoading(null)
    }
  }

  return (
    <div className="flex animate-slide-up mb-4">
      <div className="flex items-start gap-3 max-w-[85%] ml-11 w-full">
        <div className="flex-1 border-2 border-amber-500/30 bg-amber-500/5 rounded-2xl overflow-hidden">
          <div className="flex items-center gap-2 px-4 py-3 border-b border-amber-500/10 bg-amber-500/8">
            <AlertTriangle className="w-5 h-5 text-amber-400" />
            <span className="text-sm font-semibold text-amber-400">操作确认</span>
            {remaining > 0 && (
              <span className="ml-auto flex items-center gap-1 text-xs text-gray-500">
                <Clock className="w-3 h-3" /> {remaining}s 后过期
              </span>
            )}
            {isExpired && <span className="ml-auto text-xs text-gray-500">已过期</span>}
          </div>
          <div className="px-4 py-3">
            <p className="text-sm text-gray-300">{message.content}</p>
          </div>
          <div className="flex border-t border-amber-500/10">
            <button
              disabled={disabled}
              onClick={handleConfirm}
              className="flex-1 flex items-center justify-center gap-2 py-2.5 text-sm text-emerald-400 hover:bg-emerald-500/10 transition-colors disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent"
            >
              <Check className="w-4 h-4" /> {loading === 'confirm' ? '确认中...' : '确认执行'}
            </button>
            <div className="w-px bg-amber-500/10" />
            <button
              disabled={disabled}
              onClick={handleReject}
              className="flex-1 flex items-center justify-center gap-2 py-2.5 text-sm text-red-400 hover:bg-red-500/10 transition-colors disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent"
            >
              <X className="w-4 h-4" /> {loading === 'reject' ? '取消中...' : '取消'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
