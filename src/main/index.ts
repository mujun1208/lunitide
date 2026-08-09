import { join } from 'node:path'
import { app, BrowserWindow, dialog, ipcMain, Menu, shell } from 'electron'
import { is } from '@electron-toolkit/utils'
import { EngineManager } from './engine-manager'
import { AppLogger } from './logger'
import { UpdateManager } from './update-manager'
import { ProviderStore, validateProviderInput } from './provider-store'
import type { ProviderInput, ProviderTestResult } from '../shared/models'

let logger: AppLogger
let engine: EngineManager
let updater: UpdateManager
let providers: ProviderStore
let mainWindow: BrowserWindow | null = null
let quitting = false

function createWindow(): void {
  const icon = app.isPackaged
    ? join(process.resourcesPath, 'lunitide-icon.ico')
    : join(__dirname, '../../resources/lunitide-icon.ico')

  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 980,
    minHeight: 640,
    show: false,
    autoHideMenuBar: true,
    backgroundColor: '#050a14',
    title: 'Lunitide 月汐',
    icon,
    webPreferences: {
      preload: join(__dirname, '../preload/index.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true
    }
  })

  mainWindow.setMenuBarVisibility(false)
  mainWindow.once('ready-to-show', () => mainWindow?.show())
  mainWindow.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith('https://')) void shell.openExternal(url)
    return { action: 'deny' }
  })
  mainWindow.webContents.on('will-navigate', (event) => event.preventDefault())

  if (is.dev && process.env.ELECTRON_RENDERER_URL) {
    void mainWindow.loadURL(process.env.ELECTRON_RENDERER_URL)
  } else {
    void mainWindow.loadFile(join(__dirname, '../renderer/index.html'))
  }
}

app.whenReady().then(async () => {
  app.setAppUserModelId('com.lunitide.desktop')
  Menu.setApplicationMenu(null)
  logger = new AppLogger()
  engine = new EngineManager(logger)
  updater = new UpdateManager(logger)
  providers = new ProviderStore()
  await providers.initialize()
  ipcMain.handle('engine:status', () => engine.getStatus())
  ipcMain.handle('engine:restart', async () => {
    await engine.restart()
    return engine.getStatus()
  })
  ipcMain.handle('diagnostics:export', async () => {
    const options = {
      title: '导出 Lunitide 诊断信息',
      defaultPath: `lunitide-diagnostics-${new Date().toISOString().slice(0, 10)}.json`,
      filters: [{ name: 'JSON', extensions: ['json'] }]
    }
    const result = mainWindow
      ? await dialog.showSaveDialog(mainWindow, options)
      : await dialog.showSaveDialog(options)
    if (result.canceled || !result.filePath) return null
    logger.exportDiagnostics(result.filePath, engine.getStatus())
    return result.filePath
  })
  ipcMain.handle('update:status', () => updater.getStatus())
  ipcMain.handle('update:check', () => updater.check())
  ipcMain.handle('update:install', async () => {
    await engine.stop()
    updater.install()
  })
  const authorizeIpc = (event: Electron.IpcMainInvokeEvent): void => {
    if (!mainWindow || event.sender !== mainWindow.webContents || event.senderFrame !== mainWindow.webContents.mainFrame) throw new Error('拒绝未授权的 IPC 调用')
  }
  ipcMain.handle('providers:list', (event) => { authorizeIpc(event); return providers.list() })
  ipcMain.handle('providers:save', (event, input: ProviderInput) => { authorizeIpc(event); return providers.save(validateProviderInput(input)) })
  ipcMain.handle('providers:delete', (event, id: string) => { authorizeIpc(event); return providers.remove(id) })
  ipcMain.handle('providers:reveal-key', (event, id: string) => { authorizeIpc(event); return { apiKey: providers.revealApiKey(id) } })
  ipcMain.handle('providers:models', async (event, id: string) => {
    authorizeIpc(event)
    return engine.request('/v1/providers/models', providers.resolve(id))
  })
  ipcMain.handle('providers:test', async (event, id: string, model?: string): Promise<ProviderTestResult> => {
    try {
      authorizeIpc(event)
      const provider = providers.resolve(id, model)
      return await engine.request<ProviderTestResult>('/v1/providers/test', provider)
    } catch (error) {
      return { ok: false, detail: error instanceof Error ? error.message : '连接测试失败' }
    }
  })
  engine.subscribe((status) => mainWindow?.webContents.send('engine:status-changed', status))
  updater.subscribe((status) => mainWindow?.webContents.send('update:status-changed', status))

  createWindow()
  await engine.start()

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow()
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})

app.on('before-quit', (event) => {
  if (!engine || engine.getStatus().state === 'stopped') return
  event.preventDefault()
  if (quitting) return
  quitting = true
  void engine.stop().finally(() => {
    quitting = false
    app.quit()
  })
})
