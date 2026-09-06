"""Validate completed gate receipts and derive the second-stage evidence summary.

Usage: python finalize-phase2.py <evidence-directory>
No score or physical-test result is inferred from passing automated checks.
"""
from __future__ import annotations

import hashlib
import json
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


def read_text(path: Path) -> str:
    raw = path.read_bytes()
    return raw.decode("utf-16" if raw.startswith((b"\xff\xfe", b"\xfe\xff")) else "utf-8-sig")


def read_json(path: Path):
    return json.loads(read_text(path))


def require(ok: bool, message: str):
    if not ok:
        raise ValueError(message)


def sha(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def go_summary(path: Path):
    events = [json.loads(line) for line in read_text(path).splitlines() if line.strip()]
    failures = [r for r in events if r.get("Action") in ("fail", "build-fail")]
    require(not failures, f"Go failures in {path.name}: {failures}")
    started = {r["Package"] for r in events if r.get("Action") == "start"}
    finished = {r["Package"] for r in events if not r.get("Test") and r.get("Action") in ("pass", "skip")}
    require(bool(started) and started == finished, f"Incomplete package results in {path.name}")
    passed = [r for r in events if r.get("Test") and r.get("Action") == "pass"]
    require(bool(passed), f"No passing test events in {path.name}")
    return {
        "log": path.name,
        "passed_packages": sum(1 for r in events if not r.get("Test") and r.get("Action") == "pass"),
        "packages_with_test_events": len({r["Package"] for r in events if r.get("Test")}),
        "passed_tests_including_subtests": len(passed),
        "passed_top_level_tests": sum("/" not in r["Test"] for r in passed),
        "failed": len(failures),
        "skipped_tests": [{"package": r["Package"], "test": r["Test"]} for r in events if r.get("Test") and r.get("Action") == "skip"],
        "skipped_packages": [r["Package"] for r in events if not r.get("Test") and r.get("Action") == "skip"],
    }


def concatenated_json(text: str):
    decoder = json.JSONDecoder()
    offset = 0
    while offset < len(text):
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            break
        value, offset = decoder.raw_decode(text, offset)
        yield value


def main(directory: Path):
    directory = directory.resolve(strict=True)
    root = Path(__file__).resolve().parents[4]
    require(root.joinpath("go.mod").is_file(), "Cannot resolve repository root")
    groups = ("go", "race", "web", "static", "security", "release-tools")
    required_checks = {
        "go": {"go-tests", "coverage"}, "race": {"go-race"},
        "web": {"bridge", "typecheck", "web-tests", "web-build"},
        "static": {"go-vet", "go-build", "go-lint"},
        "security": {"govulncheck", "npm-audit"},
        "release-tools": {"release-tools-ps5", "release-tools-ps7", "release-exclusions"},
    }
    receipts = {name: read_json(directory / f"{name}-receipt.json") for name in groups}
    reference = receipts["go"]["source"]
    for name, receipt in receipts.items():
        require(receipt["group"] == name, f"Wrong gate group in {name}")
        require(receipt["status"] == "passed" and receipt["sourceUnchanged"], f"Unfinished or changed source in {name}")
        require(receipt["source"]["commit"] == reference["commit"] and receipt["source"]["treeSha256"] == reference["treeSha256"], f"Different source snapshot in {name}")
        require(bool(receipt["checks"]) and all(c["exitCode"] == 0 for c in receipt["checks"]), f"Failed or empty checks in {name}")
        require({c["name"] for c in receipt["checks"]} == required_checks[name] and len(receipt["checks"]) == len(required_checks[name]), f"Missing, duplicate or unexpected checks in {name}")
        for check in receipt["checks"]:
            require((directory / check["stdout"]).is_file() and (directory / check["stderr"]).is_file(), f"Missing raw output for {check['name']}")

    manifest = read_json(directory / reference["manifest"])
    files = manifest["files"]
    lines = "\n".join(f"{f['sha256']}  {f['path']}" for f in files)
    require(hashlib.sha256(lines.encode()).hexdigest() == reference["treeSha256"], "Source manifest hash is invalid")
    require(len(files) == reference["fileCount"], "Source manifest count is invalid")
    paths = subprocess.run(["git", "-C", str(root), "-c", "core.quotepath=false", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], check=True, capture_output=True).stdout.decode("utf-8").split("\0")
    current = {p for p in paths if p and not p.startswith("docs/") and root.joinpath(p).is_file()}
    head = subprocess.run(["git", "-C", str(root), "rev-parse", "HEAD"], check=True, capture_output=True, text=True).stdout.strip()
    require(head == reference["commit"], "Repository HEAD changed after validation")
    require(current == {f["path"] for f in files}, "Source files were added or removed after validation")
    for file in files:
        require(sha(root / file["path"]) == file["sha256"], f"Source changed after validation: {file['path']}")

    ordinary = go_summary(directory / "go-tests.jsonl")
    race = go_summary(directory / "go-race.jsonl")
    coverage = re.search(r"([0-9]+(?:\.[0-9]+)?)%", read_text(directory / "coverage.txt").splitlines()[-1])
    require(coverage is not None, "Missing coverage total")
    percent = float(coverage.group(1))
    floor = receipts["go"]["coverageFloor"]
    require(51 <= floor <= 100 and percent >= floor and percent == receipts["go"]["coveragePercent"], "Coverage floor or receipt mismatch")
    ordinary["coverage_percent"] = percent
    ordinary["coverage_floor_percent"] = floor
    web = read_json(directory / "web-tests.json")
    require(web["success"] and web["numFailedTests"] == 0 and web["numPassedTests"] > 0, "Renderer suite failed")
    npm = read_json(directory / "npm-audit.json")
    require(npm["metadata"]["vulnerabilities"]["total"] == 0, "Renderer dependencies need assessment")
    findings = [obj["finding"] for obj in concatenated_json(read_text(directory / "govulncheck.jsonl")) if "finding" in obj]
    reachable = [f for f in findings if any(t.get("function") for t in f.get("trace", []))]
    require(not reachable, "Reachable Go vulnerability remains")
    result = {
        "captured_at": datetime.now(timezone.utc).isoformat(),
        "scope": "code-and-automated-delivery; physical and signed-release acceptance remain user-owned",
        "evidence_directory": directory.relative_to(root).as_posix(),
        "source": reference,
        "go": ordinary,
        "full_library_race": race,
        "renderer": {"files": len(web["testResults"]), "passed": web["numPassedTests"], "failed": web["numFailedTests"], "skipped": web["numPendingTests"]},
        "checks": {name: receipts[name]["checks"] for name in groups},
        "npm_audit": npm["metadata"]["vulnerabilities"],
        "govulncheck": {"reachable": len(reachable), "module_or_package_only": findings},
        "engineering_score": None,
        "physical_system_score": None,
        "score_note": "Scores require the separate module-by-module review; this script does not award them.",
        "pending_user_acceptance": ["real microphone/loopback and three voice providers", "real external model/MCP/database and multi-device colleague connections", "real Win32/UIA/ConPTY and Windows device matrix", "publisher signing and clean install/update/uninstall matrix", "72-hour mixed workload, 14-day observation and business-quality samples"],
        "evidence_files": [{"path": p.name, "bytes": p.stat().st_size, "sha256": sha(p)} for p in sorted(directory.iterdir()) if p.is_file()],
    }
    output = directory.parent / "phase2-verification-results.json"
    output.write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(output), "go_pass": ordinary["passed_tests_including_subtests"], "race_pass": race["passed_tests_including_subtests"], "web_pass": web["numPassedTests"], "coverage": percent}, ensure_ascii=False))


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("Usage: python finalize-phase2.py <evidence-directory>")
    main(Path(sys.argv[1]))
