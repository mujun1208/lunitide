import { contextBridge, ipcRenderer } from 'electron'
import type { EngineStatus, LunitideApi } from '../shared/engine'

const api: LunitideApi = {
  getEngineStatus: () => ipcRenderer.invoke('engine:status'),
  restartEngine: () => ipcRenderer.invoke('engine:restart'),
  onEngineStatus: (listener) => {
    const handler = (_event: Electron.IpcRendererEvent, status: EngineStatus): void => listener(status)
    ipcRenderer.on('engine:status-changed', handler)
    return () => ipcRenderer.removeListener('engine:status-changed', handler)
  }
}

contextBridge.exposeInMainWorld('lunitide', api)
