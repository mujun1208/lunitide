from __future__ import annotations

import asyncio
import ctypes
import hmac
import os
from contextlib import asynccontextmanager, suppress

from fastapi import Depends, FastAPI, Header, HTTPException, status

try:
    from .gateway.router import router as gateway_router
except ImportError:  # PyInstaller launches app as a top-level module
    from gateway.router import router as gateway_router


def _process_exists(pid: int) -> bool:
    if pid <= 0:
        return False
    if os.name == "nt":
        process_query_limited_information = 0x1000
        handle = ctypes.windll.kernel32.OpenProcess(process_query_limited_information, False, pid)
        if not handle:
            return False
        ctypes.windll.kernel32.CloseHandle(handle)
        return True
    try:
        os.kill(pid, 0)
    except (OSError, ProcessLookupError):
        return False
    return True


async def _watch_parent(parent_pid: int) -> None:
    while True:
        await asyncio.sleep(2)
        if not _process_exists(parent_pid):
            os._exit(0)


def create_app(engine_token: str | None = None, parent_pid: int | None = None) -> FastAPI:
    token = engine_token if engine_token is not None else os.environ.get("LUNITIDE_ENGINE_TOKEN", "")
    configured_parent = parent_pid
    if configured_parent is None:
        raw_parent = os.environ.get("LUNITIDE_PARENT_PID", "")
        configured_parent = int(raw_parent) if raw_parent.isdigit() else 0

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        watcher = asyncio.create_task(_watch_parent(configured_parent)) if configured_parent else None
        try:
            yield
        finally:
            if watcher:
                watcher.cancel()
                with suppress(asyncio.CancelledError):
                    await watcher

    application = FastAPI(
        title="Lunitide Local Engine",
        version="0.2.0",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
        lifespan=lifespan,
    )

    def require_engine_token(authorization: str | None = Header(default=None)) -> None:
        expected = f"Bearer {token}"
        if not token or not authorization or not hmac.compare_digest(authorization, expected):
            raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail="Unauthorized")

    @application.middleware("http")
    async def limit_request_size(request, call_next):
        content_length = request.headers.get("content-length")
        if content_length and int(content_length) > 2_000_000:
            raise HTTPException(status_code=status.HTTP_413_REQUEST_ENTITY_TOO_LARGE, detail="Request too large")
        return await call_next(request)

    application.include_router(gateway_router, dependencies=[Depends(require_engine_token)])

    @application.get("/health", dependencies=[Depends(require_engine_token)])
    def health() -> dict[str, str | int]:
        return {"status": "ready", "service": "lunitide-engine", "version": "0.2.0", "pid": os.getpid()}

    return application


app = create_app()
