# Lunitide 0.4.33

## 修复与改进

- **程序列表发布者**：卸载注册写入 `Publisher=Yy.MJ`，并设置 `DisplayIcon` 指向 `Lunitide.exe`，桌面/开始菜单快捷方式使用同一枚 PE 图标。
- **月亮+蓝云图标**：桌面、任务栏、安装向导和「程序和功能」共用带明显蓝云的新图标。
- **本机 Authenticode**：`release/Install-LocalCodeSigning.ps1` 为当前 Windows 账户安装 `CN=Yy.MJ` 码签证书并写入 `LUNITIDE_SIGN_*`。这是账户内可信身份；其他电脑要 CA 颁发的 OV/EV 证书才会显示「已验证的发布者」。
- **English chrome**：产品名、窗口标题、程序列表 DisplayName、首页/侧栏默认英文（`Lunitide`，不再带「月汐」）。可用 **EN/中** 切回中文。技能/项目/会议等内页仍有中文正文。

## 验证

- Quality 等价门禁通过（`CGO_ENABLED=0`）：`verify:bridge`、`Test-OmniExcluded.ps1`、`go test -timeout 20m -count=1 ./...`、`go vet ./...`、`go build ./...`、`generate:bridge`、`typecheck`、`npm --prefix web test`、`npm --prefix web run build`、生成契约 `git diff --exit-code`
- 未跑 CI 第二岗 `go test -race -timeout 90m ./...`（本机耗时长，GitHub `windows-cgo-race` 仍会跑）
- 安装脚本注册 `Publisher=Yy.MJ` / `DisplayIcon`
- 本机 Authenticode：已运行 `Install-LocalCodeSigning.ps1`（`CN=Yy.MJ`，仅当前 Windows 账户信任）

## 安装包

- `release/out/Lunitide-Setup-0.4.33-x64.exe`（Authenticode `Valid`：`CN=Yy.MJ`，DigiCert RFC3161 时间戳。仅当前 Windows 账户信任该证书；其他电脑仍可能提示未知发布者）
- SHA-256：`fbb804ff63d458bcd482ff4de38299415334065d1b545141b748b8ee00aed0b8`
- `release/out/SHA256SUMS.txt`
- `release/out` 只保留 0.4.33 安装包与 stage
