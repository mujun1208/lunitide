# Meeting Multi-Endpoint Capture Evidence

Date: 2026-09-08. Scope: meeting audio capture only. These changes are in the
workspace; the installed desktop/engine was not rebuilt, restarted or deployed.
No provider, expert or chat modules were edited in this follow-up.

## Cause and Changes

The previous native implementation opened only the `eRender/eConsole` default
endpoint. A separate default communications endpoint and an application's
explicit output device were missed. Switching defaults while the old output
remained valid did not invalidate that capture, so its restart logic did not help.

- `internal/meetings/loopback_windows.go` now enumerates only active render
  endpoints, opens each endpoint by its stable ID and keeps COM ownership on
  the same OS thread. It never opens a microphone or a stereo-mix input.
- `internal/meetings/loopback_endpoints.go` deduplicates IDs and refreshes the
  endpoint inventory every 500 ms. A failed endpoint retries after two seconds;
  healthy captures survive another endpoint's removal or activation/read failure.
  An enumeration failure preserves already open streams. Capture can recover
  when all outputs disappear and later return, without another browser owner.
- Packets are resampled against WASAPI QPC timestamps into one 16 kHz mono
  timeline, with 40 ms collection delay and 100 ms output blocks. Idle periods
  remain silence; simultaneous sources are summed, not appended in time.
  Saturation occurs once after summing endpoints. A per-device timestamp
  watermark prevents overlapping packets from being mixed twice on reopen.
  The mixer bounds its pending window to two seconds.
- `internal/meetings/loopback.go` now reports source availability when no render
  endpoints remain, while keeping the capture owner alive for recovery. The
  existing `active` bridge field means at least one native endpoint is open,
  not that every endpoint succeeded or that audio is currently non-silent.
- The existing single browser microphone, echo-cancellation request, engine
  ownership and PCM-capable recognition path remain in use. The new native
  code does not add playback/monitoring or an additional microphone. Tests
  verify the same microphone-plus-system mix for captions and persistence,
  including failed-commit retry, without adding the microphone a second time.

API references: Microsoft documents [render endpoint enumeration](https://learn.microsoft.com/en-us/windows/win32/api/mmdeviceapi/nf-mmdeviceapi-immdeviceenumerator-enumaudioendpoints),
[capture timestamps in 100 ns units](https://learn.microsoft.com/en-us/windows/win32/api/audioclient/nf-audioclient-iaudiocaptureclient-getbuffer),
and [invalid-device recovery](https://learn.microsoft.com/en-us/windows/win32/coreaudio/recovering-from-an-invalid-device-error).

## Native Evidence

The new opt-in fixtures in
`internal/meetings/loopback_multi_live_windows_test.go` ran against the actual
Windows audio devices through the production capture path. They play generated
tones to explicitly selected endpoints and check the corresponding frequency
in returned PCM. A generic nonzero peak is not sufficient to pass this fixture.
PCM remains in memory and is not saved, recognized, or transmitted. No microphone
is opened and no real call is joined. There are no application-specific results
for Tencent or Feishu in this test.

Four active render endpoints were available. The default console output and
default communications output were different. Endpoint indexes below are
fixture-local, not persisted device selection preferences.

Final rerun, original mute settings (all four were unmuted):

| Endpoint | Role | Tone Hz | PCM Bytes | Measured Tone Amplitude |
| --- | --- | ---: | ---: | ---: |
| 0 | Default console | 733 | 35200 | 831.60 |
| 1 | Default communications | 870 | 28800 | 970.22 |
| 2 | Other active output | 1007 | 32000 | 862.57 |
| 3 | Other active output | 1144 | 28800 | 403.47 |

Final rerun, all four outputs temporarily muted and mute readback confirmed:

| Endpoint | Tone Hz | PCM Bytes | Measured Tone Amplitude |
| --- | ---: | ---: | ---: |
| 0 | 733 | 28800 | 1017.36 |
| 1 | 870 | 28800 | 988.54 |
| 2 | 1007 | 32000 | 900.96 |
| 3 | 1144 | 28800 | 403.37 |

Two simultaneous outputs (console at 733 Hz and communications at 997 Hz)
also passed. Original-settings amplitudes were 1073.07 and 996.01; muted
amplitudes were 955.83 and 893.58. The per-frequency threshold is 80 in s16
sample units. Amplitude depends on the device and captured window; differences
between runs are not interpreted as a mute-related gain change.

Both tests logged `configuration_preserved=true`: every active render and
capture endpoint's mute and master volume, plus all three default roles for
both flows, matched the pre-test snapshot. Original master volume values were
0.566929, 0.989771, 0.600000 and 0.580000 for outputs 0 through 3. The muted
fixture restores each original mute value through deferred cleanup before
checking the snapshot. It never sets volume, defaults or device enablement.

## Verification

- Full `go test ./internal/meetings -count=1 -timeout=120s` passed. The first
  focused attempt encountered the dirty workspace's migration manifest/file
  count mismatch; a later full run passed without this task changing storage
  or migration files.
- Focused `go test -race` passed for all new endpoint/availability tests plus
  existing reconnect and initially-unavailable tests (nine top-level tests).
  Coverage includes role/ID deduplication, other outputs, partial failure,
  bounded retries, enumeration failure, all-device loss and return, preserving
  healthy streams, overlapping reopen packets, silence, clipping, bounded
  backlog, and 16/44.1/48/96 kHz irregular packet resampling without sample holes
  or duration drift.
- Both native generated-tone tests passed on the final rerun (9.969 seconds
  total). Physical device unplug/replug and changing Windows defaults were
  not performed; those transitions are covered by deterministic fixtures.
- Five existing meeting frontend test files passed, 100 tests: `MeetingPage`,
  `meetingAudio`, `meetingAsr`, `meetingLoopbackQueue`, and `meetingCapture`.
  This includes native PCM with a silent microphone for all three recognition
  preferences and engine ownership without browser system capture.
- `git diff --check -- internal/meetings` passed (Git emitted only line-ending
  conversion warnings).

Reproduction from the repository root, in separate PowerShell invocations:

```powershell
go test ./internal/meetings -count=1 -timeout=120s
go test -race ./internal/meetings -run 'TestEndpoint|TestLoopbackReports|TestLoopbackReconnect|TestLoopbackInitially' -count=1 -timeout=90s
```

Opt-in hardware fixtures (the second flag authorizes temporary restored mute):

```powershell
$env:LUNITIDE_LOOPBACK_MULTI_LIVE = '1'
$env:LUNITIDE_LOOPBACK_MULTI_MUTED_LIVE = '1'
go test ./internal/meetings -run '^TestWASAPIMultiEndpoint' -count=1 -v -timeout=45s
```

Frontend, from `web`:

```powershell
npm.cmd exec -- vitest run src/meetings/MeetingPage.test.tsx src/meetings/meetingAudio.test.ts src/meetings/meetingAsr.test.ts src/meetings/meetingLoopbackQueue.test.ts src/meetings/meetingCapture.test.ts --maxWorkers=2
```

## Remaining Acceptance Boundaries

- Actual Tencent/Feishu meeting recording, their in-app device choices and
  end-to-end recognition have not been exercised. There is no claim that real
  meetings passed. Generated audio routed to separate outputs passed locally.
- Real microphone speech and acoustic echo removal were not hardware-tested.
  The single-microphone mix is verified with synthetic PCM. Browser AEC remains
  enabled, but echo removal across every speaker/driver setup is not certified.
- IDs are deduplicated, not audio content across different IDs. A virtual mixer,
  driver mirror or user-enabled microphone monitoring can already contain the
  same signal on multiple outputs. Such routing may produce duplicates or echo;
  suppressing similar audio heuristically could remove legitimate speech.
- New devices are discovered on the next successful inventory refresh, and
  failed captures retry with a delay; hotplug is recoverable, not gapless.
  Invalid driver timestamps use an approximate arrival-time fallback. Protected
  media, exclusive-mode playback and drivers that do not expose usable loopback
  remain outside the demonstrated coverage. See Microsoft's
  [loopback constraints](https://learn.microsoft.com/en-us/windows/win32/coreaudio/loopback-recording).
- Partial endpoint failures do not stop healthy audio. The current bridge/UI
  exposes aggregate availability, not a per-endpoint failure list.
- The current running application has not received these changes. Deployment
  and real-call acceptance remain pending, as requested.
