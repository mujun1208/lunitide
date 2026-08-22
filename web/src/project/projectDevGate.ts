import { deliverableBridge, type DeliverableBridge } from '../bridge/client'
import type { ProjectType } from '../generated/bridge'
import { devChecklistReady, devPhaseForType } from './checklistTypes'

export async function fetchDevGateReady(
  projectId: string,
  type: ProjectType,
  bridge: DeliverableBridge = deliverableBridge,
): Promise<boolean> {
  const devPhase = devPhaseForType(type)
  const result = await bridge.list({ projectId, phase: devPhase })
  const dev = result.items.find(item => item.documentType === 'dev_checklist')
  return devChecklistReady(dev?.status)
}
