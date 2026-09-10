import {act, cleanup, fireEvent, render, screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {afterEach, describe, expect, it, vi} from 'vitest'
import {isRegisteredWidget, rejectHostMessage} from './registry'
import {WidgetBoard} from './WidgetBoard'

describe('widget registry', () => {
  afterEach(cleanup)
  it('registers timer and checklist and rejects arbitrary js', () => {
    expect(isRegisteredWidget('timer')).toBe(true)
    expect(isRegisteredWidget('checklist')).toBe(true)
    expect(isRegisteredWidget('html')).toBe(false)
    expect(rejectHostMessage('host.message')).toBe(true)
    expect(rejectHostMessage('script')).toBe(true)
  })
  it('renders only registered components', () => {
    render(<WidgetBoard widgets={[{id: 'a', kind: 'timer', title: '倒计时'}, {id: 'b', kind: 'html' as 'timer'}]} />)
    expect(screen.getByTestId('widget-board')).toBeTruthy()
    expect(screen.getByLabelText('倒计时')).toBeTruthy()
    expect(screen.queryByLabelText('html')).toBeNull()
  })
  it('timer shows a countdown clock and checklist items toggle', async () => {
    render(<WidgetBoard widgets={[
      {id: 't1', kind: 'timer', title: '倒计时'},
      {id: 'c1', kind: 'checklist', title: '发票,回执'},
    ]} />)
    expect(screen.getByLabelText('倒计时')).toBeTruthy()
    expect(screen.getByText('05:00')).toBeTruthy()
    const item = screen.getByRole('checkbox', {name: '发票'})
    expect(item).toHaveProperty('checked', false)
    await userEvent.click(item)
    expect(item).toHaveProperty('checked', true)
  })
  it('registered data widgets show their title instead of a blank section', () => {
    render(<WidgetBoard widgets={[
      {id: 'm1', kind: 'metric', title: '营收', state: {value: '12', unit: '万'}},
      {id: 'd1', kind: 'todo', title: '今日待办'},
      {id: 'p1', kind: 'progress', title: '进度', state: {percent: 40}},
      {id: 'tbl', kind: 'table', title: '明细', state: {rows: '品名|数量\n纸|2'}},
    ]} />)
    expect(screen.getByLabelText('营收')).toBeTruthy()
    expect(screen.getByText('营收')).toBeTruthy()
    expect(screen.getByLabelText('营收数值').textContent).toContain('12')
    expect(screen.getByLabelText('今日待办')).toBeTruthy()
    expect(screen.getByLabelText('进度').getAttribute('aria-valuenow')).toBe('40')
    expect(screen.getByText('纸')).toBeTruthy()
  })
  it('timer start counts down from the html.gen default', async () => {
    vi.useFakeTimers()
    render(<WidgetBoard widgets={[{id: 't1', kind: 'timer', title: '倒计时'}]} />)
    expect(screen.getByText('05:00')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', {name: '开始'}))
    await act(async () => { vi.advanceTimersByTime(1000) })
    expect(screen.getByText('04:59')).toBeTruthy()
    vi.useRealTimers()
  })
})
