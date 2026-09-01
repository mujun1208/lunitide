const CHATS_OPEN_KEY = 'lunitide:sidebar-chats-open'
const PROJECTS_OPEN_KEY = 'lunitide:sidebar-projects-open'

export function readSidebarFlag(key: string, fallback: boolean): boolean {
  try {
    const raw = localStorage.getItem(key)
    if (raw === '0') return false
    if (raw === '1') return true
  } catch {
    /* ignore */
  }
  return fallback
}

export function writeSidebarFlag(key: string, value: boolean): void {
  try {
    localStorage.setItem(key, value ? '1' : '0')
  } catch {
    /* ignore */
  }
}

export const SIDEBAR_CHATS_OPEN_KEY = CHATS_OPEN_KEY
export const SIDEBAR_PROJECTS_OPEN_KEY = PROJECTS_OPEN_KEY

export function readChatsOpen(): boolean {
  return readSidebarFlag(CHATS_OPEN_KEY, true)
}

export function readProjectsOpen(): boolean {
  return readSidebarFlag(PROJECTS_OPEN_KEY, true)
}
