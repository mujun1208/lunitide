# Expert Lifecycle Live Check

Executed against the parent's newly deployed, already-running Lunitide-E2E
Host/Engine/renderer on 2026-09-09. No restart, redeployment, provider/model
change, enable, delete, direct database access, or video call was performed.
The original check, offline repair, and separately authorized final-build
recheck are recorded below. Only evidence/docs changed during the live checks.

Latest status: the 00:51 final-build recheck passed both content completion
and lifecycle-state preservation. No implementation or helper edits were made
during that recheck, and no additional trial followed it.

## Initial Result

Lifecycle safety passed. Creative-content acceptance is incomplete.

- Initially 21 experts were listed, all enabled. The exact name
  `短剧创作专家` was absent.
- Called the existing expert-lifecycle-check create action exactly once.
- Created expert: `01M20WTGJHRESXPNVA1J0B1Q7Y`.
- Version: `01M20WTGJHRESXPNVA1M1KXFRM`.
- Name: `短剧创作专家`; creationOrigin: `manual`; isOwn: `true`;
  source: `local`; state: `disabled`; versionCount: `1`;
  mountedPhaseCount: `0`.
- Creation returned existingExpertsUnchanged=true for all 21 older experts.
- Called trial exactly once. Its detail check validated ownership, manual
  origin, disabled state, and pinned current version before the model call.
- Trial returned the same expert ID, version ID, name, disabled state, and
  nonempty output. The helper exited 0 and reported existingExpertsUnchanged.
- A separate final inspection showed 22 experts. All listed metadata matched
  the before-trial snapshot exactly. All 21 older experts remain enabled;
  the new expert remains disabled and unmounted. No retry was made.

## Content And Diagnosis

The returned scene includes Lin Xia/Lin Zhou, rent conflict, physical actions,
10 dialogue-bearing lines, and 257 Han characters. It ends literally:

> 这时，柜台下纸箱旁，

It stops mid-sentence and never supplies the required rent-related phone-ring
ending. A nonempty bridge response is therefore not a complete story-quality
pass. The returned JSON was complete; this is not tool-output display clipping.

Confirmed source-level limitations in the tested binary:

- internal/app/expert_lifecycle_handlers.go requests MaxTokens=2000 and does
  not request DisableReasoning. It has a separate 25-second timeout.
- internal/llmadapter/openai.go does not decode choices.finish_reason.
- The trial handler accepts any nonempty text without tool calls, and does not
  return model identity, finish reason, or token usage. Therefore the success
  response cannot distinguish a complete answer from a token-limited answer.
- This attempt returned success, not EXPERT_TRIAL_FAILED or a bridge timeout.
  Extending only the timeout is not an evidence-backed fix for this result.

A completion/reasoning budget cutoff is a plausible explanation, not proven:
the necessary finish_reason/usage evidence is absent. No unrelated chat-log
failure was attributed to this trial, and no additional model call was made
to investigate it.

The offline repair below addresses explicit truncation and unnecessary
reasoning. It does not retrospectively prove the cause of this scene's cutoff.
Any live retry requires a separate explicit action; do not enable the expert
or change older experts as a workaround.

## Offline Completion Repair

- Added provider-neutral Response.FinishReason: unset, stop, length,
  content_filter, tool_calls, or other. Unknown upstream text is never exposed.
  OpenAI finish_reason and Anthropic stop_reason are normalized in Complete
  and the returned Stream response. Empty usage trailers do not erase it;
  cancellation retains already-received metadata, and explicit truncation
  cannot be overwritten by a later conflicting stop.
- Existing bounded JSON readers, stream errors/terminators, tool reconstruction,
  cache accounting, and provider compatibility handling remain unchanged.
- expert.try rejects length as EXPERT_TRIAL_INCOMPLETE and content_filter as
  EXPERT_TRIAL_FILTERED, even with nonempty output. It also rejects actual tool
  calls and a tool_calls finish reason. Failures return no success payload.
- Text-only trials now request DisableReasoning=true and a concise, complete
  final answer with the requested ending. No tools are supplied. MaxTokens=2000,
  MaxAttempts=1, the 25-second operation timeout and 30-second bridge deadline
  remain unchanged. No automatic continuation/retry was added.
- Existing UI error handling shows the failure and clears previous output.
  Regression tests verify this without enable, mount or delete side effects.
- Missing/unknown finish reasons preserve compatibility. A stop classification
  is not a semantic story-quality guarantee; the requested ending still needs
  human acceptance. No exact model identity was captured in the first trial.

Changed implementation/test files for this repair only:

- internal/llmadapter/types.go
- internal/llmadapter/completion_finish.go
- internal/llmadapter/completion_finish_test.go
- internal/llmadapter/openai.go
- internal/llmadapter/anthropic.go
- internal/app/expert_lifecycle_handlers.go
- internal/app/expert_lifecycle_handlers_test.go
- web/src/expert/ExpertCenterPage.test.tsx

The exact parser/file scope was posted to the parent for relay to Halley before
parser edits. Direct task messaging was unavailable; receipt was not confirmed.
No chat.go, chat_run_stream.go, news-diagnosis, provider, meeting, database,
deployment, restart, or parent-helper edits were made for this repair.

Verification after the final source edits:

- `go test ./internal/llmadapter ./internal/app ./internal/meetings ./internal/contract -count=1 -timeout=5m`:
  all pass (1.793s, 98.820s, 84.566s, 0.195s respectively).
- `go test ./_scratch/expert-lifecycle-check -count=1`: pass, 0.797s;
  fixture/helper tests only, no installed-app trial.
- From web, `npm test -- src/expert/ExpertCenterPage.test.tsx`: 23 tests pass.
- From web, `npm run typecheck`: pass.
- Targeted `git diff --check`: pass; only existing LF/CRLF notices.

At this offline checkpoint, source edits were complete and ready for the
parent's build. No paid call, redeployment or restart was performed as part of
the repair. The subsequently authorized acceptance check is recorded next.

## Final-Build Recheck

The parent deployed/restarted the final build at 00:48 and authorized exactly
one trial of the existing expert. At 00:51:17 +08:00 on 2026-09-09, the following
trial was invoked once, between separate read-only inspections, from
E:/Trae-Work-Projects/lunitide. These are recorded commands, not instructions
to repeat another paid check:

```powershell
$expertProfile = Join-Path $env:LOCALAPPDATA 'Lunitide-E2E'
go run ./_scratch/expert-lifecycle-check -data-dir $expertProfile -action inspect
go run ./_scratch/expert-lifecycle-check -data-dir $expertProfile -action trial -expert-id 01M20WTGJHRESXPNVA1J0B1Q7Y
go run ./_scratch/expert-lifecycle-check -data-dir $expertProfile -action inspect
```

Observed process start times were 00:48:34 (host PID 63504) and 00:48:35
(engine PID 62964). SHA-256 hashes of those running executable paths matched
the parent's supplied hashes exactly:

- Engine: `7E917815C089F7F0FFB67F92D08AC655839B69A85B214F9F8DC9CB668986058E`
- Host: `0BEF64A4545B871387D0F781FCB297AD7B3E08492C0272E32B6EB4376ED05464`

The helper exited 0 in 5.720 seconds of command wall time, including helper
compilation/inspection overhead. This is not isolated model latency. There
was no model, operation, or bridge timeout reported on this attempt.

- Returned expert ID: `01M20WTGJHRESXPNVA1J0B1Q7Y`.
- Returned version: `01M20WTGJHRESXPNVA1M1KXFRM`, unchanged from creation.
- Name: `短剧创作专家`; manual origin; isOwn=true; disabled; one version;
  mountedPhaseCount=0. The helper checked the owned/manual/disabled version
  before calling expert.try and returned the same identity/version/state.
- The helper reported existingExpertsUnchanged=true. Independent before/after
  inspections matched all 22 listed expert objects exactly. All 21 older
  experts remain enabled; no expert was recreated, enabled, deleted or edited.
- Full scene reviewed: 418 Han characters (limit 600), 12 dialogue lines
  (minimum 6), Lin Xia and Lin Zhou as the only speaking characters, counter
  setting, physical actions and the rent conflict. The ending is complete:

> （林舟扑向柜台，林夏抢先拉开抽屉，手机屏幕亮起——来电显示“王房东”，铃声仍在响。）

The preceding action locates this phone beneath the counter. The caller is
the landlord, tying the final continuing ring to rent. It does not stop
mid-sentence. Content acceptance therefore passes independently of the
helper's success exit code; this was not a nonempty-output-only pass.

Selected model/provider identity, token usage and finish reason are not
exposed by expert.try/helper and were not captured. A raw stop reason is not
inferred. No extra request was made to obtain metadata, and there was no
restart, deployment, provider change, direct database access or second trial.

## Evidence

[Initial creation/trial evidence](../../release/out/expert-lifecycle-01M20WTGJHRESXPNVA1J0B1Q7Y.json)
preserves the original incomplete scene without rewriting that result.

[Final-build completion recheck response and snapshots](../../release/out/expert-lifecycle-01M20WTGJHRESXPNVA1J0B1Q7Y-completion-recheck-20260909.json)
contains the exact new scene, both catalog snapshots, verified hashes,
process/timing metadata, and acceptance checks. Neither artifact contains a
credential or gateway nonce. This validated the installed bridge path; live
UI card selection and enable/delete were not exercised.
