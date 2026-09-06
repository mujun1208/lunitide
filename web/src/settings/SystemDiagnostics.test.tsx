import {render,screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {it,expect,vi} from 'vitest'
import {SystemDiagnostics} from './SystemDiagnostics'

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
