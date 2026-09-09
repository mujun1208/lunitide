# Automation and Weekly Stream Follow-up

Date: 2026-09-08. Scope: automation, the saved weekly draft, and shared model
stream failures. No service restart, deployment, provider configuration change,
expert lifecycle edit, meeting edit, or image/video/OCR adapter edit was made.

## Findings and Fixes

The two workflows use the same chat stream and terminal-error path. Confirmed
local defects in that path were:

- Transport and response-size classes were collapsed to `UPSTREAM_FAILED`.
  Failed model calls now log only a local error classification, validated stage,
  HTTP status when actually available, call number, and received byte counts.
  User-facing failures distinguish connection, DNS, TLS, size limits, interrupted
  streams and provider-reported stream errors. Provider text is never copied to
  these diagnostics.
- The OpenAI-compatible stream reader ignored `error` envelopes, including an
  error followed by `[DONE]`. Both chat readers accepted EOF without a protocol
  completion marker. Error frames now fail explicitly; missing `[DONE]` or
  `message_stop` fails as `STREAM_INCOMPLETE`. Partial text/reasoning and usage
  remain available. Interrupted requests are not retried, and OpenAI tool calls
  are not released from an incomplete stream.
- Provider error classes carried inside a successful HTTP stream are classified
  separately. Their classification does not invent an HTTP 4xx/5xx status.
- An unattended HTTP 400 could drop the tool set and finish as plain chat.
  Automation now retains the failure instead of claiming completion without
  search/file tools. Existing draft-trial protection remains in place.
- A weekly failure test exposed raw provider text in the Office outcome notice,
  despite a sanitized terminal event. The shared run now uses the same safe
  diagnosis for its visible/persisted failure notice.

The original external failure cause is not proven. The saved historical error
and available engine logs do not preserve its underlying transport/provider
detail. This work must not be represented as proof of a particular outage,
credential issue, prompt defect, or stable provider reliability.

## Live Evidence

These validations exercised the already-running installed build. The source
fixes in this follow-up were not deployed, so live success is not attributed to
the new stream-handling code.

### Weekly Draft

- Real draft: `01M1ZQ1NEYZJHTW18H2JAE8KCG`, `weekly-report/SKILL.md`.
- New synthetic trial session: `01M20PY8W0M9GQ0SKBJED97NGF`.
- Persisted assistant message: `01M20PZ60GRQMMS5173WT5CYA1`.
- Tools: `skill.try`, `docx.gen`, `workspace.read`.
- Final live event: `completed`, approximately 29.8 seconds after startup.
- No approval pause or desktop/application open occurred.
- Artifact: `C:/Users/mujun/AppData/Local/Lunitide-E2E/tool-workspaces/01M20PY8W0M9GQ0SKBJED97NGF/weekly-stream-check.docx`.
- Size: 5,647 bytes.
- SHA256: `6867A3D6072B8247BA4D814B89E84D8B03B191B58455849A9BB1FB23BEA2C7E8`.
- Independent ZIP/XML inspection confirmed `STREAM-CHECK-20260908`, three
  integration tasks, five fixed defects, and two planned regression rounds.
  The persisted message lists a `docx.gen` artifact with that filename.

### Existing News Automation

- Existing job: `3FCJCZEJBAEZN6TEHT76PJ5VWQ`, model `glm-5.3`.
- Original run `01M1ZVFDM4D2XCDQMT51Z3S4ZS` remains `failed` with its original
  `UPSTREAM_FAILED` error and completion timestamp.
- One manual rerun: `01M20Q1R207H3B9XJ2EG2EHMM0`.
- Fresh `automation.run.list` read and on-disk journal both confirm `succeeded`,
  finished at `2026-09-08T14:36:31Z`, with an empty error field.
- Bound session: `01M1ZHQQBC0SFTXTN6D2T6QAQP`.
- Persisted response: `01M20Q5Z1MW7PSH7A8VNDM02HN`, containing a TOP10 report.
- `message.process` contains 15 completed `web.search`, seven completed
  `web.fetch`, and one completed `mcp.search` receipt; `truncated=false`.
- The job remains disabled, with its original `35 14 * * *` schedule, provider,
  model and prompt. Normal manual execution updates its last-run timestamp.
- No new file was generated in the bound workspace by that rerun. Its successful
  status verifies execution and persistence only; it does not satisfy the
  user's separate file-delivery acceptance requirement. See the follow-up below.

The new helper initially reused a JSON destination slice, which retained an
omitted error field from the old failed receipt. Its decode destination is now
reset before each poll. A separate fresh read confirmed the successful run has
no error; no production history or scheduler code was changed for that issue.

### News DOCX Follow-up

The user additionally required an actual Word file based only on the completed
TOP10 report, with its original sources. All new installed-app attempts used
separate synthetic sessions, never the original automation's bound session.
No original prompt, schedule, provider configuration, or history was overwritten.

Reference provenance:

- Source run: `01M20Q1R207H3B9XJ2EG2EHMM0`.
- Source message: `01M20Q5Z1MW7PSH7A8VNDM02HN`.
- Report SHA256: `ba12d8a3e1c7bc01b6fa4cd632d2f34f42a689b65b450c38bc4965a0e9ced01a`.
- Ten original news rows, 40 original cells, original summary/source-attribution
  paragraphs, and 23 complete URLs recovered from the original successful
  search/fetch receipts. Truncated trailing URLs were not reconstructed.
- `reference.json` retains those values under `_scratch/news-artifact-check/`.

Installed-app conversion attempts:

| Synthetic session | Configured model | Terminal result |
| --- | --- | --- |
| `01M20RNWDPQRZ7RPGG414W2JYF` | `deepseek-v4-flash` | `UPSTREAM_FAILED`; no DOCX |
| `01M20RQQ7BWFPW0DTGMFHNN6NM` | `glm-5.3` | `UPSTREAM_FAILED`; no DOCX |
| `01M20RV1PRVZ62YARDB95GW7AJ` | `deepseek-v4-flash` | Canceled after repeated tool discovery; no DOCX |
| `01M20RY2PXASVNV4D9W1VF8NWP` | `deepseek-v4-flash` | Reference read, then `UPSTREAM_FAILED`; no DOCX |
| `01M20S0MRX2KHNZAR40VH5RYT4` | `glm-5.3` | Reference read, then `UPSTREAM_FAILED`; no DOCX |
| `01M20S5Z9HAB963EZJEPC3JNQ6` | `deepseek-v4-pro` | Reference read, then `UPSTREAM_FAILED`; no DOCX |

The initial file-reference wording exposed a separate routing gap:
`internal/app/task_route.go` recognizes `生成文档`, but not `生成 Word 文档`;
`新闻` plus the negated `不打开文件` can select R2, which includes `docx.gen`
but omits `workspace.read`. That attempt repeatedly searched for tools. A new
session beginning with `生成文档` did execute `workspace.read`, but still failed
upstream. This is not proof of the original failure's cause. The shared routing
file was not edited; this evidence is for the parent's follow-up.

A deterministic local export now provides an inspectable file, without claiming
the installed model-to-artifact path passed:

- File: `E:/Trae-Work-Projects/lunitide/_scratch/news-artifact-check/news-top10-reference.docx`.
- Generator: the repository's existing `officetools.GenDocxDoc`, no model call.
- Size: 8,678 bytes; SHA256:
  `46F29DB14399B96B84052A13DA265B2D9F91CB9815618A3BB528A44802EB97C5`.
- The helper and an independent PowerShell ZIP/XML check confirmed all 40 cells
  and all 23 original URLs; zero missing or extra URLs. Original summary,
  source attribution, run/message IDs, and the unverified-facts notice remain.
- The document explicitly states it is a local export, not an installed-app
  success receipt. `evidence.json` records `localExport=true` and
  `installedAcceptancePassed=false`; no artifact receipt was fabricated.
- This verifies OOXML structure and transcription fidelity, not rendered page
  layout. The bundled document-rendering dependency loader was unavailable.
- News facts remain **unverified**. Original retrieval URLs include unrelated
  results and access-blocked pages; their presence is not evidence supporting
  each claim. No new facts, replacement sources, or guessed URLs were added.

Byte comparisons confirmed `automation/jobs.json` and `automation/runs.jsonl`
unchanged during the export. A final read-only gateway check again confirmed
the old run failed, the separate rerun succeeded, and the job remained disabled
with cron `35 14 * * *` and model `glm-5.3`.

Acceptance remains split: an actual locally verified DOCX is delivered; successful
installed-app news DOCX generation is **blocked**. Parent-owned deployment and a
new synthetic retest are still required. These runs used the installed build,
not the undeployed source fixes, and must not be reported as validating them.

## Verification

- Full `go test ./internal/llmadapter` passed.
- Focused adapter interruption/error-frame tests passed after the final fixture
  update, including errors followed by a completion marker.
- Focused app tests passed: sanitized diagnostics, real draft loading followed
  by a stream failure, automation failed receipts preserving old history,
  existing stream classification, headless execution, and draft-trial tests.
- `go test ./internal/scheduler` passed as part of the broader run.
- `go test -tags lunitide_e2e ./_scratch/news-artifact-check` compiled the new
  acceptance helper (no package test files); its live reference/export checks
  and the independent ZIP/XML verification passed.
- The broad app run failed at
  `TestExpertBridgeTrialDoesNotEnableAndReportsFailure`,
  `internal/app/expert_lifecycle_handlers_test.go:60`, returning
  `EXPERT_TRIAL_FAILED`. This is in the separately owned expert lifecycle work;
  it was not modified here. Full-run output is retained at
  `_scratch/automation-stream-check/app-tests.jsonl`.

## Files and Reproduction

Edited existing files, preserving their pre-existing dirty changes:

- `internal/app/chat_run_stream.go`
- `internal/llmadapter/openai.go`
- `internal/llmadapter/anthropic.go`
- `internal/llmadapter/types.go` (adds only `StageStream`)

Added files:

- `internal/app/chat_model_stream_error.go`
- `internal/app/chat_model_stream_error_test.go`
- `internal/llmadapter/chat_stream_error.go`
- `internal/llmadapter/chat_stream_error_test.go`
- `_scratch/automation-stream-check/main.go`
- `_scratch/news-artifact-check/main.go` and its reference/artifact/evidence
  outputs (local scratch files).
- This follow-up audit.

Existing live-task/diagnostic helpers were not modified. The new automation
helper's default operation triggers the existing job once; use the following
read-only command to inspect the completed validation without another run:

```powershell
go run -tags lunitide_e2e ./_scratch/automation-stream-check -read-only -message 01M20Q5Z1MW7PSH7A8VNDM02HN
go test ./internal/llmadapter
go test ./internal/app -run 'TestChatModelStreamFailureDiagnosticsAreSanitized|TestWeeklyTrialStreamFailureAfterLoadingKeepsDraftAndFails|TestAutomationStreamFailurePersistsSeparateFailedRun|TestChatStreamErrorMapsProviderFailureClasses|TestHeadless|TestDraftTrial|TestChatStartDraftTrial' -count=1
```

News conversion reproduction, after parent-owned deployment:

```powershell
# Read the original completed report and receipts; does not create a session.
go run -tags lunitide_e2e ./_scratch/news-artifact-check
# Creates a NEW synthetic session and requests a DOCX using only that reference.
go run -tags lunitide_e2e ./_scratch/news-artifact-check -generate -file-reference -model deepseek-v4-flash
# Read-only verification requires a real file AND its persisted docx.gen receipt.
go run -tags lunitide_e2e ./_scratch/news-artifact-check -verify <new-session-id>
```

The separate `-local-export` mode produced the delivered reference file. It
refuses to overwrite that DOCX and never counts as installed-app acceptance.
