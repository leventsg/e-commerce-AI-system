import { createContext, useContext, useCallback, useEffect, useMemo, useState, type PropsWithChildren } from 'react'
import { STORAGE_KEYS } from '@/constants'
import { getUserInfo, login, logout as requestLogout } from '@/services/api/auth'
import { setTokenRefreshHandler, type AuthTokens } from '@/services/api/client'

interface AuthUser {
  user_id?: number
  email?: string
  user_name?: string
  avatar?: string
}

interface AuthState {
  token: string | null
  refreshToken: string | null
  user: AuthUser | null
  username: string | null
  isAuthenticated: boolean
  loginWithPassword: (email: string, password: string) => Promise<void>
  setTokens: (tokens: AuthTokens) => void
  logout: () => void
}

const AuthContext = createContext<AuthState | null>(null)

function readStoredUser(): AuthUser | null {
  const raw = localStorage.getItem(STORAGE_KEYS.USER)
  if (!raw) return null
  try {
    return JSON.parse(raw) as AuthUser
  } catch {
    localStorage.removeItem(STORAGE_KEYS.USER)
    return null
  }
}

function writeRefreshCookie(refreshToken: string) {
  document.cookie = `Refresh-Token=${encodeURIComponent(refreshToken)}; path=/`
}

function clearRefreshCookie() {
  document.cookie = 'Refresh-Token=; Max-Age=0; path=/'
}

export function AuthProvider({ children }: PropsWithChildren) {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(STORAGE_KEYS.TOKEN))
  const [refreshToken, setRefreshToken] = useState<string | null>(() => localStorage.getItem(STORAGE_KEYS.REFRESH_TOKEN))
  const [user, setUser] = useState<AuthUser | null>(() => readStoredUser())

  const username = useMemo(() => user?.user_name || user?.email || localStorage.getItem(STORAGE_KEYS.USERNAME), [user])

  const setTokens = useCallback((tokens: AuthTokens) => {
    setToken(tokens.access_token)
    setRefreshToken(tokens.refresh_token)
    localStorage.setItem(STORAGE_KEYS.TOKEN, tokens.access_token)
    localStorage.setItem(STORAGE_KEYS.REFRESH_TOKEN, tokens.refresh_token)
    writeRefreshCookie(tokens.refresh_token)
  }, [])

  const clearAuth = useCallback((emitEvent: boolean) => {
    setToken(null)
    setRefreshToken(null)
    setUser(null)
    localStorage.removeItem(STORAGE_KEYS.TOKEN)
    localStorage.removeItem(STORAGE_KEYS.REFRESH_TOKEN)
    localStorage.removeItem(STORAGE_KEYS.USERNAME)
    localStorage.removeItem(STORAGE_KEYS.USER)
    clearRefreshCookie()
    if (emitEvent) window.dispatchEvent(new CustomEvent('auth-unauthorized'))
  }, [])

  const loginWithPassword = useCallback(async (email: string, password: string) => {
    const tokens = await login({ email, password })
    setTokens(tokens)
    const info = await getUserInfo(tokens.access_token)
    const nextUser: AuthUser = {
      user_id: info.user_id,
      email: info.email,
      user_name: info.user_name,
      avatar: info.avatar,
    }
    setUser(nextUser)
    localStorage.setItem(STORAGE_KEYS.USER, JSON.stringify(nextUser))
    localStorage.setItem(STORAGE_KEYS.USERNAME, nextUser.user_name || nextUser.email || email)
  }, [setTokens])

  const logout = useCallback(() => {
    if (token) {
      requestLogout(token).catch(() => undefined)
    }
    clearAuth(true)
  }, [clearAuth, token])

  useEffect(() => {
    setTokenRefreshHandler(setTokens)
    return () => setTokenRefreshHandler(null)
  }, [setTokens])

  useEffect(() => {
    const handler = () => clearAuth(false)
    window.addEventListener('auth-unauthorized', handler)
    return () => window.removeEventListener('auth-unauthorized', handler)
  }, [clearAuth])

  return (
    <AuthContext.Provider value={{ token, refreshToken, user, username, isAuthenticated: !!token, loginWithPassword, setTokens, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
