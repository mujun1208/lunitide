// Entering 对话模式 inspects the voice path once and starts listening.
// It never bounces the user to Settings: 云端 is the default card, and a
// saved 本地 card is kept.
import { applyVoicePath, loadCompanionSettings, type CompanionSettings } from './companionSettings'
import { useWindowsDefaultMicrophone } from '../../settings/microphone'
import type { VoicePath } from './voicePersonas'

export interface PreparedCompanionEntry {
  settings: CompanionSettings
  voicePath: VoicePath
  /** True when a retired MiniCPM-o save was migrated onto 云端 on entry. */
  usedFallback?: boolean
}

export async function resolveCompanionVoicePath(settings: CompanionSettings): Promise<VoicePath> {
  if (settings.voicePath === 'local' || settings.voicePath === 'volc') return settings.voicePath
  return 'cloud'
}

/** Inspect once on 对话模式 enter: Windows-default mic + keep an explicit ready path. */
export async function prepareCompanionEntry(
  loaded: CompanionSettings = loadCompanionSettings(),
): Promise<PreparedCompanionEntry> {
  useWindowsDefaultMicrophone()
  const voicePath = await resolveCompanionVoicePath(loaded)
  const settings = voicePath === loaded.voicePath ? loaded : applyVoicePath(loaded, voicePath)
  return { settings, voicePath }
}
