import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { TaskCenter, TASK_STATE_LABELS, type TaskInfo, type TaskState } from './TaskCenter'

afterEach(cleanup)

const task = (over: Partial<TaskInfo> = {}): TaskInfo => ({
  taskId: 'job-01',
  state: 'running',
  logTail: '$ npm run build\nbuild ok',
  nextLogCursor: 128,
  startedAt: '2026-01-01T00:00:00Z',
  ...over,
})

const noop = () => {}

describe('TaskCenter 状态徽章（6 态）', () => {
  it.each(Object.entries(TASK_STATE_LABELS))('状态 %s 显示「%s」', (state, label) => {
    render(<TaskCenter tasks={[task({ taskId: 'job-01', state: state as TaskState })]} onCancel={noop} onReconnect={noop} />)
    expect(screen.getByTestId('m5-task-state-job-01')).toHaveTextContent(label)
  })

  it('渲染日志尾部与启动时间', () => {
    render(<TaskCenter tasks={[task()]} onCancel={noop} onReconnect={noop} />)
    expect(screen.getByTestId('m5-log-job-01')).toHaveTextContent('$ npm run build')
    expect(screen.getByText(/启动于/)).toBeInTheDocument()
  })

  it('空任务列表显示占位', () => {
    render(<TaskCenter tasks={[]} onCancel={noop} onReconnect={noop} />)
    expect(screen.getByText('暂无后台任务。')).toBeInTheDocument()
  })
})

describe('TaskCenter 后台化原因徽章', () => {
  it('elapsed → 超时 10 秒；output → 输出超 1MiB', () => {
    render(
      <TaskCenter
        tasks={[
          task({ taskId: 'job-a', state: 'backgrounded', backgroundReason: 'elapsed' }),
          task({ taskId: 'job-b', state: 'backgrounded', backgroundReason: 'output' }),
        ]}
        onCancel={noop}
        onReconnect={noop}
      />,
    )
    expect(screen.getByText('超时 10 秒')).toBeInTheDocument()
    expect(screen.getByText('输出超 1MiB')).toBeInTheDocument()
  })

  it('未知原因原样显示', () => {
    render(<TaskCenter tasks={[task({ state: 'backgrounded', backgroundReason: 'custom' })]} onCancel={noop} onReconnect={noop} />)
    expect(screen.getByText('custom')).toBeInTheDocument()
  })

  it('长任务自动转后台渲染「已后台化」徽章（state=backgrounded）', () => {
    render(<TaskCenter tasks={[task({ state: 'backgrounded', backgroundReason: 'elapsed' })]} onCancel={noop} onReconnect={noop} />)
    expect(screen.getByTestId('m5-task-state-job-01')).toHaveTextContent('已后台化')
  })
})

describe('TaskCenter 取消', () => {
  it('点击取消立即进入取消中 UI 并回调 taskId', () => {
    const onCancel = vi.fn()
    render(<TaskCenter tasks={[task()]} onCancel={onCancel} onReconnect={noop} />)
    const btn = screen.getByTestId('m5-cancel-job-01') as HTMLButtonElement
    fireEvent.click(btn)
    expect(onCancel).toHaveBeenCalledWith('job-01')
    expect(btn).toHaveTextContent('取消中…')
    expect(btn).toBeDisabled()
  })

  it('取消中再次点击不重复回调', () => {
    const onCancel = vi.fn()
    render(<TaskCenter tasks={[task()]} onCancel={onCancel} onReconnect={noop} />)
    const btn = screen.getByTestId('m5-cancel-job-01') as HTMLButtonElement
    fireEvent.click(btn)
    fireEvent.click(btn)
    expect(onCancel).toHaveBeenCalledOnce()
  })
})

describe('TaskCenter 重连', () => {
  it('backgrounded/orphaned 显示重连按钮并传 (taskId, cursor)', () => {
    const onReconnect = vi.fn()
    render(
      <TaskCenter
        tasks={[
          task({ taskId: 'job-bg', state: 'backgrounded', nextLogCursor: 256 }),
          task({ taskId: 'job-orphan', state: 'orphaned', nextLogCursor: 512 }),
        ]}
        onCancel={noop}
        onReconnect={onReconnect}
      />,
    )
    fireEvent.click(screen.getByTestId('m5-reconnect-job-bg'))
    fireEvent.click(screen.getByTestId('m5-reconnect-job-orphan'))
    expect(onReconnect).toHaveBeenNthCalledWith(1, 'job-bg', 256)
    expect(onReconnect).toHaveBeenNthCalledWith(2, 'job-orphan', 512)
  })

  it('非后台化/孤儿任务不显示重连按钮', () => {
    render(
      <TaskCenter
        tasks={[
          task({ taskId: 'job-run', state: 'running' }),
          task({ taskId: 'job-done', state: 'done' }),
          task({ taskId: 'job-fail', state: 'failed' }),
          task({ taskId: 'job-cancelled', state: 'cancelled' }),
        ]}
        onCancel={noop}
        onReconnect={noop}
      />,
    )
    expect(screen.queryByTestId('m5-reconnect-job-run')).toBeNull()
    expect(screen.queryByTestId('m5-reconnect-job-done')).toBeNull()
    expect(screen.queryByTestId('m5-reconnect-job-fail')).toBeNull()
    expect(screen.queryByTestId('m5-reconnect-job-cancelled')).toBeNull()
  })
})
