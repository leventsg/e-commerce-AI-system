import { describe, expect, it, vi } from 'vitest'
import { apiGet, apiPost, ApiError, setTokenRefreshHandler } from '../../../../frontend/src/services/api/client'

describe('api client', () => {
  it('unwraps successful business responses', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      code: 0,
      msg: 'ok',
      data: { name: 'Alice' },
    }), { status: 200 })))

    await expect(apiGet<{ name: string }>('/douyin/user/info', 'access')).resolves.toEqual({ name: 'Alice' })
  })

  it('uses Access-Token header and include credentials', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await apiPost('/douyin/carts/add', { product_id: 1 }, 'access')

    expect(fetchMock).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({
      credentials: 'include',
      headers: expect.objectContaining({ 'Access-Token': 'access' }),
    }))
  })

  it('updates tokens and retries once when backend returns token renewed code', async () => {
    const refreshHandler = vi.fn()
    setTokenRefreshHandler(refreshHandler)
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        code: 10004,
        msg: '令牌续期成功',
        data: { access_token: 'new-access', refresh_token: 'new-refresh' },
      }), { status: 200 }))
      .mockResolvedValueOnce(new Response(JSON.stringify({ code: 0, msg: 'ok', data: { ok: true } }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiGet('/douyin/user/info', 'old-access')).resolves.toEqual({ ok: true })
    expect(refreshHandler).toHaveBeenCalledWith({ access_token: 'new-access', refresh_token: 'new-refresh' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('dispatches auth-unauthorized on business auth failure', async () => {
    const listener = vi.fn()
    window.addEventListener('auth-unauthorized', listener)
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ code: 10007, msg: '令牌无效' }), { status: 200 })))

    await expect(apiGet('/douyin/user/info', 'bad-token')).rejects.toBeInstanceOf(ApiError)
    expect(listener).toHaveBeenCalled()
  })

  it('does not retry failed writes when backend marks business_executed', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({
      code: 500,
      msg: '业务结果已产生，但消息保存失败，请勿重复操作',
      data: { business_executed: true },
    }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiPost('/douyin/order/create', { pre_order_id: 'pre_1' }, 'access')).rejects.toMatchObject({
      data: { business_executed: true },
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
