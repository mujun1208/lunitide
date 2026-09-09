# Lunitide 0.4.72

把 0.4.71 签名包与干净检出不一致的交付缺口补上：社区 gstack 脚本进入仓库，办公导出/打开在 Windows 上按同一文件比较。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。

## 1. 社区技能嵌入与 CI 一致

- 根目录忽略从任意 `bin/` 收成只忽略仓库根 `/bin/`。
- `gstack` 上游 `bin/`、`browse/bin`、`careful/bin`、`freeze/bin` 进入 git，干净检出的 `go:embed` 与 `sources.json` 收据一致。
- 本机脏树能过、CI 干净树不过的缺口关掉。

## 2. 办公打开与 ConPTY

- 导出路径改成操作系统规范名，和 `office.artifact.open` 使用同一套 `canonpath`。
- 测试按同一文件比较，不再用 `filepath.Clean` 对 `RUNNER~1` / `runneradmin`。
- ConPTY 生命周期先等壳输出再写标记，避免托管机把未就绪写入当成失败。

## 验证

- Go：`go vet ./...` 干净；`go build ./...`（CGO=0）干净；`golangci-lint run ./...` **0 issues**；`govulncheck ./...` 受影响漏洞 **0**；覆盖率闸 **57.4% ≥ 51%**。
- 前端：`npm audit` **0**（vitest **4.1.11**）；`tsc --noEmit` 通过；`verify:bridge` 无漂移；`vitest run` **263 files / 2005 tests** 全绿。
- `./release/Test-OmniExcluded.ps1` 通过。
- 本机未跑 CI 的 90 分钟 `go test -race`（`windows-cgo-race`）；推送后由 Quality 工作流补跑。
- 未跑 `Test-Install.ps1`（本机已有官方安装，脚本会拒绝）。

## 安装包

- `release/out/Lunitide-Setup-0.4.72-x64.exe`
- `release/out/SHA256SUMS.txt`
- 从 0.4.71 升级。本机有 `LUNITIDE_SIGN_COMMAND` 与 publisher thumbprint，按生产签名打包装包。
