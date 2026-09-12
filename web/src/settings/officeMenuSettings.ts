import { useEffect, useState } from 'react'

export const OFFICE_MENU_KEY = 'lunitide:office-menu'
const OFFICE_MENU_EVENT = 'lunitide:office-menu-changed'
export type OfficeMenuSettings = { people: boolean; mro: boolean; office: boolean; meetings: boolean; agentHub: boolean }
export const DEFAULT_OFFICE_MENU: Readonly<OfficeMenuSettings> = { people: false, mro: false, office: false, meetings: false, agentHub: false }

export function loadOfficeMenu(): OfficeMenuSettings {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(OFFICE_MENU_KEY) ?? '{}')
    if (value && typeof value === 'object') {
      const saved = value as Record<string, unknown>
      return { people: saved.people === true, mro: saved.mro === true, office: saved.office === true, meetings: saved.meetings === true, agentHub: saved.agentHub === true }
    }
  } catch { /* Missing or invalid preferences use the same defaults. */ }
  return { ...DEFAULT_OFFICE_MENU }
}

export function saveOfficeMenu(key: keyof OfficeMenuSettings, enabled: boolean): void {
  localStorage.setItem(OFFICE_MENU_KEY, JSON.stringify({ ...loadOfficeMenu(), [key]: enabled }))
  window.dispatchEvent(new Event(OFFICE_MENU_EVENT))
}

export function useOfficeMenu(): OfficeMenuSettings {
  const [settings, setSettings] = useState(loadOfficeMenu)
  useEffect(() => {
    const refresh = () => setSettings(loadOfficeMenu())
    const storage = (event: StorageEvent) => { if (!event.key || event.key === OFFICE_MENU_KEY) refresh() }
    window.addEventListener(OFFICE_MENU_EVENT, refresh)
    window.addEventListener('storage', storage)
    return () => {
      window.removeEventListener(OFFICE_MENU_EVENT, refresh)
      window.removeEventListener('storage', storage)
    }
  }, [])
  return settings
}
