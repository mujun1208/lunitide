import React from 'react'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { OfficeMenuPanel } from './OfficeMenuPanel'
import { DEFAULT_OFFICE_MENU, loadOfficeMenu, OFFICE_MENU_KEY, saveOfficeMenu, useOfficeMenu } from './officeMenuSettings'

afterEach(() => { cleanup(); localStorage.removeItem(OFFICE_MENU_KEY); vi.restoreAllMocks() })

it('defaults every optional navigation entry to hidden and rejects malformed stored values', () => {
  expect(loadOfficeMenu()).toEqual(DEFAULT_OFFICE_MENU)
  localStorage.setItem(OFFICE_MENU_KEY, JSON.stringify({ people: 'false', mro: 1, office: true, meetings: false }))
  expect(loadOfficeMenu()).toEqual({ people: false, mro: false, office: true, meetings: false, agentHub: false })
  localStorage.setItem(OFFICE_MENU_KEY, '{bad')
  expect(loadOfficeMenu()).toEqual(DEFAULT_OFFICE_MENU)
})

it('shares changes between mounted consumers and retains them after remount without changing other keys', () => {
  function Consumer() { return <output>{JSON.stringify(useOfficeMenu())}</output> }
  localStorage.setItem('lunitide:test-retained-chat', 'keep history')
  const first = render(<><OfficeMenuPanel /><Consumer /></>)
  fireEvent.click(screen.getByRole('switch', { name: '同事聊天' }))
  fireEvent.click(screen.getByRole('switch', { name: '办公工作台' }))
  fireEvent.click(screen.getByRole('switch', { name: 'Agent 调度台' }))
  expect(screen.getByRole('status')).toHaveTextContent('"people":true')
  expect(screen.getByRole('status')).toHaveTextContent('"agentHub":true')
  first.unmount()
  render(<OfficeMenuPanel />)
  expect(screen.getByRole('switch', { name: '同事聊天' })).toHaveAttribute('aria-checked', 'true')
  expect(screen.getByRole('switch', { name: '办公工作台' })).toHaveAttribute('aria-checked', 'true')
  fireEvent.click(screen.getByRole('switch', { name: '办公工作台' }))
  expect(localStorage.getItem('lunitide:test-retained-chat')).toBe('keep history')
  localStorage.removeItem('lunitide:test-retained-chat')
})

it('responds to preferences changed by another app window', () => {
  render(<OfficeMenuPanel />)
  act(() => {
    localStorage.setItem(OFFICE_MENU_KEY, JSON.stringify({ meetings: true }))
    window.dispatchEvent(new StorageEvent('storage', { key: OFFICE_MENU_KEY }))
  })
  expect(screen.getByRole('switch', { name: '会议记录' })).toHaveAttribute('aria-checked', 'true')
})

it('reports failed persistence and keeps the previous checked state', () => {
  render(<OfficeMenuPanel />)
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('quota') })
  fireEvent.click(screen.getByRole('switch', { name: '同事聊天' }))
  expect(screen.getByRole('alert')).toHaveTextContent('设置未保存')
  expect(screen.getByRole('switch', { name: '同事聊天' })).toHaveAttribute('aria-checked', 'false')
  expect(() => saveOfficeMenu('office', true)).toThrow()
})
