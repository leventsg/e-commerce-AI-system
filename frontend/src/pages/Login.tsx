import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { Bot, Loader2 } from 'lucide-react'
import { useAuth } from '@/contexts'

export default function LoginPage() {
  const { loginWithPassword } = useAuth()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (!email.trim() || !password.trim()) { setError('请输入邮箱和密码'); return }
    setLoading(true); setError('')
    try {
      await loginWithPassword(email.trim(), password)
      navigate('/agent', { replace: true })
    } catch (e) {
      setError(e instanceof Error ? e.message : '登录失败，请重试')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-orange-50 via-white to-amber-50 dark:from-gray-900 dark:via-gray-950 dark:to-gray-900 px-4">
      <div className="w-full max-w-md bg-white dark:bg-gray-900 shadow-xl rounded-2xl p-8 border border-orange-100/70 dark:border-gray-800">
        <div className="flex items-center gap-2 mb-6">
          <Bot className="w-8 h-8 text-orange-500" />
          <h1 className="text-2xl font-bold text-gray-900 dark:text-gray-100">go-mall AI Console</h1>
        </div>
        <p className="text-sm text-gray-600 dark:text-gray-400 mb-6">登录 AI 客服工作台</p>

        <form className="space-y-4" onSubmit={handleSubmit}>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-200 mb-1">邮箱</label>
            <input
              type="email" value={email} onChange={e => setEmail(e.target.value)}
              className="w-full rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-orange-500 text-sm"
              placeholder="you@example.com" autoComplete="email"
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-200 mb-1">密码</label>
            <input
              type="password" value={password} onChange={e => setPassword(e.target.value)}
              className="w-full rounded-lg border border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-950 text-gray-900 dark:text-gray-100 placeholder-gray-400 dark:placeholder-gray-500 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-orange-500 text-sm"
              placeholder="••••••••" autoComplete="current-password"
            />
          </div>
          {error && (
            <div className="text-sm text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-800 rounded-lg px-3 py-2">{error}</div>
          )}
          <button
            type="submit" disabled={loading}
            className="w-full py-2.5 rounded-lg bg-orange-500 text-white font-medium text-sm hover:bg-orange-600 active:bg-orange-700 transition-colors disabled:opacity-50 disabled:cursor-wait flex items-center justify-center gap-2"
          >
            {loading && <Loader2 className="w-4 h-4 animate-spin" />}
            {loading ? '登录中...' : '登录'}
          </button>
        </form>
      </div>
    </div>
  )
}
