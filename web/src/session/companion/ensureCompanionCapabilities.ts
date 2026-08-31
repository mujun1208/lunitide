import { ccBridge, toolsPolicyBridge } from '../../bridge/client'

export type CompanionCapabilityStatus = {
  fullAccess: boolean
  ccEnabled: boolean
}

const LEGACY_RATE_CAP = 30
const DEFAULT_RATE_CAP = 60

/** Read computer-control state for 月伴. Never turn CC or full-disk policy on. */
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
    ccEnabled = Boolean(cfg.enabled)
    if (ccEnabled && cfg.maxActionsPerMinute === LEGACY_RATE_CAP) {
      await ccBridge.updateConfig({
        maxActionsPerMinute: DEFAULT_RATE_CAP,
        actor: 'companion',
      })
    }
  } catch {
    /* CC may be unavailable in tests */
  }
  return { fullAccess, ccEnabled }
}
