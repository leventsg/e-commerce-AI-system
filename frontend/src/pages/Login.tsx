import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Bot, Loader2, Lock, Mail, Sparkles, UserPlus } from 'lucide-react'
import { useAuth } from '@/contexts'

type AuthMode = 'login' | 'register'

interface LoginLocationState {
  from?: string
}

const emailPattern = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export default function LoginPage() {
  const { isAuthenticated, loginWithPassword, registerWithPassword } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as LoginLocationState | null)?.from || '/'
  const [mode, setMode] = useState<AuthMode>('login')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (isAuthenticated) {
      navigate(from, { replace: true })
    }
  }, [from, isAuthenticated, navigate])

  const passwordScore = useMemo(() => {
    let score = 0
    if (password.length >= 8) score += 1
    if (/[A-Z]/.test(password) && /[a-z]/.test(password)) score += 1
    if (/\d/.test(password)) score += 1
    if (/[^A-Za-z0-9]/.test(password)) score += 1
    return score
  }, [password])

  const switchMode = (nextMode: AuthMode) => {
    setMode(nextMode)
    setError('')
    setConfirmPassword('')
  }

  const validate = () => {
    const normalizedEmail = email.trim()
    if (!normalizedEmail || !password.trim()) return '请输入邮箱和密码'
    if (!emailPattern.test(normalizedEmail)) return '请输入有效的邮箱地址'
    if (mode === 'register') {
      if (password.length < 6) return '密码至少需要 6 位'
      if (password !== confirmPassword) return '两次输入的密码不一致'
    }
    return ''
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const validationError = validate()
    if (validationError) {
      setError(validationError)
      return
    }

    setLoading(true)
    setError('')
    try {
      if (mode === 'register') {
        await registerWithPassword(email.trim(), password, confirmPassword)
      } else {
        await loginWithPassword(email.trim(), password)
      }
      navigate(from, { replace: true })
    } catch (e) {
      setError(e instanceof Error ? e.message : mode === 'register' ? '注册失败，请重试' : '登录失败，请重试')
    } finally {
      setLoading(false)
    }
  }

  const isRegister = mode === 'register'

  return (
    <div className="min-h-screen overflow-hidden bg-[#11100d] text-stone-100">
      <div className="absolute inset-0 bg-[radial-gradient(circle_at_18%_18%,rgba(245,158,11,0.22),transparent_32%),radial-gradient(circle_at_84%_12%,rgba(20,184,166,0.16),transparent_30%),linear-gradient(135deg,#11100d_0%,#1f1b15_45%,#0d1615_100%)]" />
      <div className="absolute inset-0 opacity-[0.08] [background-image:linear-gradient(90deg,#fff_1px,transparent_1px),linear-gradient(#fff_1px,transparent_1px)] [background-size:44px_44px]" />

      <main className="relative z-10 flex min-h-screen items-center justify-center px-4 py-8">
        <section className="grid w-full max-w-5xl overflow-hidden rounded-[2rem] border border-white/10 bg-stone-950/65 shadow-2xl shadow-black/50 backdrop-blur-xl lg:grid-cols-[1.05fr_0.95fr]">
          <div className="hidden min-h-[620px] flex-col justify-between border-r border-white/10 bg-stone-900/50 p-10 lg:flex">
            <div>
              <div className="mb-12 flex items-center gap-3">
                <div className="flex h-11 w-11 items-center justify-center rounded-2xl bg-amber-400 text-stone-950">
                  <Bot className="h-6 w-6" />
                </div>
                <div>
                  <p className="text-sm font-semibold uppercase tracking-[0.24em] text-amber-300">go-mall</p>
                  <h1 className="text-2xl font-semibold tracking-tight">AI 工作台</h1>
                </div>
              </div>
              <p className="max-w-md text-5xl font-semibold leading-[1.02] tracking-tight text-stone-50">
                让客服、订单和商品能力汇到同一个入口。
              </p>
            </div>
            <div className="grid gap-3 text-sm text-stone-300">
              <div className="rounded-2xl border border-white/10 bg-white/[0.04] p-4">
                <Sparkles className="mb-3 h-5 w-5 text-teal-300" />
                登录后可以使用 AI 客服工作台查询订单、商品、优惠券和购物车状态。
              </div>
              <div className="rounded-2xl border border-amber-300/20 bg-amber-300/[0.06] p-4 text-amber-100">
                新用户注册后会自动进入工作台，无需再次登录。
              </div>
            </div>
          </div>

          <div className="p-6 sm:p-10">
            <div className="mb-8 lg:hidden">
              <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-amber-400 text-stone-950">
                <Bot className="h-6 w-6" />
              </div>
              <h1 className="text-3xl font-semibold tracking-tight">go-mall AI 工作台</h1>
            </div>

            <div className="mb-7">
              <p className="mb-2 text-sm font-medium uppercase tracking-[0.22em] text-amber-300">
                {isRegister ? 'Create account' : 'Welcome back'}
              </p>
              <h2 className="text-3xl font-semibold tracking-tight text-white">
                {isRegister ? '注册新用户' : '登录工作台'}
              </h2>
            </div>

            <div className="mb-7 grid grid-cols-2 rounded-2xl border border-white/10 bg-black/25 p-1">
              <button
                type="button"
                onClick={() => switchMode('login')}
                className={`rounded-xl px-4 py-2.5 text-sm font-medium transition ${mode === 'login' ? 'bg-white text-stone-950 shadow-sm' : 'text-stone-300 hover:text-white'}`}
              >
                登录
              </button>
              <button
                type="button"
                onClick={() => switchMode('register')}
                className={`rounded-xl px-4 py-2.5 text-sm font-medium transition ${mode === 'register' ? 'bg-white text-stone-950 shadow-sm' : 'text-stone-300 hover:text-white'}`}
              >
                注册
              </button>
            </div>

            <form className="space-y-4" onSubmit={handleSubmit}>
              <label className="block">
                <span className="mb-2 block text-sm font-medium text-stone-300">邮箱</span>
                <span className="relative block">
                  <Mail className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-500" />
                  <input
                    type="email"
                    value={email}
                    onChange={e => setEmail(e.target.value)}
                    className="h-12 w-full rounded-2xl border border-white/10 bg-white/[0.05] pl-11 pr-4 text-sm text-white outline-none transition placeholder:text-stone-500 focus:border-amber-300/60 focus:bg-white/[0.08] focus:ring-4 focus:ring-amber-300/10"
                    placeholder="you@example.com"
                    autoComplete="email"
                  />
                </span>
              </label>

              <label className="block">
                <span className="mb-2 block text-sm font-medium text-stone-300">密码</span>
                <span className="relative block">
                  <Lock className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-500" />
                  <input
                    type="password"
                    value={password}
                    onChange={e => setPassword(e.target.value)}
                    className="h-12 w-full rounded-2xl border border-white/10 bg-white/[0.05] pl-11 pr-4 text-sm text-white outline-none transition placeholder:text-stone-500 focus:border-amber-300/60 focus:bg-white/[0.08] focus:ring-4 focus:ring-amber-300/10"
                    placeholder={isRegister ? '至少 6 位密码' : '输入密码'}
                    autoComplete={isRegister ? 'new-password' : 'current-password'}
                  />
                </span>
              </label>

              {isRegister && (
                <>
                  <label className="block">
                    <span className="mb-2 block text-sm font-medium text-stone-300">确认密码</span>
                    <span className="relative block">
                      <Lock className="pointer-events-none absolute left-4 top-1/2 h-4 w-4 -translate-y-1/2 text-stone-500" />
                      <input
                        type="password"
                        value={confirmPassword}
                        onChange={e => setConfirmPassword(e.target.value)}
                        className="h-12 w-full rounded-2xl border border-white/10 bg-white/[0.05] pl-11 pr-4 text-sm text-white outline-none transition placeholder:text-stone-500 focus:border-amber-300/60 focus:bg-white/[0.08] focus:ring-4 focus:ring-amber-300/10"
                        placeholder="再次输入密码"
                        autoComplete="new-password"
                      />
                    </span>
                  </label>

                  <div className="grid grid-cols-4 gap-2" aria-hidden="true">
                    {[0, 1, 2, 3].map(index => (
                      <span
                        key={index}
                        className={`h-1.5 rounded-full transition ${passwordScore > index ? 'bg-amber-300' : 'bg-white/10'}`}
                      />
                    ))}
                  </div>
                </>
              )}

              {error && (
                <div className="rounded-2xl border border-red-400/30 bg-red-500/10 px-4 py-3 text-sm text-red-100">
                  {error}
                </div>
              )}

              <button
                type="submit"
                disabled={loading}
                className="flex h-12 w-full items-center justify-center gap-2 rounded-2xl bg-amber-300 px-4 text-sm font-semibold text-stone-950 transition hover:bg-amber-200 active:bg-amber-400 disabled:cursor-wait disabled:opacity-60"
              >
                {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : isRegister ? <UserPlus className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
                {loading ? (isRegister ? '注册中...' : '登录中...') : isRegister ? '创建账号并进入' : '进入工作台'}
              </button>
            </form>
          </div>
        </section>
      </main>
    </div>
  )
}
