# News Artifact Post-deployment Acceptance

Date: 2026-09-09, Asia/Shanghai. Exactly one requested news generation and one
small weekly draft regression ran. No restart, deployment, provider-config
mutation, or additional paid retry was performed by this task.

## Deployed Build

The running process was PID 59592, executable
`release/out/e2e-gui-20260908/lunitide-engine.exe`. Its SHA256 matched the parent:
`42D0890B750EFD828E3CFE651F1F311BF9F69DA8670F2359AB53299BD7D616AA`.

## News Result

- Command: `go run -tags lunitide_e2e ./_scratch/news-artifact-check -generate -file-reference -model deepseek-v4-flash`.
- Synthetic session: `01M20WSDNA963MC1D19AGPQXEJ`.
- Stream: `01M20WSDPP4FKEP8ZG44EMM7J9`.
- Persisted message: `01M20WT904BBSKYMW4KMMGEMWW`.
- Terminal: `failed`, code `UPSTREAM_RESPONSE_TOO_LARGE`, at
  `2026-09-08T16:15:00Z` (00:15 local time).
- Three `workspace.read` receipts cover offsets 0, 1838, 3886 through 4589,
  with the last receipt reporting `complete=true`.
- Failed model call: 4; stage: `stream`; HTTP status: 0 (not a fabricated HTTP
  error); received text: 0 bytes; received reasoning: 12,177 bytes.
- The workspace contains only the 7,101-byte `news-reference.json`. Read-only
  `-verify 01M20WSDNA963MC1D19AGPQXEJ` confirms the DOCX does not exist.
- There is no `docx.gen` receipt and no persisted artifact. The old local
  reference export is not counted as this app-generated result.

Source content remains the previously completed TOP10 message
`01M20Q5Z1MW7PSH7A8VNDM02HN`, with 10 rows and 23 recovered original URLs.
Report SHA256 remains
`ba12d8a3e1c7bc01b6fa4cd632d2f34f42a689b65b450c38bc4965a0e9ced01a`.
News facts are still not independently fact-checked.

## Diagnosis and Narrow Fixes

The deployed terminal proves a **local receive-size guard** rejected the stream,
not authentication, model-channel unavailability, or a provider 503.
`internal/app/engine.go:964` sets `MaxResponseBytes: 1 << 20`; this limits the
whole SSE wire body, including JSON framing, through
`internal/networkpolicy/client.go`'s `limitedBody.Read`. Separate defaults limit
each SSE line to 64 KiB and each event to 1 MiB.

The offline reproduction uses the real connector body limiter and OpenAI parser
with a completely synthetic HTTP transport, no sockets or credentials:

- 6,000 valid SSE chunks, 220 bytes per line; total wire body 1,332,014 bytes.
- With the current 1 MiB budget it fails at operation `read response`, after
  only 14,169 decoded reasoning bytes, before `[DONE]`.
- The identical body with an 8 MiB test-only budget completes with 18,000
  reasoning bytes. Both cases make exactly one synthetic HTTP call.

This demonstrates that the current wire budget can reject modest decoded
output, and strongly implicates it in this incident. It does **not** prove which
sub-limit fired in the deployed call: the installed adapter discarded the
allowlisted local operation. No raw upstream body was captured or replayed.

Two narrow source fixes, not deployed by this task:

1. Preserve local body/line/event size classes in `classifyStreamError`, with
   safe `UPSTREAM_RESPONSE_BODY_TOO_LARGE`, `UPSTREAM_RESPONSE_LINE_TOO_LARGE`,
   and `UPSTREAM_RESPONSE_EVENT_TOO_LARGE` mappings. Unknown operation text is
   discarded. These remain non-retryable and expose no vendor text.
2. When an upstream failure leaves no writable model output, the empty Office
   fallback no longer replaces that diagnosis with a request for missing user
   content. The deployed saved message did exactly that. A regression test
   reproduced the wrong persisted text before the fix, then passed afterward.
   Successful Office fallback behavior remains covered and unchanged.

The shared engine/network response budget was **not changed**, since doing so
could affect separately owned provider/multimodal paths. The next fix should
give chat SSE an explicit bounded wire budget without removing line/event,
generation-token, timeout, or retry protections. An 8 MiB offline control is
evidence, not a live-tested sizing guarantee.

## Weekly Result

- Model: `deepseek-v4-flash`; synthetic session: `01M20WYRKHJ62MKZ1VN8XG20DT`.
- Real draft: `01M1ZQ1NEYZJHTW18H2JAE8KCG`, version `1.0.0`.
- `skill.try` returned its full contract with manifest digest
  `1ff304a4c4c9170e154c8685aaf1a080f1c6b5e20bd7666d1cac6cfb2adf2a49`.
- The single turn completed in 42,047 ms. Two locally rejected `docx.gen`
  arguments (trivial body, then unknown `blocks[2].heading2`) were corrected
  within that turn; the third tool invocation generated the file.
- Persisted message: `01M20X01NFG208S4KQVG0TXB3A`, with a real `docx.gen`
  artifact receipt for `weekly-postdeploy-check.docx`.
- File: `C:/Users/mujun/AppData/Local/Lunitide-E2E/tool-workspaces/01M20WYRKHJ62MKZ1VN8XG20DT/weekly-postdeploy-check.docx`.
- Size: 5,769 bytes. SHA256:
  `680DEB6EB4B6EE437373FBD3A0B2F08969EEE8BF40B62F7914341D2E23313EBA`.
- Both app `workspace.read` and independent ZIP/XML inspection verified marker
  `WEEKLY-POSTDEPLOY-20260909`, 3 integrations, 5 defects, 2 regression rounds,
  the supplied timeout risk, all five draft sections, and missing-data labels.
- No desktop open, skill publish, or skill edit tool was invoked. No independent
  rendered-page layout check was performed.

## History and Scope

A final read-only check confirms original run `01M1ZVFDM4D2XCDQMT51Z3S4ZS`
remains failed with `UPSTREAM_FAILED`; the separate earlier rerun
`01M20Q1R207H3B9XJ2EG2EHMM0` remains succeeded. Job
`3FCJCZEJBAEZN6TEHT76PJ5VWQ` remains disabled with cron `35 14 * * *` and model
`glm-5.3`. This task did not edit its prompt or history.

Changed in this post-deployment follow-up:

- `internal/app/chat_run_stream.go` (empty Office fallback notice only).
- `internal/app/chat_model_stream_error.go` and its test file.
- `internal/llmadapter/chat_stream_error.go` and its test file.
- `_scratch/news-artifact-check/sse_limit_test.go` (offline reproduction).
- This audit.

No edits to `openai.go`, `anthropic.go`, or response metadata were made during
this follow-up; those remain Galileo's finish-reason work. No provider config,
diagnostics, image/video/OCR adapter, expert lifecycle, or meeting changes.

## Tests

Passed after these fixes:

```powershell
go test ./internal/llmadapter -count=1
go test ./internal/app -run 'TestOfficeFallbackEmptyReplyKeepsModelFailureDiagnosis|TestRunStreamOfficeFallbackSuccessPersistsMessageArtifactAndProcess|TestChatModel|TestWeeklyTrialStreamFailureAfterLoadingKeepsDraftAndFails|TestAutomationStreamFailurePersistsSeparateFailedRun|TestChatStreamErrorMapsProviderFailureClasses' -count=1
go test -tags lunitide_e2e ./_scratch/news-artifact-check -run TestOfflineSSEWireBudget -v -count=1
```

News app-artifact acceptance remains **failed**. Weekly live draft-to-DOCX
acceptance **passed**. No live work remains running, and no further paid request
is authorized by this result alone.

## Authorized Budget and Finish-Reason Follow-up

After the results above, the parent explicitly authorized the underlying
production-adapter budget fix and consumption of Galileo's finish metadata.
These source changes are not represented as another live acceptance pass.

- `newProductionAdapter` now sets `network.MaxResponseBytes = 8 << 20` on its
  existing local copy. `e.network`, ordinary web fetching, deadlines, SSE
  line/event limits, and request output-token budgets remain unchanged.
- Real production-adapter tests use localhost fixture servers for both OpenAI
  and Anthropic. They accept 1 MiB + 1 byte and exactly 8 MiB of valid SSE wire
  data, reject 8 MiB + 1 byte, reuse the cached connector, and reject invalid
  endpoint/protocol construction without caching it. Separate tests retain
  custom line/event guards and the ordinary network's 1 MiB body limit.
- The budget tests failed above 1 MiB before the constructor fix and passed
  afterward. No external endpoint or provider credentials are used.
- The shared turn-budget wrapper defers tool deltas until normal completion.
  Explicit `length` and `content_filter` produce sanitized
  `UPSTREAM_RESPONSE_TRUNCATED` / `UPSTREAM_RESPONSE_FILTERED` failures, discard
  the failed call's pending tools, retain partial text and usage, and do not retry.
- The chat run retains buffered partial text and prevents Office fallback from
  turning an explicitly truncated/filtered response into artifact success.
  Forced-summary finish failures also remain failures.
- Regression tests cover both finish reasons, buffered/unbuffered chat,
  persisted partial text, no tool execution or artifact, and normal tool release.
  Existing successful Office fallback remains valid. The earlier empty-fallback
  diagnostic fix is retained.

Additional files in this authorization:

- `internal/app/provider_diagnostics.go`: only the local-copy response budget
  assignment and its comment; existing parent changes are preserved.
- `internal/app/provider_response_budget_test.go`.
- `internal/app/chat_generation_budget.go`.
- `internal/app/chat_finish_reason_test.go`.
- The previously owned chat run, error mapper, and mapper test files.

Galileo's `types.go`, `openai.go`, `anthropic.go`, and finish-metadata files were
not edited. No image/OCR calls, live configuration changes, restarts, or
deployment operations were performed. Parent deployment and one bounded news
retake remain the live verification gate.

## Final Build Retake at 00:50

Exactly one new generation was requested after the parent's 00:48 deployment.
The running engine (PID 62964) matched SHA256
`7E917815C089F7F0FFB67F92D08AC655839B69A85B214F9F8DC9CB668986058E`.
No restart, image/OCR call, weekly rerun, or second news generation was made.

**Actual app artifact acceptance passed; the final chat run failed.**

- Session: `01M20YTQQA6NC87W648RXNTZSB`.
- Stream: `01M20YTQSE0YYC8PPP0W6VYDPX`.
- Persisted message: `01M20YX62BY0NMKER29RRAAAB9`.
- Real persisted `docx.gen` receipt:
  `call_00_LTVwFBsyRO1jR00EZpvv2834`.
- File: `C:/Users/mujun/AppData/Local/Lunitide-E2E/tool-workspaces/01M20YTQQA6NC87W648RXNTZSB/news-top10-acceptance.docx`.
- Size: 8,594 bytes; SHA256:
  `BA575DA07C3CD0E4A2FC4CE6942B66CF72113B31B73E9B3ADE568937D3D23D34`.
- App readback and a separate read-only verifier found the file and receipt.
  Independent ZIP/XML inspection confirmed all 10 original entries, 40 fields,
  original summary/source attribution, run/message IDs, and exactly 23 original
  URLs with zero missing or extra URLs. News facts remain independently
  **unverified**, as stated in the document. This is not the earlier local export.

After `docx.gen` and two successful document readbacks, the application injected
`web.search` with call ID `auto-01M20YX5WJNKTDRY3XYZ9SWCJC`. It returned
`ok:false / invalid arguments`. The next model call failed at
`2026-09-08T16:51:32Z` with exact safe diagnostic:

```text
model_call=9 failed: code=UPSTREAM_BAD_REQUEST stage=http http_status=400 received_text_bytes=0 received_thinking_bytes=0
```

The saved assistant text reports the provider rejection and retains the real
artifact attachment. Neither that run nor its failed history was changed to
success.

The post-generation lookup defect is identified in existing code:

- `chat_run_stream.go` injects a search when `looksLikeCurrentLookupTurn` matches
  and no earlier lookup exists, even after this document was generated/read back.
- `chat_intent.go` matches the word `新闻` without honoring this prompt's explicit
  `不联网` instruction. `fallbackWebSearchArgs` copies the whole goal into `query`.
- The actual instruction is 642 UTF-8 bytes; `toolruntime/runtime.go` rejects
  search queries above 512 bytes before invoking any web transport. This explains
  the local invalid-arguments receipt. No new news source was fetched by it.
- The subsequent provider HTTP 400 is confirmed, but its exact vendor rejection
  reason is not available from the sanitized diagnostics. A relationship to the
  synthetic tool continuation is plausible, not proven.

No production fix or blind retry was made in this retake. The only helper change
renames its broad `installedAcceptancePassed` field to
`installedArtifactAcceptancePassed` and adds `verificationScope=artifact_only`.
Its file check must not be interpreted as verifying a successful final run.
The read-only verifier also confirms automation job/journal files unchanged
during verification.

```powershell
go run -tags lunitide_e2e ./_scratch/news-artifact-check -verify 01M20YTQQA6NC87W648RXNTZSB
```

This command verifies the existing artifact only and makes no model request.
All processes from this retake have ended; no further paid attempt is pending.

## Closeout

Parent confirmed the final full Go suites passed and wrote
`2026-09-09-closeout.md`. A final read-only process check found no news-artifact,
weekly live-task, automation-check, Go test, or Go runner processes. This task has
no active live calls. No further tests or production edits were made.

The final verifier field is `installedArtifactAcceptancePassed=true`, with
`verificationScope=artifact_only`. File delivery passed; the conversion turn's
terminal remains failed with `UPSTREAM_BAD_REQUEST`. Keep both facts in any
overall closeout summary; no failed history was erased or relabeled.

## Final Parent Closure

The historical results above are unchanged. Subsequent parent retakes exposed
and repaired two additional shared workflow defects:

- On lookup-guard build `5FD11976...`, session
  `01M20ZXAKMFQJ7BQHVF26HXTBX` generated a verified file and completed, but a
  mounted PPT identity activated six irrelevant PPT nudges after DOCX success.
  The persisted response therefore contained seven repetitive summaries.
- On format-guard build `1B98DF18...`, session
  `01M210JFEDFCXP0YD7G070636J` entered the report research gate. Its cancelled
  checkpoint contains six generation attempts and two searches, but the
  unique-name counter counted only one research pass. Parent cancelled the
  acceptance stream. Exact per-tool rejection receipts were not persisted;
  the gate diagnosis comes from checkpoint state and source reconstruction,
  not a verified schema error.

Final fixes prioritize requested Office format over expert fallback, retain
offline/reference writing readiness across file reads, avoid research prompts
for that conversion, and count successful research executions individually.
Generator content/format validation and original histories remain intact.
Shared closeout guidance avoids process repetition and default suggestions.

Final installed engine SHA256:
`CCC5D4C53C86E68C4CBC416D91128774752CE39C32CEDA22CE760F06EBF085A6`.
Host PID 63144; Engine PID 15540. Previous binaries are backed up in
`release/out/e2e-gui-20260908/backup-reference-closeout-20260909-013450`.

**Final installed-app acceptance passed, including the whole turn.**

- Session: `01M211CJBNQVY0S9N2GWGD1VR0`.
- Stream: `01M211CJDSMZ8BYZWWJADMYS7P`.
- Completed message: `01M211FCNQYK5YJTAJGW2GN7FE`.
- DOCX receipt: `call_00_ZfREIfJZSpmb7ZVgJWAS0462`.
- File: `C:/Users/mujun/AppData/Local/Lunitide-E2E/tool-workspaces/01M211CJBNQVY0S9N2GWGD1VR0/news-top10-acceptance.docx`.
- 8783 bytes, 56 sections. SHA256:
  `46d62440d167c0e59755930d66fa4f9964ab371870262d2c63627bb21a2da66c`.
- One listing, three source reads, one generation, two document readbacks;
  source complete at 4589 characters, DOCX complete at 3233 characters.
- No web search/fetch or desktop action. One final persisted answer; no
  repeated final summaries. The answer still includes four verification
  bullets, so do not claim a strict three-sentence limit.
- All 10 rows, 40 fields, 23 source URLs and provenance passed independent
  artifact verification; automation job/journal files remained unchanged.
- Live terminal evidence is in
  `release/out/news-reference-final-turn-20260909.log`; artifact-only evidence
  is kept separately in `_scratch/news-artifact-check/evidence.json`.

Full final app tests passed in 83.238 seconds. Six read-first DOCX cases cover
three expert identities and two offline goal wordings; each uses exactly
three calls and persists one concise answer and artifact. No image/OCR probes
were resumed. News truth and real Office page rendering remain outside this
conversion acceptance. All parent acceptance/test processes have finished.
