package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	// OrderGet
	OrderGetDesc = `【职责】
根据订单 ID 查询单个订单的完整详情，包括商品明细、收货地址、金额信息。

【调用时机】
- 用户问"我的XX订单怎么样了"、"查一下订单号XXX"
- 用户指定了具体 order_id 想了解订单状态
- 从 order_list 结果中用户选中某个订单想查看详情

【不要调用】
- 用户想浏览所有订单 → 用 order_list
- 用户问的是结算预览而非已创建订单 → 用 checkout_detail
- 用户想取消订单 → 用 order_cancel（但可先调用本工具确认订单状态）

【前置条件】
- 必须知道 order_id（来自 order_list 结果或用户明确提供）

【执行限制】
- 只读操作，无风险，无需用户确认
- 自动校验当前用户权限，只能查看自己的订单
- order_id 为必填参数`

	OrderGetParameters = map[string]*schema.ParameterInfo{
		"order_id": {Type: schema.String, Desc: "Order ID.", Required: true},
	}

	// OrderList
	OrderListDesc = `【职责】
分页查询当前用户的订单列表，支持按订单状态筛选。

【调用时机】
- 用户问"我的订单有哪些"、"最近的订单"、"查看我的订单"
- 用户想了解特定状态的订单（如"待付款的订单"）
- 用户在进行售后操作前需要确认订单列表

【不要调用】
- 用户只想看某个具体订单 → 用 order_get
- 用户问的是购物车 → 用 cart_list
- 用户问的是结算预览 → 用 checkout_detail

【前置条件】
- 无需特定前置信息，自动获取当前用户的订单

【执行限制】
- 只读操作，无风险，无需用户确认
- page / page_size / status 均为可选参数`

	OrderListParameters = map[string]*schema.ParameterInfo{
		"page":      {Type: schema.Integer, Desc: "Page number starting from 1.", Required: false},
		"page_size": {Type: schema.Integer, Desc: "Number of orders to return.", Required: false},
		"status":    {Type: schema.String, Desc: "Optional order status filter.", Required: false},
	}

	// OrderCreate
	OrderCreateDesc = `【职责】
将已完成的结算（预订单）转为正式订单。操作不可逆，会真实扣减库存、锁定优惠券、扣款。

【调用时机】
- 用户在 checkout_detail 确认无误后说"下单"、"确认购买"、"提交订单"
- 用户已准备好收货地址和支付方式，明确表示要完成购买

【不要调用】
- 用户还没有进行 checkout_prepare → 先引导用户完成结算
- 用户还在犹豫/比较商品 → 不要主动下单
- 用户只是想看结算预览 → 用 checkout_detail
- 用户想取消已创建的订单 → 用 order_cancel（订单创建后才能取消）
- 用户没有确认收货地址和支付方式 → 先让用户补全信息

【前置条件】
- 必须先完成 checkout_prepare 获得 pre_order_id
- address_id：用户已选择的收货地址 ID
- payment_method：1=微信支付, 2=支付宝
- coupon_id：可选，用户在结算时选择使用的优惠券 ID

【执行限制】
- 高风险写操作，不可逆，执行前必须获得用户明确确认
- pre_order_id、address_id、payment_method 为必填参数
- 系统自动校验：预订单有效性、地址归属、优惠券可用性
- 同一 pre_order_id 不可重复创建订单（幂等保护）`

	OrderCreateParameters = map[string]*schema.ParameterInfo{
		"pre_order_id":   {Type: schema.String, Desc: "Pre-order ID.", Required: true},
		"coupon_id":      {Type: schema.String, Desc: "Coupon ID to use for order creation.", Required: false},
		"address_id":     {Type: schema.Integer, Desc: "Delivery address ID.", Required: true},
		"payment_method": {Type: schema.Integer, Desc: "Payment method: 1 WeChat Pay, 2 Alipay.", Required: true},
	}

	// OrderCancel
	OrderCancelDesc = `【职责】
取消用户已创建但尚未支付的订单，释放库存并退还已使用的优惠券。

【调用时机】
- 用户说"取消订单"、"不要这个订单了"、"帮我退掉XXX订单"
- 用户下单后改变主意，在支付前想取消
- 用户发现订单信息有误且无法修改，需要取消重下

【不要调用】
- 订单已支付 → 无法取消（系统会返回"已支付无法取消"错误，引导用户申请退款）
- 订单已完成/已取消/已关闭 → 状态不可变更
- 用户只是想改地址或改商品 → 当前系统不支持修改订单，需取消重下
- 用户只是询问订单状态 → 用 order_get

【前置条件】
- 必须知道 order_id（来自 order_list / order_get 结果）
- 订单状态必须为"待支付"，已支付/已完成/已退款/已取消的订单无法取消
- reason 可选，记录取消原因便于后续分析

【执行限制】
- 高风险写操作，不可逆，执行前必须获得用户明确确认
- 取消后库存和优惠券会自动退还
- 建议先调用 order_get 确认订单当前状态再执行取消
- order_id 为必填参数，reason 为可选参数`
	OrderCancelParameters = map[string]*schema.ParameterInfo{
		"order_id": {Type: schema.String, Desc: "Order ID.", Required: true},
		"reason":   {Type: schema.String, Desc: "Cancellation reason.", Required: false},
	}
)
