import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {SubagentActivityRow,parseSubagentProgress} from './SubagentActivityRow'

afterEach(cleanup)
const id='01ARZ3NDEKTSV4RRFFQ69G5FAV'
const progress=(patch:Record<string,unknown>={})=>JSON.stringify({marker:'subagent_progress',id,status:'running',profile:'Research',purpose:'核对两个独立资料来源',stage:'searching',tool:'web.search',detail:'正在执行',...patch})

it('updates real progress in one row and opens the durable report with its real id',async()=>{
 const join=vi.fn().mockResolvedValue({status:'completed',summary:'完整结果\n'.repeat(180),spentTokens:20,truncated:false,digests:[]})
 const bridge={join}
 const {container,rerender}=render(<SubagentActivityRow activity={{callId:'c1',name:'subagent.spawn',status:'tool_output',summary:progress()}} bridge={bridge}/> )
 expect(screen.getByText('核对两个独立资料来源')).toBeInTheDocument()
 expect(join).not.toHaveBeenCalled()
 expect(screen.getByRole('status')).not.toHaveTextContent('已完成')
 rerender(<SubagentActivityRow activity={{callId:'c1',name:'subagent.spawn',status:'tool_completed',summary:JSON.stringify({subagentId:id,status:'completed',profile:'research',summary:'初步结果'})}} bridge={bridge}/> )
 expect(container.querySelectorAll('details')).toHaveLength(1)
 expect(screen.getByText('核对两个独立资料来源')).toBeInTheDocument()
 expect(screen.getByRole('status')).toHaveTextContent('已完成')
 fireEvent.click(container.querySelector('summary')!)
 await waitFor(()=>expect(join).toHaveBeenCalledWith({subagentId:id,waitMs:1000,maxSummaryBytes:8192}))
 await waitFor(()=>expect(container.querySelector('pre')?.textContent).toBe('完整结果\n'.repeat(180)))
})

it('keeps independent agents separate and displays a failed task without a success fetch',async()=>{
 const join=vi.fn()
 const {container}=render(<><SubagentActivityRow activity={{callId:'a',name:'subagent.spawn',status:'tool_output',summary:progress({purpose:'来源一'})}} bridge={{join}}/><SubagentActivityRow activity={{callId:'b',name:'subagent.spawn',status:'tool_completed',summary:progress({id:'01ARZ3NDEKTSV4RRFFQ69G5FAA',purpose:'来源二',status:'failed',stage:'failed',detail:'资料服务暂时不可用'})}} bridge={{join}}/></>)
 expect(container.querySelectorAll('details')).toHaveLength(2)
 fireEvent.click(screen.getByText('来源二'))
 expect(screen.getByText('资料服务暂时不可用')).toBeInTheDocument()
 expect(join).not.toHaveBeenCalled()
 expect(container.querySelector('.is-failed')).toBeInTheDocument()
 expect(container.querySelector('.is-running')).toBeInTheDocument()
})

it('does not show raw English join result failures',async()=>{
  const join=vi.fn().mockRejectedValue(new Error('Failed to fetch'))
  const {container}=render(<SubagentActivityRow activity={{callId:'a',name:'subagent.spawn',status:'tool_completed',summary:progress({status:'completed'})}} bridge={{join}}/> )
  fireEvent.click(container.querySelector('summary')!)
  expect(await screen.findByRole('alert')).toHaveTextContent('读取结果失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('retries reading the saved result without restarting the agent',async()=>{
 const join=vi.fn().mockRejectedValueOnce(new Error('连接恢复中')).mockResolvedValue({status:'completed',summary:'结果已保存',spentTokens:10,digests:[],truncated:false})
 const {container}=render(<SubagentActivityRow activity={{callId:'a',name:'subagent.spawn',status:'tool_completed',summary:progress({status:'completed'})}} bridge={{join}}/> )
 fireEvent.click(container.querySelector('summary')!)
 fireEvent.click(await screen.findByRole('button',{name:'重试'}))
 await screen.findByText('结果已保存')
 expect(join).toHaveBeenCalledTimes(2)
})

it('accepts whole long terminal JSON and rejects fabricated or malformed identifiers',()=>{
 expect(parseSubagentProgress(JSON.stringify({subagentId:id,status:'completed',summary:'完整长报告'.repeat(400)}))?.id).toBe(id)
 for(const value of ['{broken',JSON.stringify({id:'fake',status:'completed'}),JSON.stringify({id,status:'invented'}),'null'])expect(parseSubagentProgress(value)).toBeUndefined()
})
