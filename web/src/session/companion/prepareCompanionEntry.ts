// Entering 对话模式 inspects the voice path once and starts listening.
// It never bounces the user to Settings: 云端 is the default card, and a
// saved 本地 card is kept.
import { applyVoicePath, hasExplicitCompanionVoicePath, loadCompanionSettings, saveCompanionSettings, type CompanionSettings } from './companionSettings'
import { useWindowsDefaultMicrophone } from '../../settings/microphone'
import type { VoicePath } from './voicePersonas'
import { inspectCompanionEntry, pendingCompanionLights, type CompanionEntryReport, type CompanionLightProbes } from './companionLights'

export interface PreparedCompanionEntry extends CompanionEntryReport {
  settings: CompanionSettings
  voicePath: VoicePath
  /** True when a retired MiniCPM-o save was migrated onto 云端 on entry. */
  usedFallback?: boolean
}

export async function resolveCompanionVoicePath(
  settings: CompanionSettings,
  hint?: { hasVolc?: boolean; explicitPath?: boolean },
): Promise<VoicePath> {
  if (settings.voicePath === 'local' || settings.voicePath === 'volc') return settings.voicePath
  if (hint?.hasVolc && hint.explicitPath === false) return 'volc'
  return 'cloud'
}

/** Inspect once on 对话模式 enter: Windows-default mic + keep an explicit ready path. */
export async function prepareCompanionEntry(
  loaded: CompanionSettings = loadCompanionSettings(),
  probes?: CompanionLightProbes,
): Promise<PreparedCompanionEntry> {
  useWindowsDefaultMicrophone()
  const explicitPath = hasExplicitCompanionVoicePath()
  let voicePath = await resolveCompanionVoicePath(loaded)
  let settings = voicePath === loaded.voicePath ? loaded : applyVoicePath(loaded, voicePath)
  let report = await inspectCompanionEntry(voicePath, settings.refEndpoint, probes)
  if (voicePath === 'cloud' && !explicitPath && report.hasVolc) {
    voicePath = 'volc'
    settings = applyVoicePath(loaded, 'volc')
    saveCompanionSettings(settings)
    report = await inspectCompanionEntry('volc', settings.refEndpoint, probes)
  }
  return { settings, voicePath, ...report }
}

export { pendingCompanionLights }
