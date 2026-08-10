import { apiDelete, apiGet, apiPost, apiPut } from './client'

export interface PageParams {
  page?: number
  page_size?: number
  size?: number
}

export const productApi = {
  list: (params: { page?: number; size?: number }, token?: string | null) =>
    apiGet(`/douyin/product/list?${toQuery(params)}`, token),
  detail: (id: number, token?: string | null) =>
    apiGet(`/douyin/product?${toQuery({ id })}`, token),
}

export const cartApi = {
  list: (token?: string | null) => apiGet('/douyin/carts/list', token),
  add: (product_id: number, token?: string | null) => apiPost('/douyin/carts/add', { product_id }, token),
  sub: (product_id: number, token?: string | null) => apiPost('/douyin/carts/sub', { product_id }, token),
  delete: (product_id: number, token?: string | null) => apiDelete('/douyin/carts/delete', { product_id }, token),
}

export const couponApi = {
  list: (params: { page?: number; size?: number; type?: number }, token?: string | null) =>
    apiGet(`/douyin/coupon/list?${toQuery(params)}`, token),
  detail: (coupon_id: string, token?: string | null) =>
    apiGet(`/douyin/coupon/detail?${toQuery({ coupon_id })}`, token),
  claim: (coupon_id: string, token?: string | null) =>
    apiPost('/douyin/coupon/claim', { coupon_id }, token),
  myList: (params: { page?: number; size?: number }, token?: string | null) =>
    apiGet(`/douyin/coupon/my/list?${toQuery(params)}`, token),
  usage: (params: { page?: number; size?: number }, token?: string | null) =>
    apiGet(`/douyin/coupon/my/usage?${toQuery(params)}`, token),
  // 后端当前将 coupon/calculate 定义为 GET + items[]；若 httpx 无法稳定解析对象数组，应改为 POST JSON。
  calculate: (params: { coupon_id: string; items: Array<{ product_id: number; quantity: number }> }, token?: string | null) =>
    apiGet(`/douyin/coupon/calculate?${toQuery(params as unknown as Record<string, unknown>)}`, token),
}

export const checkoutApi = {
  prepare: (data: { coupon_id?: string; order_items: Array<{ product_id: number; quantity: number }> }, token?: string | null) =>
    apiPost('/douyin/checkout/prepare', data, token),
  list: (params: { page?: number; page_size?: number }, token?: string | null) =>
    apiGet(`/douyin/checkout/list?${toQuery(params)}`, token),
  detail: (pre_order_id: string, token?: string | null) =>
    apiGet(`/douyin/checkout/detail?${toQuery({ pre_order_id })}`, token),
}

export const orderApi = {
  create: (data: { pre_order_id: string; coupon_id?: string; address_id: number; payment_method: number }, token?: string | null) =>
    apiPost('/douyin/order/create', data, token),
  cancel: (data: { order_id: string; cancel_reason?: string }, token?: string | null) =>
    apiPost('/douyin/order/cancel', data, token),
  detail: (order_id: string, token?: string | null) =>
    apiGet(`/douyin/order/detail?${toQuery({ order_id })}`, token),
  list: (params: { statuses?: number[]; page?: number; page_size?: number }, token?: string | null) =>
    apiGet(`/douyin/order/list?${toQuery(params)}`, token),
}

export const paymentApi = {
  create: (data: { order_id: string; payment_method: number }, token?: string | null) =>
    apiPost('/douyin/payment/create', data, token),
  list: (params: { method?: number; page?: number; page_size?: number }, token?: string | null) =>
    apiGet(`/douyin/payment/list?${toQuery(params)}`, token),
}

export const addressApi = {
  list: (token?: string | null) => apiGet('/douyin/user/address/list', token),
  detail: (address_id: number, token?: string | null) =>
    apiGet(`/douyin/user/address?${toQuery({ address_id })}`, token),
  add: (data: Record<string, unknown>, token?: string | null) =>
    apiPost('/douyin/user/address', data, token),
  update: (data: Record<string, unknown>, token?: string | null) =>
    apiPut('/douyin/user/address', data, token),
  delete: (address_id: number, token?: string | null) =>
    apiDelete('/douyin/user/address', { address_id }, token),
}

function toQuery(params: Record<string, unknown>) {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value === undefined || value === null || value === '') return
    if (Array.isArray(value)) {
      value.forEach(item => search.append(key, typeof item === 'object' ? JSON.stringify(item) : String(item)))
      return
    }
    search.set(key, String(value))
  })
  return search.toString()
}
