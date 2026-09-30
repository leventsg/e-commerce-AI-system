"""tau2-bench adapter for the ecommerce AI customer service."""

from .client import AiCustomerServiceClient

__all__ = ["AiCustomerServiceClient", "EcommerceGoAgent", "EcommerceGoAgentState"]


def __getattr__(name):
    # Offline scorers/preflight do not need tau2's model providers or cost-map network fetches.
    if name in {"EcommerceGoAgent", "EcommerceGoAgentState"}:
        from . import agent
        return getattr(agent, name)
    raise AttributeError(name)
