// Entering 对话模式 inspects the voice path once and starts listening.
// It never bounces the user to Settings: 云端 is the default card, and a
// saved 本地 / MiniCPM-o card is kept only when that path is actually ready.
import { applyVoicePath, loadCompanionSettings, type CompanionSettings } from './companionSettings'
import { probeOmniChannel } from '../omni/omniAudio'
import { useWindowsDefaultMicrophone } from '../../settings/microphone'
import type { VoicePath } from './voicePersonas'

export interface PreparedCompanionEntry {
  settings: CompanionSettings
  voicePath: VoicePath
  omniRequested: boolean
  omniReady: boolean
  usedFallback: boolean
}

export async function resolveCompanionVoicePath(settings: CompanionSettings): Promise<{
  voicePath: VoicePath
  omniReady: boolean
}> {
  if (settings.voicePath === 'omni') {
    const omniReady = await probeOmniChannel()
    return { voicePath: omniReady ? 'omni' : 'cloud', omniReady }
  }
  if (settings.voicePath === 'local') {
    return { voicePath: 'local', omniReady: false }
  }
  return { voicePath: 'cloud', omniReady: false }
}

/** Inspect once on 对话模式 enter: Windows-default mic + keep an explicit ready path. */
export async function prepareCompanionEntry(
  loaded: CompanionSettings = loadCompanionSettings(),
): Promise<PreparedCompanionEntry> {
  useWindowsDefaultMicrophone()
  const omniRequested = loaded.voicePath === 'omni'
  const resolved = await resolveCompanionVoicePath(loaded)
  const settings =
    resolved.voicePath === loaded.voicePath ? loaded : applyVoicePath(loaded, resolved.voicePath)
  return {
    settings,
    voicePath: resolved.voicePath,
    omniRequested,
    omniReady: resolved.omniReady,
    usedFallback: omniRequested && resolved.voicePath === 'cloud',
  }
}
