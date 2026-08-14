import { useState, useEffect } from 'react'
import { ChevronDown, ChevronRight, Loader2, CheckCircle2, XCircle, Clock, Cpu } from 'lucide-react'
import type { TraceStep } from '@/types'
import { TOOL_DISPLAY_NAMES, TOOL_ICONS, SUB_AGENTS } from '@/constants'

interface AgentThinkingBlockProps {
  trace: TraceStep[]
  isThinking: boolean
}

export function AgentThinkingBlock({ trace, isThinking }: AgentThinkingBlockProps) {
  const [isOpen, setIsOpen] = useState(true)

  useEffect(() => { setIsOpen(isThinking) }, [isThinking])

  if (trace.length === 0 && !isThinking) return null

  const steps = trace.filter(s => s.tool_name)
  const duration = steps.length > 0 ? steps.reduce((sum, s) => sum + (s.duration || 0), 0) : 0

  return (
    <div className="mt-2 border border-gray-800 rounded-xl overflow-hidden bg-gray-900/50">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="w-full flex items-center gap-2 px-3 py-2 text-xs text-gray-400 hover:bg-gray-800/50 transition-colors"
      >
        {isOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        <Cpu className="w-3.5 h-3.5 text-orange-400" />
        <span className="font-medium text-gray-300">工具调用链路</span>
        {isThinking && <Loader2 className="w-3 h-3 text-orange-400 animate-spin ml-1" />}
        {!isThinking && steps.length > 0 && (
          <span className="ml-auto text-[10px] text-gray-600">
            {steps.length} 步骤 · {duration > 0 ? `${(duration / 1000).toFixed(1)}s` : '...'}
          </span>
        )}
      </button>

      {isOpen && (
        <div className="border-t border-gray-800 px-3 py-2 space-y-1.5 animate-fade-in">
          {steps.map((step, i) => {
            const agent = SUB_AGENTS.find(a => a.tools.includes(step.tool_name || ''))
            return (
              <div key={i} className="flex items-start gap-2 text-xs">
                <div className="mt-0.5">
                  {step.status === 'running' && <Loader2 className="w-3 h-3 text-orange-400 animate-spin" />}
                  {step.status === 'success' && <CheckCircle2 className="w-3 h-3 text-emerald-400" />}
                  {step.status === 'failed' && <XCircle className="w-3 h-3 text-red-400" />}
                  {step.status === 'pending' && <Clock className="w-3 h-3 text-gray-600" />}
                </div>
                <span className="text-[10px]">{TOOL_ICONS[step.tool_name || ''] || '🔧'}</span>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-1.5">
                    <span className="font-medium text-gray-300">{TOOL_DISPLAY_NAMES[step.tool_name || ''] || step.action}</span>
                    {agent && <span className="text-[10px] text-gray-600">via {agent.display}</span>}
                  </div>
                  {step.error && <span className="text-red-400">{step.error}</span>}
                  {step.result && (
                    <pre className="text-[10px] text-gray-500 mt-0.5 truncate font-mono">
                      {JSON.stringify(step.result).slice(0, 80)}
                    </pre>
                  )}
                </div>
                {step.duration ? (
                  <span className="text-[10px] text-gray-600 font-mono shrink-0">{(step.duration / 1000).toFixed(1)}s</span>
                ) : step.status === 'running' ? (
                  <span className="text-[10px] text-gray-600 shrink-0">...</span>
                ) : null}
              </div>
            )
          })}

          {/* Thinking state when no steps yet */}
          {isThinking && steps.length === 0 && (
            <div className="flex items-center gap-2 text-xs text-gray-500 py-1">
              <Loader2 className="w-3 h-3 text-orange-400 animate-spin" />
              <span>Supervisor Agent 正在分析...</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
