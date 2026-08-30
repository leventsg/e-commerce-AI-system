# Ecommerce Domain Seed

These files define the tau2 seed for the Go AI customer-service benchmark.
The live Go backend remains the business source of truth; `db.json` and
`initial_state.initialization_data.agent_data` are descriptive fixtures and
machine-readable expectations for evaluation.

The suite is split into two layers:

- `regression`: fast checks for basic tool use, CRUD-like writes, ownership,
  schema-sensitive parameters, and confirmation gates.
- `benchmark`: harder multi-turn scenarios for context continuity,
  multi-agent routing, RAG plus tools, retry/idempotency behavior,
  partial failure handling, Saga compensation, and HITL safety.

Tasks are written as user goals rather than simulator scripts. Expected
trajectory, forbidden actions, failure injection points, and final state
expectations live in `agent_data` so future evaluators can score trajectory and
state, not only final assistant wording.

Use `scripts/install_domain.py` to copy this seed into a local tau2-bench
checkout after the checkout exists.
