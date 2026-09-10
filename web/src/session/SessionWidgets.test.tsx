import {cleanup, fireEvent, render, screen} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import type {WidgetBridge} from '../bridge/client'
import {SessionWidgetsBar} from './SessionWidgets'

afterEach(cleanup)

it('restores persisted timer state and writes pause back', async () => {
  const query = vi.fn().mockResolvedValue({
    items: [{
      id: 'board', revision: 2, owner: '01ARZ3NDEKTSV4RRFFQ69G5FAW', title: '倒计时', status: 'active',
      widgets: [{id: 'a', kind: 'timer', state: {seconds: 119, running: false}}],
    }],
  })
  const update = vi.fn().mockResolvedValue({id: 'board', revision: 3, owner: 's', title: '倒计时', status: 'active'})
  const widgetApi = {query, update} as unknown as WidgetBridge
  render(<SessionWidgetsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh widgetApi={widgetApi} />)
  expect(await screen.findByText('01:59')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', {name: '开始'}))
  fireEvent.click(screen.getByRole('button', {name: '暂停'}))
  expect(update).toHaveBeenCalled()
  const payload = update.mock.calls[update.mock.calls.length - 1][0] as {revision: number; widgets: Array<{state?: {running?: boolean}}>}
  expect(payload.revision).toBe(2)
  expect(payload.widgets[0].state?.running).toBe(false)
})

it('mounts registered session widgets and skips host html', async () => {
  const query = vi.fn().mockResolvedValue({
    items: [{
      id: 'board', revision: 1, owner: '01ARZ3NDEKTSV4RRFFQ69G5FAW', title: '倒计时', status: 'active',
      widgets: [{id: 'a', kind: 'timer'}],
    }],
  })
  const widgetApi = {query} as unknown as WidgetBridge
  render(<SessionWidgetsBar sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAW" zh widgetApi={widgetApi} />)
  expect(await screen.findByTestId('widget-board')).toBeTruthy()
  expect(screen.getByLabelText('倒计时')).toBeTruthy()
})
