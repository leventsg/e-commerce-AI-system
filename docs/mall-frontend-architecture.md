# Go Mall C 端前端总体架构设计

## 1. 项目定位

该前端系统面向普通消费者，基于 React + Next.js 构建，承载完整的电商购物闭环：

1. 用户注册、登录、个人中心
2. 商品浏览、商品详情、加购
3. 购物车管理
4. 预结算、优惠券试算、地址选择
5. 订单创建、支付、订单查询
6. 我的优惠券、支付记录、地址簿管理

这套前端不只是“页面集合”，而应当是一个围绕“浏览 -> 下单 -> 支付 -> 履约查询”主链路组织起来的 C 端商城框架。

## 2. 建议的前端技术方案

### 2.1 核心框架

- `Next.js 15+`：采用 App Router，方便做服务端渲染、路由分组、BFF 代理和 SEO。
- `React 19`：承载组件化开发与交互逻辑。
- `TypeScript`：统一接口类型、领域模型和状态约束。

### 2.2 状态与数据层

- `TanStack Query`：管理商品、购物车、订单、优惠券、地址等服务端状态。
- `Zustand`：仅保存轻量客户端状态，如购物车侧边抽屉开关、结算页临时 UI 状态、筛选面板开关。
- `React Hook Form + Zod`：处理登录、注册、地址表单、支付选择等输入场景。

### 2.3 UI 与工程能力

- `Tailwind CSS + CSS Variables`：统一设计令牌，支持电商 UI 的快速搭建。
- `shadcn/ui` 或自研基础组件层：沉淀 Button、Card、Drawer、Tabs、Dialog、Form、Toast。
- `next/font`：建议采用一组“展示字体 + 正文字体”的组合，做出更有消费感和品牌感的首页与活动氛围。

## 3. 视觉方向

建议整体视觉走“轻奢编辑型商城”方向，而不是通用后台感页面：

- 首页强调大图、专题、优惠券入口和精选商品瀑布流。
- 商品详情强调图片、价格、库存、销量和购买动作。
- 结算和支付页强调信息清晰、节奏紧凑、少打扰。
- 个人中心强调订单、资产、地址、优惠券的一站式管理。

这样既符合 `frontend-design` 的要求，也更适合 C 端电商的转化目标。

## 4. 总体分层

建议采用四层结构：

### 4.1 路由层

由 Next.js `app/` 目录承载页面与布局，负责：

- 页面切分
- 路由守卫
- SEO 元信息
- SSR/Streaming

### 4.2 业务模块层

以领域拆分模块，而不是按页面散落逻辑：

- `auth`
- `user`
- `product`
- `cart`
- `checkout`
- `coupon`
- `order`
- `payment`

每个模块内部包含：

- `api`
- `types`
- `hooks`
- `components`
- `utils`

### 4.3 BFF / API Client 层

前端不要直接在所有组件里散写接口调用，统一通过两层处理：

1. 浏览器调用 Next.js 的 `app/api/*` Route Handlers
2. Route Handlers 再去请求 Go Mall 后端服务

这样做的原因很重要：

- 后端鉴权依赖 `Access-Token` header 和 `Refresh-Token` cookie
- 登录接口当前只返回 token，并没有看到后端直接设置 cookie
- 因此前端最好通过 BFF 统一写入、刷新、转发 token，避免把刷新逻辑散在浏览器端

### 4.4 基础设施层

- 请求封装
- 错误码处理
- 日志埋点
- 主题与设计令牌
- 图片与资源处理

## 5. 推荐目录结构

```text
src/
  app/
    (marketing)/
      page.tsx
      products/
        page.tsx
        [id]/
          page.tsx
    (trade)/
      cart/
        page.tsx
      checkout/
        page.tsx
        [preOrderId]/
          page.tsx
      pay/
        [orderId]/
          page.tsx
    (account)/
      account/
        layout.tsx
        profile/
          page.tsx
        address/
          page.tsx
        coupons/
          page.tsx
        orders/
          page.tsx
          [orderId]/
            page.tsx
        payments/
          page.tsx
    auth/
      login/
        page.tsx
      register/
        page.tsx
    api/
      auth/
        login/route.ts
        logout/route.ts
        refresh/route.ts
      mall/
        product/...
        carts/...
        checkout/...
        order/...
        payment/...
        coupon/...
        user/...
  modules/
    auth/
    user/
    product/
    cart/
    checkout/
    coupon/
    order/
    payment/
  components/
    layout/
    common/
    feedback/
  lib/
    request/
    auth/
    constants/
    utils/
  styles/
    globals.css
```

## 6. 页面与模块职责

### 6.1 首页与商品域 `product`

对应接口：

- `GET /douyin/product/list`
- `GET /douyin/product?id=...`

承担职责：

- 首页商品推荐区
- 商品列表页
- 商品详情页
- “立即购买”和“加入购物车”的主入口

模块内部建议拆成：

- `ProductList`
- `ProductCard`
- `ProductDetail`
- `ProductPrice`
- `ProductPurchaseBar`

### 6.2 认证与用户域 `auth` + `user`

对应接口：

- `POST /douyin/user/register`
- `POST /douyin/user/login`
- `POST /douyin/user/logout`
- `GET /douyin/user/info`
- `PUT /douyin/user/update`
- 地址相关接口 `user/address*`

承担职责：

- 登录、注册、登出
- 用户资料展示和修改
- 收货地址管理
- 个人中心基础信息展示

前端重点：

- 登录成功后由 BFF 写入 `Refresh-Token` cookie
- `Access-Token` 建议通过服务端 cookie 或受控 session 方案统一管理
- 个人中心 layout 统一预取用户信息和快捷入口数据

### 6.3 购物车域 `cart`

对应接口：

- `GET /douyin/carts/list`
- `POST /douyin/carts/add`
- `POST /douyin/carts/sub`
- `DELETE /douyin/carts/delete`

承担职责：

- 购物车列表页
- 顶部购物车数量徽标
- 商品数量增减
- 删除购物车商品

前端注意：

- 当前接口是按 `product_id` 加减，没有看到“直接设置数量”的接口
- 因此数量步进器应基于 `add` / `sub` 实现
- 购物车选择态可能需要前端本地维护，因为后端接口未暴露“选中/取消选中”

### 6.4 预结算域 `checkout`

对应接口：

- `POST /douyin/checkout/prepare`
- `GET /douyin/checkout/list`
- `GET /douyin/checkout/detail`

承担职责：

- 从购物车或立即购买进入预结算
- 生成 `pre_order_id`
- 查看预结算详情
- 管理待提交的订单草稿

这是一条非常关键的桥接链路：

- 商品详情页“立即购买”可直接构造 `order_items`
- 购物车页“去结算”从购物车数据生成 `order_items`
- 预结算返回 `pre_order_id`
- 创建订单时再使用 `pre_order_id`

### 6.5 优惠券域 `coupon`

对应接口：

- `GET /douyin/coupon/list`
- `GET /douyin/coupon/detail`
- `POST /douyin/coupon/claim`
- `GET /douyin/coupon/calculate`
- `GET /douyin/coupon/my/list`
- `GET /douyin/coupon/my/usage`

承担职责：

- 优惠券广场
- 优惠券详情
- 领券
- 结算页优惠券试算
- 我的优惠券
- 优惠券使用记录

与其他模块的关系最强：

- 与商品域联动：详情页可展示可领券入口
- 与结算域联动：用于试算折扣
- 与订单域联动：创建订单时提交 `coupon_id`

### 6.6 订单域 `order`

对应接口：

- `POST /douyin/order/create`
- `POST /douyin/order/cancel`
- `GET /douyin/order/detail`
- `GET /douyin/order/list`

承担职责：

- 订单创建
- 订单列表
- 订单详情
- 取消订单

推荐在前端统一维护订单状态映射：

- `1` 创建
- `2` 待支付
- `3` 已支付
- `4` 已完成
- `5` 已取消
- `6` 已关闭
- `7` 退款中/退款

### 6.7 支付域 `payment`

对应接口：

- `POST /douyin/payment/create`
- `GET /douyin/payment/list`

承担职责：

- 订单发起支付
- 支付链接展示
- 支付记录查询

支付方式映射建议：

- `1` 微信支付
- `2` 支付宝

支付状态映射建议：

- `1` 未支付
- `2` 已支付
- `3` 支付失败
- `4` 全额退款
- `5` 已过期

## 7. 模块之间的关系

### 7.1 主交易链路

```text
商品列表/商品详情
  -> 加入购物车 或 立即购买
  -> 预结算 checkout/prepare
  -> 选择地址
  -> 选择优惠券并试算
  -> 创建订单 order/create
  -> 发起支付 payment/create
  -> 订单详情 / 支付记录 / 订单列表
```

### 7.2 账户资产链路

```text
登录
  -> 获取用户信息
  -> 管理地址
  -> 查看我的优惠券
  -> 查看订单
  -> 查看支付记录
```

### 7.3 优惠券联动链路

```text
优惠券列表/详情
  -> 领券
  -> 我的优惠券
  -> 结算试算
  -> 下单时带 coupon_id
```

## 8. 路由设计建议

### 8.1 对外页面路由

- `/`：首页
- `/products`：商品列表
- `/products/[id]`：商品详情
- `/cart`：购物车
- `/checkout`：提交订单入口页
- `/checkout/[preOrderId]`：预结算详情页
- `/pay/[orderId]`：支付页
- `/account/profile`：个人信息
- `/account/address`：地址簿
- `/account/coupons`：我的优惠券
- `/account/orders`：订单列表
- `/account/orders/[orderId]`：订单详情
- `/account/payments`：支付记录
- `/auth/login`
- `/auth/register`

### 8.2 路由守卫

公开访问：

- 首页
- 商品列表
- 商品详情
- 登录注册

登录后访问：

- 购物车
- 结算
- 个人中心
- 订单、支付、优惠券、地址

不过这里有一个必须注意的后端现实：

- 当前各 API 服务都挂了 `WrapperAuthMiddleware`
- 从代码看，商品接口也可能需要 token
- 如果后端未给商品接口开白名单，前端必须实现“游客 token”或推动后端调整白名单

## 9. 数据请求策略

### 9.1 SSR 适合的页面

- 首页
- 商品列表
- 商品详情
- 订单详情

### 9.2 CSR 适合的页面

- 购物车实时数量变化
- 地址弹窗编辑
- 优惠券领取与试算
- 支付轮询或支付结果刷新

### 9.3 缓存策略

- 商品列表与详情：短时缓存，可 `revalidate`
- 购物车、订单、地址、优惠券：用户态数据，走 React Query
- 支付与结算：强一致优先，不做长缓存

## 10. API Client 设计建议

建议统一封装为：

- `serverRequest()`：在 Next.js 服务端组件和 Route Handler 使用
- `clientRequest()`：在浏览器端调用 BFF 使用
- `handleBizCode()`：统一处理后端 `code/msg`
- `refreshAndRetry()`：处理 token 续签与重试

推荐实现原则：

1. 浏览器永远优先请求自己的 `/api/*`
2. `/api/*` 再转发给 Go 后端
3. token 续签只在 BFF 层处理
4. UI 层只关心成功数据和业务错误文案

## 11. 组件体系建议

### 11.1 通用组件

- `AppHeader`
- `AppFooter`
- `SearchBar`
- `EmptyState`
- `ErrorState`
- `PriceTag`
- `QuantityStepper`
- `CouponBadge`
- `AddressCard`
- `OrderStatusTag`

### 11.2 业务组件

- `ProductCard`
- `ProductGallery`
- `CartItemCard`
- `CheckoutSummary`
- `CouponPickerDrawer`
- `PaymentMethodSelector`
- `OrderTimeline`

## 12. 当前接口对前端设计的影响

这是落地时最需要提前统一的几件事：

### 12.1 登录态管理

后端认证中间件依赖：

- `Access-Token` header
- `Refresh-Token` cookie

但当前登录逻辑只看到返回 token，没有看到显式下发 cookie。  
所以前端架构应默认采用 `Next.js BFF` 写 cookie，而不是纯浏览器直连后端。

### 12.2 GET 接口参数风格

文档里部分 GET 接口示例写成 body，但前端实现时建议统一转成 query 参数，例如：

- `product?id=1`
- `order/detail?order_id=...`
- `checkout/detail?pre_order_id=...`

### 12.3 商品筛选能力不足

当前商品接口只有分页和详情，没有搜索、分类筛选、排序接口。  
因此商品列表页可以先做基础分页框架，但“搜索/筛选/排序”建议设计成可扩展占位能力。

### 12.4 购物车能力较轻

当前购物车接口支持：

- 加一
- 减一
- 删除
- 查询列表

没有批量更新、批量删除、勾选状态、直接设置数量接口。  
因此购物车 UI 需要更偏轻量实现，复杂交互先放在前端本地状态层。

## 13. 推荐的实施优先级

### Phase 1：商城主链路

- 首页
- 商品列表
- 商品详情
- 登录注册
- 购物车

### Phase 2：交易闭环

- 预结算
- 地址管理
- 优惠券选择与试算
- 创建订单
- 支付页

### Phase 3：用户中心

- 订单列表与详情
- 我的优惠券
- 支付记录
- 用户资料

## 14. 结论

这套前端最适合按“领域模块 + App Router + BFF”来搭：

- `product` 负责流量入口
- `cart` 负责购物过程承接
- `checkout` 负责下单前的统一编排
- `coupon` 和 `address` 作为交易增强模块
- `order` 和 `payment` 负责闭环收口
- `user` 负责账户与资产中心

模块之间最核心的依赖关系是：

`product/cart -> checkout -> order -> payment -> account`

如果你后面准备继续往前走，下一步最适合直接做的是：

1. 先把这个方案落成一个 Next.js 项目骨架
2. 然后优先实现 `BFF 鉴权层 + 商品域 + 购物车域`
3. 再接 `checkout/order/payment` 主交易链路
