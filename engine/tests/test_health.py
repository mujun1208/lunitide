import importlib

from fastapi.testclient import TestClient


def load_app(monkeypatch):
    monkeypatch.setenv("LUNITIDE_ENGINE_TOKEN", "test-token")
    import app as engine_app
    return importlib.reload(engine_app).app


def test_health_rejects_missing_token(monkeypatch):
    client = TestClient(load_app(monkeypatch))
    assert client.get("/health").status_code == 401


def test_health_accepts_valid_token(monkeypatch):
    client = TestClient(load_app(monkeypatch))
    response = client.get("/health", headers={"Authorization": "Bearer test-token"})
    assert response.status_code == 200
    assert response.json()["status"] == "ready"
