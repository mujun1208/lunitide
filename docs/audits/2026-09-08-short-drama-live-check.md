# Short-Drama Expert Post-Deploy Check

The preparation-only status below records 2026-09-08. The subsequent authorized
[2026-09-09 live check](2026-09-09-expert-lifecycle-live-check.md) created the
previously absent expert and ran one trial: lifecycle safety passed, but the
returned scene was incomplete. No enable/delete, retry, or restart was done.

## Test Failure Reconciliation

Halley's saved run in `_scratch/automation-stream-check/app-tests.jsonl`
started at 2026-09-08 22:42:24 +08:00. The expert test fixture was corrected
at 22:42:26; that already-running test binary still used the old fixture and
reported the failure at line 60 at 22:42:45.

The old fixture implemented provider `Get` but inherited an empty `List`.
`expert.try` selects its model through `List`, so no completion was possible.
The current fixture explicitly returns a configured model from both methods,
has an independent completion adapter, and asserts the selected model ID.
A separate test covers an empty catalog: retryable failure, no adapter call,
and the expert remains disabled. No production model-selection or provider
module was changed for this test correction.

Fresh verification after fixture isolation:

- `go test ./internal/app -count=1 -timeout=5m`: passed, 58.675s.
- `go test ./_scratch/expert-lifecycle-check -count=1`: passed; fixture validity,
  inspect-only behavior, old-engine rejection, post-failure state comparison,
  and detection of changes to existing experts.
- `go run ./_scratch/expert-lifecycle-check -help`: compiled and printed usage;
  this exits before loading a nonce or opening a gateway connection.

## Ready Inputs

- Profile: `_scratch/expert-lifecycle-check/short-drama-create.json`.
- Trial: `_scratch/expert-lifecycle-check/short-drama-trial.txt`.
- Gateway helper: `_scratch/expert-lifecycle-check/main.go`.

The profile is named `短剧创作专家`, version `1.0.0`, with all six sections
filled in. It binds no external skills or MCP services. The trial requests a
fictional bookstore scene with a sibling conflict over rent, actual dialogue,
and a specific ending. This validates the real persona completion path; it
does not claim to validate tool execution or Word/video generation.

## Deployment Prerequisites

The parent task deploys the engine and renderer from the same source, including
migration `0148_expert_lifecycle.sql` and the generated `expert.try`/
`expert.delete` bridge contracts. Existing expert states must be preserved.
The parent owns model setup: the profile must have an enabled, credentialed LLM.
Trial uses the saved chat-model preference when available, otherwise the normal
LLM catalog fallback. These checks do not switch models or alter credentials.

## UI Path

1. Open 专家中心 > 添加专家 > 手动填写. Enter the name, division, description,
   version and six sections from the profile JSON. Click 创建专家.
2. Confirm the new `短剧创作专家` name/card is selected under 我创建的 and its
   state is 已停用. Capture the saved expert ID and current version ID.
3. Click 试用. Enter the trial text and click 开始试答. Require a nonempty
   scene with 林夏/林舟, actions and at least six dialogue lines, ending in a
   rent-related phone ring. Inspect the content; a successful HTTP response
   alone is not a creative-quality pass.
4. Close the dialog and refresh the expert list. The expert must still be
   disabled. Every previously existing expert must retain its original state.
5. If validating enable separately, click 启用 only after recording the disabled
   trial evidence. The expert should become available for normal session use.
   This is a separate action, not part of the trial.

If the name already exists, inspect its ID, origin, ownership and state before
using it. Do not recreate, rename, reclassify or delete an existing row merely
to make the check pass. Legacy records are not proof of manual creation.

## Gateway Path

Run from `E:/Trae-Work-Projects/lunitide` after the parent has deployed.
Use the intended running profile's directory, explicitly. For the established
E2E profile in this workspace:

```powershell
$expertProfile = Join-Path $env:LOCALAPPDATA 'Lunitide-E2E'
go run ./_scratch/expert-lifecycle-check -data-dir $expertProfile -action inspect
go run ./_scratch/expert-lifecycle-check -data-dir $expertProfile -action create
```

Record `result.expertId` from creation. Do not run creation a second time if the
row exists. Substitute that returned ID below:

```powershell
go run ./_scratch/expert-lifecycle-check -data-dir $expertProfile -action trial -expert-id '<returned expertId>'
go run ./_scratch/expert-lifecycle-check -data-dir $expertProfile -action inspect
```

The helper defaults to inspection, requires an explicit profile, and never
enables, deletes, starts or restarts anything. Creation is explicit; trial
requires an already-disabled, own manually-created expert. It retrieves the
current version before trial and compares the expert list before/after even
when the model call fails. Errors include the bridge code and correlation ID,
without credentials or raw upstream errors. Exit code 0 plus
`existingExpertsUnchanged=true` verifies state preservation, not story quality.

The gateway nonce is read only by the future invocation and is never printed.
The helper does not open the database file or rewrite failed history. The
prepared fixture and helper were checked locally without invoking these live
commands; only `-help` was executed against the compiled helper.

## Evidence To Retain

Record creation ID/name/version/origin/state, the real trial response text,
the before/after expert states, and the error code/correlation if trial fails.
On a trial failure, keep the disabled profile and repair the model connection
through the parent-owned model workflow before retrying. Do not auto-enable
the expert as a workaround.
