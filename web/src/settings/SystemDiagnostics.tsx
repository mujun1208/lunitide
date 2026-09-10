import {useRef,useState,useEffect} from 'react'
import {getSystemHealthBridge,type SystemHealthBridge} from '../bridge/client'
import type {SystemDiagnosticsResult} from '../generated/bridge'

const states: Record<SystemDiagnosticsResult['components'][number]['state'],string>={healthy:'检查正常',configured:'已配置',not_configured:'未配置',disabled:'已关闭',degraded:'需要处理'}

function diagnosticsUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

function readinessCodeLabel(code?:string){
  switch(code){
    case 'CAPABILITY_NOT_READY': return '缺模型或配置'
    case 'DEPENDENCY_MISSING': return '缺依赖'
    case 'SCOPE_DENIED': return '无权限'
    case 'STORAGE_UNAVAILABLE': return '存储暂不可用'
    case 'LOCAL_CHECK_FAILED': return '本地检查失败'
    case 'AUDIT_CHAIN_BROKEN': return '审计链异常'
    case 'AUDIT_CHECK_INCOMPLETE': return '审计未完成'
    case 'POLICY_INVALID': return '策略无效'
    case 'POLICY_PENDING': return '策略未应用'
    case 'SCHEDULER_STORAGE_ERROR': return '调度存储异常'
    case 'SCHEDULER_STALE': return '调度心跳超时'
    default: return '需要处理'
  }
}

export function SystemDiagnostics({bridge}:{bridge?:SystemHealthBridge}) {
  const [result,setResult]=useState<SystemDiagnosticsResult>()
  const [error,setError]=useState('')
  const [busy,setBusy]=useState(false)
  const active=useRef(false)
  const epoch=useRef(0)
  useEffect(()=>()=>{epoch.current++},[])
  const check=async()=>{
    if(active.current)return
    active.current=true;setBusy(true);setError('')
    const generation=epoch.current
    try {
      const next=await (bridge??getSystemHealthBridge()).diagnostics()
      if(generation===epoch.current)setResult(next)
    } catch(e) {if(generation===epoch.current)setError(diagnosticsUserError(e,'系统检查失败'))}
    finally {active.current=false;if(generation===epoch.current)setBusy(false)}
  }
  return <section aria-label="系统状态检查">
    <div className="setting-row">
      <div><div className="setting-label">系统状态检查</div><p className="setting-desc">检查本地数据、审计和运行配置。外部连接、麦克风与桌面操作请在对应模块测试。</p></div>
      <button disabled={busy} onClick={()=>void check()}>{busy?'正在检查…':'检查系统状态'}</button>
    </div>
    {error&&<p role="alert">{error}</p>}
    {result&&<>
      <p role="status">检查时间：{new Date(result.checkedAt).toLocaleString()} · 追踪编号：{result.traceId}</p>
      <table><thead><tr><th>模块</th><th>状态</th><th>检查结果</th></tr></thead><tbody>
        {result.components.map(item=><tr key={item.id}><td>{item.name}</td><td>{states[item.state]}</td><td>{item.detail}{item.code&&`（${readinessCodeLabel(item.code)}）`}</td></tr>)}
      </tbody></table>
    </>}
  </section>
}
