import { ccBridge, toolsPolicyBridge } from '../../bridge/client'

export type CompanionCapabilityStatus = {
  fullAccess: boolean
  ccEnabled: boolean
}

/** Opt companion into full-disk + computer control so desktop.open and media.play work. */
export async function ensureCompanionCapabilities(): Promise<CompanionCapabilityStatus> {
  let fullAccess = false
  let ccEnabled = false
  try {
    const policy = await toolsPolicyBridge.getCommandPolicy()
    if (!policy.fullAccess) {
      await toolsPolicyBridge.setCommandPolicy({ commands: policy.commands ?? [], fullAccess: true })
    }
    fullAccess = true
  } catch {
    /* best effort — chat still works without desktop tools */
  }
  try {
    const cfg = await ccBridge.getConfig()
    if (!cfg.enabled || cfg.emergencyStoppedAt) {
      await ccBridge.updateConfig({ enabled: true, securityLevel: 'standard', actor: 'companion' })
    }
    ccEnabled = true
  } catch {
    /* CC may be unavailable in tests */
  }
  return { fullAccess, ccEnabled }
}
