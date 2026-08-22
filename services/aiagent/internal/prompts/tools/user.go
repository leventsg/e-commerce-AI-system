package tool_prompts

import "github.com/cloudwego/eino/schema"

var (
	UserGetDesc = `【职责】
查询当前登录用户的账号资料，包括邮箱、用户名、头像和账号时间信息。

【调用时机】
- 用户问“我的资料”“我的账号信息”“我现在的用户名/邮箱是什么”
- 下单、收货地址等流程中需要核对当前用户基础资料

【执行限制】
- 只读操作，无需确认
- 不接收 user_id，系统会自动使用当前登录用户身份`

	UserGetParameters = map[string]*schema.ParameterInfo{}

	UserUpdateDesc = `【职责】
修改当前登录用户的用户名或头像。

【调用时机】
- 用户明确要求修改用户名、昵称或头像
- 用户提供了新的 user_name 或 avatar_url

【不要调用】
- 用户要修改密码、注销、登出或删除账号；当前 AI 客服不处理这些账号安全操作

【执行限制】
- 低风险写操作，无需确认，直接执行
- 至少提供 user_name 或 avatar_url 之一
- 不接收 user_id，系统会自动使用当前登录用户身份`

	UserUpdateParameters = map[string]*schema.ParameterInfo{
		"user_name":  {Type: schema.String, Desc: "新的用户名或昵称。", Required: false},
		"avatar_url": {Type: schema.String, Desc: "新的头像 URL。", Required: false},
	}

	AddressListDesc = `【职责】
查询当前登录用户的全部收货地址。

【调用时机】
- 用户问“我的收货地址”“地址列表”“我有哪些地址”
- 下单前用户需要选择地址 ID

【执行限制】
- 只读操作，无需确认
- 不接收 user_id，系统会自动使用当前登录用户身份`

	AddressListParameters = map[string]*schema.ParameterInfo{}

	AddressGetDesc = `【职责】
查询当前登录用户的单个收货地址详情。

【调用时机】
- 用户指定 address_id 查询某个地址
- 下单、修改或删除地址前需要核对地址内容

【执行限制】
- 只读操作，无需确认
- address_id 必须来自用户明确提供或 address_list 的结果`

	AddressGetParameters = map[string]*schema.ParameterInfo{
		"address_id": {Type: schema.Integer, Desc: "收货地址 ID。", Required: true},
	}

	AddressAddDesc = `【职责】
为当前登录用户新增收货地址。

【调用时机】
- 用户明确说“新增地址”“添加收货地址”“帮我保存这个地址”
- 用户已经提供收件人、手机号、省、市、详细地址

【执行限制】
- 低风险写操作，无需确认，直接执行
- 必须提供 recipient_name、phone_number、province、city、detailed_address
- is_default 可选，默认 false
- 不接收 user_id，系统会自动使用当前登录用户身份`

	AddressAddParameters = map[string]*schema.ParameterInfo{
		"recipient_name":   {Type: schema.String, Desc: "收件人姓名。", Required: true},
		"phone_number":     {Type: schema.String, Desc: "收件人手机号。", Required: true},
		"province":         {Type: schema.String, Desc: "省份。", Required: true},
		"city":             {Type: schema.String, Desc: "城市。", Required: true},
		"detailed_address": {Type: schema.String, Desc: "详细收货地址。", Required: true},
		"is_default":       {Type: schema.Boolean, Desc: "是否设为默认地址。", Required: false},
	}

	AddressUpdateDesc = `【职责】
修改当前登录用户已有的收货地址。

【调用时机】
- 用户明确要求修改某个 address_id 的收件人、手机号、省、市、详细地址或默认地址状态

【执行限制】
- 低风险写操作，无需确认，直接执行
- 必须提供 address_id，以及完整地址字段；缺少字段时先追问
- address_id 必须属于当前登录用户，由后端校验`

	AddressUpdateParameters = map[string]*schema.ParameterInfo{
		"address_id":       {Type: schema.Integer, Desc: "要修改的收货地址 ID。", Required: true},
		"recipient_name":   {Type: schema.String, Desc: "收件人姓名。", Required: true},
		"phone_number":     {Type: schema.String, Desc: "收件人手机号。", Required: true},
		"province":         {Type: schema.String, Desc: "省份。", Required: true},
		"city":             {Type: schema.String, Desc: "城市。", Required: true},
		"detailed_address": {Type: schema.String, Desc: "详细收货地址。", Required: true},
		"is_default":       {Type: schema.Boolean, Desc: "是否设为默认地址。", Required: false},
	}

	AddressDeleteDesc = `【职责】
删除当前登录用户已有的收货地址。

【调用时机】
- 用户明确说“删除这个地址”“移除地址”“不要这个收货地址了”

【执行限制】
- 低风险写操作，无需确认，直接执行
- address_id 必须来自用户明确提供或 address_list 的结果
- address_id 必须属于当前登录用户，由后端校验`

	AddressDeleteParameters = map[string]*schema.ParameterInfo{
		"address_id": {Type: schema.Integer, Desc: "要删除的收货地址 ID。", Required: true},
	}
)
