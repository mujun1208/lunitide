import {cleanup,render,screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {afterEach,it,expect,vi} from 'vitest'
import {SystemDiagnostics} from './SystemDiagnostics'

afterEach(cleanup)

it('does not show raw English diagnostics failures',async()=>{
  const bridge={health:vi.fn(),diagnostics:vi.fn().mockRejectedValue(new Error('Failed to fetch'))}
  render(<SystemDiagnostics bridge={bridge}/>)
  await userEvent.click(screen.getByRole('button',{name:'检查系统状态'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('系统检查失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('shows configured, unavailable and failed checks separately with trace',async()=>{
  const bridge={health:vi.fn(),diagnostics:vi.fn().mockResolvedValue({state:'degraded',checkedAt:'2026-09-06T08:00:00Z',traceId:'01ARZ3NDEKTSV4RRFFQ69G5FAV',components:[
    {id:'db',name:'数据库',state:'degraded',detail:'连接失败',code:'DB_DOWN',durationMs:10},
    {id:'voice',name:'语音',state:'configured',detail:'资源已配置',durationMs:0},
    {id:'cc',name:'电脑控制',state:'disabled',detail:'用户关闭',durationMs:0},
  ]})}
  render(<SystemDiagnostics bridge={bridge}/>);await userEvent.click(screen.getByText('检查系统状态'))
  expect(await screen.findByText('需要处理')).toBeInTheDocument()
  expect(screen.getByText('已配置')).toBeInTheDocument();expect(screen.getByText('已关闭')).toBeInTheDocument()
  expect(screen.getByRole('status')).toHaveTextContent('01ARZ3NDEKTSV4RRFFQ69G5FAV')
  expect(bridge.diagnostics).toHaveBeenCalledOnce()
})

it('maps HAT-05 readiness codes to Chinese labels instead of raw codes',async()=>{
  const bridge={health:vi.fn(),diagnostics:vi.fn().mockResolvedValue({state:'degraded',checkedAt:'2026-09-06T08:00:00Z',traceId:'01ARZ3NDEKTSV4RRFFQ69G5FAV',components:[
    {id:'text_chat',name:'文字对话',state:'not_configured',detail:'请先配置并启用供应商、凭据和模型',code:'CAPABILITY_NOT_READY',durationMs:1},
    {id:'gui',name:'界面操作',state:'not_configured',detail:'模型供应商服务未装配',code:'DEPENDENCY_MISSING',durationMs:1},
    {id:'cc',name:'电脑控制',state:'disabled',detail:'急停锁存中，恢复后需重新明确启用',code:'SCOPE_DENIED',durationMs:1},
  ]})}
  const view=render(<SystemDiagnostics bridge={bridge}/>)
  await userEvent.click(view.getByRole('button',{name:'检查系统状态'}))
  expect(await view.findByText(/缺模型或配置/)).toBeInTheDocument()
  expect(view.getByText(/缺依赖/)).toBeInTheDocument()
  expect(view.getByText(/无权限/)).toBeInTheDocument()
  expect(view.queryByText(/CAPABILITY_NOT_READY/)).not.toBeInTheDocument()
  expect(view.queryByText(/DEPENDENCY_MISSING/)).not.toBeInTheDocument()
  expect(view.queryByText(/SCOPE_DENIED/)).not.toBeInTheDocument()
})

it('maps STORAGE_UNAVAILABLE to Chinese instead of the raw code',async()=>{
  const bridge={health:vi.fn(),diagnostics:vi.fn().mockResolvedValue({state:'degraded',checkedAt:'2026-09-06T08:00:00Z',traceId:'01ARZ3NDEKTSV4RRFFQ69G5FAV',components:[
    {id:'text_chat',name:'文字对话',state:'degraded',detail:'供应商目录暂时不可用',code:'STORAGE_UNAVAILABLE',durationMs:1},
    {id:'db',name:'数据库',state:'degraded',detail:'本地状态检查失败，请结合本次追踪编号定位',code:'LOCAL_CHECK_FAILED',durationMs:1},
    {id:'audit',name:'权限与操作审计',state:'degraded',detail:'审计链校验失败，发布受阻；请保留数据进行恢复核对',code:'AUDIT_CHAIN_BROKEN',durationMs:1},
    {id:'policy',name:'运行策略',state:'degraded',detail:'策略状态无法解析',code:'POLICY_INVALID',durationMs:1},
  ]})}
  const view=render(<SystemDiagnostics bridge={bridge}/>)
  await userEvent.click(view.getByRole('button',{name:'检查系统状态'}))
  expect(await view.findByText(/存储暂不可用/)).toBeInTheDocument()
  expect(view.getByText(/本地检查失败/)).toBeInTheDocument()
  expect(view.getByText(/审计链异常/)).toBeInTheDocument()
  expect(view.getByText(/策略无效/)).toBeInTheDocument()
  expect(view.queryByText(/STORAGE_UNAVAILABLE/)).not.toBeInTheDocument()
  expect(view.queryByText(/POLICY_INVALID/)).not.toBeInTheDocument()
  expect(view.queryByText(/LOCAL_CHECK_FAILED/)).not.toBeInTheDocument()
  expect(view.queryByText(/AUDIT_CHAIN_BROKEN/)).not.toBeInTheDocument()
})

it('falls back unknown readiness codes to Chinese',async()=>{
  const bridge={health:vi.fn(),diagnostics:vi.fn().mockResolvedValue({state:'degraded',checkedAt:'2026-09-06T08:00:00Z',traceId:'01ARZ3NDEKTSV4RRFFQ69G5FAV',components:[
    {id:'ocr',name:'OCR',state:'degraded',detail:'识别通道异常',code:'OCR_TIMEOUT',durationMs:1},
  ]})}
  const view=render(<SystemDiagnostics bridge={bridge}/>)
  await userEvent.click(view.getByRole('button',{name:'检查系统状态'}))
  expect((await view.findAllByText(/需要处理/)).length).toBeGreaterThan(0)
  expect(view.queryByText(/OCR_TIMEOUT/)).not.toBeInTheDocument()
})
