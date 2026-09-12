import { expect, it } from 'vitest'
import { AGENT_HUB_DIR_PICK_MS, capBridgeDeadlineMs } from './client'

it('lets agentHub.dir.pick and agentHub.inbox wait for native dialogs', () => {
  expect(capBridgeDeadlineMs('agentHub.dir.pick', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.inbox', AGENT_HUB_DIR_PICK_MS)).toBe(AGENT_HUB_DIR_PICK_MS)
  expect(capBridgeDeadlineMs('agentHub.detect', AGENT_HUB_DIR_PICK_MS)).toBe(30_000)
})
