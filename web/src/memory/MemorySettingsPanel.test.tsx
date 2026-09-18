import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { MemorySettingsPanel } from './MemorySettingsPanel'

afterEach(cleanup)

it('TestMemoryModePolicy: renders three exclusive capture modes', () => {
  const onChange = vi.fn()
  render(
    <MemorySettingsPanel
      draft={{ captureMode: 'auto', personalMemoryEnabled: true, projectMemoryEnabled: true, revision: 1 }}
      onChange={onChange}
      onSave={vi.fn()}
    />,
  )
  expect(screen.getAllByRole('radio')).toHaveLength(3)
  expect(screen.getByRole('switch', { name: '个人记忆' })).toBeChecked()
  expect(screen.getByRole('switch', { name: '项目记忆' })).toBeChecked()
  fireEvent.click(screen.getByRole('radio', { name: /手动/ }))
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ captureMode: 'manual' }))
})

it('TestMemoryScopeSwitches: personal and project switches are independently named', () => {
  const onChange = vi.fn()
  render(
    <MemorySettingsPanel
      draft={{ captureMode: 'auto', personalMemoryEnabled: true, projectMemoryEnabled: false, revision: 1 }}
      onChange={onChange}
      onSave={vi.fn()}
    />,
  )
  expect(screen.getByRole('switch', { name: '个人记忆' })).toBeChecked()
  expect(screen.getByRole('switch', { name: '项目记忆' })).not.toBeChecked()
  fireEvent.click(screen.getByRole('switch', { name: '项目记忆' }))
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ projectMemoryEnabled: true }))
})
