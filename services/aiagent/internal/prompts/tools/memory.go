package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	// SearchUserMemory
	SearchUserMemoryDesc = `【职责】
检索当前用户较早的长期记忆事件，补充当前上下文没有覆盖的偏好、承诺、历史选择或未完成事项。

【调用时机】
- 用户提到"之前"、"上次"、"我以前说过"等历史线索
- 当前摘要、近期消息和工具结果不足以判断用户偏好或历史决定
- 需要查找较早的 milestone/event 记忆来消解指代或延续任务

【不要调用】
- 当前消息、近期历史或 runtime context 已足够回答
- 需要实时商品、订单、购物车、优惠券或库存事实 → 使用对应业务工具
- 想读取用户身份信息或跨用户数据

【前置条件】
- keywords 应使用 1-5 个短关键词，来自用户当前问题和上下文
- match 可选 any/all，默认 any
- type 仅限 milestone/event

【执行限制】
- 只读操作，无需确认
- 不接受 user_id 参数；用户身份只由后端注入`

	SearchUserMemoryParameters = map[string]*schema.ParameterInfo{
		"keywords": {
			Type:     schema.Array,
			Desc:     "从用户当前请求或上下文中提取 1-5 个简短关键词。",
			Required: false,
			ElemInfo: &schema.ParameterInfo{Type: schema.String},
		},
		"match": {Type: schema.String, Desc: "关键词匹配模式：any（任一匹配）或 all（全部匹配）。默认为 any。", Required: false},
		"type":  {Type: schema.String, Desc: "记忆事件类型：milestone（里程碑）或 event（普通事件）。", Required: false},
		"since": {Type: schema.String, Desc: "事件起始时间边界，格式为 YYYY-MM-DD 或 RFC3339。", Required: false},
		"until": {Type: schema.String, Desc: "事件结束时间边界，格式为 YYYY-MM-DD 或 RFC3339。", Required: false},
		"limit": {Type: schema.Integer, Desc: "最大返回事件数量。默认为 10，上限为 30。", Required: false},
	}
)
