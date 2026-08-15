import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import type{PlanBridge}from'../bridge/client'
import type{PlanDTO,PlanNodeDTO,PlanRunDTO}from'../generated/bridge'
import{CoordinationPlanPanel}from'./CoordinationPlanPanel'
const P='01ARZ3NDEKTSV4RRFFQ69G5FAA',N='01ARZ3NDEKTSV4RRFFQ69G5FAB',R='01ARZ3NDEKTSV4RRFFQ69G5FAC',C='01ARZ3NDEKTSV4RRFFQ69G5FAD',now='2026-01-01T00:00:00Z'
const plan={id:P,projectId:P,name:'计划',description:'',version:1,status:'draft',createdAt:now,updatedAt:now}as PlanDTO,node={id:N,planId:P,name:'节点',description:'',status:'pending',riskLevel:'low',workerRole:'',sequence:1,createdAt:now,updatedAt:now}as PlanNodeDTO
const root={id:R,planId:P,nodeId:N,role:'planner',todo:{id:R,title:'根项',description:''},status:'queued',depth:0,createdAt:now,updatedAt:now,version:1}as PlanRunDTO,child={...root,id:C,parentRunId:R,todo:{...root.todo,id:C,title:'子项'},depth:1}as PlanRunDTO
afterEach(cleanup)
const api=()=>({list:vi.fn().mockResolvedValue({items:[plan]}),listNodes:vi.fn().mockResolvedValue({items:[node]}),runTree:vi.fn().mockResolvedValue({items:[root,child]}),createTodo:vi.fn().mockResolvedValue({run:root,executionStarted:false}),spawnRun:vi.fn().mockResolvedValue({run:child,executionStarted:false}),startRun:vi.fn().mockResolvedValue({run:root,executionStarted:false}),joinRun:vi.fn().mockResolvedValue({run:root,executionStarted:false}),cancelRun:vi.fn().mockResolvedValue({run:root,executionStarted:false})})
it('shows only the work-plan checklist and leaves orchestration to the system',async()=>{const b=api();render(<CoordinationPlanPanel projectId={P} bridge={b as unknown as PlanBridge}/>);expect(await screen.findByText('0/2 已完成')).toBeInTheDocument();expect(screen.getByText('工作计划清单')).toBeInTheDocument();expect(screen.getByText('子项').closest('li')).toHaveStyle({paddingLeft:'16px'});expect(screen.queryByText('高级管理')).toBeNull();expect(screen.queryByLabelText('协调节点')).toBeNull();expect(b.listNodes).not.toHaveBeenCalled();expect(b.createTodo).not.toHaveBeenCalled();expect(b.startRun).not.toHaveBeenCalled()})
