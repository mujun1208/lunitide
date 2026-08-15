# stdio Production-Escape Red Team Evidence (M6 slice 5C)

- Generated: 2026-08-15T01:24:34Z
- Platform: windows/amd64
- Schema: stdio-5c-evidence/1
- Policy digest (5B config binding): `8715358b0eb04ebf7ed7d36f7f0cb82d9794cac7110e79328b0bda073dc95147`
- Gate open at generation: false
- Bundle digest: `2ddd22dc79ca74b5d36c402f935b51540d8188f85e1596d74d39c7bad20f86ea`
- Verdict: **PASS**

## Records

| # | record | enforced by | verdict | attacks | digest |
|---|--------|-------------|---------|---------|--------|
| 1 | host filesystem escapes (guard channel + bare-channel probe) (`host-file`) | guard-level | PASS | 3 | `a3ddaa2e2902` |
| 2 | network egress (guard channel + bare-channel probe) (`network`) | guard-level | PASS | 7 | `2402cf717c55` |
| 3 | parent environment / secret inheritance (`secret`) | os-level | PASS | 1 | `12a82c5250d6` |
| 4 | fork bomb and process-tree survival (`proctree`) | os-level | PASS | 1 | `1bc0bb813b1d` |
| 5 | memory exhaustion (`resource`) | os-level | PASS | 1 | `c835d63d4a5d` |
| 6 | protocol cheating (forged session/sequence/digest) (`protocol`) | protocol-level | PASS | 4 | `047c206ac0fb` |
| 7 | host-crash journal recovery (`crash-recovery`) | runtime-level | PASS | 3 | `557e184bec6d` |
| 8 | revocation freezes late results (M6-SBX-004) (`revoke`) | runtime-level | PASS | 3 | `90fe41df71fc` |
| 9 | fault injection (journal/audit failures) (`fault-injection`) | runtime-level | PASS | 2 | `611c671a93f3` |
| 10 | 16 parallel signed launches (`capacity`) | runtime-level | PASS | 1 | `140a250d9411` |

## Attack detail

### host-file — host filesystem escapes (guard channel + bare-channel probe)

- enforced by: guard-level
- host check: markerReadable=true hostGuardRejectsEscapes=true hostGuardAllowsLegit=true

| vector | blocked | observation |
|--------|---------|-------------|
| direct-host-file | true | guard: worker: sandbox escape (file): absolute path outside sandbox: C:\Users\mujun\AppData\Local\Temp\TestRedTeamProductionEscape2629504839\001\rt\rt-host-file\host-marker-secret.txt |
| junction-escape | true | guard: worker: sandbox escape (file): resolved path escapes sandbox: C:\Users\mujun\AppData\Local\Temp\TestRedTeamProductionEscape2629504839\001\rt\rt-host-file\root\escape-dir\secret.txt -> C:\Users\mujun\AppData\Local\Temp\TestRedTeamProductionEscape2629504839\001\rt\rt-host-file\host-secret-dir\secret.txt |
| positive-control | true | guard: <nil> |

### network — network egress (guard channel + bare-channel probe)

- enforced by: guard-level
- host check: hostGuardAgrees=true targets=7

| vector | blocked | observation |
|--------|---------|-------------|
| imds-aws | true | guard: worker: sandbox escape (network): cloud metadata endpoint: 169.254.169.254 |
| imds-gcp | true | guard: worker: sandbox escape (network): cloud metadata endpoint: metadata.google.internal |
| intranet-rdp | true | guard: worker: sandbox escape (network): target not on allowlist: 10.0.0.1:3389 |
| host-loopback | true | guard: worker: sandbox escape (network): loopback dial: 127.0.0.1 |
| not-allowlisted | true | guard: worker: sandbox escape (network): target not on allowlist: evil.example.com:443 |
| dns-rebinding-resolved-ip | true | guard: worker: sandbox escape (network): target not on allowlist: 10.0.0.99:443 |
| allowlisted-positive-control | true | guard: <nil> |

### secret — parent environment / secret inheritance

- enforced by: os-level
- host check: hostGetenv="leak-me-if-you-can" envCount=3

| vector | blocked | observation |
|--------|---------|-------------|
| env-inheritance | true | env=[STDIOWORKER_SESSION=766e0a55a20f0c6579f21832faa4e8aa STDIOWORKER_SPEC_DIGEST=71b7141231c9e326175400b302adb08900224743a75b24a706b09b479c47f497 SystemRoot=C:\WINDOWS] leakedVariables=0 |

### proctree — fork bomb and process-tree survival

- enforced by: os-level
- host check: grandchilds=3 allDeadAfterRun=true

| vector | blocked | observation |
|--------|---------|-------------|
| fork-bomb | true | spawned=3 rejected=13 pids=[43528 50044 25660] (job active-process quota) |

### resource — memory exhaustion

- enforced by: os-level
- host check: job commit quota is enforced by the OS (SetInformationJobObject JobMemoryLimit)

| vector | blocked | observation |
|--------|---------|-------------|
| memory-exhaustion | true | virtual alloc refused after 192 MiB: The paging file is too small for this operation to complete. (requested 352 MiB, committed 192 MiB) |

### protocol — protocol cheating (forged session/sequence/digest)

- enforced by: protocol-level
- host check: expiredAuditEvents=4 cheaters=4

| vector | blocked | observation |
|--------|---------|-------------|
| forged-session | true | verdict=EXPIRED (session binding + strict sequence + spec digest + heartbeat watchdogs) |
| seq-gap | true | verdict=EXPIRED (session binding + strict sequence + spec digest + heartbeat watchdogs) |
| digest-lie | true | verdict=EXPIRED (session binding + strict sequence + spec digest + heartbeat watchdogs) |
| silent | true | verdict=EXPIRED (session binding + strict sequence + spec digest + heartbeat watchdogs) |

### crash-recovery — host-crash journal recovery

- enforced by: runtime-level
- host check: recoveredAuditEvents=1

| vector | blocked | observation |
|--------|---------|-------------|
| host-crash-orphans | true | marked CRASHED, 0 unrecovered remain |
| torn-journal-tail | true | corrupt tail tolerated, prior good state recovered |
| double-recovery | true | second walk is idempotent |

### revoke — revocation freezes late results (M6-SBX-004)

- enforced by: runtime-level
- host check: journalRevoked=true

| vector | blocked | observation |
|--------|---------|-------------|
| revoke-kills-tree | true | state=REVOKED rootPidAlive=false |
| late-result-freeze | true | result frozen nil, Wait returns ErrRevoked (M6-SBX-004) |
| revocation-audited | true | actions=[stdio.worker.launched stdio.worker.revoked] |

### fault-injection — fault injection (journal/audit failures)

- enforced by: runtime-level
- host check: fail-fast on durability loss, best-effort on observability loss

| vector | blocked | observation |
|--------|---------|-------------|
| journal-write-failure | true | launchErr=true orphanRuns=0 |
| audit-sink-failure | true | run state=COMPLETED despite failing audit sink |

### capacity — 16 parallel signed launches

- enforced by: runtime-level
- host check: launchedAuditEvents=26 completedAuditEvents=21

| vector | blocked | observation |
|--------|---------|-------------|
| parallel-launch-storm | true | completed=16/16 elapsed=531ms |

## Gate notes

- A 5C red-team PASS alone authorizes nothing: production stdio enablement additionally requires the Security Owner sign-off recorded outside this repository.
- The runtime Gate stays closed and M6-MCP-004 keeps the stdio transport disabled at the registry gate.
- host-file and network are enforced at the worker.Guard layer; the bare-channel probes record the current OS boundary honestly (no AppContainer in this build).
- secret, proctree and resource are OS-enforced (explicit environment block, Job Object quotas).
- This bundle binds the same frozen policy digest the 5B launch specs verify (config-digest contract of 5A/5B/5C).
