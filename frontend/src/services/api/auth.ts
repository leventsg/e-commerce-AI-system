import { apiGet, apiPost, type AuthTokens } from './client'

export interface LoginRequest {
  email: string
  password: string
}

export interface RegisterRequest {
  email: string
  password: string
  confirmPassword: string
}

export interface UserInfo {
  user_id: number
  logout_at?: string
  created_at?: string
  update_at?: string
  email: string
  user_name: string
  avatar: string
}

export function login(req: LoginRequest) {
  return apiPost<AuthTokens, LoginRequest>('/douyin/user/login', req)
}

export function register(req: RegisterRequest) {
  return apiPost<AuthTokens, RegisterRequest>('/douyin/user/register', req)
}

export function logout(token?: string | null) {
  return apiPost<Record<string, never>, Record<string, never>>('/douyin/user/logout', {}, token)
}

export function getUserInfo(token?: string | null) {
  return apiGet<UserInfo>('/douyin/user/info', token)
}
