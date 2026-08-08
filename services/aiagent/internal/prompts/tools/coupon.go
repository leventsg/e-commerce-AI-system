package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	// CouponList
	CouponListDesc = `【职责】
查询平台所有可领取的优惠券列表，支持按类型筛选和分页。

【调用时机】
- 用户问"有什么优惠券"、"有没有满减券"、"看看优惠活动"
- 用户想了解当前可用的优惠券再决定是否领取
- 用户在购物前想看看有没有合适的券

【不要调用】
- 用户想看我已领取的券 → 用 coupon_my_list
- 用户想看某张券的详情 → 用 coupon_detail
- 用户想使用优惠券计算折扣 → 用 coupon_calculate

【前置条件】
- type 可选筛选：1=满减券, 2=折扣券, 3=立减券
- page / page_size 控制分页

【执行限制】
- 只读操作，无风险，无需用户确认
- 所有参数均为可选`

	CouponListParameters = map[string]*schema.ParameterInfo{
		"page":      {Type: schema.Integer, Desc: "Page number starting from 1.", Required: false},
		"page_size": {Type: schema.Integer, Desc: "Number of coupons to return.", Required: false},
		"type":      {Type: schema.Integer, Desc: "Optional coupon type: 1 full reduction, 2 discount, 3 fixed amount.", Required: false},
	}

	// CouponDetail
	CouponDetailDesc = `【职责】
查询单张优惠券的详细信息，包括券面值、最低消费门槛、有效期、剩余数量。

【调用时机】
- 用户问"这张券怎么用"、"满多少才能用"、"什么时候到期"
- 用户在领取前想了解优惠券的具体规则
- 从 coupon_list 结果中用户想进一步查看某张券

【不要调用】
- 用户想浏览所有可领券 → 用 coupon_list
- 用户想看自己已领的券 → 用 coupon_my_list
- 用户想计算券的实际折扣 → 用 coupon_calculate

【前置条件】
- 必须知道 coupon_id（来自 coupon_list 或用户提供）

【执行限制】
- 只读操作，无风险，无需用户确认
- coupon_id 为必填参数`

	CouponDetailParameters = map[string]*schema.ParameterInfo{
		"coupon_id": {Type: schema.String, Desc: "Coupon ID.", Required: true},
	}

	// CouponClaim
	CouponClaimDesc = `【职责】
为当前用户领取一张优惠券，领取后该券会出现在用户的券包中。

【调用时机】
- 用户说"领这张券"、"帮我领取优惠券"、"我要这个券"
- 用户在 coupon_detail 查看后决定领取

【不要调用】
- 用户只是想看券的详情 → 用 coupon_detail
- 用户想浏览可领券列表 → 用 coupon_list
- 优惠券已领完或已过期 → 系统会返回错误，告知用户

【前置条件】
- 必须知道 coupon_id（来自 coupon_list / coupon_detail 结果）
- 优惠券需仍有剩余数量且在有效期内

【执行限制】
- 低风险写操作，无需用户确认，直接执行
- 同一用户对同一券通常只能领取一次
- coupon_id 为必填参数`

	CouponClaimParameters = map[string]*schema.ParameterInfo{
		"coupon_id": {Type: schema.String, Desc: "Coupon ID.", Required: true},
	}

	// CouponMyList
	CouponMyListDesc = `【职责】
查询当前用户已领取的优惠券列表，支持按状态筛选（可用/已用/已过期）。

【调用时机】
- 用户问"我的优惠券"、"我领了哪些券"、"还有哪些券能用"
- 用户在结算前想确认自己有哪些可用的券
- 用户想查看券的使用状态

【不要调用】
- 用户想浏览平台所有可领券 → 用 coupon_list
- 用户想看券的使用明细记录 → 用 coupon_usage_list
- 用户想计算某张券的折扣 → 用 coupon_calculate

【前置条件】
- 无需特定前置信息，自动获取当前用户的券包
- status 可选筛选券的状态

【执行限制】
- 只读操作，无风险，无需用户确认
- 所有参数均为可选`

	CouponMyListParameters = map[string]*schema.ParameterInfo{
		"page":      {Type: schema.Integer, Desc: "Page number starting from 1.", Required: false},
		"page_size": {Type: schema.Integer, Desc: "Number of user coupons to return.", Required: false},
		"status":    {Type: schema.String, Desc: "Optional user coupon status filter.", Required: false},
	}

	// CouponUsageList
	CouponUsageListDesc = `【职责】
查询当前用户优惠券的使用历史记录，包括哪张券在哪个订单中使用、抵扣了多少金额。

【调用时机】
- 用户问"我用过哪些券"、"这张券什么时候用的"、"优惠券使用记录"
- 用户想核对券的使用情况和抵扣金额

【不要调用】
- 用户想看当前持有的券 → 用 coupon_my_list
- 用户想浏览可领券 → 用 coupon_list
- 用户问的是订单详情 → 用 order_get

【前置条件】
- 无需特定前置信息，自动获取当前用户的使用记录

【执行限制】
- 只读操作，无风险，无需用户确认
- page / page_size 为可选参数`

	CouponUsageListParameters = map[string]*schema.ParameterInfo{
		"page":      {Type: schema.Integer, Desc: "Page number starting from 1.", Required: false},
		"page_size": {Type: schema.Integer, Desc: "Number of usage records to return.", Required: false},
	}

	// CouponCalculate
	CouponCalculateDesc = `【职责】
试算优惠券在指定商品上的折扣效果，返回原价、折扣金额、最终应付金额，以及券是否可用及不可用原因。纯计算，不会锁定或消耗优惠券。

【调用时机】
- 用户问"用这张券能省多少"、"这个券能用吗"、"帮我算一下优惠"
- 用户在 checkout_detail 中看到金额后想确认优惠券效果
- 用户在多张券之间犹豫，想比较哪个更划算

【不要调用】
- 用户还没有选定商品 → 先让用户浏览商品
- 用户还没有选择具体的券 → 先用 coupon_list / coupon_my_list
- 用户想直接下单 → 优惠券在 order_create 时会自动校验

【前置条件】
- coupon_id：要试算的优惠券 ID（来自 coupon_my_list 或 coupon_list）
- items：要试算的商品列表，每个商品需 product_id 和 quantity

【执行限制】
- 只读操作，不会消耗或锁定优惠券，无风险，无需用户确认
- 返回 is_usable 标明券是否可用，unusable_reason 说明不可用原因
- coupon_id 和 items 为必填参数`
	CouponCalculateParameters = map[string]*schema.ParameterInfo{
		"coupon_id": {Type: schema.String, Desc: "Coupon ID.", Required: true},
		"items": {
			Type:     schema.Array,
			Desc:     "Products and quantities used to calculate the discount.",
			Required: true,
			ElemInfo: &schema.ParameterInfo{
				Type: schema.Object,
				SubParams: map[string]*schema.ParameterInfo{
					"product_id": {Type: schema.Integer, Desc: "Product ID.", Required: true},
					"quantity":   {Type: schema.Integer, Desc: "Product quantity.", Required: true},
				},
			},
		},
	}
)
