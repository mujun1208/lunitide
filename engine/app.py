from __future__ import annotations

import hmac
import os

from fastapi import Depends, FastAPI, Header, HTTPException, status


ENGINE_TOKEN = os.environ.get("LUNITIDE_ENGINE_TOKEN", "")

app = FastAPI(
    title="Lunitide Local Engine",
    version="0.1.0",
    docs_url=None,
    redoc_url=None,
)


def require_engine_token(authorization: str | None = Header(default=None)) -> None:
    expected = f"Bearer {ENGINE_TOKEN}"
    if not ENGINE_TOKEN or not authorization or not hmac.compare_digest(authorization, expected):
        raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Unauthorized")


@app.get("/health", dependencies=[Depends(require_engine_token)])
def health() -> dict[str, str | int]:
    return {
        "status": "ready",
        "service": "lunitide-engine",
        "version": "0.1.0",
        "pid": os.getpid(),
    }
