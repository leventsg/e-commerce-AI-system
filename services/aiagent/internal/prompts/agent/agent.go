package agent

import _ "embed"

//go:embed agent_system_prompt.txt
var SystemPrompt string

const SupervisorAgentDesc = "电商客服智能编排器：理解用户目标，调度四个业务 Agent 和通用客服 Agent，按依赖协调任务并汇总真实结果。"

//go:embed supervisor_system_prompt.txt
var SupervisorSystemPrompt string

//go:embed product_discovery_agent_desc.txt
var ProductDiscoveryAgentDesc string

//go:embed product_discovery_agent_system_prompt.txt
var ProductDiscoveryAgentSystemPrompt string

//go:embed shopping_decision_agent_desc.txt
var ShoppingDecisionAgentDesc string

//go:embed shopping_decision_agent_system_prompt.txt
var ShoppingDecisionAgentSystemPrompt string

//go:embed order_fulfillment_agent_desc.txt
var OrderFulfillmentAgentDesc string

//go:embed order_fulfillment_agent_system_prompt.txt
var OrderFulfillmentAgentSystemPrompt string

//go:embed after_sales_agent_desc.txt
var AfterSalesAgentDesc string

//go:embed after_sales_agent_system_prompt.txt
var AfterSalesAgentSystemPrompt string

//go:embed general_agent_desc.txt
var GeneralAgentDesc string

//go:embed general_agent_system_prompt.txt
var GeneralAgentSystemPrompt string
