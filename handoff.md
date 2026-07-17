# AI Customer Service Handoff

<<<<<<< HEAD
## What We Are Doing

We are implementing the AI customer service feature for the Go mall project, following `docs/ai-customer-service-implementation-plan.md` task by task.

The current completed milestone is **Task 8: 接入查询与推荐工具**. The next planned milestone is **Task 9: 接入低风险写操作**.

Core architecture to keep in mind:

- `services/aiagent` is the AI orchestration RPC service.
- Tools are **Eino local tools**, not MCP tools.
- The AI tool layer only orchestrates existing business RPCs. It must not replace product, inventory, order, cart, coupon, checkout, users, audit, or auth services.
- `Tool Registry` defines tool schema and local metadata.
- `Execution Guard` is the safety boundary before any tool handler calls business RPC.
- Authenticated context is the only trusted source for `user_id`.

## What Is Done

Task 7 is complete:

- Added `services/aiagent/internal/tools/executor.go`.
- Added Execution Guard behavior:
  - rejects unknown tools,
  - sanitizes untrusted arguments recursively,
  - strips sensitive keys such as `user_id`, `token`, `session_id`, and `auth`,
  - injects authenticated `UserID` through `HandlerRequest.UserID`,
  - applies registry metadata timeouts,
  - returns `tool_result` events with `success` or `failed`,
  - avoids failure content that claims success,
  - supports an optional recorder hook.
- Shared argument sanitizing was extracted into `common/utils/argx`.
- Planner and Executor now use the shared sanitizer.

Task 8 is complete:

- Added query/recommendation tool implementation under `services/aiagent/internal/tools`.
- Query tools are wired into the local Eino tool registry through `NewQueryTools`.
- `ServiceContext` now constructs:
  - `ToolRegistry`,
  - `ToolExecutor`,
  - `QueryTools`,
  - RPC clients reused by query handlers.
- Added trusted tool execution context:
  - `ToolExecutionContext`
  - `WithToolExecutionContext`
  - `queryInvokableTool.InvokableRun`
- Eino tool execution rejects missing trusted user context before calling RPC.
- Model/tool JSON arguments can contain `user_id`, but it is ignored and sanitized; RPC calls receive the authenticated user ID only.

Implemented query handlers:

- `product.search` -> `ProductCatalogService.QueryProduct`
- `product.detail` -> `ProductCatalogService.GetProduct`
- `product.recommend` -> `ProductCatalogService.RecommendProduct`
- `inventory.get` -> `Inventory.GetInventory`
- `order.get` -> `OrderService.GetOrder`
- `order.list` -> `OrderService.ListOrders`
- `cart.list` -> `Cart.CartItemList`
- `coupon.list` -> `Coupons.ListCoupons`
- `coupon.detail` -> `Coupons.GetCoupon`
- `coupon.my_list` -> `Coupons.ListUserCoupons`
- `coupon.usage_list` -> `Coupons.ListCouponUsages`
- `coupon.calculate` -> `Coupons.CalculateCoupon`
- `checkout.detail` -> `CheckoutService.GetCheckoutDetail`

Important files:

- `services/aiagent/internal/tools/registry.go`
- `services/aiagent/internal/tools/executor.go`
- `services/aiagent/internal/tools/query_tools.go`
- `services/aiagent/internal/tools/product_tools.go`
- `services/aiagent/internal/tools/inventory_tools.go`
- `services/aiagent/internal/tools/order_tools.go`
- `services/aiagent/internal/tools/cart_tools.go`
- `services/aiagent/internal/tools/coupon_tools.go`
- `services/aiagent/internal/tools/checkout_tools.go`
- `services/aiagent/internal/tools/query_tools_test.go`
- `services/aiagent/internal/svc/servicecontext.go`
- `common/utils/argx`

## Verification Already Run

Task 8 verification was run successfully in the previous session:

```bash
go test ./services/aiagent/internal/tools -run TestQueryTools -count=1
go test ./services/aiagent/... -count=1
git diff --check
```

Observed result:

- Query tool tests passed.
- Full `services/aiagent/...` tests passed.
- `git diff --check` passed.

`go test ./apis/ai/...` was not runnable because `apis/ai` does not exist yet in this workspace.

Current workspace note:

- `git status --short` produced no output at the time this handoff was written.
- The code-review graph warning says it was built on `feat/add_ai_agent_task7`, while the current branch is `feat/add_ai_agent_task8`; rebuild the graph before relying on graph analysis.

## Current Status / Blockers

There is no active technical blocker for Task 8.

The main remaining integration gap is runtime chat flow wiring:

- Query tools are registered and executable through Eino tool interfaces.
- The actual chat orchestration path still needs to pass authenticated `ToolExecutionContext` when invoking tools.
- If a future task wires ToolsNode/Agent/Graph execution, it must wrap tool execution context with `WithToolExecutionContext(ctx, ToolExecutionContext{...})`.

The next implementation task is Task 9:

- `cart.add`
- `cart.sub`
- `coupon.claim`
- audit/tool-call recording for low-risk writes
=======
## Current Task

We are implementing the AI customer service feature for the Go mall project, specifically the `services/aiagent` Intent Planner portion from `docs/ai-customer-service-implementation-plan.md` Task 6.

The current focus is `services/aiagent/internal/planner/planner.go`: the Planner should use a fast LLM for structured intent routing, retry invalid model output, fall back to rule planning, sanitize untrusted model arguments, and use bounded conversation history to resolve multi-turn references like "这个", "刚才那个订单", or "加两件".

Important project constraints from `AGENTS.md`:

- User identity must never come from the client, model output, tool arguments, or conversation metadata.
- Planner output is only a candidate plan. Tool Registry and Execution Guard remain the safety boundary.
- High-risk operations must get confirmation from registry metadata, not from the model.
- Tool arguments must not include `user_id`, `token`, `session_id`, or `auth`.
- Eino types should stay isolated inside `services/aiagent/internal/eino/**` and AI agent internals.

## What Is Done

The Planner now has a context-aware request shape:

- `PlanRequest{Message, History}` is used instead of passing only a string.
- `Plan` trims the current message, tries fast LLM planning first, and falls back to rule planning.
- LLM planning still retries and validates through Tool Registry.

The LLM input construction was corrected:

- `buildLLMPlannerMessages` starts with `schema.SystemMessage(intentprompt.IntentSystemPrompt)`.
- Bounded history is appended as role-specific Eino messages:
  - `user` -> `schema.UserMessage`
  - `assistant` -> `schema.AssistantMessage`
  - `tool` -> `schema.ToolMessage`
- Current user input is always appended last as `schema.UserMessage(text)`.
- History is capped at `maxPlannerContextMessages = 8`.
- History keeps chronological order after selecting the latest valid messages.
- Duplicate current user message in history is skipped.
- History content is compacted and redacted before being sent to the model.
- Tool history metadata reads `tool_call_id` and `tool_name`.

Security and validation behavior is covered:

- Model output arguments are sanitized recursively.
- Sensitive keys removed from model arguments: `user_id`, `token`, `session_id`, `auth`.
- Sensitive text is redacted in history context.
- Required tool arguments are checked against Tool Registry schema.
- `RequireConfirmation` is still set only from Tool Registry metadata.

Tests added or updated in `services/aiagent/internal/planner/planner_test.go`:

- LLM result wins before rules.
- High-risk confirmation comes from registry metadata.
- LLM arguments containing `user_id` are removed.
- LLM error / invalid JSON retry behavior.
- Unknown LLM tools fall back to rule planner.
- Context is sent before current user message.
- History roles are preserved as `schema.User`, `schema.Assistant`, and `schema.Tool`.
- Tool history preserves `ToolName` and `ToolCallID`.
- Current message priority over context.
- Sensitive history content is not leaked.
- Missing context causes a clarifying question rather than guessed parameters.

Also fixed a compatibility bug:

- `orderIDPattern` had drifted to UUID-only matching.
- It now supports both UUID-style order IDs and numeric order IDs with 6+ digits, so existing tests like `202406300001` pass.

## Verification

Fresh verification was run successfully:

```bash
go test ./services/aiagent/internal/planner -run Test -count=1
```

Result:

```text
ok  	github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/planner	0.518s
```

Full aiagent service verification was also run successfully:

```bash
go test ./services/aiagent/...
```

Result:

```text
ok  	github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/conversation
ok  	github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/planner
ok  	github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/prompts/intent
ok  	github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools
```

Packages without test files were reported normally.

## Current Status / Blockers

There is no current technical blocker in the Planner work.

One process/documentation item remains to check:

- `docs/ai-customer-service-implementation-plan.md` Task 6 checkboxes may still be unchecked even though Planner implementation and tests are in place. Next session should inspect and update the task checklist if the project convention is to mark completed plan steps.

Environment note:

- A previous system warning said the code-review graph was built on `faet/add_ai_agent_task5`, while the current branch is `feat/add_ai_agent_task6`. If using code-review-graph tooling, rebuild the graph before trusting graph analysis.
>>>>>>> main

## Next Plan

Recommended next steps:

<<<<<<< HEAD
1. Start Task 9 from `docs/ai-customer-service-implementation-plan.md`.
2. Add focused tests first in `services/aiagent/internal/tools/write_tools_test.go`.
3. Implement low-risk write handlers in existing tool files:
   - `services/aiagent/internal/tools/cart_tools.go`
   - `services/aiagent/internal/tools/coupon_tools.go`
4. Reuse the existing `Executor`; do not bypass Execution Guard.
5. Add or wire a recorder/audit path so write calls record `ai_tool_calls` and audit data.
6. Confirm `cart.add`, `cart.sub`, and `coupon.claim` do not require confirmation, but still count as write operations and must be auditable.
7. Run:

```bash
go test ./services/aiagent/internal/tools -run TestWriteTools -count=1
go test ./services/aiagent/... -count=1
```

Before claiming the whole AI customer service feature complete, run:
=======
1. Inspect `docs/ai-customer-service-implementation-plan.md` Task 6 and mark completed steps if appropriate.
2. Continue from the implementation plan after Task 6, likely wiring Planner into the AI Agent chat flow if that is not done yet.
3. Check `services/aiagent/internal/logic/chatlogic.go`; earlier it was still a generated placeholder, so Planner may not yet be integrated into the runtime RPC flow.
4. Ensure Conversation Manager history is passed into `Planner.Plan(ctx, PlanRequest{Message, History})` when ChatLogic is implemented.
5. When integrating runtime flow, preserve the security chain:
   - auth context supplies user ID,
   - Conversation Manager enforces conversation ownership,
   - Planner creates only a candidate tool plan,
   - Tool Registry validates tool identity and risk,
   - Execution Guard injects user ID and performs RPC calls,
   - high-risk actions create confirmations before execution.
6. Run at minimum:

```bash
go test ./services/aiagent/...
go test ./apis/ai/...
```

Before claiming the whole AI customer service feature is complete, run:
>>>>>>> main

```bash
go test ./...
```

## Pitfalls: Do Not Repeat

<<<<<<< HEAD
- Do not implement these as MCP tools. They are local Eino tools registered inside `services/aiagent/internal/tools`.
- Do not let model arguments provide or override `user_id`.
- Do not add `user_id` to any Eino tool schema.
- Do not call business RPCs directly from an Eino handler without passing through `Executor`.
- Do not trust conversation metadata, tool args, model output, or client payload for identity.
- Do not let failed RPC/tool results become assistant success messages.
- Do not expose raw RPC responses wholesale; keep tool result payloads compact and user-safe.
- Do not manually edit generated go-zero files.
- Do not change existing product/order/cart/coupon/checkout RPC behavior for AI needs unless the implementation plan explicitly says to.
- Do not forget that high-risk actions are not Task 9. Delete cart item, create order, cancel order, and coupon-on-checkout paths must go through confirmation work in later tasks.
- Do not rely on the stale code-review graph without rebuilding it on the current branch.
=======
- Do not put history content into a single `SystemMessage`. System messages are for policy/instructions. History must preserve role semantics.
- Do not trust history metadata or model output for `user_id`; all identity must come from login/auth context.
- Do not let the model decide confirmation requirements. Confirmation must come from Tool Registry metadata.
- Do not let model arguments keep `user_id`, `token`, `session_id`, or `auth`, even nested inside maps or slices.
- Do not remove numeric order ID support. Existing Planner tests and likely business inputs use values like `202406300001`.
- Do not pass unlimited history to the LLM. Keep bounded context, currently max 8 history messages.
- Do not place current user message before history. Current message must be last so it has priority.
- Do not silently skip tests after touching Planner behavior. Use TDD-style red/green where practical, then run `go test ./services/aiagent/...`.
- Do not trust the stale code-review graph warning; rebuild before relying on it.
>>>>>>> main

