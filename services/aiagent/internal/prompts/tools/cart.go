package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	// CartList
	CartListDesc = `
【职责】
查询当前登录用户的购物车商品列表，返回购物车中所有商品及其数量。

【调用时机】
- 用户询问"我的购物车有什么"、"查看购物车"、"购物车里有啥"
- 用户想确认购物车中有哪些商品后再决定下一步操作
- 用户在添加/删除购物车商品后想验证结果

【不要调用】
- 用户只想了解某个商品详情但没有提到购物车 → 用 product_detail
- 用户想搜索或浏览商品 → 用 product_search 或 product_recommend
- 用户要求下单/结算但没有先提购物车 → 先确认用户是否需要查看购物车

【前置条件】
- 无需任何参数，自动获取当前登录用户的购物车

【执行限制】
- 只读操作，无风险，无需用户确认
- 分页参数 page / page_size 为可选，模型应根据上下文合理设置`

	CartListParameters = map[string]*schema.ParameterInfo{
		"page":      {Type: schema.Integer, Desc: "Page number starting from 1.", Required: false},
		"page_size": {Type: schema.Integer, Desc: "Number of cart items to return.", Required: false},
	}

	// CartAdd
	CartAddDesc = `【职责】
将指定商品添加到当前用户的购物车，支持指定数量。如果商品已在购物车中则累加数量。

【调用时机】
- 用户明确说"加入购物车"、"添加到购物车"、"帮我加购这个"
- 用户说"买这个"但尚未进入结算流程 → 先加入购物车
- 用户在浏览商品后说"放购物车"、"先加着"
- 用户说"加 N 件"、"要 3 个"时 → 设置 quantity=N

【不要调用】
- 用户只是想看商品详情但没有加购意图 → 用 product_detail
- 用户要求减少数量 → 用 cart_sub
- 用户要求删除购物车条目 → 用 cart_delete
- 用户直接要求下单/结算 → 应用 checkout_prepare
- 用户没有指定具体商品 → 先让用户明确要加购哪个商品

【前置条件】
- 必须知道 product_id（来自 product_search / product_detail / product_recommend 结果）
- quantity 默认 1，范围 1-100
- sku_id 可选，仅当用户明确指定 SKU 时传入

【执行限制】
- 低风险写操作，无需用户确认，直接执行
- product_id 为必填参数，quantity 为可选（默认 1）`

	CartAddParameters = map[string]*schema.ParameterInfo{
		"product_id": {Type: schema.Integer, Desc: "Product ID.", Required: true},
		"sku_id":     {Type: schema.Integer, Desc: "SKU ID.", Required: false},
		"quantity":   {Type: schema.Integer, Desc: "Quantity to add, default 1, max 100.", Required: false},
	}

	// CartSub
	CartSubDesc = `【职责】
减少当前用户购物车中某个商品的数量，支持指定减少数量。数量减到不足时不可继续减少。

【调用时机】
- 用户说"减少数量"、"少买 N 个"、"减 N 件"、"不要那么多"
- 用户想将某个商品数量减少但不想完全删除
- 用户说"只留 1 个"时可以计算出需要减多少

【不要调用】
- 用户想完全删除某个购物车条目 → 用 cart_delete
- 用户想增加数量 → 用 cart_add
- 用户没有指定具体的购物车条目 → 先调用 cart_list 让用户确认
- 减少后会清空该条目 → 提示用户改用 cart_delete

【前置条件】
- 必须知道 cart_item_id（来自 cart_list 的结果）
- quantity 默认 1，不能超过该条目当前数量
- 建议先调用 cart_list 确认条目存在且数量足够

【执行限制】
- 低风险写操作，无需用户确认，直接执行
- cart_item_id 为必填参数，quantity 为可选（默认 1）`

	CartSubParameters = map[string]*schema.ParameterInfo{
		"cart_item_id": {Type: schema.Integer, Desc: "Cart item ID.", Required: true},
		"quantity":     {Type: schema.Integer, Desc: "Quantity to subtract, default 1.", Required: false},
	}

	// CartDelete
	CartDeleteDesc = `【职责】
从当前用户购物车中完全删除某个商品条目。

【调用时机】
- 用户说"删掉这个"、"移除"、"不要了"、"去掉购物车里的XX"
- 用户明确表达要完全移除某个购物车条目
- 减少数量至 0 时应引导至此操作

【不要调用】
- 用户只想减少数量而非完全删除 → 用 cart_sub
- 用户不确定要删除哪个 → 先调用 cart_list 让用户选择
- 用户要求清空整个购物车 → 先列出所有条目，逐个确认后删除

【前置条件】
- 必须知道 cart_item_id（来自 cart_list 的结果）
- 建议先调用 cart_list 让用户确认要删除的具体条目

【执行限制】
- 高风险写操作，删除不可恢复，执行前必须获得用户明确确认
- cart_item_id 为必填参数`
	CartDeleteParameters = map[string]*schema.ParameterInfo{
		"cart_item_id": {Type: schema.Integer, Desc: "Cart item ID.", Required: true},
	}
)
