# Agent Hub capability matrix (V1.2)

| Adapter | LookPath | Non-interactive argv | Stream JSON | Production Detect `available` | Execute |
|---|---|---|---|---|---|
| Codex | `codex` | `codex exec --json --skip-git-repo-check --ignore-user-config --sandbox <sandbox> --cd <workDir> -o <workDir>/codex-last-message.md` + prompt on stdin | yes | exe found **and** this row is YES (`--version` is display-only) | yes |
| Cursor | `cursor-agent`（含 `%LOCALAPPDATA%\cursor-agent`） | `cursor-agent -p --force --trust --output-format stream-json` + prompt on stdin, `Dir=workDir` | yes | same | yes |
| Kimi | `kimi`（含 `~/.kimi-code/bin` 与 `~/.kimi-code/node_modules/.bin`） | `kimi -p <prompt> --output-format stream-json`, `Dir=workDir` | yes | exe found **and** this row is YES (`--version` is display-only） | yes |

Parser fixtures: `testdata/codex-hello.jsonl`, `testdata/cursor-hello.jsonl`, `testdata/kimi-hello.jsonl`.

Windows sandbox flags are not treated as a security boundary. The UI says so.
