import httpx
from fastapi.testclient import TestClient

from engine.app import create_app
from engine.gateway.adapters import anthropic, openai
from engine.gateway.router import _error_detail
from engine.gateway.schemas import ProviderModelsResult, ProviderTestResult


def test_gateway_requires_engine_token():
    client = TestClient(create_app("test-token", parent_pid=0))
    assert client.post("/v1/providers/test", json={}).status_code == 401


def test_provider_schema_never_echoes_api_key():
    client = TestClient(create_app("test-token", parent_pid=0))
    response = client.post("/v1/providers/test", headers={"Authorization": "Bearer test-token"}, json={"protocol": "openai", "baseUrl": "not-a-url", "model": "", "apiKey": "secret-key"})
    assert response.status_code == 422
    assert "secret-key" not in response.text


def test_protocol_endpoints_and_headers():
    url, headers, _ = openai.request_parts("https://api.openai.com/v1", "model", "secret", [], False)
    assert url == "https://api.openai.com/v1/chat/completions"
    assert headers["Authorization"] == "Bearer secret"
    url, headers, _ = anthropic.request_parts("https://api.anthropic.com", "model", "secret", [], False)
    assert url == "https://api.anthropic.com/v1/messages"
    assert headers["x-api-key"] == "secret"


def test_response_normalization():
    assert openai.response_content({"choices": [{"message": {"content": "hello"}}]}) == "hello"
    assert anthropic.response_content({"content": [{"type": "text", "text": "hello"}]}) == "hello"


def test_errors_are_understandable_and_do_not_include_request_details():
    request = httpx.Request("POST", "https://example.com", headers={"Authorization": "Bearer secret"})
    error = httpx.HTTPStatusError("raw secret", request=request, response=httpx.Response(429, request=request))
    detail = _error_detail(error)
    assert "频繁" in detail and "secret" not in detail


def test_anthropic_system_messages_are_lifted_to_top_level():
    _, _, payload = anthropic.request_parts(
        "https://api.anthropic.com", "model", "secret",
        [{"role": "system", "content": "Be concise"}, {"role": "user", "content": "Hello"}], False,
    )
    assert payload["system"] == "Be concise"
    assert [message["role"] for message in payload["messages"]] == ["user"]


def test_openai_rejects_malformed_success_payload():
    import pytest
    with pytest.raises(ValueError):
        openai.response_content({"choices": []})


def test_provider_result_metadata_and_model_list_are_secret_free():
    result = ProviderTestResult(ok=False, detail="API Key 无效", model="model-a", httpStatus=401, latencyMs=12, checkedAt="2026-01-01T00:00:00Z")
    assert result.model_dump()["httpStatus"] == 401
    models = ProviderModelsResult(models=["model-a", "model-b"], detail="已获取 2 个模型")
    payload = models.model_dump_json()
    assert "model-a" in payload and "apiKey" not in payload
