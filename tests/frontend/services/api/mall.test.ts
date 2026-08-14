import { describe, expect, it, vi } from 'vitest'
import { cartApi, couponApi, orderApi, productApi } from '../../../../frontend/src/services/api/mall'

describe('mall service', () => {
  it('builds product list and detail requests', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await productApi.list({ page: 2, size: 20 }, 'access')
    await productApi.detail(12, 'access')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/douyin/product/list?page=2&size=20', expect.any(Object))
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/douyin/product?id=12', expect.any(Object))
  })

  it('wraps cart delete and order cancel without changing high-risk semantics', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await cartApi.delete(9, 'access')
    await orderApi.cancel({ order_id: 'ord_1', cancel_reason: '不想买了' }, 'access')

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/douyin/carts/delete', expect.objectContaining({
      method: 'DELETE',
      body: JSON.stringify({ product_id: 9 }),
    }))
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/douyin/order/cancel', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ order_id: 'ord_1', cancel_reason: '不想买了' }),
    }))
  })

  it('encodes coupon calculate items as repeated JSON query values', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ code: 0, msg: 'ok', data: {} }), { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await couponApi.calculate({ coupon_id: 'coupon_1', items: [{ product_id: 1, quantity: 2 }] }, 'access')

    const url = fetchMock.mock.calls[0][0] as string
    expect(url).toContain('/douyin/coupon/calculate?')
    expect(url).toContain('coupon_id=coupon_1')
    expect(decodeURIComponent(url)).toContain('items={"product_id":1,"quantity":2}')
  })
})
