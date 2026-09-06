import React from 'react'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TemplateBridge } from '../bridge/client'
import type { TemplateListResult } from '../generated/bridge'
import { AssetManagerPage } from './AssetManagerPage'
import { listTemplatePages } from './templatePages'

const template = (id: string, name: string): TemplateListResult['items'][number] => ({
  id, name, templateCode: id, templateType: 'document', status: 'draft',
  createdAt: '2026-09-06T00:00:00Z', updatedAt: '2026-09-06T00:00:00Z', version: 1,
})
const bridge = (list: TemplateBridge['list']): TemplateBridge => ({ list, create: vi.fn(), enable: vi.fn(), void: vi.fn(), restore: vi.fn(), delete: vi.fn(), fileStage: vi.fn() })
afterEach(cleanup)

describe('asset pagination', () => {
  it('loads the next page and searches all pages on the server', async () => {
    const list = vi.fn().mockResolvedValueOnce({ items: [template('1', '第一页')], nextCursor: 'next' })
      .mockResolvedValueOnce({ items: [template('2', '后续模版')] })
      .mockResolvedValueOnce({ items: [template('3', '历史模版')] })
    render(<AssetManagerPage templates={bridge(list)} />)
    expect(await screen.findByText('第一页')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '加载更多模版' }))
    expect(await screen.findByText('后续模版')).toBeInTheDocument()
    expect(screen.getByText('第一页')).toBeInTheDocument()
    expect(list).toHaveBeenNthCalledWith(2, { cursor: 'next' })
    fireEvent.change(screen.getByLabelText('搜索模版'), { target: { value: '历史' } })
    expect(await screen.findByText('历史模版')).toBeInTheDocument()
    expect(list).toHaveBeenLastCalledWith({ query: '历史' })
    expect(screen.queryByText('第一页')).not.toBeInTheDocument()
  })

  it('discards a late page after the filter changes', async () => {
    let resolvePage!: (page: TemplateListResult) => void
    const list = vi.fn().mockResolvedValueOnce({ items: [template('1', '第一页')], nextCursor: 'next' })
      .mockImplementationOnce(() => new Promise(resolve => { resolvePage = resolve }))
      .mockResolvedValueOnce({ items: [template('3', '当前搜索')] })
    render(<AssetManagerPage templates={bridge(list)} />)
    fireEvent.click(await screen.findByRole('button', { name: '加载更多模版' }))
    fireEvent.change(screen.getByLabelText('搜索模版'), { target: { value: '当前' } })
    expect(await screen.findByText('当前搜索')).toBeInTheDocument()
    await act(async () => resolvePage({ items: [template('2', '旧查询页面')] }))
    await waitFor(() => expect(screen.queryByText('旧查询页面')).not.toBeInTheDocument())
    expect(screen.getByText('当前搜索')).toBeInTheDocument()
  })

  it('traverses all selection pages and rejects a repeating cursor', async () => {
    const list = vi.fn().mockResolvedValueOnce({ items: [template('1', 'One')], nextCursor: 'next' })
      .mockResolvedValueOnce({ items: [template('2', 'Two')] })
    expect((await listTemplatePages(bridge(list), { status: 'enabled' })).items).toHaveLength(2)
    expect(list).toHaveBeenLastCalledWith({ status: 'enabled', cursor: 'next' })
    await expect(listTemplatePages(bridge(vi.fn().mockResolvedValue({ items: [], nextCursor: 'repeat' })), {})).rejects.toThrow('分页状态异常')
  })
})
