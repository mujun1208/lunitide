import {SubagentActivityRow} from './SubagentActivityRow'
import {useEffect, useState} from 'react'
import type {MessageBridge} from '../bridge/client'
import type {MessageProcessResult} from '../generated/bridge'
import {ThinkingPanel} from './MarkdownMessage'
import {TOOL_LABELS} from './toolLabels'

/** Load process details only on expansion; transcript paging stays lightweight. */
export function DurableMessageProcess({sessionId,messageId,bridge,onCopy}:{
  sessionId:string; messageId:string; bridge:Pick<MessageBridge,'process'>
  onCopy?:(text:string)=>void|Promise<void>
}) {
  const [open,setOpen]=useState(false)
  const [record,setRecord]=useState<MessageProcessResult>()
  const [error,setError]=useState('')
  const [attempt,setAttempt]=useState(0)
  useEffect(()=>{
    if(!open||record||!bridge.process)return
    let active=true
    setError('')
    void bridge.process({sessionId,messageId}).then(value=>{
      if(active)setRecord(value)
    }).catch(cause=>{
      if(active)setError(cause instanceof Error?cause.message:'过程记录暂时无法读取')
    })
    return()=>{active=false}
  },[open,sessionId,messageId,bridge,attempt,record])
  return <ThinkingPanel text={record?.thinking??''} open={open} onToggle={setOpen}
    status="已结束" onCopy={onCopy}
    skillCount={record?.tools.filter(tool=>tool.name==='skill.invoke').length}
    searchCount={record?.tools.filter(tool=>tool.name==='web.search').length}>
    <>
      {error?<p role="status">{error} <button type="button" onClick={()=>setAttempt(value=>value+1)}>重新读取</button></p>:!record?<p role="status">正在读取过程记录…</p>:null}
      {record?.equipment&&<div className="skill-ref-tags" aria-label="本轮装备">
        {record.equipment.experts.map(name=><span key={`expert:${name}`}>{name}</span>)}
        {record.equipment.skills?.map(name=><span key={`skill:${name}`}>{name}</span>)}
      </div>}
      {!!record?.tools.length&&<ol className="tool-activity-list" aria-label="本轮工具记录">
        {record.tools.map((tool,index)=><li key={`${tool.callId}:${index}`}>
          {tool.name==='subagent.spawn'||tool.name==='subagent.join'?<SubagentActivityRow activity={tool}/>:<>
          <b>{TOOL_LABELS[tool.name]??tool.name}</b>
          <span> · {tool.status==='tool_completed'?'已返回':tool.status==='approval_required'?'当时等待确认':'已开始'}</span>
          {tool.summary&&<pre style={{whiteSpace:'pre-wrap',overflowWrap:'anywhere'}}>{tool.summary}</pre>}
          </>}
        </li>)}
      </ol>}
      {record&&!record.thinking&&!record.tools.length&&!record.equipment&&<p className="notice">这条消息没有保存过程记录。</p>}
      {record?.truncated&&<p className="notice">这轮过程较长，仅保留了部分记录。</p>}
    </>
  </ThinkingPanel>
}
