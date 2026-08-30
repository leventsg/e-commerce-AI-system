# tau2 Ecommerce Evaluation Adapter

This directory keeps the tau2-bench integration separate from the Go business
services. It provides a thin adapter that treats the existing AI customer
service API as the system under test.

## What This Contains

- `tau2_ecommerce/agent.py`: tau2-compatible agent adapter for `/douyin/ai/chat`.
- `tau2_ecommerce/client.py`: HTTP + SSE client for the Go AI customer service.
- `tau2_ecommerce/sse.py`: small SSE parser used by the adapter and tests.
- `domains/ecommerce/`: minimal ecommerce domain seed files.
- `scripts/fetch_tau2_bench.ps1`: pulls tau2-bench into the repo root.
- `scripts/install_domain.py`: installs the domain, agent wrapper, example, and registry entries into a local tau2 checkout.

## Minimal Setup

From the repository root:

```powershell
powershell -ExecutionPolicy Bypass -File eval\tau2-ecommerce\scripts\fetch_tau2_bench.ps1
py -3 eval\tau2-ecommerce\scripts\install_domain.py --tau2-root tau2-bench
```

Then install tau2-bench following its upstream instructions. If `uv` is not
available, the minimal editable install is:

```powershell
cd tau2-bench
py -3 -m pip install -e .
py -3 -m pip install audioop-lts
```

`audioop-lts` is only needed on Python 3.13 because `audioop` was removed from
the standard library. The adapter is intentionally not wired into Go code. It
talks to the service over HTTP/SSE.

On Windows, use UTF-8 output for tau2 commands that print symbols:

```powershell
$env:PYTHONIOENCODING = "utf-8"
tau2 check-data
```

## Verify tau2 Registration

After running `install_domain.py`, tau2 should list the ecommerce domain, task
set, and agent:

```powershell
cd tau2-bench
py -3 -c "import importlib; m=importlib.import_module('tau2.registry'); print('ecommerce' in m.registry.get_domains(), 'ecommerce' in m.registry.get_task_sets(), 'ecommerce_go_agent' in m.registry.get_agents())"
```

To inspect the generated domain server:

```powershell
$env:PYTHONIOENCODING = "utf-8"
tau2 domain ecommerce
```

## Smoke Test The Adapter Without tau2

Set the API endpoint and token, then send one message:

```powershell
$env:PYTHONPATH = "eval\tau2-ecommerce"
$env:AI_CS_BASE_URL = "http://localhost:8007"
$env:AI_CS_AUTH_TOKEN = "<access-token>"
$env:AI_CS_REFRESH_TOKEN = "<refresh-token>"
py -3 -m tau2_ecommerce.smoke "帮我查一下我的订单"
```

## Run Through tau2

Start the Go API service first, then set your customer-service endpoint and an
LLM key for tau2's user simulator and NL evaluator:

```powershell
$env:AI_CS_BASE_URL = "http://localhost:8007"
$env:AI_CS_AUTH_TOKEN = "<access-token>"
$env:AI_CS_REFRESH_TOKEN = "<refresh-token>"
$env:OPENAI_API_KEY = "<openai-key>"
```

Then run:

```powershell
cd tau2-bench
py -3 examples\agents\ecommerce_go_agent.py
```

For a quick adapter-only check, use:

```powershell
py -3 examples\agents\ecommerce_go_agent.py --direct-smoke --dump-events
```

Do not point `AI_CS_BASE_URL` at the frontend dev server, such as
`http://localhost:5174`. The adapter needs the AI API gateway. In this repo the
default local AI API port is `8007` from `apis/ai/etc/ai-api.yaml`.

The Go AI gateway reads auth from `Access-Token` and a `Refresh-Token` cookie.
The adapter maps `AI_CS_AUTH_TOKEN` to the `Access-Token` header and
`AI_CS_REFRESH_TOKEN` to the cookie.

DeepSeek also works through LiteLLM:

```powershell
$env:DEEPSEEK_API_KEY = "<deepseek-key>"
py -3 examples\agents\ecommerce_go_agent.py
```

The script defaults DeepSeek runs to `deepseek/deepseek-chat`. Override either
provider with:

```powershell
$env:TAU2_EVAL_LLM = "deepseek/deepseek-chat"
```

## Recommended Eval Shape

Run tau2 against a sandbox API profile:

- test users only
- sandbox MySQL/Redis
- deterministic product, coupon, cart, checkout, and order fixtures
- all write tools still pass through Execution Guard, confirmation, and audit

High-risk actions should be judged through `confirmation_required` events. The
adapter can auto-approve, auto-reject, or stop at the confirmation event.
