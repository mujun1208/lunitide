import { afterEach, expect, it } from 'vitest'
import { readChatsOpen, readOfficeOpen, readProjectsOpen, readSidebarFlag, SIDEBAR_CHATS_OPEN_KEY, SIDEBAR_OFFICE_OPEN_KEY, SIDEBAR_PROJECTS_OPEN_KEY, writeSidebarFlag } from './sidebarSplit'

afterEach(() => {
  localStorage.removeItem(SIDEBAR_CHATS_OPEN_KEY)
  localStorage.removeItem(SIDEBAR_PROJECTS_OPEN_KEY)
  localStorage.removeItem(SIDEBAR_OFFICE_OPEN_KEY)
})

it('defaults both sidebar groups open', () => {
  expect(readChatsOpen()).toBe(true)
  expect(readProjectsOpen()).toBe(true)
  expect(readOfficeOpen()).toBe(true)
})

it('persists fold flags as 0/1', () => {
  writeSidebarFlag(SIDEBAR_CHATS_OPEN_KEY, false)
  writeSidebarFlag(SIDEBAR_PROJECTS_OPEN_KEY, true)
  expect(readSidebarFlag(SIDEBAR_CHATS_OPEN_KEY, true)).toBe(false)
  expect(readSidebarFlag(SIDEBAR_PROJECTS_OPEN_KEY, true)).toBe(true)
  expect(readChatsOpen()).toBe(false)
  expect(readProjectsOpen()).toBe(true)
  writeSidebarFlag(SIDEBAR_OFFICE_OPEN_KEY, false)
  expect(readOfficeOpen()).toBe(false)
})
