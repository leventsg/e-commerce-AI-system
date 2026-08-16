package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	GetToolCallResultDesc = `【职责】
按 tool_call_id 读取当前用户当前对话中某次历史工具调用的真实执行结果，补充 <recent_tool_calls> 里没有展开的 result。

【调用时机】
- runtime context 的 <recent_tool_calls> 只提供了工具名、参数和 tool_call_id，但当前回答需要该工具调用的真实 result
- 需要核对较早工具调用返回的订单、购物车、优惠券、商品或库存事实
- 用户追问"刚才查到的那个结果"、"上一次工具结果"等，并且已有对应 tool_call_id

【不要调用】
- <latest_tool_result> 已经包含足够完整的结果
- 需要获取实时业务事实，应调用商品、订单、购物车、结算、优惠券或库存业务工具
- 没有明确 tool_call_id，或想按用户身份、conversation_id、时间范围搜索

【前置条件】
- tool_call_id 必须来自 runtime context 或本轮工具调用事件，不要编造
- 只传 tool_call_id，不传 user_id、conversation_id 或 session_id

【执行限制】
- 只读操作，无需确认
- 只能读取当前用户当前 conversation 的工具调用记录
- 返回的是 ai_tool_calls.result 的真实 JSON，不是摘要`

	GetToolCallResultParameters = map[string]*schema.ParameterInfo{
		"tool_call_id": {Type: schema.String, Desc: "要读取结果的模型工具调用 ID，必须来自上下文中的 tool_call_id。", Required: true},
	}
)
