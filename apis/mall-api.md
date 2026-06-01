# API 接口文档

## User 模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| GET | `user/info` | 获取用户信息 |
| POST | `user/login` | 用户登录 |
| POST | `user/register` | 用户注册 |
| POST | `user/logout` | 用户登出 |
| PUT | `user/update` | 更新用户信息 |
| POST | `user/delete` | 删除用户 |

---

## 用户地址模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| POST | `user/address` | 添加用户地址 |
| GET | `user/address` | 获取单个地址 |
| GET | `user/address/list` | 获取所有地址信息 |
| PUT | `user/address` | 更新地址 |
| DELETE | `user/address` | 删除地址 |

---

## Product 模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| GET | `product/list` | 获取商品列表 |
| GET | `/product` | 获取商品详细信息 |

---

## Carts 模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| GET | `carts/list` | 获取购物车 |
| POST | `/carts/add` | 添加商品到购物车 |
| POST | `/carts/sub` | 扣减购物车商品 |
| DELETE | `carts/delete` | 删除购物车商品 |

---

## Order 模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| POST | `order/create` | 创建订单 |
| POST | `order/cancel` | 取消订单 |
| GET | `order/detail` | 获取订单详情 |
| GET | `order/list` | 获取订单列表 |

---

## Payment 模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| GET | `/payment/list` | 获取支付记录 |
| POST | `payment/create` | 创建支付 |

---

## Coupon 模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| GET | `/coupon/detail` | 获取优惠券详情 |
| POST | `/coupon/claim` | 领取优惠券 |
| GET | `/coupon/list` | 获取优惠券列表 |
| GET | `/coupon/my/list?page=1&size=10` | 获取我的优惠券 |
| GET | `/coupon/my/usage?page=1&size=10` | 获取优惠券使用记录 |
| GET | `coupon/calculate` | 计算优惠券优惠金额 |

---

## Checkout 模块

| 请求方法 | 接口 URL | 接口作用 |
|---|---|---|
| GET | `checkout/list` | 获取结算列表 |
| POST | `checkout/prepare` | 创建预结算 |
| GET | `/checkout/detail` | 获取结算详情 |
