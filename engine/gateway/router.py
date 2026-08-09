from __future__ import annotations

import asyncio
import json
import ipaddress
import socket
import time
from datetime import datetime, timezone
from urllib.parse import urlparse

import httpx
from fastapi import APIRouter, HTTPException
from fastapi.responses import StreamingResponse

from .adapters import anthropic, openai
from .schemas import ChatRequest, ProviderModelsResult, ProviderRequest, ProviderTestResult

router = APIRouter(prefix="/v1", tags=["gateway"])


def _adapter(protocol: str):
    return openai if protocol == "openai" else anthropic


async def _validate_destination(url: str) -> None:
    parsed = urlparse(url)
    local_names = {"localhost", "127.0.0.1", "::1"}
    if parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise ValueError("unsafe destination")
    if parsed.scheme != "https" and not (parsed.scheme == "http" and parsed.hostname in local_names):
        raise ValueError("unsafe destination")
    if parsed.hostname in local_names:
        return
    try:
        addresses = await asyncio.to_thread(socket.getaddrinfo, parsed.hostname, parsed.port or 443, type=socket.SOCK_STREAM)
    except socket.gaierror as error:
        raise httpx.ConnectError("dns lookup failed") from error
    for address in addresses:
        ip = ipaddress.ip_address(address[4][0])
        if not ip.is_global:
            raise ValueError("unsafe destination")


def _error_detail(error: Exception) -> str:
    if isinstance(error, httpx.TimeoutException): return "连接模型服务超时，请检查网络或 Base URL"
    if isinstance(error, httpx.ConnectError): return "无法连接模型服务，请检查网络或 Base URL"
    if isinstance(error, httpx.HTTPStatusError):
        status = error.response.status_code
        if status == 401: return "API Key 无效或没有访问权限"
        if status == 403: return "模型服务拒绝访问，请检查账号权限"
        if status == 404: return "未找到模型或接口，请检查模型名称和 Base URL"
        if status == 429: return "请求过于频繁或额度不足，请稍后重试"
        if status >= 500: return "模型服务暂时不可用，请稍后重试"
        return f"模型服务返回错误（HTTP {status}）"
    return "模型请求失败，请检查供应商配置"


async def _send_with_retry(client: httpx.AsyncClient, url: str, **kwargs) -> httpx.Response:
    for attempt in range(3):
        response = await client.post(url, **kwargs)
        if response.status_code != 429 or attempt == 2:
            response.raise_for_status(); return response
        await asyncio.sleep(0.5 * (attempt + 1))
    raise RuntimeError("unreachable")


def _parts(provider: ProviderRequest, messages: list[dict[str, str]], stream: bool):
    adapter = _adapter(provider.protocol)
    return adapter, adapter.request_parts(str(provider.baseUrl), provider.model, provider.apiKey.get_secret_value(), messages, stream)


@router.post("/providers/test", response_model=ProviderTestResult)
async def test_provider(provider: ProviderRequest) -> ProviderTestResult:
    adapter, (url, headers, payload) = _parts(provider, [{"role": "user", "content": "Reply OK"}], False)
    payload["max_tokens"] = 1
    started = time.monotonic()
    try:
        await _validate_destination(url)
        async with httpx.AsyncClient(timeout=httpx.Timeout(20.0, connect=8.0), follow_redirects=False) as client:
            response = await _send_with_retry(client, url, headers=headers, json=payload)
            content = adapter.response_content(response.json())
            if not content:
                raise ValueError("empty provider response")
        return ProviderTestResult(ok=True, detail="API Key、模型和接口均验证成功", model=provider.model, httpStatus=response.status_code, latencyMs=round((time.monotonic() - started) * 1000), checkedAt=datetime.now(timezone.utc).isoformat())
    except (httpx.HTTPError, RuntimeError, ValueError, TypeError, IndexError) as error:
        status = error.response.status_code if isinstance(error, httpx.HTTPStatusError) else None
        return ProviderTestResult(ok=False, detail=_error_detail(error), model=provider.model, httpStatus=status, latencyMs=round((time.monotonic() - started) * 1000), checkedAt=datetime.now(timezone.utc).isoformat())


@router.post("/providers/models", response_model=ProviderModelsResult)
async def provider_models(provider: ProviderRequest) -> ProviderModelsResult:
    base = str(provider.baseUrl).rstrip("/")
    path = urlparse(base).path.rstrip("/")
    url = f"{base}/models" if path.endswith("/v1") else f"{base}/v1/models"
    headers = {"Authorization": f"Bearer {provider.apiKey.get_secret_value()}"} if provider.protocol == "openai" else {"x-api-key": provider.apiKey.get_secret_value(), "anthropic-version": "2023-06-01"}
    try:
        await _validate_destination(url)
        async with httpx.AsyncClient(timeout=httpx.Timeout(20.0, connect=8.0), follow_redirects=False) as client:
            async with client.stream("GET", url, headers=headers) as response:
                response.raise_for_status()
                body = bytearray()
                async for chunk in response.aiter_bytes():
                    body.extend(chunk)
                    if len(body) > 1_000_000:
                        raise ValueError("model list response too large")
        data = json.loads(body).get("data", [])
        models = sorted({model_id for item in data if isinstance(item, dict) and isinstance(item.get("id"), str) and 0 < len(model_id := item["id"].strip()) <= 200})[:50]
        if not models:
            raise ValueError("empty model list")
        return ProviderModelsResult(models=models, detail=f"已从上游获取 {len(models)} 个模型")
    except (httpx.HTTPError, ValueError, TypeError) as error:
        raise HTTPException(status_code=502, detail=_error_detail(error)) from None


@router.post("/chat/completions")
async def chat_completions(request: ChatRequest):
    messages = [message.model_dump() for message in request.messages]
    adapter, (url, headers, payload) = _parts(request, messages, request.stream)
    if not request.stream:
        try:
            await _validate_destination(url)
            async with httpx.AsyncClient(timeout=httpx.Timeout(60.0, connect=8.0), follow_redirects=False) as client:
                response = await _send_with_retry(client, url, headers=headers, json=payload)
            body = response.json()
            return {"content": adapter.response_content(body), "usage": body.get("usage")}
        except (httpx.HTTPError, RuntimeError, ValueError) as error:
            raise HTTPException(status_code=502, detail=_error_detail(error)) from None

    async def events():
        yield f"data: {json.dumps({'type': 'start'}, ensure_ascii=False)}\n\n"
        try:
            await _validate_destination(url)
            async with httpx.AsyncClient(timeout=httpx.Timeout(60.0, connect=8.0), follow_redirects=False) as client:
                async with client.stream("POST", url, headers=headers, json=payload) as response:
                    response.raise_for_status()
                    async for event in adapter.stream_events(response):
                        yield f"data: {json.dumps(event, ensure_ascii=False)}\n\n"
            yield f"data: {json.dumps({'type': 'done'}, ensure_ascii=False)}\n\n"
        except (httpx.HTTPError, ValueError) as error:
            yield f"data: {json.dumps({'type': 'error', 'detail': _error_detail(error)}, ensure_ascii=False)}\n\n"

    return StreamingResponse(events(), media_type="text/event-stream", headers={"Cache-Control": "no-cache"})
