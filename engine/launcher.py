from __future__ import annotations

import argparse
import json
import os
import sys


def read_handshake() -> None:
    line = sys.stdin.readline()
    if not line:
        raise RuntimeError("Missing engine handshake")
    payload = json.loads(line)
    token = payload.get("token")
    parent_pid = payload.get("parentPid")
    if not isinstance(token, str) or len(token) < 32:
        raise RuntimeError("Invalid engine handshake token")
    os.environ["LUNITIDE_ENGINE_TOKEN"] = token
    if isinstance(parent_pid, int) and parent_pid > 0:
        os.environ["LUNITIDE_PARENT_PID"] = str(parent_pid)


def main() -> None:
    parser = argparse.ArgumentParser(description="Lunitide local engine")
    parser.add_argument("--port", type=int, required=True)
    args = parser.parse_args()
    read_handshake()

    import uvicorn
    from app import app
    uvicorn.run(app, host="127.0.0.1", port=args.port, log_level="warning")


if __name__ == "__main__":
    main()
