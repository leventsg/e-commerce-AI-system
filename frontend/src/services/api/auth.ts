import { apiGet, apiPost, apiPut, type AuthTokens } from './client'

export interface LoginRequest {
  email: string
  password: string
}

export interface RegisterRequest {
  email: string
  password: string
  confirmPassword: string
  code: string
}

export interface EmailCodeRequest {
  email: string
  scene: 'register' | 'forgot_password'
}

export interface ForgotPasswordRequest {
  email: string
  code: string
  password: string
  confirmPassword: string
}

export interface UpdatePasswordRequest {
  code: string
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

export function sendEmailCode(req: EmailCodeRequest) {
  return apiPost<Record<string, never>, EmailCodeRequest>('/douyin/user/email/code', req)
}

export function resetForgottenPassword(req: ForgotPasswordRequest) {
  return apiPost<Record<string, never>, ForgotPasswordRequest>('/douyin/user/password/reset', req)
}

export function sendPasswordCode(token?: string | null) {
  return apiPost<Record<string, never>, Record<string, never>>('/douyin/user/password/code', {}, token)
}

export function updatePassword(req: UpdatePasswordRequest, token?: string | null) {
  return apiPut<Record<string, never>, UpdatePasswordRequest>('/douyin/user/password', req, token)
}

export function logout(token?: string | null) {
  return apiPost<Record<string, never>, Record<string, never>>('/douyin/user/logout', {}, token)
}

export function getUserInfo(token?: string | null) {
  return apiGet<UserInfo>('/douyin/user/info', token)
}
