import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DiffReview, CHANGESET_STATE_LABELS, CHANGE_LABELS, type DiffChangeKind } from './DiffReview'

afterEach(cleanup)

const item = (change: DiffChangeKind) => ({
  path: `src/a-${change}.ts`,
  change,
  lines: [
    { kind: 'ctx' as const, text: 'const a = 1' },
    { kind: 'add' as const, text: 'const b = 2' },
    { kind: 'del' as const, text: 'const c = 3' },
  ],
})

const base = { changesetId: 'cs-01', onAccept: vi.fn().mockResolvedValue(undefined), onReject: vi.fn().mockResolvedValue(undefined) }

describe('DiffReview 状态覆盖（changeset 4 态 × 变更 3 类）', () => {
  for (const [state, stateLabel] of Object.entries(CHANGESET_STATE_LABELS)) {
    for (const change of ['add', 'modify', 'delete'] as DiffChangeKind[]) {
      it(`${state}/${change} 渲染「${stateLabel}」与「${CHANGE_LABELS[change]}」徽章`, () => {
        render(<DiffReview {...base} state={state} items={[item(change)]} />)
        expect(screen.getByTestId('m5-changeset-state')).toHaveTextContent(stateLabel)
        expect(screen.getByText(CHANGE_LABELS[change])).toBeInTheDocument()
        expect(screen.getByText(`src/a-${change}.ts`)).toBeInTheDocument()
      })
    }
  }

  it('未知 changeset 状态兜底原样显示', () => {
    render(<DiffReview {...base} state="mystery" items={[item('add')]} />)
    expect(screen.getByTestId('m5-changeset-state')).toHaveTextContent('mystery')
  })
})

describe('DiffReview 行级 diff', () => {
  it('add 绿 del 红 ctx 灰（按修饰类渲染）', () => {
    const { container } = render(<DiffReview {...base} state="staged" items={[item('modify')]} />)
    expect(container.querySelector('.m5-diff-line-add')).not.toBeNull()
    expect(container.querySelector('.m5-diff-line-del')).not.toBeNull()
    expect(container.querySelector('.m5-diff-line-ctx')).not.toBeNull()
    expect(container.querySelectorAll('.m5-diff-line')).toHaveLength(3)
  })

  it('无行内容时不渲染行级区', () => {
    const { container } = render(<DiffReview {...base} state="staged" items={[{ path: 'x', change: 'delete' }]} />)
    expect(container.querySelector('.m5-diff-lines')).toBeNull()
  })
})

describe('DiffReview 接受/拒绝', () => {
  it('点击接受回调 path，期间按钮进入 loading 文案', async () => {
    let release: () => void = () => {}
    const gate = new Promise<void>(r => { release = r })
    const onAccept = vi.fn().mockReturnValue(gate)
    render(<DiffReview {...base} onAccept={onAccept} state="staged" items={[item('modify')]} />)
    const btn = screen.getByTestId('m5-accept-src/a-modify.ts') as HTMLButtonElement
    fireEvent.click(btn)
    expect(onAccept).toHaveBeenCalledWith('src/a-modify.ts')
    expect(btn).toHaveTextContent('接受中…')
    expect(btn).toBeDisabled()
    release()
    await waitFor(() => expect(screen.getByTestId('m5-accept-src/a-modify.ts')).toHaveTextContent('接受'))
    expect(screen.getByTestId('m5-accept-src/a-modify.ts')).not.toBeDisabled()
  })

  it('点击拒绝回调 path', () => {
    const onReject = vi.fn().mockResolvedValue(undefined)
    render(<DiffReview {...base} onReject={onReject} state="staged" items={[item('modify')]} />)
    fireEvent.click(screen.getByTestId('m5-reject-src/a-modify.ts'))
    expect(onReject).toHaveBeenCalledWith('src/a-modify.ts')
  })
})

describe('DiffReview 冲突态', () => {
  it('conflicted 显示冲突横幅并禁用全部按钮', () => {
    render(<DiffReview {...base} state="conflict" conflicted items={[item('add')]} />)
    expect(screen.getByTestId('m5-conflict-banner')).toHaveTextContent('工作区基线已漂移，本变更集不会应用')
    expect(screen.getByTestId('m5-accept-src/a-add.ts')).toBeDisabled()
    expect(screen.getByTestId('m5-reject-src/a-add.ts')).toBeDisabled()
  })

  it('conflicted 时点击按钮不触发回调', () => {
    const onAccept = vi.fn().mockResolvedValue(undefined)
    render(<DiffReview {...base} onAccept={onAccept} state="conflict" conflicted items={[item('add')]} />)
    fireEvent.click(screen.getByTestId('m5-accept-src/a-add.ts'))
    expect(onAccept).not.toHaveBeenCalled()
  })

  it('默认无冲突横幅', () => {
    render(<DiffReview {...base} state="staged" items={[item('add')]} />)
    expect(screen.queryByTestId('m5-conflict-banner')).toBeNull()
  })

  it('空变更集显示占位', () => {
    render(<DiffReview {...base} state="staged" items={[]} />)
    expect(screen.getByText('变更集没有可审阅的文件。')).toBeInTheDocument()
  })
})
