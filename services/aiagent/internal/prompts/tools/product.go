package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	// ProductDetail
	ProductDetailDesc = `【职责】
根据商品 ID 查询单个商品的详细信息，包括名称、描述、图片、价格、库存、销量。

【调用时机】
- 用户询问"这个商品详情是什么"、"帮我看看这个"
- 用户指定了具体 product_id 想了解详细信息
- 从 product_search 结果中用户选中某个商品想进一步了解

【不要调用】
- 用户只是泛泛搜索商品 → 用 product_search
- 用户想要个性化推荐 → 用 product_recommend
- 用户只想看库存 → 用 inventory_get

【前置条件】
- 必须知道 product_id（通常来自 product_search 结果或用户明确指定）

【执行限制】
- 只读操作，无风险，无需用户确认
- product_id 为必填参数`

	ProductDetailParameters = map[string]*schema.ParameterInfo{
		"product_id": {Type: schema.Integer, Desc: "Product ID.", Required: true},
	}

	// ProductRecommend
	ProductRecommendDesc = `【职责】
根据用户的购物需求描述，智能推荐匹配的商品列表。

【调用时机】
- 用户说"推荐一下"、"有什么好的XX推荐"、"帮我挑挑"
- 用户表达了购物意向但没有指定具体商品名称
- product_search 结果不理想，用户需要个性化推荐
- 用户说了预算范围（如"500以内"）和偏好的品类

【不要调用】
- 用户明确知道商品名称 → 用 product_search
- 用户已经看中了具体商品 → 用 product_detail
- 用户问的是购物车/订单相关 → 用 cart_list / order_list

【前置条件】
- query 参数强烈建议填写，描述用户的购物需求场景
- category / min_price / max_price 为辅助筛选条件，根据对话上下文提取
- limit 控制返回数量，默认不宜过多

【执行限制】
- 只读操作，无风险，无需用户确认
- 所有参数均为可选，但建议至少提供 query 以获得有意义的推荐`

	ProductRecommendParameters = map[string]*schema.ParameterInfo{
		"query":     {Type: schema.String, Desc: "user query.", Required: false},
		"category":  {Type: schema.String, Desc: "Preferred product category.", Required: false},
		"min_price": {Type: schema.Number, Desc: "Minimum acceptable price.", Required: false},
		"max_price": {Type: schema.Number, Desc: "Maximum acceptable price.", Required: false},
		"limit":     {Type: schema.Integer, Desc: "Maximum number of recommendations.", Required: false},
	}

	// ProductSearch
	ProductSearchName = "prompts"
	ProductSearchDesc = `【职责】
按条件搜索商品，支持按名称、关键词、分类、价格区间、是否新品/热销等维度筛选，结果分页返回。

【调用时机】
- 用户说"搜一下XX"、"找找有没有YY"、"看看ZZ商品"
- 用户提到了商品名称或关键词，想查找匹配的商品
- 用户说"有没有便宜点的XX" → 设置 price.max 筛选
- 用户说"有没有新上的XX" → 设置 new=true

【不要调用】
- 用户已经明确要某个具体商品 → 用 product_detail
- 用户说"推荐一下"而非"搜索一下" → 用 product_recommend
- 用户问的是购物车/订单/优惠券 → 对应 cart_* / order_* / coupon_* 工具

【前置条件】
- 至少应提供 name 或 keyword 之一，否则返回全量商品
- category / price / new / hot 用于缩小范围，从对话中提取
- paginator 控制分页，默认首页少量返回

【执行限制】
- 只读操作，无风险，无需用户确认
- 所有参数均为可选，不传任何参数时返回全量商品列表`

	ProductSearchParameters = map[string]*schema.ParameterInfo{
		"name":     {Type: schema.String, Desc: "Product name search.", Required: false},
		"keyword":  {Type: schema.String, Desc: "Keyword search.", Required: false},
		"category": {Type: schema.Array, Desc: "Product category filter.", Required: false, ElemInfo: &schema.ParameterInfo{Type: schema.String}},
		"new":      {Type: schema.Boolean, Desc: "Filter new products only.", Required: false},
		"hot":      {Type: schema.Boolean, Desc: "Filter hot products only.", Required: false},
		"price": {
			Type: schema.Object, Desc: "Price range filter.", Required: false,
			SubParams: map[string]*schema.ParameterInfo{
				"min": {Type: schema.Integer, Desc: "Minimum price in cents.", Required: false},
				"max": {Type: schema.Integer, Desc: "Maximum price in cents.", Required: false},
			},
		},
		"paginator": {
			Type: schema.Object, Desc: "Pagination settings.", Required: false,
			SubParams: map[string]*schema.ParameterInfo{
				"page":      {Type: schema.Integer, Desc: "Page number starting from 1.", Required: false},
				"page_size": {Type: schema.Integer, Desc: "Number of results per page.", Required: false},
			},
		},
	}
)
