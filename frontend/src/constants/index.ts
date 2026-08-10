export const API_BASE = import.meta.env.VITE_API_BASE || ''
export const CONVERSATIONS_PAGE_SIZE = 30

export const STORAGE_KEYS = {
  TOKEN: 'go-mall-token',
  REFRESH_TOKEN: 'go-mall-refresh-token',
  USERNAME: 'go-mall-username',
  USER: 'go-mall-user',
  THEME: 'go-mall-theme',
} as const

/** 20 个工具的中文名映射 */
export const TOOL_DISPLAY_NAMES: Record<string, string> = {
  product_search: '商品搜索',
  product_detail: '商品详情',
  product_recommend: '商品推荐',
  inventory_get: '库存查询',
  order_get: '订单详情',
  order_list: '订单列表',
  order_create: '创建订单',
  order_cancel: '取消订单',
  checkout_prepare: '结算准备',
  checkout_detail: '结算详情',
  cart_list: '购物车列表',
  cart_add: '添加购物车',
  cart_sub: '减少购物车',
  cart_delete: '删除购物车',
  coupon_list: '优惠券列表',
  coupon_detail: '优惠券详情',
  coupon_claim: '领取优惠券',
  coupon_my_list: '我的优惠券',
  coupon_usage_list: '使用记录',
  coupon_calculate: '试算优惠',
}

/** 工具图标（emoji） */
export const TOOL_ICONS: Record<string, string> = {
  product_search: '🔍', product_detail: '📋', product_recommend: '✨',
  inventory_get: '📦', order_get: '🧾', order_list: '📋',
  order_create: '🛒', order_cancel: '❌',
  checkout_prepare: '💳', checkout_detail: '📊',
  cart_list: '🛒', cart_add: '➕', cart_sub: '➖', cart_delete: '🗑️',
  coupon_list: '🎫', coupon_detail: '🎟️', coupon_claim: '🎁',
  coupon_my_list: '🎫', coupon_usage_list: '📜', coupon_calculate: '🧮',
}

/** 子 Agent 定义 */
export const SUB_AGENTS = [
  { name: 'product_agent', display: '商品 Agent', tools: ['product_search','product_detail','product_recommend','inventory_get'] },
  { name: 'order_agent', display: '订单 Agent', tools: ['order_get','order_list','order_cancel'] },
  { name: 'cart_checkout_agent', display: '购物车结算 Agent', tools: ['cart_list','cart_add','cart_sub','cart_delete','checkout_prepare','checkout_detail','order_create'] },
  { name: 'coupon_agent', display: '优惠券 Agent', tools: ['coupon_list','coupon_detail','coupon_claim','coupon_my_list','coupon_usage_list','coupon_calculate'] },
]

/** 高风险工具列表 */
export const HIGH_RISK_TOOLS = ['cart_delete', 'order_create', 'order_cancel']
