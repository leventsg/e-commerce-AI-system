import { createContext, useContext, useState, useCallback, type PropsWithChildren } from 'react'
import { STORAGE_KEYS } from '@/constants'

interface AuthState {
  token: string | null
  username: string | null
  isAuthenticated: boolean
  login: (token: string, username: string) => void
  logout: () => void
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: PropsWithChildren) {
  const [token, setToken] = useState<string | null>(() => localStorage.getItem(STORAGE_KEYS.TOKEN))
  const [username, setUsername] = useState<string | null>(() => localStorage.getItem(STORAGE_KEYS.USERNAME))

  const login = useCallback((t: string, u: string) => {
    setToken(t); setUsername(u)
    localStorage.setItem(STORAGE_KEYS.TOKEN, t)
    localStorage.setItem(STORAGE_KEYS.USERNAME, u)
  }, [])

  const logout = useCallback(() => {
    setToken(null); setUsername(null)
    localStorage.removeItem(STORAGE_KEYS.TOKEN)
    localStorage.removeItem(STORAGE_KEYS.USERNAME)
    window.dispatchEvent(new CustomEvent('auth-unauthorized'))
  }, [])

  return (
    <AuthContext.Provider value={{ token, username, isAuthenticated: !!token, login, logout }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
