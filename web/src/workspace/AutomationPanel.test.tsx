import{cleanup,fireEvent,render,screen,waitFor}from'@testing-library/react'
import{afterEach,expect,it,vi}from'vitest'
import type{AutomationBridge}from'../bridge/client'
import type{AutomationJobListResult,AutomationRunListResult,AutomationStatusResult}from'../generated/bridge'
import{AutomationPanel}from'./AutomationPanel'
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV'
afterEach(cleanup)
const job=(over:Partial<AutomationJobListResult['jobs'][number]>={}):AutomationJobListResult['jobs'][number]=>({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX',name:'日报',cron:'30 8 * * 1-5',prompt:'生成日报',providerId:P,modelId:'gpt-test',sessionId:P,executionMode:'auto-edit',enabled:true,createdAt:'2026-08-16T00:00:00Z',updatedAt:'2026-08-16T00:00:00Z',...over})
const run=(over:Partial<AutomationRunListResult['runs'][number]>={}):AutomationRunListResult['runs'][number]=>({id:'run-1',jobId:'01ARZ3NDEKTSV4RRFFQ69G5FAX',jobName:'日报',state:'succeeded',trigger:'cron',summary:'日报完成',totalTokens:42,startedAt:'2026-08-16T00:30:00Z',...over})
const status=(over:Partial<AutomationStatusResult>={}):AutomationStatusResult=>({running:true,lastHeartbeat:'2026-08-16T00:00:00Z',nextFire:{},runningJobs:[],...over})
type Cfg={jobs?:AutomationJobListResult['jobs'];runs?:AutomationRunListResult['runs'];status?:AutomationStatusResult}
function bridge(cfg:Cfg={}):{b:AutomationBridge;setJob:ReturnType<typeof vi.fn>;deleteJob:ReturnType<typeof vi.fn>;trigger:ReturnType<typeof vi.fn>}{const setJob=vi.fn().mockResolvedValue({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX',createdAt:'2026-08-16T00:00:00Z'}),deleteJob=vi.fn().mockResolvedValue({ok:true}),trigger=vi.fn().mockResolvedValue({triggered:true});const listJobs=vi.fn().mockImplementation(()=>Promise.resolve({jobs:cfg.jobs??[]}))
const b={listJobs,setJob,deleteJob,triggerJob:trigger,listRuns:vi.fn().mockImplementation(()=>Promise.resolve({runs:cfg.runs??[]})),status:vi.fn().mockImplementation(()=>Promise.resolve(cfg.status??status()))}as unknown as AutomationBridge;return{b,setJob,deleteJob,trigger}}
it('shows empty hint and scheduler heartbeat when nothing exists',async()=>{const{b}=bridge();render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
expect(await screen.findByText(/还没有定时任务/)).toBeInTheDocument();expect(screen.getByText('调度器运行中')).toBeInTheDocument()})
it('renders jobs with cron, mode, next fire and drives trigger/toggle/delete',async()=>{const{b,trigger,setJob,deleteJob}=bridge({jobs:[job({lastRunAt:'2026-08-16T00:30:00Z'})],status:status({nextFire:{'01ARZ3NDEKTSV4RRFFQ69G5FAX':'2026-08-17T00:30:00Z'},runningJobs:['01ARZ3NDEKTSV4RRFFQ69G5FAX']})});render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
expect(await screen.findByText('日报')).toBeInTheDocument();expect(screen.getByText('30 8 * * 1-5')).toBeInTheDocument();expect(screen.getByText('自动编辑')).toBeInTheDocument();expect(screen.getByText('正在执行…')).toBeInTheDocument()
fireEvent.click(screen.getByRole('button',{name:'立即运行'}));await waitFor(()=>expect(trigger).toHaveBeenCalledWith({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX'}));expect(await screen.findByText(/已触发「日报」/)).toBeInTheDocument()
fireEvent.click(screen.getByRole('button',{name:'停用'}));await waitFor(()=>expect(setJob).toHaveBeenCalledWith(expect.objectContaining({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX',enabled:false})))
fireEvent.click(screen.getByRole('button',{name:'删除'}));await waitFor(()=>expect(deleteJob).toHaveBeenCalledWith({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX'}));expect(await screen.findByText('任务已删除')).toBeInTheDocument()})
it('validates the create form then saves through setJob',async()=>{const{b,setJob}=bridge();render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
fireEvent.click(screen.getByRole('button',{name:'新建任务'}))
fireEvent.click(screen.getByRole('button',{name:'保存'}));expect(screen.getByText('请填写任务名称')).toBeInTheDocument();expect(setJob).not.toHaveBeenCalled()
fireEvent.change(screen.getByLabelText('任务名称'),{target:{value:'站会摘要'}});fireEvent.change(screen.getByLabelText('执行提示词'),{target:{value:'汇总昨日待办'}});fireEvent.click(screen.getByRole('button',{name:'保存'}))
await waitFor(()=>expect(setJob).toHaveBeenCalledWith(expect.objectContaining({name:'站会摘要',cron:'30 8 * * *',prompt:'汇总昨日待办',providerId:P,modelId:'gpt-test',sessionId:P,executionMode:'auto-edit',enabled:true})));expect(await screen.findByText('任务已保存')).toBeInTheDocument()})
it('renders run history with expandable succeeded summary and failed error',async()=>{const{b}=bridge({runs:[run(),run({id:'run-2',state:'failed',trigger:'manual',summary:undefined,error:'无头执行失败 (AUTOMATION_RUN_FAILED)'})]});render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
const rows=await screen.findAllByRole('button',{name:/日报|失败|成功/});fireEvent.click(rows[0]);expect(await screen.findByText('日报完成')).toBeInTheDocument()
fireEvent.click(rows[1]);expect(await screen.findByRole('alert')).toHaveTextContent('AUTOMATION_RUN_FAILED');expect(screen.getByText(/手动/)).toBeInTheDocument()})
