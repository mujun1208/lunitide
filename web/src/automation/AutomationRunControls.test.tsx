import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react'
import {afterEach, expect, it, vi} from 'vitest'
import type {AutomationBridge} from '../bridge/client'
import type {AutomationRunListResult} from '../generated/bridge'
import {AutomationStopButton, AutomationTimezoneField} from './AutomationRunControls'
import {AutomationCreateDialog, type AutomationDraft} from './AutomationCreateDialog'

afterEach(cleanup)
const run: AutomationRunListResult['runs'][number] = {
  id:'run-one',jobId:'job-one',jobName:'新闻整理',state:'running',trigger:'manual',startedAt:'2026-09-07T00:00:00Z',totalTokens:0,
}

it('cancels only the displayed occurrence, prevents double clicks and refreshes without editing the schedule', async()=>{
  let resolve!:(value:{cancellationRequested:boolean})=>void
  const cancelRun=vi.fn(()=>new Promise<{cancellationRequested:boolean}>(done=>{resolve=done}))
  const setJob=vi.fn(),triggerJob=vi.fn(),refresh=vi.fn()
  const bridge={cancelRun,setJob,triggerJob} as unknown as AutomationBridge
  render(<AutomationStopButton run={run} bridge={bridge} onRequested={refresh}/> )
  const button=screen.getByRole('button',{name:'停止 新闻整理'})
  fireEvent.click(button);fireEvent.click(button)
  expect(button).toBeDisabled()
  expect(cancelRun).toHaveBeenCalledExactlyOnceWith({jobId:'job-one',runId:'run-one'})
  await act(async()=>resolve({cancellationRequested:true}))
  expect(refresh).toHaveBeenCalledOnce()
  expect(screen.getByRole('status')).toHaveTextContent('已产生的结果会保留')
  expect(setJob).not.toHaveBeenCalled();expect(triggerJob).not.toHaveBeenCalled()
})

it('ignores an old cancellation reply after the card has moved to a new run', async()=>{
  let resolve!:(value:{cancellationRequested:boolean})=>void
  const cancelRun=vi.fn(()=>new Promise<{cancellationRequested:boolean}>(done=>{resolve=done}))
  const bridge={cancelRun} as unknown as AutomationBridge,refresh=vi.fn()
  const view=render(<AutomationStopButton run={run} bridge={bridge} onRequested={refresh}/> )
  fireEvent.click(screen.getByRole('button'))
  view.rerender(<AutomationStopButton run={{...run,id:'run-two'}} bridge={bridge} onRequested={refresh}/> )
  await act(async()=>resolve({cancellationRequested:true}))
  expect(screen.queryByRole('status')).toBeNull()
  expect(screen.getByRole('button')).toBeEnabled()
  expect(refresh).not.toHaveBeenCalled()
})

it('keeps a failed stop retryable and never offers cancellation for a finished occurrence', async()=>{
  const cancelRun=vi.fn().mockRejectedValueOnce(new Error('连接中断')).mockResolvedValueOnce({cancellationRequested:false})
  const bridge={cancelRun} as unknown as AutomationBridge,refresh=vi.fn()
  const view=render(<AutomationStopButton run={run} bridge={bridge} onRequested={refresh}/> )
  fireEvent.click(screen.getByRole('button'))
  await screen.findByText('连接中断')
  fireEvent.click(screen.getByRole('button'))
  await waitFor(()=>expect(refresh).toHaveBeenCalledOnce())
  expect(screen.getByRole('status')).toHaveTextContent('这次执行已经结束')
  view.rerender(<AutomationStopButton run={{...run,state:'failed',cancelled:true}} bridge={bridge} onRequested={refresh}/> )
  expect(screen.queryByRole('button')).toBeNull()
})

it('shows UTC for legacy jobs and preserves any existing IANA zone as a selectable value',()=>{
  const onChange=vi.fn()
  const view=render(<AutomationTimezoneField onChange={onChange}/> )
  expect(screen.getByRole('combobox')).toHaveValue('UTC')
  view.rerender(<AutomationTimezoneField value="America/New_York" onChange={onChange}/> )
  expect(screen.getByRole('combobox')).toHaveValue('America/New_York')
  fireEvent.change(screen.getByRole('combobox'),{target:{value:'Asia/Shanghai'}})
  expect(onChange).toHaveBeenCalledWith('Asia/Shanghai')
})

it('preserves a midnight schedule when its frequency changes',()=>{
  const draft:AutomationDraft={name:'夜间整理',cron:'15 0 * * *',timezone:'Asia/Shanghai',prompt:'汇总',providerId:'p',modelId:'m',sessionId:'s',executionMode:'auto-edit',sessionMode:'isolated',runOnce:false,webhookUrl:'',enabled:true}
  const onChange=vi.fn()
  render(<AutomationCreateDialog open draft={draft} onChange={onChange} onClose={vi.fn()} onSubmit={vi.fn()}/> )
  fireEvent.change(screen.getByLabelText('触发频率'),{target:{value:'weekly'}})
  expect(onChange).toHaveBeenCalledWith(expect.objectContaining({cron:'15 0 * * 1',timezone:'Asia/Shanghai'}))
})
