export type ToolProfile = 'minimal' | 'coding' | 'colleague'

const GENERAL_KEY = 'lunitide:general'

export function loadToolProfile(): ToolProfile | '' {
  try {
    const raw = localStorage.getItem(GENERAL_KEY)
    if (!raw) return ''
    const v = (JSON.parse(raw) as { toolProfile?: unknown }).toolProfile
    if (v === 'minimal' || v === 'coding' || v === 'colleague') return v
  } catch { /* ignore */ }
  return ''
}

export function chatStartToolProfile(): { toolProfile?: ToolProfile } {
  const profile = loadToolProfile()
  return profile ? { toolProfile: profile } : {}
}
