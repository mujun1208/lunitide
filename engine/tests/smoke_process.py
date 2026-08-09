from __future__ import annotations

import json
import os
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
TOKEN = "m1-smoke-token"


def free_port() -> int:
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


def request(port: int, token: str | None) -> tuple[int, dict]:
    headers = {"Authorization": f"Bearer {token}"} if token else {}
    req = urllib.request.Request(f"http://127.0.0.1:{port}/health", headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=1) as response:
            return response.status, json.loads(response.read())
    except urllib.error.HTTPError as error:
        return error.code, {}


def main() -> None:
    port = free_port()
    env = {**os.environ, "LUNITIDE_ENGINE_TOKEN": TOKEN, "LUNITIDE_PARENT_PID": str(os.getpid())}
    process = subprocess.Popen(
        [sys.executable, "-m", "uvicorn", "app:app", "--host", "127.0.0.1", "--port", str(port), "--log-level", "warning"],
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    try:
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if process.poll() is not None:
                stderr = process.stderr.read().decode(errors="replace") if process.stderr else ""
                raise RuntimeError(f"engine exited early: {stderr}")
            try:
                code, body = request(port, TOKEN)
                if code == 200 and body.get("status") == "ready":
                    break
            except OSError:
                time.sleep(0.1)
        else:
            raise TimeoutError("engine did not become ready in 15 seconds")

        assert request(port, None)[0] == 401
        assert request(port, "wrong")[0] == 401
        assert request(port, TOKEN)[0] == 200
        print(f"engine_smoke_ok pid={process.pid} port={port}")
    finally:
        process.terminate()
        try:
            process.wait(timeout=3)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)
    assert process.poll() is not None


if __name__ == "__main__":
    main()
