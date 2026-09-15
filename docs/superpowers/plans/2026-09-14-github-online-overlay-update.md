# GitHub Online Overlay Update Implementation Plan

> **For agentic workers:** Use executing-plans in this session. TDD. Do not commit unless the user asked.

**Goal:** Check GitHub `latest.json` and download the Setup on「立即升级」, then reuse the existing silent overlay install.

**Architecture:** Composite feed lookup (local first, remote latest.json if needed). Stream the exe through networkpolicy into `%LOCALAPPDATA%\Lunitide\updates`. NsisInstaller.Download fills a missing local file. Build-Release optionally uploads to `v{VERSION}`.

**Tech Stack:** Go 1.26, existing `desktopupdate` + `networkpolicy` + `m7app` + NSIS `/S`.

**Spec:** `docs/superpowers/specs/2026-09-14-github-online-overlay-update-design.md`

## Global Constraints

- Same `latest.json` schema; unknown fields fail closed.
- No installer URL field in JSON.
- First install of this build is still manual.
- Do not claim product 100% or live banner proof.

---

### Task 1: Stream download in networkpolicy

**Files:**
- Modify: `internal/networkpolicy/fetch.go`
- Test: `internal/networkpolicy/fetch_test.go`

**Produces:** `Copy(ctx, rawURL, w, FetchOptions) (written int64, FetchResult, error)` — hop-validated like Fetch; writes at most `MaxBodyBytes`; sets `Truncated`. `Fetch` uses `Copy` into a buffer.

- [ ] Failing test: httptest file larger than cap is truncated; 200 body copies to writer.
- [ ] Implement `Copy`; `Fetch` delegates to it.
- [ ] `go test ./internal/networkpolicy -count=1`

### Task 2: Remote latest.json + installer URL

**Files:**
- Create: `internal/desktopupdate/remote.go`
- Test: `internal/desktopupdate/remote_test.go`

**Produces:**
- `DefaultFeedURL` = `https://github.com/mujun1208/lunitide/releases/latest/download/latest.json`
- `FeedURL()` honors `LUNITIDE_UPDATE_FEED_URL`
- `InstallerURL(feedURL, version)` → GitHub `releases/download/v{ver}/Lunitide-Setup-{ver}-x64.exe` or sibling of custom feed
- `ParseRemoteLatest(raw) (FeedDocument, error)` reuse `ParseFeed`
- `allowedUpdateURL(raw)` only github.com / *.githubusercontent.com, or test http localhost when feed override is loopback

- [ ] Tests for URL mapping and reject `https://evil.example/latest.json` as installer host.
- [ ] Implement.
- [ ] `go test ./internal/desktopupdate -count=1 -run TestRemote`

### Task 3: Fetch remote latest + download into store

**Files:**
- Modify: `internal/desktopupdate/remote.go`, `install.go`
- Test: `internal/desktopupdate/remote_test.go`, `install_test.go`

**Produces:**
- `FetchRemoteLatest(ctx, get, channel) (FeedDocument, bool, error)`
- `EnsureLocalInstaller(ctx, destDir, doc, getFile) (Offer, error)`
- `NsisInstaller.Download` calls Locate then EnsureLocalInstaller
- `CompositeLookup(local Feed, get, channel)` for Check

- [ ] httptest: latest.json + exe; lookup returns version; Download writes file matching sha256; digest mismatch fails.
- [ ] Wire Download; keep existing silent `/S` test green.
- [ ] `go test ./internal/desktopupdate ./internal/app -count=1 -run 'TestNsis|TestRemote|TestCheckAdopts|TestEnsure'`

### Task 4: Wire Check + banner copy + gh upload

**Files:**
- Modify: `internal/bootstrap/wire.go`
- Modify: `web/src/settings/AppUpdateBanner.tsx` (status text only)
- Modify: `release/Build-Release.ps1`
- Test: `web/src/settings/AppUpdateBanner.test.tsx` if copy asserted; `internal/app` feed test if needed

- [ ] FeedLookup: local Latest, then FetchRemoteLatest; pick newer; remote error without local → no update.
- [ ] Banner: `正在下载并安装更新…`
- [ ] After writing latest.json, `Publish-GitHubRelease` via `gh` when authenticated; failure is a warning.
- [ ] `go test ./internal/desktopupdate ./internal/app ./internal/networkpolicy -count=1 -timeout 4m`
