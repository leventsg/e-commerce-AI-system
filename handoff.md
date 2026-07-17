# AI Customer Service Handoff

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

## Next Plan

Recommended next steps:

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

```bash
go test ./...
```

## Pitfalls: Do Not Repeat

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

