# Shared desktop execution and voice latency

## Scope

The latest request is to use one computer-execution chain for typed chat and
all three voice input modes, and fix common repetition and outcome-reporting
errors without weakening permissions or speech completeness.

This audit does not claim to revalidate all earlier meeting, expert, Office,
asset, OCR, image-generation, or automation reports.

## Findings and changes

- The entries already converged on `chat.start` / `runStream`, but important
  continuation, buffering and receipt rules were restricted to companion mode.
  Computer tasks now share these rules regardless of input modality.
- Explicit browser-opening plus lookup requests could be routed as pure
  information requests, filtering out desktop tools. Routing now retains both
  the desktop and lookup capabilities.
- Automatic rescue did not recognize equivalent actions performed by other
  tools. It could open again after `desktop.browse`, or type after another
  input tool. Current-turn receipts now suppress equivalent automatic rescue.
- Exact duplicate desktop calls previously replayed only "already done this
  turn", losing the proof of success. They now replay the original receipt.
- Identical failed or explicitly uncertain mutations are bounded even when
  frame IDs change. Fresh observations remain allowed; corrected targets and
  arguments can still be tried. Browser reads, snapshots and tab lists are
  treated as fresh observations, not cached mutations.
- A single browser lookup task reuses its search entry after obtaining query
  evidence instead of launching another search tab for each refined query.
  Explicit result URLs and multiple-page requests remain eligible.
- Long result clipping could remove trailing L0 evidence. The evidence is now
  retained. JSON decoding also handles braces inside evidence strings.
- Verified playback, field input and process-exit receipts can correct a
  contradictory model answer. Unverified input is not reported as written.
  Disk writes remain distinct from synchronization of an open editor buffer.
- Single-operation closeouts no longer replace unrelated parts of a compound
  browser/media result. Open-only detection excludes follow-on work.
- Speculative desktop completion text is buffered until receipt checks. A
  buffered draft is explicitly identified as not yet delivered during a
  continuation, preventing a final answer that only says "already stated".
- Read-only voice answers may stream once this turn has successful lookup
  evidence. No ASR endpoint, interruption, or silence duration was shortened.
- Tool transport completion is labeled as returned, not task completion.
  The terminal companion status no longer derives whole-task success/failure
  from a truncated last-tool string. The backend result supplies the outcome.

## Verification

- Go regression packages: `internal/app`, `internal/toolruntime`,
  `internal/ccapp`, `internal/llmadapter`, `internal/tts`, `internal/voice/...`.
- Focused execution tests cover typed/local/cloud/Volc labels, verified versus
  unverified input, duplicate next-track and typing, process exit, recovery
  after a failed target, bounded retries, fresh browser observations,
  equivalent rescue, evidence clipping, compound results and search-entry reuse.
- Frontend: 67 test files and 751 tests passed using the companion,
  ToolTrajectory and SessionPage test selections. Includes interruption,
  stopping, multi-turn input, speech streaming and lifecycle tests.
- `npm run build` passed, including bridge generation and TypeScript checking.
  Vite reported its existing large-chunk warning.
- E2E host and engine built with the same `lunitide_e2e` profile.
- Targeted `git diff --check` passed; preexisting changes were preserved.

## Live observations

These measure `message.append` / `chat.start` to text, not acoustic
end-of-speech to audible response. Cache state and provider load differ;
they are diagnostic samples, not a controlled performance benchmark.

| Sample | Observed timing |
| --- | --- |
| Initial GLM weather | Tool start 8.5 s; final answer 40.5 s |
| GLM weather after read-only streaming change | Lookup complete 6.1 s; first result text 10.2 s; complete 10.5 s |
| DeepSeek cached weather after change | Lookup complete 1.2 s; first result text 1.9 s; complete 2.3 s |
| GLM browser/news, after initial rescue fix | Substantive final summary at 59.4 s; no spurious desktop.open |
| GLM browser/news on shared-contract build | Substantive result at 48.5 s; one redundant refined search launch exposed and subsequently covered by the search-entry reuse fix |
| GLM browser/weather on final deployed build | Exactly one desktop.browse followed by weather.get; tool start 11.4 s; query complete 13.1 s; final text 18.1 s |

Live session references:

- `01M20C07SC4N85W2P3SCGJ72XH`: GLM weather.
- `01M20C2M8G2MY358T9R324TJZQ`: DeepSeek cached weather.
- `01M20CJMRDN671P3DSH6J6NQHF`: browser/news after initial rescue fix.
- `01M20MBMY0XWQE72H0CBZ9HK6A`: browser/news on shared-contract build.
- `01M20P4K4ZB9F5QMC5Q0P1EE9H`: browser/weather on final deployed build.
- `01M20JWA317PHV66KCNWN4EQAJ`: typed browser entry stopped at the
  existing approval gate; no approval granted and no desktop action executed.

The typed and voice runtime integration tests exercise the same executor with
controlled permissions. The live typed test is not an end-to-end success claim.
Existing approval and standing computer-control authorization were not changed.

## Release and limits

Final built engine SHA256:
`6E56D53CF795EF7CFD6EA58B1C3362E629732480A2198333EF61CB802E18ABF4`

Host SHA256:
`7777D34CCC59C270E4EFE20A721EF7033FAB004A80C0C8E827EF4A0BDADB5FB6`

Renderer: `main-BYH7wjnF.js`, SHA256:
`366B898E766C7325D176FDC7FA62AD588C009E0180496E4DDB2119075AC5A324`

Deployment target: `release/out/e2e-gui-20260908`, with renderer under
`web/dist`. Prior binaries and index are retained in timestamped backups.
The final hashes were verified in the deployment target; the host and engine
were running from that directory after restart. The deployed index references
`main-BYH7wjnF.js`. No active meeting recording was present before either update.

Remaining acceptance boundaries:

- No microphone-to-speaker hardware latency measurement was performed for all
  three modes. Simulated voice latency tests are not hardware measurements.
- The default browser's actual rendered page was not inspected through an
  attached automation surface. Results retain a page-verification caveat.
- No test wrote into the user's unsaved Notepad document or forcibly closed
  their applications. Input/playback/process checks here use runtime tests.
- Provider response time and poor search relevance can still cause delay.
  This change does not promise instant replies or universal GUI reliability.
