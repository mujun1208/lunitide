import { localAsrStatus } from '../session/companion/localAsr'
import { startLocalCompanionSpeech } from '../session/companion/localSpeech'
import { startCompanionSpeech, type CompanionSpeechHandle, type CompanionSpeechOptions } from '../session/companion/speech'

/** Meeting capture reuses companion ASR. Mic-only; never treats TTS as user speech. */
export async function startMeetingSpeech(options: CompanionSpeechOptions): Promise<CompanionSpeechHandle> {
  const probe = await localAsrStatus()
  const preferLocal = probe?.supported === true && probe.ready === true
  const open = preferLocal ? startLocalCompanionSpeech : startCompanionSpeech
  return open({
    ...options,
    duplex: true,
    spokenText: () => '',
  })
}
