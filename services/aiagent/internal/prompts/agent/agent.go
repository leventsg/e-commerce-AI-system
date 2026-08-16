package agent

import _ "embed"

//go:embed agent_system_prompt.txt
var SystemPrompt string

const SupervisorAgentDesc = "电商客服智能编排器：分析用户意图，选择合适的子Agent，拆解复杂任务，协调多Agent工作流，并综合生成最终回复。"

//go:embed product_agent_desc.txt
var ProductAgentDesc string

//go:embed order_agent_desc.txt
var OrderAgentDesc string

//go:embed cart_checkout_agent_desc.txt
var CartCheckoutAgentDesc string

//go:embed coupon_agent_desc.txt
var CouponAgentDesc string

//go:embed general_agent_desc.txt
var GeneralAgentDesc string

//go:embed supervisor_system_prompt.txt
var SupervisorSystemPrompt string

//go:embed product_agent_system_prompt.txt
var ProductAgentSystemPrompt string

//go:embed order_agent_system_prompt.txt
var OrderAgentSystemPrompt string

//go:embed cart_checkout_agent_system_prompt.txt
var CartCheckoutAgentSystemPrompt string

//go:embed coupon_agent_system_prompt.txt
var CouponAgentSystemPrompt string

//go:embed general_agent_system_prompt.txt
var GeneralAgentSystemPrompt string
