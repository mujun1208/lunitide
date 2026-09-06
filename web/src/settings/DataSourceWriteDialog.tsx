import React, { useEffect, useRef, useState } from 'react'
import { createMutationAttempt, type DatasourceBridge, type DatasourceWriteOperation, type MutationAttempt } from '../bridge/client'
import { Dialog } from '../ui/Dialog'
import { useZh } from '../i18n/language'

export type DatasourceWriteAPI = Pick<DatasourceBridge, 'writePrepare'|'writeCommit'|'writeGet'|'writeList'>
export function DataSourceWriteDialog({ connectionId, name, api, onClose }: {
  connectionId:string; name:string; api:DatasourceWriteAPI; onClose:()=>void
}):React.JSX.Element {
  const zh = useZh()
  const [sql,setSQL] = useState('')
  const [operation,setOperation] = useState<DatasourceWriteOperation|null>(null)
  const [history,setHistory] = useState<DatasourceWriteOperation[]>([])
  const [error,setError] = useState('')
  const [busy,setBusy] = useState(false)
  const alive = useRef(true)
  const working = useRef(false)
  const attempt = useRef<MutationAttempt<{connectionId:string;sql:string}>|null>(null)
  const apiRef = useRef(api); apiRef.current = api
  useEffect(() => {
    alive.current = true
    void apiRef.current.writeList({connectionId}).then(result => {
      if (alive.current) setHistory(result.items)
    }).catch(e => { if(alive.current) setError(e instanceof Error ? e.message : 'Could not load operation history') })
    return () => { alive.current = false }
  },[connectionId])
  const perform = async (action:()=>Promise<DatasourceWriteOperation>) => {
    if(working.current) return
    working.current = true; setBusy(true); setError('')
    try { const result = await action(); if(alive.current) setOperation(result) }
    catch(e) { if(alive.current) setError(e instanceof Error ? e.message : (zh?'操作失败':'Operation failed')) }
    finally { working.current=false; if(alive.current) setBusy(false) }
  }
  const prepare = () => perform(() => {
    const payload = {connectionId,sql:sql.trim()}
    if(!attempt.current || attempt.current.payload.sql !== payload.sql) attempt.current = createMutationAttempt('datasource.write.prepare',payload)
    return api.writePrepare(payload,{attempt:attempt.current})
  })
  const commit = () => operation && perform(() => api.writeCommit({id:operation.id,digest:operation.digest}))
  const stateLabel = (state:DatasourceWriteOperation['state']) => ({
    prepared:zh?'待确认':'Awaiting confirmation', executing:zh?'执行中，正在等待结果':'Waiting for execution result',
    completed:zh?'写入已完成':'Write completed', unknown:zh?'结果未确认，请核查数据库':'Outcome unknown; inspect the database',
  })[state]
  return <Dialog open title={zh?`数据库写入 · ${name}`:`Database write · ${name}`} onClose={onClose} wide>
    <p>{zh?'仅支持已探测的本机连接。先检查操作内容，再确认执行；远程连接保持只读。':'Only probed local connections support writes. Review the statement before confirming. Remote connections remain read-only.'}</p>
    {!operation && <>
      <label>{zh?'SQL 语句':'SQL statement'}<textarea rows={6} maxLength={16384} value={sql} disabled={busy} onChange={e=>setSQL(e.target.value)} /></label>
      <button disabled={busy||!sql.trim()} onClick={()=>void prepare()}>{zh?'检查写入':'Review write'}</button>
    </>}
    {operation && <>
      <p>{operation.connectionName}</p><pre>{operation.sql}</pre>
      <p role="status">{stateLabel(operation.state)}</p>
      {operation.state==='prepared' && <>
        <p>{zh?'确认将执行上方语句。此确认十分钟内有效。':'Confirm to execute the statement above. The confirmation expires after ten minutes.'}</p>
        <button disabled={busy} onClick={()=>void commit()}>{zh?'确认执行':'Confirm execution'}</button>
        <button disabled={busy} onClick={()=>{setSQL(operation.sql);setOperation(null);attempt.current=null}}>{zh?'返回修改':'Edit statement'}</button>
      </>}
      <button disabled={busy} onClick={()=>void perform(()=>api.writeGet({id:operation.id}))}>{zh?'核查执行结果':'Check execution result'}</button>
      {operation.result && <pre>{JSON.stringify(operation.result.rows,null,2)}</pre>}
    </>}
    {error && <p role="alert">{error}</p>}
    {history.length>0 && <details><summary>{zh?'最近操作记录':'Recent operations'}</summary><ul>
      {history.map(item=><li key={item.id}><button disabled={busy} onClick={()=>void perform(()=>api.writeGet({id:item.id}))}>{item.createdAt} · {stateLabel(item.state)}</button></li>)}
    </ul></details>}
    <button onClick={onClose}>{zh?'关闭':'Close'}</button>
  </Dialog>
}
