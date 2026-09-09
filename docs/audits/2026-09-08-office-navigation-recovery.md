# Office Navigation and History Recovery

## Confirmed Causes

- The E2E desktop host had the `lunitide_e2e` build tag, but its adjacent engine did not. The host used the E2E profile while the engine opened the production profile. An authenticated gateway connection alone does not prove that both binaries use the same data directory.
- The saved task ID was restored on ordinary Office sidebar navigation, including task IDs absent from the active profile.
- Failed task reads shared an error state with task-list refreshes. A successful list refresh could clear the task error while the conversation still displayed its loading placeholder.
- Reference attachments and the conversation host shared a sibling React key.

## Changes

- Ordinary sidebar navigation always opens Office home and the recent task list, including repeated clicks while already inside Office.
- Explicit artifact links retain task navigation. Merely remembering the last task no longer reopens it.
- Task pages offer a visible back-to-list action, including failed and incomplete reads. Read failures and deadlines expose retry controls.
- List errors and task errors are independent. Already-loaded conversations remain visible if artifact synchronization fails. Late reads cannot reopen a task after leaving it.
- Office binding and initial task/list reads have a 20-second UI deadline. File mutations are not automatically repeated.
- Reference and conversation keys are distinct.
- `release/Build-E2EBinaries.ps1` builds both binaries together and verifies their E2E tags.

## Verification

- Renderer: 18 Vitest files, 141 tests passed, including Office navigation, recovery, route binding, App sidebar, and companion model regressions.
- Renderer typecheck and production build passed. Vite retains its existing large-chunk warning.
- Relevant Go organization, Office, and engine tests passed. The filtered datadir and desktop packages reported no matching tests.
- Playwright: home, failed read, retry, return, reopen, repeated menu navigation at widths 1440, 1000, and 390. No browser errors or document-width overflow. Screenshots are under `release/out/office-navigation-*.png`.
- The screenshot harness uses fixture API responses, not a native host or live model.

## Installed Test Copy

Updated `release/out/e2e-gui-20260908` with matching E2E binaries and the renderer, then restarted it. Prior binaries and renderer are preserved in timestamped `before-office-navigation` backups. Neither the production database nor E2E task records were migrated, reassigned, or deleted.

- Engine SHA256: `7B7F53F338A248643B51C307719D873B751AF6C25274597E957F8D3B96CD2A28`
- Desktop SHA256: `3CD0B3ED0E55EF4D0B55FFFDDD6B26EC1C93FFB7E2FDF97F5E93EEFCAAA37643`
- Renderer: `main-ClY7bQ8V.js`, `OfficeStudioRoute-DCr29za-.js`

After restart, a read-only live gateway check returned the existing business-plan task, its two artifacts, its original session, and all 12 history messages (`hasMore=false`). The task snapshot was complete. No chat run was started and no prompt was resent. Native UI interaction with that restored session was not separately automated.
