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
const bridge = (list: TemplateBridge['list']): TemplateBridge => ({ list, open: vi.fn(), create: vi.fn(), enable: vi.fn(), void: vi.fn(), restore: vi.fn(), delete: vi.fn(), fileStage: vi.fn() })
afterEach(cleanup)

it.each(['draft', 'enabled', 'void'] as const)('opens a viewing copy of a %s asset without enabling or changing it', async status => {
  const item = { ...template('1', '周报模版'), status, fileName: '周报.docx' }
  const api = bridge(vi.fn().mockResolvedValue({ items: [item] }))
  vi.mocked(api.open).mockResolvedValue({ opened: true })
  render(<AssetManagerPage templates={api} />)
  fireEvent.click(await screen.findByRole('button', { name: '查看附件' }))
  expect(await screen.findByRole('status')).toHaveTextContent('已打开 周报.docx 的查看副本')
  expect(api.open).toHaveBeenCalledWith({ id: '1' })
  expect(api.enable).not.toHaveBeenCalled()
})

it('reports an attachment open failure and allows retry', async () => {
  const api = bridge(vi.fn().mockResolvedValue({ items: [{ ...template('1', '周报'), fileName: '周报.docx' }] }))
  vi.mocked(api.open).mockRejectedValueOnce(new Error('附件已丢失')).mockResolvedValueOnce({ opened: true })
  render(<AssetManagerPage templates={api} />)
  fireEvent.click(await screen.findByRole('button', { name: '查看附件' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('附件已丢失')
  fireEvent.click(screen.getByRole('button', { name: '查看附件' }))
  expect(await screen.findByRole('status')).toHaveTextContent('已打开')
})

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

it('rejects oversized assets without reading them and permits the next valid upload', async () => {
  const api = bridge(vi.fn().mockResolvedValue({ items: [] }))
  vi.mocked(api.create).mockResolvedValue({ ...template('saved', '模板'), status: 'draft' })
  render(<AssetManagerPage templates={api} />)
  fireEvent.click(await screen.findByRole('button', { name: /上传资产/ }))
  fireEvent.change(screen.getByLabelText('文件类型 *'), { target: { value: '业务需求分析报告' } })
  fireEvent.change(screen.getByPlaceholderText('描述模版用途和适用范围'), { target: { value: '需求模板' } })
  const large = new File(['x'], '模板.txt')
  const readLarge = vi.fn()
  Object.defineProperties(large, { size: { value: 11 * 1024 * 1024 }, arrayBuffer: { value: readLarge } })
  fireEvent.change(screen.getByLabelText('附件 *'), { target: { files: [large] } })
  fireEvent.click(screen.getByRole('button', { name: '上传并保存' }))
  expect(await screen.findByText('文件超过 10 MiB 限制')).toBeInTheDocument()
  expect(readLarge).not.toHaveBeenCalled()
  expect(api.create).not.toHaveBeenCalled()
  const valid = new File(['ok'], '模板.txt')
  fireEvent.change(screen.getByLabelText(/附件 \*/), { target: { files: [valid] } })
  fireEvent.click(screen.getByRole('button', { name: '上传并保存' }))
  await waitFor(() => expect(api.create).toHaveBeenCalledTimes(1))
  expect(await screen.findByText(/模版已上传，编号 saved/)).toBeInTheDocument()
})
