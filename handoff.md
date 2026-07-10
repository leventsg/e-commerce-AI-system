# AI Customer Service Handoff

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

## Next Plan

Recommended next steps:

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

```bash
go test ./...
```

## Pitfalls: Do Not Repeat

- Do not put history content into a single `SystemMessage`. System messages are for policy/instructions. History must preserve role semantics.
- Do not trust history metadata or model output for `user_id`; all identity must come from login/auth context.
- Do not let the model decide confirmation requirements. Confirmation must come from Tool Registry metadata.
- Do not let model arguments keep `user_id`, `token`, `session_id`, or `auth`, even nested inside maps or slices.
- Do not remove numeric order ID support. Existing Planner tests and likely business inputs use values like `202406300001`.
- Do not pass unlimited history to the LLM. Keep bounded context, currently max 8 history messages.
- Do not place current user message before history. Current message must be last so it has priority.
- Do not silently skip tests after touching Planner behavior. Use TDD-style red/green where practical, then run `go test ./services/aiagent/...`.
- Do not trust the stale code-review graph warning; rebuild before relying on it.

