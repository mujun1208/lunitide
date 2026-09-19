# Lunitide 0.4.95

0.4.94 装上之后，媒体还在播时 Chat 首页右下角会一直钉着迷你条（文件名、待核验、暂停、关闭）。播放控制只该出现在媒体中心。0.4.95 把这条浮层从 Chat / 设置 / Hub 拿掉，不改播放内核。覆盖升级仍走 GitHub `latest.json`。不改 poison / 月伴 TTS 音色 / 玉盘像素 / 星尘配方。不声称产品 100%。不把本职 9.5 说成已完成。

已装 0.4.94 可直接覆盖安装。`v0.4.95` 的 Setup + `latest.json` 挂到 GitHub Latest。不要覆盖 `v0.4.94` / `v0.4.93` / `v0.4.92` 及更早 tag 的 digest。不要重打 `v0.4.94`。

## 1. 迷你条

- `miniPlayerPhase` 恒为隐藏。Chat 首页、设置、Work 不再钉播放浮层。
- 选择、播放、暂停、关闭只在媒体中心。后台仍可由已打开的播放器出声，只是不再占首页。
- 组件本身还在，测试仍覆盖有人主动要 `active` 时的文案；产品路径不再请求它。

## 2. 发布

- GitHub Latest 指向 `v0.4.95`。旧版 tag 保留。本地只保留这一次签名安装包。

## 3. 不做

- 不重打 `v0.4.94` 及更早。
- 不加公网 Gateway，不加无人批准的 `skill_manage`。
- 不把综合尺 10 分说成本职完成。14 天 soak 和本机 CGO race 不在本包证明。
- 本职 9.5 仍差打开记事本专跑、Cursor Hub 登录，以及装上本包后再看 Chat 首页是否还钉条。

## 产物

- `release/out-0.4.95/Lunitide-Setup-0.4.95-x64.exe`
- `release/out-0.4.95/latest.json`
- `release/out-0.4.95/SHA256SUMS.txt`
