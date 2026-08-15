import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConvertWizard } from './ConvertWizard'

afterEach(cleanup)

const MiB = 1024 * 1024

const preview = {
  sourceWorkspaceId: 'ws-01',
  targetPath: 'E:/projects/moon-app',
  files: [
    { path: 'src/main.ts', change: 'create', size: 2 * MiB },
    { path: 'README.md', change: 'create', size: 4 * 1024 },
    { path: 'docs/guide.md', change: 'update', size: 1024 },
  ],
  gitStatus: '目标已是 Git 仓库（main 分支，干净）',
  conflicts: [],
  previewDigest: 'digest-abcdef-1234',
}

const base = { onConfirm: vi.fn().mockResolvedValue(undefined), onCancel: vi.fn() }

describe('ConvertWizard 三步展示', () => {
  it('显示目标路径', () => {
    render(<ConvertWizard {...base} preview={preview} />)
    expect(screen.getByText('E:/projects/moon-app')).toBeInTheDocument()
  })

  it('渲染文件清单（路径 + 大小 + 变更）', () => {
    render(<ConvertWizard {...base} preview={preview} />)
    expect(screen.getByText('src/main.ts')).toBeInTheDocument()
    expect(screen.getByText('README.md')).toBeInTheDocument()
    expect(screen.getByText('docs/guide.md')).toBeInTheDocument()
    expect(screen.getByText('2.0 MiB')).toBeInTheDocument()
    expect(screen.getByText('4.0 KiB')).toBeInTheDocument()
    expect(screen.getByText('1.0 KiB')).toBeInTheDocument()
    expect(screen.getByText('update')).toBeInTheDocument()
  })

  it('显示 Git 状态；未提供时显示默认提示', () => {
    const { unmount } = render(<ConvertWizard {...base} preview={preview} />)
    expect(screen.getByText('目标已是 Git 仓库（main 分支，干净）')).toBeInTheDocument()
    unmount()
    render(<ConvertWizard {...base} preview={{ ...preview, gitStatus: undefined }} />)
    expect(screen.getByText('目标路径尚未初始化 Git 仓库')).toBeInTheDocument()
  })
})

describe('ConvertWizard 冲突策略预览', () => {
  it('无冲突显示无冲突提示', () => {
    render(<ConvertWizard {...base} preview={preview} />)
    expect(screen.getByTestId('m5-no-conflict')).toHaveTextContent('无冲突')
    expect(screen.queryByTestId('m5-conflict-warning')).toBeNull()
  })

  it('冲突非空时黄色警示列出冲突文件并提示备份后覆盖', () => {
    render(<ConvertWizard {...base} preview={{ ...preview, conflicts: ['README.md', 'src/main.ts'] }} />)
    const warning = screen.getByTestId('m5-conflict-warning')
    expect(warning).toHaveTextContent('将备份后覆盖')
    expect(warning).toHaveTextContent('README.md')
    expect(warning).toHaveTextContent('src/main.ts')
  })
})

describe('ConvertWizard 确认与取消', () => {
  it('确认按钮文案带文件数并回调全部 paths', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    render(<ConvertWizard {...base} onConfirm={onConfirm} preview={preview} />)
    const btn = screen.getByTestId('m5-confirm')
    expect(btn).toHaveTextContent('将复制 3 个文件到目标项目')
    fireEvent.click(btn)
    await waitFor(() => expect(onConfirm).toHaveBeenCalledOnce())
    expect(onConfirm).toHaveBeenCalledWith({ paths: ['src/main.ts', 'README.md', 'docs/guide.md'] })
  })

  it('确认期间按钮禁用（loading 态）', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>(r => { release = r })
    const onConfirm = vi.fn().mockReturnValue(gate)
    render(<ConvertWizard {...base} onConfirm={onConfirm} preview={preview} />)
    const btn = screen.getByTestId('m5-confirm') as HTMLButtonElement
    fireEvent.click(btn)
    expect(btn).toBeDisabled()
    release()
    await waitFor(() => expect(screen.getByTestId('m5-confirm')).not.toBeDisabled())
  })

  it('取消只触发 onCancel，不触发 onConfirm（源状态不变）', () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    const onCancel = vi.fn()
    render(<ConvertWizard {...base} onConfirm={onConfirm} onCancel={onCancel} preview={preview} />)
    fireEvent.click(screen.getByTestId('m5-cancel'))
    expect(onCancel).toHaveBeenCalledOnce()
    expect(onConfirm).not.toHaveBeenCalled()
  })
})

describe('ConvertWizard busy 态', () => {
  it('busy 时确认与取消按钮均禁用', () => {
    render(<ConvertWizard {...base} preview={preview} busy />)
    expect(screen.getByTestId('m5-confirm')).toBeDisabled()
    expect(screen.getByTestId('m5-cancel')).toBeDisabled()
  })

  it('busy 时点击取消不触发 onCancel', () => {
    const onCancel = vi.fn()
    render(<ConvertWizard {...base} preview={preview} busy onCancel={onCancel} />)
    fireEvent.click(screen.getByTestId('m5-cancel'))
    expect(onCancel).not.toHaveBeenCalled()
  })
})
