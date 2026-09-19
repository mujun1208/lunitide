import type { CapabilityRolesGetResult } from '../generated/bridge'

export type CapabilityGate = { ready: boolean; reason: string }

export function roleConfigured(roles: CapabilityRolesGetResult['roles'], role: CapabilityRolesGetResult['roles'][number]['role']): boolean {
  const row = roles.find(item => item.role === role)
  return !!(row?.providerId && row?.modelId)
}

export function computerControlGate(roles: CapabilityRolesGetResult['roles'], zh = true): CapabilityGate {
  if (!roleConfigured(roles, 'gui')) {
    return { ready: false, reason: zh ? '未配置 GUI 能力' : 'GUI capability is not configured' }
  }
  return { ready: true, reason: '' }
}

export function visionCapabilityGate(roles: CapabilityRolesGetResult['roles'], zh = true): CapabilityGate {
  if (!roleConfigured(roles, 'vision')) {
    return { ready: false, reason: zh ? '未配置视觉能力' : 'Vision capability is not configured' }
  }
  return { ready: true, reason: '' }
}
