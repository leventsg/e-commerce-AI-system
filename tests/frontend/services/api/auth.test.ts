import { describe, expect, it, vi } from 'vitest'
import { getUserInfo, login } from '../../../../frontend/src/services/api/auth'

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
})
