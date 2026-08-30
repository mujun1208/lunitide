import { ccBridge, toolsPolicyBridge } from '../../bridge/client'

export type CompanionCapabilityStatus = {
  fullAccess: boolean
  ccEnabled: boolean
}

const LEGACY_RATE_CAP = 30
const DEFAULT_RATE_CAP = 60

/** Enable computer control for 月伴. Never silently turn on full-disk command policy. */
export async function ensureCompanionCapabilities(): Promise<CompanionCapabilityStatus> {
  let fullAccess = false
  let ccEnabled = false
  try {
    const policy = await toolsPolicyBridge.getCommandPolicy()
    fullAccess = Boolean(policy.fullAccess)
  } catch {
    /* best effort — chat still works without desktop tools */
  }
  try {
    const cfg = await ccBridge.getConfig()
    if (cfg.emergencyStopped) {
      return { fullAccess, ccEnabled: false }
    }
    const patch: { enabled?: boolean; securityLevel?: 'standard'; maxActionsPerMinute?: number; actor: 'companion' } = {
      actor: 'companion',
    }
    let dirty = false
    if (!cfg.enabled) {
      patch.enabled = true
      patch.securityLevel = 'standard'
      dirty = true
    }
    if (cfg.maxActionsPerMinute === LEGACY_RATE_CAP) {
      patch.maxActionsPerMinute = DEFAULT_RATE_CAP
      dirty = true
    }
    if (dirty) {
      await ccBridge.updateConfig(patch)
    }
    ccEnabled = true
  } catch {
    /* CC may be unavailable in tests */
  }
  return { fullAccess, ccEnabled }
}
