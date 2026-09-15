import { expect, it } from 'vitest'
import { AGENT_HUB_DIR_PICK_MS, capBridgeDeadlineMs } from './client'

it('lets agentHub.dir.pick and agentHub.inbox wait for native dialogs', () => {
  expect(capBridgeDeadlineMs('agentHub.dir.pick', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.inbox', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.detect', AGENT_HUB_DIR_PICK_MS)).toBe(30_000)
})

it('lets agentHub.install wait as long as a native dialog', () => {
  expect(capBridgeDeadlineMs('agentHub.install', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
})

it('lets project.root.pick wait for the same native dialog cap', () => {
  expect(capBridgeDeadlineMs('project.root.pick', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
})
