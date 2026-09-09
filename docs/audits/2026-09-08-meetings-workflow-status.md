# Meetings and Workflow Status Audit

Date: 2026-09-08. This is a partial repair, not an all-features completion claim.

## Meetings

Confirmed cause: the default `cloud` listener is browser Web Speech. It opens
its own microphone and cannot consume the mixed microphone/system PCM. Audible
speakers can reach that microphone; muted speakers cannot. A previous caption
also prevented the delayed fallback from replacing this listener.

Changed mixed-source meetings to select a PCM-capable listener immediately.
The browser-cloud preference now uses the available local recognizer for this
recording; explicit local and Volc selections remain unchanged. Saved provider
preferences are not rewritten. Engine-owned capture is retained instead of
opening a second browser capture. Signal and fallback notices remain visible.
Unavailable recognition is reported, not described as a guaranteed later transcript.

Evidence:

- `TestWASAPILoopbackCapturesMutedOutput` passed on this computer, with the
  original mute state preserved. `original_muted=true`, `probe_muted=true`,
  nonzero PCM: 960 bytes, peak 505.
- Component tests feed native nonzero PCM with silent microphone samples and
  verify that the mixed recording reaches recognition, including all three
  listener preferences and recognizer-unavailable handling.
- Live `voice.status`: supported and ready.

Boundary of that earlier verification: this was default-render-endpoint loopback. Simultaneous playback on
other devices, a separate communications endpoint, protected media, and actual
Tencent/Feishu meetings have not all been verified. This does not certify that
every application's audio is captured on every device configuration.

Follow-up: the workspace capture implementation now enumerates active render
endpoints, mixes them on one timestamped timeline, and recovers endpoints
independently. Local generated-tone fixtures passed on all four available
outputs, including a separate communications output, both at original mute
settings and with temporarily muted outputs. Two simultaneous outputs passed
as well; original mute, volume and default-role settings were verified intact.
See [multi-endpoint capture evidence](2026-09-08-meetings-multi-endpoint-capture.md)
for tests, measured results and remaining boundaries. This follow-up did not
restart or deploy the application and does not certify real Tencent/Feishu calls.

## Weekly Skill

The real saved draft has ID `01M1ZQ1NEYZJHTW18H2JAE8KCG` and entry point
`weekly-report/SKILL.md`. Runtime previously accepted only root `SKILL.md`.
Safe package-relative Markdown entry points now load the stored manifest prompt;
arbitrary entry files are neither read nor executed. Traversal and executable
paths remain rejected. Trial does not publish the draft.

Another blocker routed even session-relative Office generation through full-disk
approval. Local session output now uses the scoped executor. Desktop, absolute,
and other external destinations still require their existing permissions.

Two host-side intent bugs were fixed:

- A negated desktop destination was treated as a positive write request,
  including during fallback generation.
- Merely mentioning opening or starting could inject a desktop-open call after
  a completed artifact. Only imperative opening clauses now qualify. Office
  generation alone also does not inject screen capture.

Live evidence and remaining failure:

- `skill.try` now succeeds for the real draft.
- DeepSeek generated real Word artifacts. Session
  `01M209APJWXYY16N3GST70PHXR` contains `weekly-report-test.docx`, 5834 bytes.
  ZIP/XML inspection confirmed the synthetic sample marker and supplied facts.
- Those successful generation runs then paused for an unwanted desktop-open
  approval; this led to the host-side intent fixes above.
- Latest post-fix sessions `01M20A6CTVNJFTT4BR0YA9F72V` and
  `01M20AFPZXTVC1QVDF2N5REGPZ` failed with `UPSTREAM_FAILED` after loading the
  skill. GLM also failed during this audit. Stable live completion is NOT passed.
- Known malformed adapter responses now have a separate sanitized stream error;
  the latest failures still reported the generic class. Their underlying cause
  remains unresolved; it is not evidence that the user's prompt was wrong.

## Assets

Added `template.open` and a View Attachment action for draft, enabled, and void
assets. The endpoint takes only an asset ID, enforces organization ownership,
validates the stored file name, and opens an isolated viewing copy. Opening does
not enable or modify the asset. Failure removes the temporary copy.

Service tests cover isolation, state independence, path rejection, ownership and
opener failure. UI tests cover success and retry. Playwright fixture checks at
1440, 1000, and 390 pixels passed with no overflow or browser errors. Screenshots:
`release/out/asset-view-{width}.png`. This browser fixture does not exercise the
user's actual Office installation.

## Office

Existing code removes the duplicate side task description and displays reference
attachments in the chat area, excluding them from deliverables. PDF references
use the PDF viewer; Office references open a viewing copy with a local application,
not a synthetic list of XML fragments presented as the original layout.

Home/menu navigation, return-to-task-list, timeout/retry and history-profile
repairs are documented in `2026-09-08-office-navigation-recovery.md`. This audit
reran the route tests and confirmed the reference-opening implementation. It did
not certify all Office/PDF rendering and OCR scenarios.

## Experts: Not Repaired

The active profile lists 21 enabled existing experts. No saved custom short-drama
expert was found. Create still defaults to enabled; a reliable manual-origin
marker/filter, disabled trial and own-expert deletion are missing. No expert
lifecycle code was changed in this audit. Local source alone cannot identify
manual experts because bundled experts also have local source.

## Automation: Not Accepted

The existing news automation run `01M1ZVFDM4D2XCDQMT51Z3S4ZS` remains failed with
`UPSTREAM_FAILED`. History/session recovery and scoped file generation apply to
this path, but no successful real automation rerun was completed. Original
history was not rewritten to look successful.

## Verification and Installed Build

- 13 frontend test files, 153 tests passed (meetings, assets, Office route).
- Full Go tests passed for `internal/app`, `internal/skillapp`,
  `internal/meetings`, and `internal/contract`. After the final error-class edit,
  focused stream-error and Office/desktop-intent tests passed again.
- Renderer typecheck/build passed; pre-existing chunk size warnings remain.
- The asset fixture server was stopped.
- Updated and restarted `release/out/e2e-gui-20260908`; both binaries use
  `lunitide_e2e`. No active recording was present before restart. Prior binaries
  and renderer are retained in timestamped backups.
- Engine SHA256: `FC4B91209C97917F86C62622F4468268697CDC0697136C0F6C7BF9833759B735`.
- Desktop SHA256: `7777D34CCC59C270E4EFE20A721EF7033FAB004A80C0C8E827EF4A0BDADB5FB6`.
- Renderer `main-CgLktK5w.js` SHA256:
  `9510DDA11F6D3099DCD5ABCC1757267930400AD2CF125DCFFA3D59B84627BA9C`.
  Installed and workspace build hashes match.

Synthetic trial sessions and their files remain for inspection. Provider keys,
user documents, existing expert states and the failed automation history were
not replaced or silently changed.
