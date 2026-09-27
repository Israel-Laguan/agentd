"""LiteLLM pre-call hook: expose agentd's task correlation to the upstream.

agentd (gateway option send_task_metadata) sends task_id/agent_id/role in the
request's `metadata` map. LiteLLM treats `metadata` as its own logging/spend
field and does not forward it upstream (it also drops `user` and client `x-`
headers), so mockllm never sees which task a request belongs to.

This hook copies those fields into a top-level `agentd_metadata` key, which
LiteLLM forwards unchanged to OpenAI-compatible backends. It also proves the
metadata actually reached LiteLLM, which is what send_task_metadata promises.

Registered in config.yaml under litellm_settings.callbacks.
"""

from litellm.integrations.custom_logger import CustomLogger

FORWARD_KEY = "agentd_metadata"
FIELDS = ("task_id", "agent_id", "role")


class AgentdCorrelation(CustomLogger):
    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        metadata = data.get("metadata") or data.get("litellm_metadata") or {}
        fields = {key: metadata[key] for key in FIELDS if metadata.get(key)}
        if fields:
            data[FORWARD_KEY] = fields
        return data


proxy_handler_instance = AgentdCorrelation()
