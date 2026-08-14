import { Cpu, User } from 'lucide-react'
import { useAuth, useTheme } from '@/contexts'

export function TopBar() {
  const { username } = useAuth()
  const { toggleTheme } = useTheme()

  return (
    <div className="h-12 flex items-center justify-between px-4 border-b border-gray-800 bg-gray-950 shrink-0">
      <div className="flex items-center gap-3">
        <div className="flex items-center gap-2">
          <div className="w-7 h-7 rounded-lg bg-orange-500/15 border border-orange-500/20 flex items-center justify-center">
            <Cpu className="w-3.5 h-3.5 text-orange-500" />
          </div>
          <span className="text-sm font-semibold text-gray-200 tracking-tight">go-mall AI Console</span>
        </div>
        <span className="w-px h-4 bg-gray-800" />
        <div className="flex items-center gap-1.5">
          <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" />
          <span className="text-[10px] text-gray-500 font-mono">Eino Agent · v1.0</span>
        </div>
      </div>
      <div className="flex items-center gap-2">
        <select className="text-[10px] bg-gray-900 border border-gray-800 rounded-lg px-2 py-1 text-gray-400 outline-none focus:border-orange-500/30 font-mono">
          <option>deepseek-v3</option>
          <option>gpt-4o</option>
          <option>claude-3.5-sonnet</option>
        </select>
        <button onClick={toggleTheme} className="w-7 h-7 rounded-lg hover:bg-gray-900 border border-transparent hover:border-gray-800 flex items-center justify-center transition-colors text-gray-500 text-xs">
          🌓
        </button>
        <div className="w-7 h-7 rounded-lg bg-orange-500/15 border border-orange-500/20 flex items-center justify-center" title={username || 'User'}>
          <User className="w-3.5 h-3.5 text-orange-400" />
        </div>
      </div>
    </div>
  )
}
