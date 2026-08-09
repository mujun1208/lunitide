import { contextBridge, ipcRenderer } from 'electron'
import type { EngineStatus, LunitideApi } from '../shared/engine'
import type { UpdateStatus } from '../shared/update'

const api: LunitideApi = {
  getEngineStatus: () => ipcRenderer.invoke('engine:status'),
  restartEngine: () => ipcRenderer.invoke('engine:restart'),
  exportDiagnostics: () => ipcRenderer.invoke('diagnostics:export'),
  onEngineStatus: (listener) => {
    const handler = (_event: Electron.IpcRendererEvent, status: EngineStatus): void => listener(status)
    ipcRenderer.on('engine:status-changed', handler)
    return () => ipcRenderer.removeListener('engine:status-changed', handler)
  },
  getUpdateStatus: () => ipcRenderer.invoke('update:status'),
  checkForUpdates: () => ipcRenderer.invoke('update:check'),
  installUpdate: () => ipcRenderer.invoke('update:install'),
  onUpdateStatus: (listener) => {
    const handler = (_event: Electron.IpcRendererEvent, status: UpdateStatus): void => listener(status)
    ipcRenderer.on('update:status-changed', handler)
    return () => ipcRenderer.removeListener('update:status-changed', handler)
  },
  listProviders: () => ipcRenderer.invoke('providers:list'),
  saveProvider: (input) => ipcRenderer.invoke('providers:save', input),
  deleteProvider: (id) => ipcRenderer.invoke('providers:delete', id),
  revealProviderApiKey: (id) => ipcRenderer.invoke('providers:reveal-key', id),
  fetchProviderModels: (id) => ipcRenderer.invoke('providers:models', id),
  testProvider: (id, model) => ipcRenderer.invoke('providers:test', id, model)
}

contextBridge.exposeInMainWorld('lunitide', api)
