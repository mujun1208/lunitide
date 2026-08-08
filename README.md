# Lunitide 月汐

本地优先的 AI 桌面工作台。当前实现块一 M1：Electron + React 桌面壳与受保护的 Python FastAPI 本地引擎。

## 开发启动

```powershell
python -m pip install -r engine/requirements-dev.txt
npm install
npm run dev
```

## 验证

```powershell
npm run typecheck
npm run test:engine
npm run build
```

## M1 安全边界

- Renderer 开启 context isolation、sandbox，关闭 Node integration。
- preload 只暴露引擎状态查询、订阅和手动重启。
- Python 引擎只绑定 `127.0.0.1` 动态端口，每次启动使用随机 Bearer Token。
- Electron 主进程负责引擎生命周期，60 秒内最多自动恢复 3 次。
