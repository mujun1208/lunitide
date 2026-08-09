from __future__ import annotations

import json
from collections.abc import AsyncIterator

import httpx


def request_parts(base_url: str, model: str, api_key: str, messages: list[dict[str, str]], stream: bool):
    return (
        f"{base_url.rstrip('/')}/chat/completions",
        {"Authorization": f"Bearer {api_key}"},
        {"model": model, "messages": messages, "stream": stream},
    )


def response_content(payload: dict) -> str:
    choices = payload.get("choices")
    if not isinstance(choices, list) or not choices or not isinstance(choices[0], dict):
        raise ValueError("invalid OpenAI response")
    content = choices[0].get("message", {}).get("content")
    if not isinstance(content, str):
        raise ValueError("invalid OpenAI response")
    return content


async def stream_events(response: httpx.Response) -> AsyncIterator[dict]:
    async for line in response.aiter_lines():
        if not line.startswith("data:"):
            continue
        data = line[5:].strip()
        if data == "[DONE]":
            break
        payload = json.loads(data)
        choice = payload.get("choices", [{}])[0]
        content = choice.get("delta", {}).get("content")
        if content:
            yield {"type": "delta", "content": content}
        if payload.get("usage"):
            yield {"type": "usage", "usage": payload["usage"]}
