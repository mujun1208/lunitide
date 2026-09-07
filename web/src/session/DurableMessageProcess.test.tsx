import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {DurableMessageProcess} from './DurableMessageProcess'
import type {MessageProcessResult} from '../generated/bridge'
import {subagentBridge} from '../bridge/client'

afterEach(()=>{cleanup();vi.restoreAllMocks()})
const sessionId='01ARZ3NDEKTSV4RRFFQ69G5FAV',messageId='01ARZ3NDEKTSV4RRFFQ69G5FAA'
const result:MessageProcessResult={messageId,thinking:'先核对输入。\n\n再检查文件是否存在。',equipment:{experts:['AI 工程师'],skills:['文件检查']},tools:[{callId:'read-1',name:'workspace.read',status:'tool_completed',summary:'读取完成'}],truncated:false}

it('loads only after expanding and preserves the multi-paragraph process across toggles',async()=>{
  const process=vi.fn().mockResolvedValue(result)
  const {container}=render(<DurableMessageProcess sessionId={sessionId} messageId={messageId} bridge={{process}}/>)
  expect(process).not.toHaveBeenCalled()
  fireEvent.click(screen.getByText('任务过程'))
  await screen.findByText('再检查文件是否存在。')
  expect(process).toHaveBeenCalledWith({sessionId,messageId})
  expect(screen.getByText('读取完成')).toBeInTheDocument()
  expect(screen.getByText('文件检查')).toBeInTheDocument()
  fireEvent.click(container.querySelector('summary')!)
  fireEvent.click(container.querySelector('summary')!)
  await waitFor(()=>expect(process).toHaveBeenCalledTimes(1))
})

it('offers a read retry without executing historical approvals',async()=>{
  const process=vi.fn().mockRejectedValueOnce(new Error('连接正在恢复')).mockResolvedValue({...result,tools:[{callId:'ask-1',name:'user.ask',status:'approval_required',summary:'选一个方案'}]})
  render(<DurableMessageProcess sessionId={sessionId} messageId={messageId} bridge={{process}}/>)
  fireEvent.click(screen.getByText('任务过程'))
  fireEvent.click(await screen.findByRole('button',{name:'重新读取'}))
  await screen.findByText('选一个方案')
  expect(screen.queryByRole('button',{name:/批准|确认|提交/})).not.toBeInTheDocument()
  expect(process).toHaveBeenCalledTimes(2)
})

it('reopens saved subagent results with the persisted run id',async()=>{
  const subagentId='01ARZ3NDEKTSV4RRFFQ69G5FAB'
  const process=vi.fn().mockResolvedValue({...result,tools:[{callId:'spawn-one',name:'subagent.spawn',status:'tool_completed',summary:JSON.stringify({marker:'subagent_progress',id:subagentId,purpose:'历史来源核对',status:'completed',detail:'已结束'})}]})
  const join=vi.spyOn(subagentBridge,'join').mockResolvedValue({status:'completed',summary:'历史完整报告'.repeat(100),digests:[],spentTokens:20,truncated:false})
  const view=render(<DurableMessageProcess sessionId={sessionId} messageId={messageId} bridge={{process}}/>)
  fireEvent.click(screen.getByText('任务过程'))
  await screen.findByText('历史来源核对')
  view.unmount()
  render(<DurableMessageProcess sessionId={sessionId} messageId={messageId} bridge={{process}}/>)
  fireEvent.click(screen.getByText('任务过程'))
  fireEvent.click(await screen.findByText('历史来源核对'))
  await waitFor(()=>expect(join).toHaveBeenCalledWith({subagentId,waitMs:1000,maxSummaryBytes:8192}))
  await screen.findByText('历史完整报告'.repeat(100))
})
