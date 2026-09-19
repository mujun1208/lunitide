import { expect, it } from 'vitest'
import type { CapabilityRolesGetResult } from '../generated/bridge'
import { computerControlGate, roleConfigured, visionCapabilityGate } from './capabilityGate'

const roles = (filled: Partial<Record<CapabilityRolesGetResult['roles'][number]['role'], boolean>>): CapabilityRolesGetResult['roles'] =>
  (['chat', 'flash', 'vision', 'embed', 'judge', 'gui'] as const).map(role => ({
    role,
    allowJudgeEqChat: false,
    ...(filled[role] ? { providerId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', modelId: role } : {}),
  }))

it('disables computer control when GUI is not bound', () => {
  expect(roleConfigured(roles({}), 'gui')).toBe(false)
  expect(computerControlGate(roles({})).ready).toBe(false)
  expect(computerControlGate(roles({})).reason).toBe('未配置 GUI 能力')
  expect(computerControlGate(roles({ gui: true })).ready).toBe(true)
})

it('disables vision-backed surfaces when vision is not bound', () => {
  expect(visionCapabilityGate(roles({})).ready).toBe(false)
  expect(visionCapabilityGate(roles({ vision: true })).ready).toBe(true)
})
