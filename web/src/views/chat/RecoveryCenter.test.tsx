import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RecoveryCenter } from './RecoveryCenter'

afterEach(cleanup)

const ev = (seq: number, summary = `事件 ${seq}`) => ({ seq, type: 'message.append', summary, at: '2026-01-01T00:00:00Z' })

describe('RecoveryCenter 事件重放列表', () => {
  it('渲染事件条目（序号、类型、摘要）', () => {
    render(<RecoveryCenter events={[ev(1), ev(2), ev(3)]} recovering={false} />)
    expect(screen.getByText('#1')).toBeInTheDocument()
    expect(screen.getAllByText('message.append')).toHaveLength(3)
    expect(screen.getByText('事件 3')).toBeInTheDocument()
  })

  it('序列连续时不显示缺口警示', () => {
    render(<RecoveryCenter events={[ev(1), ev(2), ev(3)]} recovering={false} />)
    expect(screen.queryByText(/序列缺口/)).toBeNull()
  })

  it('序列不连续处显示「序列缺口 N→M」警示条', () => {
    render(<RecoveryCenter events={[ev(1), ev(2), ev(7), ev(8)]} recovering={false} />)
    expect(screen.getByText('序列缺口 2→7')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('多处缺口逐一警示', () => {
    render(<RecoveryCenter events={[ev(1), ev(3), ev(9)]} recovering={false} />)
    expect(screen.getByText('序列缺口 1→3')).toBeInTheDocument()
    expect(screen.getByText('序列缺口 3→9')).toBeInTheDocument()
  })

  it('空事件列表显示占位文案', () => {
    render(<RecoveryCenter events={[]} recovering={false} />)
    expect(screen.getByText('暂无可重放事件。')).toBeInTheDocument()
  })
})

describe('RecoveryCenter 恢复按钮', () => {
  it('点击触发 onReplay', () => {
    const onReplay = vi.fn()
    render(<RecoveryCenter events={[ev(1)]} onReplay={onReplay} recovering={false} />)
    fireEvent.click(screen.getByTestId('m5-replay-btn'))
    expect(onReplay).toHaveBeenCalledOnce()
  })

  it('recovering 时按钮禁用并显示恢复中文案', () => {
    const onReplay = vi.fn()
    render(<RecoveryCenter events={[ev(1)]} onReplay={onReplay} recovering />)
    const btn = screen.getByTestId('m5-replay-btn') as HTMLButtonElement
    expect(btn).toBeDisabled()
    expect(btn).toHaveTextContent('恢复中…')
    fireEvent.click(btn)
    expect(onReplay).not.toHaveBeenCalled()
  })

  it('未提供 onReplay 时按钮禁用', () => {
    render(<RecoveryCenter events={[ev(1)]} recovering={false} />)
    expect(screen.getByTestId('m5-replay-btn')).toBeDisabled()
  })
})
