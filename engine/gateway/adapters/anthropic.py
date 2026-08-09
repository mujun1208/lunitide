from __future__ import annotations

import json
from collections.abc import AsyncIterator
from urllib.parse import urlparse

import httpx


def request_parts(base_url: str, model: str, api_key: str, messages: list[dict[str, str]], stream: bool):
    base = base_url.rstrip("/")
    path = urlparse(base).path.rstrip("/")
    endpoint = f"{base}/messages" if path.endswith("/v1") else f"{base}/v1/messages"
    system = "\n\n".join(message["content"] for message in messages if message["role"] == "system")
    payload = {"model": model, "messages": [message for message in messages if message["role"] != "system"], "max_tokens": 4096, "stream": stream}
    if system:
        payload["system"] = system
    return (endpoint, {"x-api-key": api_key, "anthropic-version": "2023-06-01"}, payload)


def response_content(payload: dict) -> str:
    return "".join(str(block.get("text", "")) for block in payload.get("content", []) if block.get("type") == "text")


async def stream_events(response: httpx.Response) -> AsyncIterator[dict]:
    async for line in response.aiter_lines():
        if not line.startswith("data:"):
            continue
        payload = json.loads(line[5:].strip())
        event_type = payload.get("type")
        if event_type == "content_block_delta":
            content = payload.get("delta", {}).get("text")
            if content:
                yield {"type": "delta", "content": content}
        elif event_type == "message_delta" and payload.get("usage"):
            yield {"type": "usage", "usage": payload["usage"]}
