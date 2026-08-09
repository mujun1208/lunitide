from fastapi.testclient import TestClient

from engine.app import create_app


def test_health_rejects_missing_token():
    client = TestClient(create_app("test-token", parent_pid=0))
    assert client.get("/health").status_code == 401


def test_health_rejects_wrong_token():
    client = TestClient(create_app("test-token", parent_pid=0))
    response = client.get("/health", headers={"Authorization": "Bearer wrong-token"})
    assert response.status_code == 401


def test_health_rejects_all_requests_when_server_token_is_empty():
    client = TestClient(create_app("", parent_pid=0))
    response = client.get("/health", headers={"Authorization": "Bearer "})
    assert response.status_code == 401


def test_health_accepts_valid_token():
    client = TestClient(create_app("test-token", parent_pid=0))
    response = client.get("/health", headers={"Authorization": "Bearer test-token"})
    assert response.status_code == 200
    assert response.json()["status"] == "ready"
    assert response.json()["service"] == "lunitide-engine"
    assert isinstance(response.json()["pid"], int)


def test_openapi_is_not_exposed():
    client = TestClient(create_app("test-token", parent_pid=0))
    assert client.get("/openapi.json", headers={"Authorization": "Bearer test-token"}).status_code == 404
