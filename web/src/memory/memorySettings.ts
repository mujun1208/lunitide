import { createMutationAttempt, type MemoryOpsBridge } from '../bridge/client'
import type { MemorySettingsGetResult, MemorySettingsUpdatePayload } from '../generated/bridge'

export type MemorySettingsDraft = {
  captureMode: 'auto' | 'manual' | 'off'
  personalMemoryEnabled: boolean
  projectMemoryEnabled: boolean
  revision: number
}

function fromWire(result: MemorySettingsGetResult): MemorySettingsDraft {
  return {
    captureMode: result.captureMode,
    personalMemoryEnabled: result.personalMemoryEnabled,
    projectMemoryEnabled: result.projectMemoryEnabled,
    revision: result.revision,
  }
}

export async function loadMemorySettings(ops: MemoryOpsBridge): Promise<MemorySettingsDraft> {
  const result = await ops.getSettings({})
  return fromWire(result)
}

export async function saveMemorySettings(ops: MemoryOpsBridge, draft: MemorySettingsDraft): Promise<MemorySettingsDraft> {
  const payload: MemorySettingsUpdatePayload = {
    captureMode: draft.captureMode,
    personalMemoryEnabled: draft.personalMemoryEnabled,
    projectMemoryEnabled: draft.projectMemoryEnabled,
    expectedRevision: draft.revision,
  }
  const result = await ops.updateSettings(payload, {
    attempt: createMutationAttempt('memory.settings.update', payload),
  })
  return fromWire(result)
}

export function memoryModeLabel(mode: MemorySettingsDraft['captureMode']): string {
  if (mode === 'manual') return '仅手动'
  if (mode === 'off') return '已关闭'
  return '自动记忆'
}

export function memoryScopeSummary(draft: MemorySettingsDraft): string {
  const personal = draft.personalMemoryEnabled ? '个人开' : '个人关'
  const project = draft.projectMemoryEnabled ? '项目开' : '项目关'
  return `${personal} · ${project}`
}
