# Video Proxy Compatibility

Status: implemented and locally verified. No deployment, restart, live paid
generation, live database access, DB sync, or parent-helper edit was performed.
Installed-app generation and binary video decoding remain with the parent.

## Evidence And Routing

The parent reported an initial native Seedance POST 404 on z.apiyihe.org, a
non-API HTML response under /volc/v1, and a successful NewAPI job taking about
135 seconds. The successful response uses an outer proxy task ID which differs
from the nested upstream task ID.

The [official NewAPI router](https://raw.githubusercontent.com/QuantumNous/new-api/main/router/video-router.go)
was read for this implementation. It defines POST /v1/video/generations and
GET /v1/video/generations/:task_id. Response fixtures follow the parent's live
observations; the router alone does not define those response bodies.

1. OpenAI-compatible Seedance video models retain the native /api/v3 task route
   selected by adapterForModel. Existing explicitly configured native bases
   still work; image and other model routes are not wrapped.
2. Only a completely read initial native POST 404 with no task-acceptance
   evidence gets a private, phase-specific fallback marker. Generic HTTP 404
   errors cannot authorize a retry. A contradictory 404 containing a task ID,
   a read failure, oversized body, timeout, 500, or malformed 2xx cannot do so.
3. The app lazily creates a separate policy-checked /v1 adapter through e.adapter,
   preserving scheme, host, port, provider identity, protocol, and credential
   origin. It does not change the stored provider, mutate the native connector,
   use ../, or probe HTML routes.
4. GenerateProxyVideo submits exactly once with model, prompt, and duration 5.
   It polls the accepted proxy task ID immediately and then every 1.5 seconds,
   bounded by the caller's context. Poll failures never restart submission.
5. Creation requires a safe outer task_id; an optional outer id must agree.
   Creation and polling have separate schemas: poll data.id is ignored
   database-row metadata (the recorded response contains numeric 154), not
   task identity. Polling requires code=success and the matching data.task_id. Only outer
   SUCCESS permits result_url, or data.content.video_url if result_url is
   absent. Nested upstream ID/status never identifies or completes the proxy
   job. Queued/running URLs are not output. Unknown states, unsafe IDs/URLs,
   HTML, failed tasks, and malformed envelopes fail closed.
6. Output remains MediaResult with the proxy task ID and video/mp4 URL. URLs
   must be bounded absolute HTTP(S), with a host and no userinfo, fragment,
   whitespace/control characters, or backslash. This code does not download
   media or send credentials to the returned CDN URL.
7. The video catalog loop stops after a generation/lease failure instead of
   trying another paid model after an uncertain result. Image fallback behavior
   is unchanged. User-initiated requests remain separate actions, not retries
   automatically dispatched by this implementation.

## Deadlines

- Renderer provider.test requests and their method-specific cap: 360000 ms.
- Shared Host/Engine method ceiling: 360000 ms. Ordinary RPCs retain 30000 ms.
- Provider-test credential lease: six minutes, bounded by any shorter caller
  deadline. Model discovery retains its separate 30-second lease.
- Envelope schema already allowed up to 600000 ms. A positive provider.test
  example at 360000 ms and a contract test now pin compatibility with the cap.
- Existing per-request network limits remain; they bound each submission or
  poll, not the total polling lifetime. A network timeout is terminal, never
  permission to submit again.
- Host, Engine, and renderer must ship together. An old Host still rejects
  the longer credential lease even if the Engine alone is updated.

## File Inventory

Paths are relative to the repository root. Shared dirty files retain the
parent's pre-existing changes; the descriptions below identify this task only.

| File | This task's changes |
| --- | --- |
| internal/llmadapter/openai_proxy_video.go | New exported proxy generator interface/method, typed initial-404 marker, strict outer-task parser and polling. |
| internal/llmadapter/openai_media.go | Native task POST returns the phase marker immediately; removes the extra guessed native path; bounded complete task-response read. Seedream parameters preserved. |
| internal/app/video_proxy_adapter.go | New lazy same-origin /v1 wrapper using e.adapter. |
| internal/app/provider_diagnostics.go | Narrow adapterForModel wrapper and operation-specific lease durations. Parent OCR probe/diagnostics preserved. |
| internal/app/model_catalog.go | Stops video catalog resubmission after failures. |
| internal/bridge/deadline.go | Six-minute provider.test ceiling shared by Host and Engine. |
| internal/secretlease/broker_windows.go | Six-minute provider-test lease; separate unchanged discovery TTL. |
| web/src/bridge/client.ts | Provider test deadline and method-aware clamp. |
| api/bridge/v1/envelope.schema.json | Long provider.test positive example; existing schema/method changes preserved. |
| internal/llmadapter/openai_proxy_video_test.go | Native/proxy phase, identity, status, URL, timeout, read/size and no-resubmit tests. |
| internal/app/video_proxy_adapter_test.go | Real loopback policy connector routing, native preservation, no catalog retry, Engine/lease deadline tests. |
| internal/app/desktop_execution_regression_test.go | Existing Seedance/image routing fixture now explicitly declares OpenAI-compatible protocol. |
| internal/secretlease/broker_windows_test.go | Operation-specific TTL boundary assertions. |
| internal/hostbridge/provider_deadline_test.go | Host accepts six-minute diagnostics and rejects excessive/ordinary-method long requests. |
| internal/contract/provider_deadline_test.go | Schema example/ceiling and discovery-cap parity. |
| web/src/bridge/provider.deadline.test.ts | Simulated 135-second job, one-shot timeout, ordinary method caps. |
| docs/audits/2026-09-08-video-proxy-compatibility.md | This audit and handoff. |

No changes to common.go's MODEL_CHANNEL_UNAVAILABLE handling, shared chat.go
or chat_run_stream.go, DB-sync code, the main parent helper, or meeting modules.
No generated bindings needed to change; bridge generation verification passes.

## Verification

- Initial combined command passed before the numeric poll-ID follow-up below:
  go test ./internal/app ./internal/llmadapter ./internal/secretlease
  ./internal/hostbridge ./internal/contract ./internal/bridge
  ./internal/engineclient -count=1 -timeout=5m.
- Full internal/app: 63.585s. A preceding full run exposed the existing
  Seedance routing fixture's missing protocol; it now declares the protocol.
- Full internal/llmadapter: 1.644s, including the final oversized-response
  guard and the parent's Seedream/model-channel regressions.
- Full internal/secretlease: 0.217s; internal/hostbridge: 0.314s;
  internal/contract: 0.183s; internal/bridge: 0.235s;
  internal/engineclient: 0.381s.
- npm test -- src/bridge src/provider --maxWorkers=2: 16 files, 149 tests passed.
- npm run typecheck: passed.
- npm run verify:bridge: passed.
- Scoped git diff --check: passed (only the repo's LF/CRLF notices).
- All added tests use local fixtures, not the live provider or user database.

### Numeric Poll-ID Follow-Up

The parent supplied a recorded response with data.id=154,
data.task_id=task_gdtUzbGS2qbg5q9UP2AH2ZSzrlYhqT6g, outer status SUCCESS,
and nested upstream id=cgt-20260908230859-p6tvm. The initial shared create/poll
schema incorrectly decoded the database row ID as a string. This is repaired
using a creation-only string ID field and a poll task schema without that
metadata field. Poll task_id still has to match the accepted ID exactly.

Follow-up files: internal/llmadapter/openai_proxy_video.go,
internal/llmadapter/openai_proxy_video_test.go,
internal/app/video_proxy_adapter_test.go, and this audit.

- Exact recorded-shape regression passes and returns the proxy task ID/URL.
- Numeric-row queued polls still wait; numeric rows with missing/wrong task_id
  fail without resubmitting. Numeric/conflicting creation IDs are rejected.
- The real loopback app connector fixture now also includes data.id=154.
- Full internal/llmadapter: passed, 1.724s.
- Focused app VideoProxy/ProviderVideoTest/VideoGeneration/Seedance routing
  regressions: passed, 0.169s.
- Subsequent build-readiness gate after the numeric-ID correction:
  go test ./internal/app ./internal/llmadapter -count=1 -timeout=5m passed;
  full app 63.765s, full adapter 1.735s. The parent's earlier JSON run also
  passed (64.376s), but predated this correction; this fresh run closes that
  coverage gap. Implementation is ready for the parent's build/live check.
- The parent's helper now sends deadlineMs=360000 with a seven-minute client
  context, compatible with the six-minute provider-test ceiling. Expert try
  retains its independent 25-second timeout. Reported OCR/image upstream
  MODEL_CHANNEL_UNAVAILABLE responses remain a separate unresolved dependency;
  this task has neither saved the new key nor made additional provider calls.
- No deployment or additional paid submissions were performed. Parent-reported
  prior MP4 decode: blue square, 5.088 seconds, 1280x720, approximately 2.1 MB;
  this is not a new installed-app acceptance run by this task.

## Parent Acceptance

1. Deploy matching Host, Engine, and renderer using the parent's process.
2. Keep the existing provider, credentials, and model configuration. Select
   the enabled video model doubao-seedance-2-0-260128 and invoke the installed
   app's provider connection test once, or its intended video-generation flow
   once. Do not run both just to duplicate the paid acceptance job.
3. For a direct bridge helper, set provider.test envelope deadlineMs=360000
   and its outer client context to at least that plus transport margin. A
   helper still sending 30000 cannot benefit from the new maximum. This task
   deliberately did not edit or execute the parent's main helper.
4. Expected wire order on the reported proxy: native POST returns 404; one
   POST /v1/video/generations creates a job; only GETs for that returned proxy
   task ID follow. The nested upstream ID is not a polling target.
5. Retain the proxy task ID, elapsed time, final outer SUCCESS and returned
   result URL. The parent downloads/decodes the actual video and verifies the
   requested content and duration; URL/status alone is not media acceptance.
6. On a timeout or polling error, do not automatically rerun the probe. The
   upstream task may remain billable. Inspect that existing job first.

Remaining boundaries: live installed-app acceptance and video decoding are
not claimed; no durable task-resume/cancel UI was added; compatibility is
enabled specifically for OpenAI-compatible Seedance video models. The exposed
proxy generator can support future explicitly selected proxy video routes.
