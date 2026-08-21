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

  const payload = await parseJson<unknown>(res)
  if (!res.ok) {
    const errorPayload = asApiResponse<unknown>(payload)
    throw new ApiError(errorPayload?.msg || `HTTP ${res.status}`, errorPayload?.code ?? -1, res.status, errorPayload?.data)
  }

  const apiPayload = asApiResponse<T>(payload)
  if (!apiPayload) {
    return payload as T
  }

  if (apiPayload.code === TOKEN_RENEWED_CODE && !hasRetried) {
    const tokens = applyTokenRenewal(apiPayload as ApiResponse<unknown>)
    if (tokens) {
      return apiRequest<T>(path, init, tokens.access_token, true)
    }
  }

  if (apiPayload.code !== SUCCESS_CODE) {
    if (isAuthCode(apiPayload.code)) {
      window.dispatchEvent(new CustomEvent('auth-unauthorized'))
    }
    throw new ApiError(apiPayload.msg || '请求失败', apiPayload.code, res.status, apiPayload.data)
  }

  return apiPayload.data as T
}

async function parseJson<T>(res: Response): Promise<T> {
  const text = await res.text()
  if (!text) return { code: SUCCESS_CODE, msg: 'ok' } as T
  return JSON.parse(text) as T
}

function isAuthCode(code: number) {
  return code >= 10000 && code < 10010 && code !== TOKEN_RENEWED_CODE
}

function asApiResponse<T>(payload: unknown): ApiResponse<T> | null {
  if (!payload || typeof payload !== 'object') return null
  if (!('code' in payload)) return null
  const response = payload as Partial<ApiResponse<T>>
  return typeof response.code === 'number' ? response as ApiResponse<T> : null
}
