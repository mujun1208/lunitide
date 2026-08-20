import React,{useEffect,useMemo,useState}from'react'
import{BridgeClientError,registryBridge as defaultRegistryBridge,type RegistryBridge}from'../bridge/client'
import type{DbQueryResult,OpenapiParseResult,ProjectDTO}from'../generated/bridge'

type EndpointRow={method:string;path:string;operationId:string}
type Tab='openapi'|'db'

const problem=(e:unknown)=>e instanceof BridgeClientError?e:new BridgeClientError(e instanceof Error?e.message:'请求失败','CLIENT_ERROR',false,'renderer')

const sampleOpenAPISpec=`{"openapi":"3.0.3","info":{"title":"Sample API","version":"1.0.0","description":"Paste or edit your OpenAPI document here."},"paths":{"/health":{"get":{"operationId":"getHealth","summary":"Health check","responses":{"200":{"description":"ok"}}}},"/items":{"get":{"operationId":"listItems","responses":{"200":{"description":"ok"}}},"post":{"operationId":"createItem","responses":{"201":{"description":"created"}}}}}}`

function extractEndpoints(spec:string):EndpointRow[]{
 try{
  const doc=JSON.parse(spec) as{paths?:Record<string,Record<string,{operationId?:string}>>}
  const paths=doc.paths??{},methods=['get','post','put','patch','delete','head','options'],out:EndpointRow[]=[]
  for(const[path,item]of Object.entries(paths)){if(!item||typeof item!=='object')continue;for(const m of methods){const op=item[m];if(op)out.push({method:m.toUpperCase(),path,operationId:op.operationId??''})}}
  return out.sort((a,b)=>a.path.localeCompare(b.path)||a.method.localeCompare(b.method))
 }catch{return[]}
}

export function RegistryPanel({project,phase,bridge=defaultRegistryBridge,readOnly=false}:{project:ProjectDTO;phase:number;bridge?:RegistryBridge;readOnly?:boolean}):React.JSX.Element{
 const defaultTab:Tab=phase===4?'openapi':'db'
 const[tab,setTab]=useState<Tab>(defaultTab)
 const[spec,setSpec]=useState(sampleOpenAPISpec)
 const[parseResult,setParseResult]=useState<OpenapiParseResult|null>(null)
 const[endpoints,setEndpoints]=useState<EndpointRow[]>([])
 const[sql,setSql]=useState("SELECT name, type FROM sqlite_master WHERE type IN ('table','view') ORDER BY name")
 const[dbResult,setDbResult]=useState<DbQueryResult|null>(null)
 const[busy,setBusy]=useState(false)
 const[error,setError]=useState('')
 const runId=useMemo(()=>`registry-${project.id}`,[project.id])
 useEffect(()=>{setTab(phase===4?'openapi':'db')},[phase])

 const parseOpenAPI=async()=>{if(readOnly||busy||spec.trim().length<100)return;setBusy(true);setError('');setParseResult(null);setEndpoints([]);try{const result=await bridge.parseOpenAPI({spec,name:project.projectCode});setParseResult(result);setEndpoints(extractEndpoints(spec))}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const queryDb=async()=>{if(readOnly||busy||!sql.trim())return;setBusy(true);setError('');setDbResult(null);try{const result=await bridge.queryDb({runId,sql:sql.trim(),maxRows:200,timeoutMs:8000});setDbResult(result)}catch(e){setError(problem(e).message)}finally{setBusy(false)}}

 const rows=dbResult?.rows??[]
 const columns=dbResult?.columns??[]

 return <aside className="pm-registry-panel" aria-label="OpenAPI 与数据库注册表"><header className="pm-deliverable-head"><div><b>{phase===3?'数据库注册表':phase===4?'OpenAPI 注册表':'集成注册表'}</b><small>{phase===3?'阶段 3 · 数据库':phase===4?'阶段 4 · 接口':'阶段 '+phase}</small></div></header><div className="registry-tabs" role="tablist" aria-label="注册表类型"><button type="button" role="tab" aria-selected={tab==='db'} className={tab==='db'?'on':''} onClick={()=>setTab('db')}>数据库</button><button type="button" role="tab" aria-selected={tab==='openapi'} className={tab==='openapi'?'on':''} onClick={()=>setTab('openapi')}>OpenAPI</button></div><div className="registry-body">{error&&<p className="error" role="alert"><b>{error}</b></p>}{tab==='openapi'?<div className="registry-pane"><label>OpenAPI 规范<textarea className="registry-spec" value={spec} onChange={e=>setSpec(e.target.value)} rows={10} disabled={readOnly||busy} spellCheck={false}/></label><button className="primary" disabled={readOnly||busy||spec.trim().length<100} onClick={()=>void parseOpenAPI()}>{busy?'解析中…':'解析规范'}</button>{parseResult&&<div className="registry-meta" role="status"><span>title: {parseResult.title??'—'}</span><span>operations: {parseResult.operationCount}</span><span>digest: <code>{parseResult.digest.slice(0,12)}…</code></span></div>}{endpoints.length>0&&<div className="registry-table-wrap"><table className="registry-table"><thead><tr><th>Method</th><th>Path</th><th>Operation ID</th></tr></thead><tbody>{endpoints.map(row=><tr key={`${row.method}:${row.path}`}><td>{row.method}</td><td><code>{row.path}</code></td><td>{row.operationId||'—'}</td></tr>)}</tbody></table></div>}</div>:<div className="registry-pane"><label>SQL 查询<textarea className="registry-spec" value={sql} onChange={e=>setSql(e.target.value)} rows={5} disabled={readOnly||busy} spellCheck={false}/></label><button className="primary" disabled={readOnly||busy||!sql.trim()} onClick={()=>void queryDb()}>{busy?'查询中…':'执行查询'}</button>{dbResult&&<div className="registry-meta" role="status"><span>rows: {dbResult.rowCount}{dbResult.truncated?' (truncated)':''}</span><span>digest: <code>{dbResult.resultDigest.slice(0,12)}…</code></span></div>}{columns.length>0&&<div className="registry-table-wrap"><table className="registry-table"><thead><tr>{columns.map(c=><th key={c}>{c}</th>)}</tr></thead><tbody>{rows.map((row,i)=><tr key={i}>{columns.map((_,j)=><td key={j}>{formatCell((row as unknown[])?.[j])}</td>)}</tr>)}</tbody></table></div>}</div>}</div></aside>
}

function formatCell(v:unknown):string{if(v==null)return '—';if(typeof v==='object')return JSON.stringify(v);return String(v)}
