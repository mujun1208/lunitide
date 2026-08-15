import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { Artifacts, DOWNLOAD_STATE_LABELS } from './Artifacts'

afterEach(cleanup)

const sha = 'abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789'

const art = (over: Partial<Parameters<typeof Artifacts>[0]['artifacts'][number]> = {}) => ({
  id: 'ar-01',
  mime: 'text/plain',
  size: 2048 * 1024,
  sha256: sha,
  generator: 'agent:planner',
  downloadState: 'blocked' as const,
  createdAt: '2026-01-01T00:00:00Z',
  ...over,
})

describe('Artifacts 列表渲染', () => {
  it('显示 sha256 前 12 位缩写、大小、生成者与 mime', () => {
    render(<Artifacts artifacts={[art()]} onAllowDownload={() => {}} onDownload={() => {}} />)
    expect(screen.getByText('abcdef012345')).toBeInTheDocument()
    expect(screen.getByText('2.0 MiB')).toBeInTheDocument()
    expect(screen.getByText('生成者 agent:planner')).toBeInTheDocument()
    expect(screen.getByText('text/plain')).toBeInTheDocument()
  })

  it('mime 图标四类：文本/图片/压缩/其他', () => {
    const { container } = render(
      <Artifacts
        artifacts={[
          art({ id: 'a1', mime: 'text/plain' }),
          art({ id: 'a2', mime: 'image/png' }),
          art({ id: 'a3', mime: 'application/zip' }),
          art({ id: 'a4', mime: 'application/octet-stream' }),
        ]}
        onAllowDownload={() => {}}
        onDownload={() => {}}
      />,
    )
    const icons = [...container.querySelectorAll<HTMLElement>('.m5-mime-icon')]
    expect(icons.map(i => i.dataset.category)).toEqual(['text', 'image', 'archive', 'other'])
    expect(icons[0]).toHaveTextContent('📄')
    expect(icons[1]).toHaveTextContent('🖼️')
    expect(icons[2]).toHaveTextContent('🗜️')
    expect(icons[3]).toHaveTextContent('📎')
  })

  it('下载三态徽章：已阻断/已允许/已下载', () => {
    render(
      <Artifacts
        artifacts={[
          art({ id: 'a1', downloadState: 'blocked' }),
          art({ id: 'a2', downloadState: 'allowed' }),
          art({ id: 'a3', downloadState: 'downloaded' }),
        ]}
        onAllowDownload={() => {}}
        onDownload={() => {}}
      />,
    )
    expect(screen.getAllByText(DOWNLOAD_STATE_LABELS.blocked)).toHaveLength(1)
    expect(screen.getAllByText(DOWNLOAD_STATE_LABELS.allowed)).toHaveLength(1)
    expect(screen.getAllByText(DOWNLOAD_STATE_LABELS.downloaded)).toHaveLength(1)
  })

  it('空列表显示占位', () => {
    render(<Artifacts artifacts={[]} onAllowDownload={() => {}} onDownload={() => {}} />)
    expect(screen.getByText('暂无产物。')).toBeInTheDocument()
  })
})

describe('Artifacts 预览判定', () => {
  it('text/* 与 image/* 显示预览按钮', () => {
    render(
      <Artifacts
        artifacts={[art({ id: 'a1', mime: 'text/plain' }), art({ id: 'a2', mime: 'image/png' })]}
        onAllowDownload={() => {}}
        onDownload={() => {}}
      />,
    )
    expect(screen.getAllByRole('button', { name: /预览/ })).toHaveLength(2)
  })

  it('非 text/* image/*（zip、octet-stream）不显示预览按钮', () => {
    render(
      <Artifacts
        artifacts={[art({ id: 'a1', mime: 'application/zip' }), art({ id: 'a2', mime: 'application/octet-stream' })]}
        onAllowDownload={() => {}}
        onDownload={() => {}}
      />,
    )
    expect(screen.queryByRole('button', { name: /预览/ })).toBeNull()
  })
})

describe('Artifacts 下载三态按钮', () => {
  it('blocked：confirm 确认后调用 onAllowDownload', () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const onAllowDownload = vi.fn()
    render(<Artifacts artifacts={[art({ id: 'ar-01' })]} onAllowDownload={onAllowDownload} onDownload={() => {}} />)
    fireEvent.click(screen.getByTestId('m5-allow-ar-01'))
    expect(confirm).toHaveBeenCalledOnce()
    expect(onAllowDownload).toHaveBeenCalledWith('ar-01')
    confirm.mockRestore()
  })

  it('blocked：confirm 取消则不调用 onAllowDownload', () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const onAllowDownload = vi.fn()
    render(<Artifacts artifacts={[art({ id: 'ar-01' })]} onAllowDownload={onAllowDownload} onDownload={() => {}} />)
    fireEvent.click(screen.getByTestId('m5-allow-ar-01'))
    expect(onAllowDownload).not.toHaveBeenCalled()
    confirm.mockRestore()
  })

  it('allowed：点击下载调用 onDownload', () => {
    const onDownload = vi.fn()
    render(<Artifacts artifacts={[art({ id: 'ar-01', downloadState: 'allowed' })]} onAllowDownload={() => {}} onDownload={onDownload} />)
    fireEvent.click(screen.getByTestId('m5-download-ar-01'))
    expect(onDownload).toHaveBeenCalledWith('ar-01')
  })

  it('downloaded：下载按钮禁用且无允许下载按钮', () => {
    render(<Artifacts artifacts={[art({ id: 'ar-01', downloadState: 'downloaded' })]} onAllowDownload={() => {}} onDownload={() => {}} />)
    expect(screen.getByTestId('m5-download-ar-01')).toBeDisabled()
    expect(screen.queryByTestId('m5-allow-ar-01')).toBeNull()
  })
})
