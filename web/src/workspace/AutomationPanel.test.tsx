import{cleanup,fireEvent,render,screen,waitFor}from'@testing-library/react'
import{afterEach,expect,it,vi}from'vitest'
import type{AutomationBridge}from'../bridge/client'
import type{AutomationJobListResult,AutomationRunListResult,AutomationStatusResult}from'../generated/bridge'
import{AutomationPanel}from'./AutomationPanel'
const P='01ARZ3NDEKTSV4RRFFQ69G5FAV'
afterEach(cleanup)
const job=(over:Partial<AutomationJobListResult['jobs'][number]>={}):AutomationJobListResult['jobs'][number]=>({revision:'a'.repeat(64),id:'01ARZ3NDEKTSV4RRFFQ69G5FAX',name:'日报',cron:'30 8 * * 1-5',prompt:'生成日报',providerId:P,modelId:'gpt-test',sessionId:P,executionMode:'auto-edit',enabled:true,createdAt:'2026-08-16T00:00:00Z',updatedAt:'2026-08-16T00:00:00Z',...over})
const run=(over:Partial<AutomationRunListResult['runs'][number]>={}):AutomationRunListResult['runs'][number]=>({id:'run-1',jobId:'01ARZ3NDEKTSV4RRFFQ69G5FAX',jobName:'日报',state:'succeeded',trigger:'cron',summary:'日报完成',totalTokens:42,startedAt:'2026-08-16T00:30:00Z',...over})
const status=(over:Partial<AutomationStatusResult>={}):AutomationStatusResult=>({running:true,lastHeartbeat:'2026-08-16T00:00:00Z',nextFire:{},runningJobs:[],...over})
type Cfg={jobs?:AutomationJobListResult['jobs'];runs?:AutomationRunListResult['runs'];status?:AutomationStatusResult}
function bridge(cfg:Cfg={}):{b:AutomationBridge;setJob:ReturnType<typeof vi.fn>;deleteJob:ReturnType<typeof vi.fn>;trigger:ReturnType<typeof vi.fn>}{const setJob=vi.fn().mockResolvedValue({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX',createdAt:'2026-08-16T00:00:00Z'}),deleteJob=vi.fn().mockResolvedValue({ok:true}),trigger=vi.fn().mockResolvedValue({triggered:true});const listJobs=vi.fn().mockImplementation(()=>Promise.resolve({jobs:cfg.jobs??[]}))
const b={listJobs,setJob,deleteJob,triggerJob:trigger,listRuns:vi.fn().mockImplementation(()=>Promise.resolve({runs:cfg.runs??[]})),status:vi.fn().mockImplementation(()=>Promise.resolve(cfg.status??status()))}as unknown as AutomationBridge;return{b,setJob,deleteJob,trigger}}
it('shows empty hint and scheduler heartbeat when nothing exists',async()=>{const{b}=bridge();render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
expect(await screen.findByText(/还没有定时任务/)).toBeInTheDocument();expect(screen.getByText('定时调度已开启 · 当前无任务执行')).toBeInTheDocument()})
it('renders jobs with cron, mode, next fire and drives trigger/toggle/delete',async()=>{const{b,trigger,setJob,deleteJob}=bridge({jobs:[job({lastRunAt:'2026-08-16T00:30:00Z'})],status:status({nextFire:{'01ARZ3NDEKTSV4RRFFQ69G5FAX':'2026-08-17T00:30:00Z'},runningJobs:['01ARZ3NDEKTSV4RRFFQ69G5FAX']})});render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
expect(await screen.findByText('日报')).toBeInTheDocument();expect(screen.getByText('30 8 * * 1-5')).toBeInTheDocument();expect(screen.getByText('自动审批')).toBeInTheDocument();expect(screen.getByText('正在执行…')).toBeInTheDocument()
fireEvent.click(screen.getByRole('button',{name:'立即运行'}));await waitFor(()=>expect(trigger).toHaveBeenCalledWith({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX'}));expect(await screen.findByText(/已触发「日报」/)).toBeInTheDocument()
fireEvent.click(screen.getByRole('button',{name:'停用'}));await waitFor(()=>expect(setJob).toHaveBeenCalledWith(expect.objectContaining({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX',enabled:false})))
fireEvent.click(screen.getByRole('button',{name:'删除'}));await waitFor(()=>expect(deleteJob).toHaveBeenCalledWith({id:'01ARZ3NDEKTSV4RRFFQ69G5FAX'}));expect(await screen.findByText('任务已删除')).toBeInTheDocument()})
it('validates the create form then saves through setJob',async()=>{const{b,setJob}=bridge();render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
fireEvent.click(screen.getByRole('button',{name:'新建任务'}))
fireEvent.click(screen.getByRole('button',{name:'保存'}));expect(screen.getByText('请填写任务名称')).toBeInTheDocument();expect(setJob).not.toHaveBeenCalled()
fireEvent.change(screen.getByLabelText('任务名称'),{target:{value:'站会摘要'}});fireEvent.change(screen.getByLabelText('执行提示词'),{target:{value:'汇总昨日待办'}});fireEvent.click(screen.getByRole('button',{name:'保存'}))
await waitFor(()=>expect(setJob).toHaveBeenCalledWith(expect.objectContaining({name:'站会摘要',cron:'30 8 * * *',prompt:'汇总昨日待办',providerId:P,modelId:'gpt-test',sessionId:P,executionMode:'auto-edit',sessionMode:'bound',runOnce:false,enabled:true}),expect.objectContaining({attempt:expect.any(Object)})));expect(await screen.findByText('任务已保存')).toBeInTheDocument()})
it('saves an isolated one-shot 20-minute job',async()=>{const{b,setJob}=bridge();render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
fireEvent.click(screen.getByRole('button',{name:'新建任务'}))
fireEvent.change(screen.getByLabelText('任务名称'),{target:{value:'稍后提醒'}})
fireEvent.change(screen.getByLabelText('执行提示词'),{target:{value:'提醒开会'}})
fireEvent.change(screen.getByLabelText('会话模式'),{target:{value:'isolated'}})
fireEvent.click(screen.getByRole('button',{name:'20 分钟后'}))
fireEvent.click(screen.getByRole('button',{name:'保存'}))
await waitFor(()=>expect(setJob).toHaveBeenCalledWith(expect.objectContaining({name:'稍后提醒',sessionMode:'isolated',runOnce:true}),expect.objectContaining({attempt:expect.any(Object)})))
const payload=setJob.mock.calls[0][0] as {cron:string}
expect(payload.cron.startsWith('at:')).toBe(true)})
it('renders run history with expandable succeeded summary and failed error',async()=>{const{b}=bridge({runs:[run(),run({id:'run-2',state:'failed',trigger:'manual',summary:undefined,error:'无头执行失败 (AUTOMATION_RUN_FAILED)'})]});render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>)
const rows=await screen.findAllByRole('button',{name:/日报|失败|成功/});fireEvent.click(rows[0]);expect(await screen.findByText('日报完成')).toBeInTheDocument()
fireEvent.click(rows[1]);expect(await screen.findByRole('alert')).toHaveTextContent('AUTOMATION_RUN_FAILED');expect(screen.getByText(/手动/)).toBeInTheDocument()})
it('runs mode shows history without the job editor',async()=>{const{b}=bridge({runs:[run()]});render(<AutomationPanel mode="runs" bridge={b}/>)
expect(await screen.findByText('运行中心')).toBeInTheDocument();expect(screen.queryByRole('button',{name:'新建任务'})).toBeNull();expect(screen.getByText('日报')).toBeInTheDocument();expect(screen.getByText('成功')).toBeInTheDocument()})

it('retries a lost create acknowledgement with the same operation key',async()=>{
 const {b,setJob}=bridge();setJob.mockRejectedValueOnce(new Error('连接中断')).mockResolvedValueOnce({id:P,createdAt:'2026-09-06T00:00:00Z',revision:'a'.repeat(64)})
 render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>);
 fireEvent.click(screen.getByRole('button',{name:'新建任务'}));fireEvent.change(screen.getByLabelText('任务名称'),{target:{value:'幂等任务'}});fireEvent.change(screen.getByLabelText('执行提示词'),{target:{value:'生成摘要'}});
 fireEvent.click(screen.getByRole('button',{name:'保存'}));await screen.findByText('连接中断');fireEvent.click(screen.getByRole('button',{name:'保存'}));await screen.findByText('任务已保存');
 expect(setJob).toHaveBeenCalledTimes(2);expect(setJob.mock.calls[0][1].attempt.idempotencyKey).toBe(setJob.mock.calls[1][1].attempt.idempotencyKey)
});
it('ignores old list replies after replacing the active bridge',async()=>{
 const old=bridge();let resolveOld!:(value:AutomationJobListResult)=>void;old.b.listJobs=vi.fn(()=>new Promise<AutomationJobListResult>(resolve=>{resolveOld=resolve}));const current=bridge({jobs:[job({name:'当前任务'})]});
 const ui=render(<AutomationPanel bridge={old.b}/>);ui.rerender(<AutomationPanel bridge={current.b}/>);await screen.findByText('当前任务');resolveOld({jobs:[job({name:'旧任务'})]});await new Promise(resolve=>setTimeout(resolve,0));
 expect(screen.queryByText('旧任务')).toBeNull();expect(screen.getByText('当前任务')).toBeInTheDocument();
});

it.each([undefined,'Asia/Shanghai'])('keeps the existing timezone when editing or toggling (%s)',async(timezone)=>{
 const {b,setJob}=bridge({jobs:[job({timezone})]});render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>);
 await screen.findByText('日报');fireEvent.click(screen.getByRole('button',{name:'停用'}));
 await waitFor(()=>expect(setJob).toHaveBeenCalledWith(expect.objectContaining({enabled:false,timezone:timezone||'UTC'})));
 fireEvent.click(screen.getByRole('button',{name:'编辑'}));
 expect(screen.getByRole('combobox',{name:'任务时区'})).toHaveValue(timezone||'UTC');
 fireEvent.change(screen.getByLabelText('任务名称'),{target:{value:'日报修订'}});fireEvent.click(screen.getByRole('button',{name:'保存'}));
 await waitFor(()=>expect(setJob).toHaveBeenLastCalledWith(expect.objectContaining({name:'日报修订',timezone:timezone||'UTC'}),expect.any(Object)));
});

it('stops the current run from a job card without disabling or rerunning the job',async()=>{
 const active=run({state:'running'});const {b,setJob,trigger}=bridge({jobs:[job()],runs:[active]});
 const cancelRun=vi.fn().mockResolvedValue({cancellationRequested:true});b.cancelRun=cancelRun;
 render(<AutomationPanel sessionId={P} providerId={P} modelId="gpt-test" bridge={b}/>);
 fireEvent.click(await screen.findByRole('button',{name:'停止 日报'}));
 await waitFor(()=>expect(cancelRun).toHaveBeenCalledWith({jobId:active.jobId,runId:active.id}));
 expect(setJob).not.toHaveBeenCalled();expect(trigger).not.toHaveBeenCalled();
});
