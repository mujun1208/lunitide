import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { RunHeader, RUN_STATE_LABELS, WORKSPACE_STATE_LABELS } from './RunHeader'

afterEach(cleanup)

const MiB = 1024 * 1024
const GiB = 1024 * MiB

const ws = {
  displayPath: '/tmp/run-ws-01',
  usedBytes: 1 * GiB,
  quotaSoft: 1536 * MiB,
  quotaHard: 2 * GiB,
  state: 'active',
  leaseExpiry: '2026-01-01T12:00:00Z',
}

describe('RunHeader 状态徽章', () => {
  it.each(Object.entries(RUN_STATE_LABELS))('Run 状态 %s 显示「%s」', (state, label) => {
    render(<RunHeader state={state} lastEventSeq={1} />)
    expect(screen.getByTestId('m5-run-state')).toHaveTextContent(label)
  })

  it('未知 Run 状态兜底原样显示', () => {
    render(<RunHeader state="weird_state" lastEventSeq={1} />)
    expect(screen.getByTestId('m5-run-state')).toHaveTextContent('weird_state')
  })

  it.each(Object.entries(WORKSPACE_STATE_LABELS))('工作区状态 %s 显示「%s」', (state, label) => {
    render(<RunHeader state="running" lastEventSeq={1} workspace={{ ...ws, state }} />)
    expect(screen.getByTestId('m5-ws-state')).toHaveTextContent(label)
  })

  it('未知工作区状态兜底原样显示', () => {
    render(<RunHeader state="running" lastEventSeq={1} workspace={{ ...ws, state: 'mystery' }} />)
    expect(screen.getByTestId('m5-ws-state')).toHaveTextContent('mystery')
  })
})

describe('RunHeader 事件序号与临时空间', () => {
  it('显示事件序号', () => {
    render(<RunHeader state="running" lastEventSeq={42} />)
    expect(screen.getByTestId('m5-run-seq')).toHaveTextContent('事件 #42')
  })

  it('未提供 workspace 时不渲染临时空间徽标', () => {
    render(<RunHeader state="running" lastEventSeq={1} />)
    expect(screen.queryByTestId('m5-workspace')).toBeNull()
  })

  it('显示位置、配额（2048MiB → 2.0 GiB）与清理期限', () => {
    render(
      <RunHeader
        state="running"
        lastEventSeq={7}
        workspace={{ ...ws, usedBytes: 2048 * MiB, quotaHard: 4096 * MiB, leaseExpiry: '2026-03-01T08:30:00Z' }}
      />,
    )
    expect(screen.getByText('/tmp/run-ws-01')).toBeInTheDocument()
    expect(screen.getByText('2.0 GiB / 4.0 GiB')).toBeInTheDocument()
    expect(screen.getByText(/清理期限/)).toBeInTheDocument()
  })

  it('三色带进度条宽度按 used/soft 占 quotaHard 比例渲染', () => {
    const { container } = render(<RunHeader state="running" lastEventSeq={1} workspace={ws} />)
    const used = container.querySelector<HTMLElement>('.m5-quota-used')
    const soft = container.querySelector<HTMLElement>('.m5-quota-soft')
    expect(used).not.toBeNull()
    expect(soft).not.toBeNull()
    expect(used!).toHaveStyle({ width: '50%' })
    expect(soft!).toHaveStyle({ width: '75%' })
  })

  it('配额越界时进度条宽度收敛到 100%', () => {
    const { container } = render(
      <RunHeader state="running" lastEventSeq={1} workspace={{ ...ws, usedBytes: 4 * GiB }} />,
    )
    expect(container.querySelector<HTMLElement>('.m5-quota-used')!).toHaveStyle({ width: '100%' })
  })
})
