import { ccBridge, toolsPolicyBridge } from '../../bridge/client'

/** Fired whenever computer-control / command policy changes (e.g. from设置→电脑控制).
 *  The companion listens for this so enabling CC reflects in the stage banner
 *  immediately, not only the next time 月伴 is opened. */
export const CC_CONFIG_EVENT = 'lunitide:cc-config'

export function notifyCcConfigChanged(): void {
  try { window.dispatchEvent(new Event(CC_CONFIG_EVENT)) } catch { /* non-DOM env */ }
}

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
