import { app } from 'electron'
import { autoUpdater } from 'electron-updater'
import type { UpdateStatus } from '../shared/update'
import { AppLogger } from './logger'

type StatusListener = (status: UpdateStatus) => void

export class UpdateManager {
  private listeners = new Set<StatusListener>()
  private status: UpdateStatus = {
    state: app.isPackaged ? 'idle' : 'unavailable',
    currentVersion: app.getVersion(),
    detail: app.isPackaged ? '可以检查新版本' : '开发模式不检查更新'
  }

  constructor(private readonly logger: AppLogger) {
    autoUpdater.autoDownload = true
    autoUpdater.autoInstallOnAppQuit = true
    autoUpdater.on('checking-for-update', () => this.setStatus('checking', '正在检查新版本…'))
    autoUpdater.on('update-available', (info) => {
      this.setStatus('available', `发现新版本 ${info.version}，正在下载…`, info.version)
    })
    autoUpdater.on('update-not-available', () => this.setStatus('not-available', '当前已是最新版本'))
    autoUpdater.on('download-progress', (progress) => {
      this.status = {
        ...this.status,
        state: 'downloading',
        percent: Math.round(progress.percent),
        detail: `正在下载更新 ${Math.round(progress.percent)}%`
      }
      this.emit()
    })
    autoUpdater.on('update-downloaded', (info) => {
      this.setStatus('downloaded', `版本 ${info.version} 已下载，重启后安装`, info.version)
    })
    autoUpdater.on('error', (error) => {
      this.logger.error('updater', '更新失败', { error: error.message })
      this.setStatus('error', `检查更新失败：${error.message}`)
    })
  }

  getStatus(): UpdateStatus {
    return { ...this.status }
  }

  subscribe(listener: StatusListener): () => void {
    this.listeners.add(listener)
    listener(this.getStatus())
    return () => this.listeners.delete(listener)
  }

  async check(): Promise<UpdateStatus> {
    if (!app.isPackaged) return this.getStatus()
    try {
      await autoUpdater.checkForUpdates()
    } catch (error) {
      this.logger.error('updater', '检查更新调用失败', { error: String(error) })
      this.setStatus('error', `检查更新失败：${String(error)}`)
    }
    return this.getStatus()
  }

  install(): void {
    if (this.status.state === 'downloaded') autoUpdater.quitAndInstall(false, true)
  }

  private setStatus(state: UpdateStatus['state'], detail: string, availableVersion?: string): void {
    this.status = { state, detail, currentVersion: app.getVersion(), availableVersion }
    this.emit()
  }

  private emit(): void {
    const snapshot = this.getStatus()
    for (const listener of this.listeners) listener(snapshot)
  }
}
