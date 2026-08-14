import { API_BASE } from '@/constants'

export const SUCCESS_CODE = 0
export const TOKEN_RENEWED_CODE = 10004

export interface ApiResponse<T> {
  code: number
  msg: string
  data?: T
}

export interface AuthTokens {
  access_token: string
  refresh_token: string
}

export class ApiError extends Error {
  code: number
  status: number
  data?: unknown

  constructor(message: string, code: number, status: number, data?: unknown) {
    super(message)
    this.code = code
    this.status = status
    this.data = data
  }
}

let tokenRefreshHandler: ((tokens: AuthTokens) => void) | null = null

export function setTokenRefreshHandler(handler: ((tokens: AuthTokens) => void) | null) {
  tokenRefreshHandler = handler
}

export function createAuthHeaders(token?: string | null): Record<string, string> {
  return token ? { 'Access-Token': token } : {}
}

export function applyTokenRenewal(payload: ApiResponse<unknown>): AuthTokens | null {
  if (payload.code !== TOKEN_RENEWED_CODE) return null
  const data = payload.data as Partial<AuthTokens> | undefined
  if (!data?.access_token || !data?.refresh_token) return null
  const tokens = { access_token: data.access_token, refresh_token: data.refresh_token }
  tokenRefreshHandler?.(tokens)
  return tokens
}

export async function apiGet<T>(path: string, token?: string | null): Promise<T> {
  return apiRequest<T>(path, { method: 'GET' }, token)
}

export async function apiPost<T, D = unknown>(path: string, data: D, token?: string | null): Promise<T> {
  return apiRequest<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  }, token)
}

export async function apiPut<T, D = unknown>(path: string, data: D, token?: string | null): Promise<T> {
  return apiRequest<T>(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  }, token)
}

export async function apiDelete<T, D = unknown>(path: string, data?: D, token?: string | null): Promise<T> {
  return apiRequest<T>(path, {
    method: 'DELETE',
    headers: data ? { 'Content-Type': 'application/json' } : undefined,
    body: data ? JSON.stringify(data) : undefined,
  }, token)
}

export async function apiRequest<T>(
  path: string,
  init: RequestInit = {},
  token?: string | null,
  hasRetried = false,
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: 'include',
    headers: {
      ...init.headers,
      ...createAuthHeaders(token),
    },
  })

  const payload = await parseJson<ApiResponse<T> | ApiResponse<AuthTokens>>(res)
  if (!res.ok) {
    throw new ApiError(payload?.msg || `HTTP ${res.status}`, payload?.code ?? -1, res.status, payload?.data)
  }

  if (payload.code === TOKEN_RENEWED_CODE && !hasRetried) {
    const tokens = applyTokenRenewal(payload as ApiResponse<unknown>)
    if (tokens) {
      return apiRequest<T>(path, init, tokens.access_token, true)
    }
  }

  if (payload.code !== SUCCESS_CODE) {
    if (isAuthCode(payload.code)) {
      window.dispatchEvent(new CustomEvent('auth-unauthorized'))
    }
    throw new ApiError(payload.msg || '请求失败', payload.code, res.status, payload.data)
  }

  return payload.data as T
}

async function parseJson<T>(res: Response): Promise<T> {
  const text = await res.text()
  if (!text) return { code: SUCCESS_CODE, msg: 'ok' } as T
  return JSON.parse(text) as T
}

function isAuthCode(code: number) {
  return code >= 10000 && code < 10010 && code !== TOKEN_RENEWED_CODE
}
