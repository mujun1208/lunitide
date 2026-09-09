# Desktop execution and proxy video check

## Scope

Latest request: reliable desktop music playback, full application exit, and
opening the real desktop browser. Also retain the earlier proxy video API task.
This audit does not mark all historical screenshot reports resolved.

## Implemented

- Windows SMTC media-session control selects one exact known application identity.
  Play/pause/next/previous/stop read the resulting state. Next/previous also require
  a changed track title. No global media key is sent after an ambiguous session
  timeout. Native requests use a fixed embedded script and JSON stdin, not
  model-generated PowerShell. Windows PowerShell 5 is selected explicitly because
  PowerShell 7 does not provide this WinRT interop.
- Generic music requests no longer search for a fabricated song. Response-style
  clauses such as "report the result in one sentence" are excluded from music
  query extraction and do not trigger Office document generation.
- `desktop.quit` requires an exact known application and explicit full-exit intent.
  It checks executable identity, limits process operations to the current Windows
  session, waits for exit, and re-enumerates remaining processes. Unknown names and
  missing approval are rejected. Unsaved-work warnings remain in the tool contract.
- `desktop.browse` opens HTTP(S) URLs or an encoded search query in the real default
  browser. Dispatch acknowledgement is not reported as page-load verification.
- Desktop task routing now retains both tools; standing CC permission and
  current-turn-only guards include them. No CC setting was enabled by this work.
- Fresh screenshots/observations are not skipped as duplicate mutations. Nested
  `ok:false` and stale-frame errors are failures, including in the frontend.
- Accessibility output no longer includes enormous internal app URL values that
  obscure controls. Internal node identities/hit data and input values remain.
- Seedance requests use `/api/v3/contents/generations/tasks`; polling requires the
  matching task ID and a terminal success with a valid video URL. Submission is
  not retried on transient/ambiguous failures that could create duplicate jobs.

## Real-machine evidence

- SMTC initially reported Soda Music (`汽水音乐`) as Paused. The new play action
  returned Playing with a current title and `verified=true`.
- The next action changed the title to `香烟与吻痕（我在月光下许过情真）`, artist
  `伏特佳`, and returned Playing / verified=true.
- Shuffle was false. This is verified playback, not verified shuffle mode.
- Dialog session `01M203YGXNREHJA5WYXVSYA0M2` reached verified media playback and a
  short final response. It still issued a redundant desktop.open/play pair.
- Dialog session `01M203ZHNGV2C1E4MCE9MAKMWD` opened the desktop Edge browser. Its
  observation contained `https://www.bing.com`, the Bing tab, and the search input.
  This run preceded the final route fix and took an unnecessarily long UI path.
- After routing was fixed, session `01M20448B2RFZTXGJ4CH7DXVY6` directly called
  desktop.browse. CC was disabled by then, so that run could not verify the page.
  The subsequent receipt regression prevents claiming page-load success without
  observation. It is covered by tests, not another live screen-control run.
- Full exit was tested against dedicated native fixture processes, verifying that
  the requested process exited and an unrelated fixture stayed running. Real
  WeChat was not terminated during testing.
- Video provider test at 16:25:40 returned HTTP 404 in 362 ms, with no task ID or
  generated video. Provider `yiheAPI` keeps its existing image model, credentials,
  and origin; added video model is `doubao-seedance-2-0-260128`. This is NOT a
  successful end-to-end video generation test.

## Validation and deployment

- Final command passed: `go test ./internal/app ./internal/toolruntime
  ./internal/ccapp ./internal/llmadapter ./internal/winexec`.
- Frontend earlier in this change: 54 test files / 621 tests passed; TypeScript
  and Vite build passed. Later changes were backend only.
- Final engine build passed and was copied to the E2E test installation, not a
  separate production installation. Existing backups were retained.
- Test executable: `release/out/e2e-gui-20260908/lunitide-desktop-e2e.exe`.
- Final engine SHA256:
  `4C6DD452CAC23F5F3CCFCA4245335F076AF381B08BE3B7616360F8EDE812C16C`.
- Test application was restarted and left running. CC config remained disabled,
  revision 10, updated 2026-09-08T09:03:26Z. No credentials are logged here.

## Remaining acceptance gaps

- No claim of OpenClaw feature/performance parity. Generic playback and next are
  proven; true shuffle and named-song search still need acceptance coverage.
- Native media sessions must exist and support the operation. Other players may
  need accessibility fallback. Coordinate/frame guards were not weakened.
- Live GUI runs exposed title/focus ambiguity and coordinate hit mismatches when
  windows changed. Large-tree output is improved, but universal GUI reliability
  and elimination of all redundant model actions are not established.
- Real WeChat/Soda Music full-exit acceptance is still outstanding; do not confuse
  safe fixture coverage with testing a user's live applications.
- Latest voice-model execution uses shared backend code, but microphone capture,
  interruption/resume, and ASR accuracy were not revalidated live across all three
  ASR/TTS paths in this desktop-focused pass.
- Proxy video 404 requires verifying the deployed proxy route and available model
  ID with the provider. Do not label the saved provider as operational merely
  because configuration was accepted. Query documentation:
  https://bfov2bxeom.apifox.cn/api-448226721
- Historical meeting mute-independent loopback, asset preview, expert lifecycle,
  weekly-report skill, automation artifacts, and complete PDF-to-PPT generation
  are not closed by this audit.
