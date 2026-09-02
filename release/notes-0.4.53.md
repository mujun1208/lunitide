# Lunitide 0.4.53

对 0.4.52 落地的**阶段三 M1/M2 与阶段四 S2**（默认关闭治理开关）做一次整体复盘复核，修正一处 M1 自动接受与提名工作流之间的**一致性缺口**，并对第二条自动提名路径做对齐。开关默认全关时，出厂行为与 0.4.52 完全一致。

## 修复

- **M1 自动接受不再遗留悬挂提名**：`ConfirmCandidate` 的人工确认路径在落库后会调用 `NominationService.MarkDecided` 结清对应提名；而 0.4.52 的 `AutoAcceptCandidate` 旁路在写入 fact 后**未结清提名**，会把提名行留在 `nominated` 态而其候选已 `confirmed`（终态），导致提名列表出现无法再确认的"僵尸"待办。现在自动接受成功后同样调用 `MarkDecided`（幂等，对无提名行的 feedback 候选静默跳过），行为与人工确认完全一致。
- **对齐第二条自动提名路径**：`maybeNominateExpertLast`（专家要点自动提名）此前不触发 M1 自动接受。现在与主回合 `maybeAutoNominateTurn` 一致，走同一 `maybeAutoAcceptCandidate` 旁路——两条链路对齐，行为统一。

## 复盘结论（无需改动项）

- **M2 共享总线**：顺序发言 + 有界渲染无并发/越界问题；默认关闭时 `governanceFlags()` 为 nil 也安全回退并行路径，既有理事会测试不受影响。
- **S2 交接票据**：默认双关（引擎开关 + 闸决策）下一律 M8-028 拒绝并审计；作为前向脚手架，票据单次消费的强约束待有消费方时再补，当前无消费方，风险为零。
- **治理开关**：仅在 `cmd/engine` 启动读环境变量，直接构造引擎的测试均为 nil→全关，冻结不变量成立。

## 验证

- `go vet ./...`、全量 `go test ./...`（0 失败）绿
- `go test -race`（131 包，排除 `cmd/desktop` 的已知 `.rsrc` 链接器工具链限制）无数据竞争
- `golangci-lint run`（app / m8app / m8core / config）**0 issues**
- 前端 `tsc --noEmit` 通过、`vitest run` **171 files / 1307 tests** 全绿
- 新增单测：`TestAutoAcceptSettlesNomination`——提名 → 自动接受 → 提名被结清、无悬挂待办

## 安装包

- `release/out/Lunitide-Setup-0.4.53-x64.exe`（真签名，Authenticode 已验、DigiCert 时间戳）
- `release/out/SHA256SUMS.txt`
- 从 0.4.52 升级
