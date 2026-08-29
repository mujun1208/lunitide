import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

vi.mock('../bridge/client', () => ({
  providerBridge: { list: vi.fn().mockResolvedValue({ items: [] }) },
}))

import { SubagentsPanel } from './SubagentsPanel'
import {
  capsForPack,
  defaultSubagentSettings,
  loadSubagentSettings,
  saveSubagentSettings,
} from './subagentSettings'

afterEach(() => cleanup())

beforeEach(() => {
  localStorage.clear()
})

function packRadio(label: string, scope: Pick<typeof screen, 'getByRole'> = screen) {
  return scope.getByRole('radio', {
    name: (_accessible, el) => el instanceof HTMLElement && el.querySelector('b')?.textContent === label,
  })
}

async function fillNewProfile(user: ReturnType<typeof userEvent.setup>, id = 'docs') {
  await user.type(screen.getByPlaceholderText('my-research'), id)
  await user.type(screen.getByRole('textbox', { name: '名称' }), 'Docs')
  await user.type(screen.getByRole('textbox', { name: '系统提示' }), 'Summarize docs.')
}

describe('SubagentsPanel capability packs', () => {
  test('shows named packs instead of a raw readCaps checkbox grid', () => {
    render(<SubagentsPanel />)
    expect(screen.getByRole('radiogroup', { name: '新 profile 能力包' })).toBeInTheDocument()
    expect(packRadio('全部权限')).toHaveAttribute('aria-checked', 'true')
    expect(packRadio('只读权限')).toBeInTheDocument()
    expect(packRadio('网络检索')).toBeInTheDocument()
    expect(packRadio('浏览器操作')).toBeInTheDocument()
    expect(screen.queryByText('readCaps')).not.toBeInTheDocument()
    const advanced = screen.getByText('高级：逐项能力').closest('details')
    expect(advanced).toBeTruthy()
    expect(advanced).not.toHaveAttribute('open')
  })

  test('selecting 只读 persists the read cap set', async () => {
    const user = userEvent.setup()
    render(<SubagentsPanel />)
    await fillNewProfile(user)
    await user.click(packRadio('只读权限'))
    await user.click(screen.getByRole('button', { name: '添加自定义 profile' }))
    expect(loadSubagentSettings().customProfiles[0]?.readCaps).toEqual(capsForPack('read'))
  })

  test('selecting 全部 enables every supported cap', async () => {
    const user = userEvent.setup()
    render(<SubagentsPanel />)
    await fillNewProfile(user)
    await user.click(packRadio('全部权限'))
    await user.click(screen.getByRole('button', { name: '添加自定义 profile' }))
    expect(loadSubagentSettings().customProfiles[0]?.readCaps).toEqual(capsForPack('all'))
  })

  test('changing a saved profile pack writes the mapped caps immediately', async () => {
    saveSubagentSettings({
      ...defaultSubagentSettings(),
      customProfiles: [{
        id: 'docs',
        displayName: 'Docs',
        systemPrompt: 'Summarize docs.',
        readCaps: capsForPack('web'),
      }],
    })
    const user = userEvent.setup()
    render(<SubagentsPanel />)
    const group = screen.getByRole('radiogroup', { name: 'Docs 能力包' })
    await user.click(packRadio('浏览器操作', within(group)))
    expect(loadSubagentSettings().customProfiles[0]?.readCaps).toEqual(capsForPack('browser'))
  })

  test('恢复默认 clears custom profiles and resets the draft pack', async () => {
    const user = userEvent.setup()
    render(<SubagentsPanel />)
    await fillNewProfile(user)
    await user.click(packRadio('只读权限'))
    await user.click(screen.getByRole('button', { name: '添加自定义 profile' }))
    expect(loadSubagentSettings().customProfiles).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: '恢复默认' }))
    expect(loadSubagentSettings().customProfiles).toEqual([])
    expect(screen.queryByRole('radiogroup', { name: 'Docs 能力包' })).not.toBeInTheDocument()
    expect(packRadio('全部权限')).toHaveAttribute('aria-checked', 'true')
  })
})
