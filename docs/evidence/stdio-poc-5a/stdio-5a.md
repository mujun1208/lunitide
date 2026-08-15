# stdio Strong-Isolation POC Evidence (M6 slice 5A)

- Generated: 2026-08-14T23:51:54Z
- Platform: windows
- Schema: stdio-poc-evidence/1
- Bundle digest: `b74dc646a11a859b6d62983e51e6add36656c9130466a37cfe9d5681d33229cc`
- Verdict: **PASS**

## Assumptions

| # | assumption | enforced by | verdict | attacks | digest |
|---|------------|-------------|---------|---------|--------|
| 1 | host filesystem escapes (../, symlink, junction) (`host-file`) | guard-level | PASS | 5 | `ee4ff4656433` |
| 2 | network egress to host/metadata/intranet targets (`network`) | guard-level | PASS | 7 | `b43f93cd3426` |
| 3 | parent environment / secret inheritance (`secret`) | os-level | PASS | 5 | `6849bfde9846` |
| 4 | fork bomb and process-tree survival (`proctree`) | os-level | PASS | 1 | `4de4f081d426` |
| 5 | memory exhaustion (`resource`) | os-level | PASS | 1 | `eaadec005118` |
| 6 | protocol cheating (oversize/malformed/forged frames) (`protocol`) | protocol-level | PASS | 6 | `86d6d41bc6d8` |

## Attack detail

### host-file — host filesystem escapes (../, symlink, junction)

- enforced by: guard-level
- host check: true (markerReadable=true hostGuardRejectsEscapes=true hostGuardAllowsLegit=true junction="C:\\Users\\mujun\\AppData\\Local\\Temp\\stdio-poc-1521444036\\sandbox\\escape-dir\\secret.txt" symlink="")

| vector | blocked | observation |
|--------|---------|-------------|
| absolute-host-marker | true | worker: sandbox escape (file): absolute path outside sandbox: C:\Users\mujun\AppData\Local\Temp\stdio-poc-1521444036\host-marker-secret.txt |
| parent-traversal | true | worker: sandbox escape (file): parent traversal in path: ..\host-marker-secret.txt |
| deep-traversal | true | worker: sandbox escape (file): parent traversal in path: ..\..\..\..\host-marker-secret.txt |
| junction-escape | true | worker: sandbox escape (file): resolved path escapes sandbox: C:\Users\mujun\AppData\Local\Temp\stdio-poc-1521444036\sandbox\escape-dir\secret.txt -> C:\Users\mujun\AppData\Local\Temp\stdio-poc-1521444036\host-secret-dir\secret.txt |
| in-root-positive-control | true | guard ALLOWED and read 5 bytes |

### network — network egress to host/metadata/intranet targets

- enforced by: guard-level
- host check: true (hostGuardAgrees=true targets=7)

| vector | blocked | observation |
|--------|---------|-------------|
| imds-aws | true | worker: sandbox escape (network): cloud metadata endpoint: 169.254.169.254 |
| imds-gcp | true | worker: sandbox escape (network): cloud metadata endpoint: metadata.google.internal |
| intranet-rdp | true | worker: sandbox escape (network): target not on allowlist: 10.0.0.1:3389 |
| host-loopback | true | worker: sandbox escape (network): loopback dial: 127.0.0.1 |
| not-allowlisted | true | worker: sandbox escape (network): target not on allowlist: evil.example.com:443 |
| dns-rebinding-resolved-ip | true | worker: sandbox escape (network): target not on allowlist: 10.0.0.99:443 |
| allowlisted-positive-control | true | guard ALLOWED dial |

### secret — parent environment / secret inheritance

- enforced by: os-level
- host check: true (hostGetenv="poc-host-secret-7f3a")

| vector | blocked | observation |
|--------|---------|-------------|
| env-inheritance | true | variable absent from child environment |
| env-inheritance | true | variable absent from child environment |
| env-inheritance | true | variable absent from child environment |
| env-inheritance | true | variable absent from child environment |
| env-inheritance | true | variable absent from child environment |

### proctree — fork bomb and process-tree survival

- enforced by: os-level
- host check: true (rootExit=1 waitErr=<nil> grandchilds=3 allDead=true pids=[4240 35220 27088])

| vector | blocked | observation |
|--------|---------|-------------|
| fork-bomb | true | spawned=3 rejected=13 pids=[4240 35220 27088] (job active-process quota) |

### resource — memory exhaustion

- enforced by: os-level
- host check: true (committed 140 MiB then VirtualAlloc failed: The paging file is too small for this operation to complete. (job commit quota))

| vector | blocked | observation |
|--------|---------|-------------|
| memory-exhaustion | true | committed 140 MiB then VirtualAlloc failed: The paging file is too small for this operation to complete. (job commit quota) |

### protocol — protocol cheating (oversize/malformed/forged frames)

- enforced by: protocol-level
- host check: true (trailingReportErr=<nil>)

| vector | blocked | observation |
|--------|---------|-------------|
| zero-length-frame | true | classified=malformed |
| garbage-payload | true | classified=malformed |
| forged-type | true | classified=forged |
| forged-nonce | true | classified=forged |
| forged-probe | true | classified=forged |
| oversize-declared | true | classified=oversize |

## Gate notes

- POC PASS only permits entering 5B (controlled implementation) development.
- stdio transport stays DISABLED: M6-MCP-004 remains in force.
- host-file and network assumptions are enforced at the guard layer (the stdio worker runtime funnels access through the host guard); the OS-level boundary (AppContainer) is 5B scope.
- secret, proctree and resource assumptions are OS-enforced (explicit environment block, Job Object quotas).
- 5A/5B/5C evidence must bind the same build/config digest before production enablement.
