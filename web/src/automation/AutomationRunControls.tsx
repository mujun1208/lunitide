import {useEffect, useRef, useState} from 'react'
import type {AutomationBridge} from '../bridge/client'
import type {AutomationRunListResult} from '../generated/bridge'

type Run = AutomationRunListResult['runs'][number]

export function localAutomationTimezone(): string {
  try { return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' }
  catch { return 'UTC' }
}

export function AutomationTimezoneField({value, onChange}: {value?: string; onChange:(zone:string)=>void}) {
  const zones = [...new Set([value || 'UTC', localAutomationTimezone(), 'Asia/Shanghai', 'UTC'])]
  return <label>任务时区<select aria-label="任务时区" value={value || 'UTC'} onChange={event=>onChange(event.target.value)}>
    {zones.map(zone=><option key={zone} value={zone}>{zone === 'UTC' ? 'UTC（世界标准时间）' : zone}</option>)}
  </select></label>
}

export function AutomationStopButton({run, bridge, onRequested}: {run?: Run; bridge:AutomationBridge; onRequested:()=>void|Promise<unknown>}) {
  const busyRef = useRef(false)
  const alive = useRef(true)
  const scope = useRef(0)
  const [busy,setBusy] = useState(false)
  const [notice,setNotice] = useState('')
  useEffect(()=>{
    alive.current=true
    scope.current++
    busyRef.current=false
    setBusy(false)
    setNotice('')
    return()=>{alive.current=false;scope.current++}
  },[bridge,run?.id])
  if(!run || run.state!=='running' || !bridge.cancelRun)return null
  const stop=async()=>{
    if(busyRef.current)return
    const requestScope=scope.current
    busyRef.current=true;setBusy(true);setNotice('')
    try {
      const result=await bridge.cancelRun!({jobId:run.jobId,runId:run.id})
      if(!alive.current||requestScope!==scope.current)return
      setNotice(result.cancellationRequested?'正在停止本次执行，已产生的结果会保留。':'这次执行已经结束，请查看最新记录。')
      await onRequested()
    } catch(error) {
      if(alive.current&&requestScope===scope.current){
        const detail=error instanceof Error?error.message.trim():''
        setNotice(/[\u4e00-\u9fff]/.test(detail)?detail:'停止请求失败，请重试')
      }
    } finally {
      if(alive.current&&requestScope===scope.current){busyRef.current=false;setBusy(false)}
    }
  }
  return <><button type="button" disabled={busy} onClick={()=>void stop()} aria-label={`停止 ${run.jobName}`}>{busy?'正在停止…':'停止本次'}</button>{notice&&<span role="status">{notice}</span>}</>
}
