import { describe, expect, it, vi } from 'vitest'
import {
  getUserInfo,
  login,
  register,
  resetForgottenPassword,
  sendEmailCode,
  sendPasswordCode,
  updatePassword,
} from '../../../../frontend/src/services/api/auth'

describe('auth service', () => {
  it('logs in with email and password', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      code: 0,
      msg: 'ok',
      data: { access_token: 'access', refresh_token: 'refresh' },
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(login({ email: 'you@example.com', password: 'secret' })).resolves.toEqual({
      access_token: 'access',
      refresh_token: 'refresh',
    })
    expect(fetchMock).toHaveBeenCalledWith('/douyin/user/login', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ email: 'you@example.com', password: 'secret' }),
    }))
  })

  it('loads current user info with the access token', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      code: 0,
      msg: 'ok',
      data: { user_id: 1, email: 'you@example.com', user_name: 'Alice', avatar: '' },
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(getUserInfo('access')).resolves.toMatchObject({ user_name: 'Alice' })
    expect(fetchMock).toHaveBeenCalledWith('/douyin/user/info', expect.objectContaining({
      method: 'GET',
      headers: expect.objectContaining({ 'Access-Token': 'access' }),
    }))
  })

  it('registers with an email verification code', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      code: 0,
      msg: 'ok',
      data: { access_token: 'access', refresh_token: 'refresh' },
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await register({ email: 'you@example.com', password: 'secret', confirmPassword: 'secret', code: '123456' })
    expect(fetchMock).toHaveBeenCalledWith('/douyin/user/register', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ email: 'you@example.com', password: 'secret', confirmPassword: 'secret', code: '123456' }),
    }))
  })

  it('sends public email codes for register and forgot password scenes', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await sendEmailCode({ email: 'you@example.com', scene: 'forgot_password' })
    expect(fetchMock).toHaveBeenCalledWith('/douyin/user/email/code', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ email: 'you@example.com', scene: 'forgot_password' }),
    }))
  })

  it('resets a forgotten password with email and code', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await resetForgottenPassword({ email: 'you@example.com', code: '123456', password: 'newsecret', confirmPassword: 'newsecret' })
    expect(fetchMock).toHaveBeenCalledWith('/douyin/user/password/reset', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ email: 'you@example.com', code: '123456', password: 'newsecret', confirmPassword: 'newsecret' }),
    }))
  })

  it('updates current user password with authenticated endpoints', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await sendPasswordCode('access')
    await updatePassword({ code: '123456', password: 'newsecret', confirmPassword: 'newsecret' }, 'access')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/douyin/user/password/code', expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'Access-Token': 'access' }),
    }))
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/douyin/user/password', expect.objectContaining({
      method: 'PUT',
      headers: expect.objectContaining({ 'Access-Token': 'access' }),
      body: JSON.stringify({ code: '123456', password: 'newsecret', confirmPassword: 'newsecret' }),
    }))
  })
})
