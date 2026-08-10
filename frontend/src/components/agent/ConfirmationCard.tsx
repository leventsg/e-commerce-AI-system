import { AlertTriangle, Clock, Check, X } from 'lucide-react'
import type { UIMessage } from '@/types'

interface ConfirmationCardProps { message: UIMessage }

export function ConfirmationCard({ message }: ConfirmationCardProps) {
  const remaining = message.expiresAt ? Math.max(0, Math.floor((message.expiresAt * 1000 - Date.now()) / 1000)) : 0

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
          </div>
          <div className="px-4 py-3">
            <p className="text-sm text-gray-300">{message.content}</p>
          </div>
          <div className="flex border-t border-amber-500/10">
            <button className="flex-1 flex items-center justify-center gap-2 py-2.5 text-sm text-emerald-400 hover:bg-emerald-500/10 transition-colors">
              <Check className="w-4 h-4" /> 确认执行
            </button>
            <div className="w-px bg-amber-500/10" />
            <button className="flex-1 flex items-center justify-center gap-2 py-2.5 text-sm text-red-400 hover:bg-red-500/10 transition-colors">
              <X className="w-4 h-4" /> 取消
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
