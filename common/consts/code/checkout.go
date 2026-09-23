package code

// 状态码
const (
	SettlementSuccess            = 60001
	SettlementFailed             = 60002
	QueryOrderTotalFailed        = 60003
	QueryOrderListFailed         = 60004
	QueryOrderProductFailed      = 60005
	QueryOrderProductInfoFailed  = 60006
	GenerateOrderFailed          = 60007
	PreOrderExisted              = 60008
	OrderProductEmpty            = 60009
	OutOfInventory               = 60010
	QueryUserCouponFailed        = 60010
	InternalFailed               = 60011
	QuerySettlementRecordFailed  = 60012
	UpdateSettlementStatusFailed = 60013
	OutOfRecord                  = 60014
	CheckoutTokenInvalid         = 60015
	CheckoutTokenExpired         = 60016
	CheckoutTokenConflict        = 60017
	CheckoutProcessing           = 60018
	CheckoutOutcomeUnknown       = 60019
)

// 状态码描述
const (
	InternalFailedMsg               = "内部错误"
	SettlementSuccessMsg            = "结算成功"
	SettlementFailedMsg             = "结算失败"
	QueryOrderTotalFailedMsg        = "查询订单总数失败"
	QueryOrderListFailedMsg         = "查询订单列表失败"
	QueryOrderProductFailedMsg      = "查询订单商品失败"
	QueryOrderProductInfoFailedMsg  = "查询订单商品信息失败"
	GenerateOrderFailedMsg          = "生成订单ID失败"
	PreOrderExistedMsg              = "用户预订单已存在"
	OrderProductEmptyMsg            = "订单商品为空"
	OutOfInventoryMsg               = "库存不足"
	QueryUserCouponFailedMsg        = "查询用户优惠券失败"
	QuerySettlementRecordFailedMsg  = "查询结算记录失败"
	UpdateSettlementStatusFailedMsg = "更新结算状态失败"
	OutOfRecordMsg                  = "查询的结果记录不存在"
	CheckoutTokenInvalidMsg         = "结算令牌无效"
	CheckoutTokenExpiredMsg         = "结算令牌已过期，请重新发起结算"
	CheckoutTokenConflictMsg        = "同一结算令牌不能修改商品、数量或优惠券"
	CheckoutProcessingMsg           = "结算正在处理中，请使用原令牌稍后查询"
	CheckoutOutcomeUnknownMsg       = "结算结果待核对，请勿重新提交购买，联系支持核实"
)
