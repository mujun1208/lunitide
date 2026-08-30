import type { VoicePath } from '../session/companion/voicePersonas'

export type MeetingListen = Extract<VoicePath, 'cloud' | 'volc' | 'local'>

export type MeetingSettings = {
  listen: MeetingListen
  modelId: string
}

const KEY = 'lunitide:meeting'

export function defaultMeetingSettings(): MeetingSettings {
  return { listen: 'cloud', modelId: '' }
}

export function loadMeetingSettings(): MeetingSettings {
  const fallback = defaultMeetingSettings()
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as Partial<MeetingSettings>
    const listen = parsed.listen
    return {
      listen: listen === 'volc' || listen === 'local' || listen === 'cloud' ? listen : 'cloud',
      modelId: typeof parsed.modelId === 'string' ? parsed.modelId : '',
    }
  } catch {
    return fallback
  }
}

export function saveMeetingSettings(next: MeetingSettings): MeetingSettings {
  const value: MeetingSettings = {
    listen: next.listen === 'volc' || next.listen === 'local' ? next.listen : 'cloud',
    modelId: next.modelId.trim(),
  }
  localStorage.setItem(KEY, JSON.stringify(value))
  return value
}
