import {useEffect, useState} from 'react'
import type {SkillBridge} from '../bridge/client'
import type {SkillDTO} from '../generated/bridge'
import './skillPackage.css'

function sessionSkillUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

/** Session provenance is supplied by the engine, never inferred from timestamps. */
export function useSessionSkillArtifacts(sessionId:string, bridge:SkillBridge, liveIds:string[]) {
  const [items,setItems]=useState<SkillDTO[]>([]),[error,setError]=useState(''),[revision,setRevision]=useState(0)
  const liveKey=liveIds.join('|')
  useEffect(()=>{
    let active=true
    setItems([]);setError('')
    void (async()=>{
      const result=await bridge.list({sourceSessionId:sessionId})
      const currentIds=new Set(liveKey.split('|').filter(Boolean))
      const known=(result.items??[]).filter(item=>{
        if(currentIds.has(item.id))return true
        try{return JSON.parse(item.manifestJson).originSessionId===sessionId}catch{return false}
      })
      const extra=await Promise.allSettled(liveKey.split('|').filter(id=>id&&!known.some(item=>item.id===id)).map(id=>bridge.get({id})))
      if(!active)return
      setItems([...known,...extra.flatMap(item=>item.status==='fulfilled'?[item.value]:[])])
      if(extra.some(item=>item.status==='rejected'))setError('部分新建技能暂时无法读取，请刷新。')
    })().catch(cause=>{if(active)setError(sessionSkillUserError(cause,'技能产物暂时无法读取'))})
    return()=>{active=false}
  },[sessionId,bridge,liveKey,revision])
  return {items,error,revision,reload:()=>setRevision(value=>value+1)}
}

export function SessionSkillArtifacts({items,error,onReload,onOpen,onTry,onCenter,readOnly=false}:{items:SkillDTO[];error?:string;onReload:()=>void;onOpen:(skill:SkillDTO)=>void;onTry:(skill:SkillDTO)=>void;onCenter?:(skill:SkillDTO)=>void;readOnly?:boolean}) {
  if(!items.length&&!error)return null
  return <section className="skill-created-cards" aria-label="本会话创建的技能">
    {error&&<p role="status">{error} <button type="button" onClick={onReload}>刷新技能产物</button></p>}
    {items.map(skill=><article className="skill-created-card" key={skill.id}>
      <div><strong>{skill.displayName}</strong><small>{skill.status==='draft'?'草稿 · 可在当前对话试用':skill.status==='published'?'已正式发布':skill.status==='disabled'?'已禁用':'已弃用'} · v{skill.version}</small></div>
      <div><button type="button" onClick={()=>onOpen(skill)}>查看目录与文件</button>{(skill.status==='draft'||skill.status==='published')&&<button type="button" disabled={readOnly} onClick={()=>onTry(skill)}>{skill.status==='draft'?'在当前对话试用':'在当前对话使用'}</button>}{onCenter&&<button type="button" onClick={()=>onCenter(skill)}>去技能中心</button>}</div>
    </article>)}
  </section>
}
