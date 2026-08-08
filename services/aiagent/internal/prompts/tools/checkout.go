package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	// CheckoutPrepare
	CheckoutPrepareDesc = `【职责】
对用户选定的商品发起结算，生成预订单（pre_order_id），同时预扣库存确保商品不会被他人抢走。结算有效期内用户可随时下单。

【调用时机】
- 用户选好商品后说"结算"、"下单"、"去买单"、"结账"
- 用户已从购物车或商品详情中明确了要购买的商品和数量
- 用户想使用优惠券 → 可传入 coupon_id 参与结算

【不要调用】
- 用户还没有选定商品 → 先用 product_search / product_detail 浏览
- 用户只是想加购物车 → 用 cart_add
- 用户只想看结算详情（已有 pre_order_id）→ 用 checkout_detail
- 用户想直接创建正式订单 → 必须先完成结算获得 pre_order_id

【前置条件】
- order_items：必须明确每个商品的 product_id 和购买数量
- coupon_id 可选，来自 coupon_list / coupon_my_list 结果
- 商品信息通常来自购物车 (cart_list) 或用户直接指定

【执行限制】
- 低风险写操作（仅预扣库存，未真正扣款），无需用户确认
- 结算有有效期（约30分钟），过期后库存自动释放
- 相同商品+数量的重复结算会被幂等拦截（5分钟内返回已有 pre_order_id）
- order_items 为必填，coupon_id 为可选`

	CheckoutPrepareParameters = map[string]*schema.ParameterInfo{
		"order_items": {
			Type:     schema.Array,
			Desc:     "Products and quantities to reserve for checkout.",
			Required: true,
			ElemInfo: &schema.ParameterInfo{
				Type: schema.Object,
				SubParams: map[string]*schema.ParameterInfo{
					"product_id": {Type: schema.Integer, Desc: "Product ID.", Required: true},
					"quantity":   {Type: schema.Integer, Desc: "Purchase quantity.", Required: true},
				},
			},
		},
		"coupon_id": {Type: schema.String, Desc: "Coupon ID to apply during checkout.", Required: false},
	}

	// CheckoutDetail
	CheckoutDetailDesc = `【职责】
查看结算预订单的详细信息，包括商品明细快照、原始金额、最终金额、优惠券折扣、过期时间。

【调用时机】
- 用户完成结算后说"看看结算详情"、"确认一下订单信息"
- 检查结算金额是否正确、优惠券是否生效
- 在下单 (order_create) 之前做最后确认

【不要调用】
- 用户还没有进行 checkout_prepare → 先引导完成结算
- 用户想看已创建的正式订单 → 用 order_get
- 用户想修改结算内容 → 当前不支持修改，需等待过期后重新结算

【前置条件】
- 必须已完成 checkout_prepare 获得 pre_order_id

【执行限制】
- 只读操作，无风险，无需用户确认
- 自动校验当前用户权限
- pre_order_id 为必填参数`
	CheckoutDetailParameters = map[string]*schema.ParameterInfo{
		"pre_order_id": {Type: schema.String, Desc: "Pre-order ID.", Required: true},
	}
)
