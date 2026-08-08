package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	// InventoryGet
	InventoryGetDesc = `【职责】
查询指定商品的当前库存数量和已售数量。

【调用时机】
- 用户问"这个还有货吗"、"库存多少"、"还有几件"
- 用户在下单前想确认商品是否有货
- 用户看到限量/抢购商品时询问剩余数量

【不要调用】
- 用户想看商品完整信息（价格、描述等）→ 用 product_detail
- 用户想搜索商品 → 用 product_search
- 用户已进入结算流程 → 库存由 checkout_prepare 自动校验

【前置条件】
- 必须知道 product_id（来自 product_search / product_detail 结果）

【执行限制】
- 只读操作，无风险，无需用户确认
- product_id 为必填参数`

	InventoryGetParameters = map[string]*schema.ParameterInfo{
		"product_id": {Type: schema.Integer, Desc: "Product ID.", Required: true},
	}
)
