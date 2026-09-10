import {act, cleanup, fireEvent, render, screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {afterEach, describe, expect, it, vi} from 'vitest'
import type {WidgetBridge} from '../bridge/client'
import {SessionWidgetsBar} from './SessionWidgets'
import {WidgetBoard} from '../widgets/WidgetBoard'

afterEach(cleanup)

describe('robot closed-loop simulated user (UI)', () => {
  it('R14 restores timer after reopen and writes pause', async () => {
    const query = vi.fn().mockResolvedValue({
      items: [{
        id: 'board', revision: 2, owner: '01ARZ3NDEKTSV4RRFFQ69G5FAW', title: '发票,回执', status: 'active',
        widgets: [
          {id: 'timer', kind: 'timer', state: {seconds: 119, running: false}},
          {id: 'list', kind: 'checklist'},
        ],
      }],
    })
    const update = vi.fn().mockResolvedValue({id: 'board', revision: 3, owner: 's', title: '发票,回执', status: 'active'})
    render(<SessionWidgetsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh widgetApi={{query, update} as unknown as WidgetBridge} />)
    expect(await screen.findByText('01:59')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', {name: '开始'}))
    fireEvent.click(screen.getByRole('button', {name: '暂停'}))
    const payload = update.mock.calls[update.mock.calls.length - 1][0] as {revision: number; widgets: Array<{state?: {running?: boolean; seconds?: number}}>}
    expect(payload.revision).toBe(2)
    expect(payload.widgets[0].state?.running).toBe(false)
    expect(payload.widgets[0].state?.seconds).toBe(119)
  })

  it('R15 checklist toggle and metric value are visible to a human click', async () => {
    render(<WidgetBoard widgets={[
      {id: 'list', kind: 'checklist', title: '发票,回执', state: {checked: '发票'}},
      {id: 'metric', kind: 'metric', title: '营收', state: {value: '12', unit: '万'}},
    ]} />)
    const invoice = screen.getByRole('checkbox', {name: '发票'})
    expect(invoice).toHaveProperty('checked', true)
    await userEvent.click(screen.getByRole('checkbox', {name: '回执'}))
    expect(screen.getByRole('checkbox', {name: '回执'})).toHaveProperty('checked', true)
    expect(screen.getByLabelText('营收数值').textContent).toContain('12')
  })

  it('R16 timer started from html.gen default counts down', async () => {
    vi.useFakeTimers()
    render(<WidgetBoard widgets={[{id: 't1', kind: 'timer', title: '倒计时'}]} />)
    fireEvent.click(screen.getByRole('button', {name: '开始'}))
    await act(async () => { vi.advanceTimersByTime(1000) })
    expect(screen.getByText('04:59')).toBeTruthy()
    vi.useRealTimers()
  })
})
