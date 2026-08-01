package agent

import _ "embed"

//go:embed agent_system_prompt.txt
var SystemPrompt string

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
