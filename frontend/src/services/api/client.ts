import { API_BASE } from '@/constants'

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) { super(message); this.status = status }
}

export function createAuthHeaders(token?: string): Record<string, string> {
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export async function apiGet<T>(path: string, token?: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, { headers: createAuthHeaders(token) })
  if (res.status === 401) { window.dispatchEvent(new CustomEvent('auth-unauthorized')); throw new ApiError('Unauthorized', 401) }
  if (!res.ok) throw new ApiError(await res.text(), res.status)
  return res.json()
}

export async function apiPost<T, D = unknown>(path: string, data: D, token?: string): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...createAuthHeaders(token) },
    body: JSON.stringify(data),
  })
  if (res.status === 401) { window.dispatchEvent(new CustomEvent('auth-unauthorized')); throw new ApiError('Unauthorized', 401) }
  if (!res.ok) throw new ApiError(await res.text(), res.status)
  return res.json()
}
