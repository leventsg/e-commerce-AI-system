import { useEffect, useState, type FormEvent } from 'react'
import { Cpu, KeyRound, Loader2, Lock, ShieldCheck, User, X } from 'lucide-react'
import { useAuth, useTheme } from '@/contexts'
import { sendPasswordCode, updatePassword } from '@/services/api/auth'

export function TopBar() {
  const { token, user, username } = useAuth()
  const { toggleTheme } = useTheme()
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [codeLoading, setCodeLoading] = useState(false)
  const [cooldown, setCooldown] = useState(0)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    if (cooldown <= 0) return
    const timer = window.setTimeout(() => setCooldown(value => Math.max(0, value - 1)), 1000)
    return () => window.clearTimeout(timer)
  }, [cooldown])

  const resetPasswordForm = () => {
    setCode('')
    setPassword('')
    setConfirmPassword('')
    setMessage('')
    setError('')
  }

  const handleSendCode = async () => {
    setError('')
    setMessage('')
    setCodeLoading(true)
    try {
      await sendPasswordCode(token)
      setMessage('验证码已发送到注册邮箱')
      setCooldown(60)
    } catch (e) {
      setError(e instanceof Error ? e.message : '验证码发送失败')
    } finally {
      setCodeLoading(false)
    }
  }

  const handleUpdatePassword = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setMessage('')
    if (!code.trim() || !password || !confirmPassword) {
      setError('请填写验证码和新密码')
      return
    }
    if (password !== confirmPassword) {
      setError('两次输入的密码不一致')
      return
    }
    setLoading(true)
    try {
      await updatePassword({ code: code.trim(), password, confirmPassword }, token)
      setMessage('密码已更新')
      setCode('')
      setPassword('')
      setConfirmPassword('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '密码更新失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="relative h-12 flex items-center justify-between px-4 border-b border-gray-800 bg-gray-950 shrink-0">
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
        <button
          type="button"
          onClick={() => {
            resetPasswordForm()
            setPasswordOpen(open => !open)
          }}
          className="w-7 h-7 rounded-lg bg-orange-500/15 border border-orange-500/20 flex items-center justify-center transition-colors hover:bg-orange-500/25"
          title={username || 'User'}
        >
          <User className="w-3.5 h-3.5 text-orange-400" />
        </button>
      </div>
      {passwordOpen && (
        <div className="absolute right-3 top-14 z-40 w-[min(24rem,calc(100vw-1.5rem))] rounded-2xl border border-gray-800 bg-gray-950 p-4 text-gray-100 shadow-2xl shadow-black/60">
          <div className="mb-4 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-xl border border-orange-500/20 bg-orange-500/10">
                <KeyRound className="h-4 w-4 text-orange-300" />
              </div>
              <div>
                <p className="text-sm font-semibold text-gray-100">修改密码</p>
                <p className="text-xs text-gray-500">验证码会发送到注册邮箱</p>
              </div>
            </div>
            <button
              type="button"
              onClick={() => setPasswordOpen(false)}
              className="flex h-7 w-7 items-center justify-center rounded-lg text-gray-500 transition hover:bg-gray-900 hover:text-gray-300"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          <form className="space-y-3" onSubmit={handleUpdatePassword}>
            <label className="block">
              <span className="mb-1.5 block text-xs font-medium text-gray-400">注册邮箱</span>
              <input
                value={user?.email || username || ''}
                readOnly
                className="h-10 w-full rounded-xl border border-gray-800 bg-gray-900/80 px-3 text-sm text-gray-300 outline-none"
              />
            </label>
            <label className="block">
              <span className="mb-1.5 block text-xs font-medium text-gray-400">邮箱验证码</span>
              <span className="flex gap-2">
                <span className="relative min-w-0 flex-1">
                  <ShieldCheck className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-600" />
                  <input
                    value={code}
                    onChange={e => setCode(e.target.value)}
                    className="h-10 w-full rounded-xl border border-gray-800 bg-gray-900/80 pl-9 pr-3 text-sm text-gray-100 outline-none transition focus:border-orange-400/50"
                    placeholder="6 位验证码"
                    inputMode="numeric"
                  />
                </span>
                <button
                  type="button"
                  onClick={handleSendCode}
                  disabled={codeLoading || cooldown > 0}
                  className="h-10 shrink-0 rounded-xl border border-orange-400/30 bg-orange-400/10 px-3 text-xs font-semibold text-orange-100 transition hover:bg-orange-400/20 disabled:cursor-not-allowed disabled:opacity-60"
                >
                  {codeLoading ? '发送中' : cooldown > 0 ? `${cooldown}s` : '发送验证码'}
                </button>
              </span>
            </label>
            <label className="block">
              <span className="mb-1.5 block text-xs font-medium text-gray-400">新密码</span>
              <span className="relative block">
                <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-600" />
                <input
                  type="password"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  className="h-10 w-full rounded-xl border border-gray-800 bg-gray-900/80 pl-9 pr-3 text-sm text-gray-100 outline-none transition focus:border-orange-400/50"
                  placeholder="至少 6 位密码"
                  autoComplete="new-password"
                />
              </span>
            </label>
            <label className="block">
              <span className="mb-1.5 block text-xs font-medium text-gray-400">确认密码</span>
              <span className="relative block">
                <Lock className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-600" />
                <input
                  type="password"
                  value={confirmPassword}
                  onChange={e => setConfirmPassword(e.target.value)}
                  className="h-10 w-full rounded-xl border border-gray-800 bg-gray-900/80 pl-9 pr-3 text-sm text-gray-100 outline-none transition focus:border-orange-400/50"
                  placeholder="再次输入新密码"
                  autoComplete="new-password"
                />
              </span>
            </label>
            {message && <div className="rounded-xl border border-emerald-400/20 bg-emerald-400/10 px-3 py-2 text-xs text-emerald-100">{message}</div>}
            {error && <div className="rounded-xl border border-red-400/20 bg-red-400/10 px-3 py-2 text-xs text-red-100">{error}</div>}
            <button
              type="submit"
              disabled={loading}
              className="flex h-10 w-full items-center justify-center gap-2 rounded-xl bg-orange-300 text-sm font-semibold text-gray-950 transition hover:bg-orange-200 disabled:cursor-wait disabled:opacity-60"
            >
              {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <KeyRound className="h-4 w-4" />}
              更新密码
            </button>
          </form>
        </div>
      )}
    </div>
  )
}
