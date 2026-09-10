import {useEffect, useMemo, useRef, useState} from 'react'
import {skillBridge, type SkillBridge} from '../bridge/client'
import type {SkillPackageListResult, SkillPackageReadResult} from '../generated/bridge'
import './skillPackage.css'

function skillPackageUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}

type Entry = SkillPackageListResult['entries'][number]
type FileNode = {path:string; name:string; directory:boolean; size:number; children:FileNode[]}

// Parent folders are inferred only from paths returned by the package API.
export function skillPackageTree(entries:Entry[]):FileNode[] {
  const root:FileNode={path:'',name:'',directory:true,size:0,children:[]}
  for(const entry of entries) {
    const parts=entry.path.split('/').filter(Boolean)
    let parent=root
    parts.forEach((name,index)=>{
      const path=parts.slice(0,index+1).join('/'), directory=index<parts.length-1||entry.kind==='directory'
      let node=parent.children.find(item=>item.path===path)
      if(!node){node={path,name,directory,size:entry.size,children:[]};parent.children.push(node)}
      parent=node
    })
  }
  const sort=(nodes:FileNode[]):FileNode[]=>nodes.sort((a,b)=>Number(b.directory)-Number(a.directory)||a.name.localeCompare(b.name)).map(node=>({...node,children:sort(node.children)}))
  return sort(root.children)
}

function PackageNode({node,selected,onSelect}:{node:FileNode;selected:string;onSelect:(path:string)=>void}) {
  return node.directory?<li><details open><summary>{node.name}</summary><ul>{node.children.map(child=><PackageNode key={child.path} node={child} selected={selected} onSelect={onSelect}/>)}</ul></details></li>:<li><button type="button" title={node.path} aria-pressed={selected===node.path} onClick={()=>onSelect(node.path)}><span>{node.name}</span><small>{node.size<1024?`${node.size} B`:`${(node.size/1024).toFixed(1)} KB`}</small></button></li>
}

export function SkillPackagePanel({skillId,bridge=skillBridge,refreshKey=0}:{skillId:string;bridge?:SkillBridge;refreshKey?:number}) {
  const [listing,setListing]=useState<SkillPackageListResult>()
  const [path,setPath]=useState('')
  const [content,setContent]=useState<SkillPackageReadResult>()
  const [offsets,setOffsets]=useState<number[]>([0])
  const [loading,setLoading]=useState(false),[reading,setReading]=useState(false),[error,setError]=useState(''),[readError,setReadError]=useState(''),[reload,setReload]=useState(0)
  const generation=useRef(0),request=useRef(0)
  useEffect(()=>{
    const gen=++generation.current
    setListing(undefined);setPath('');setContent(undefined);setOffsets([0]);setError('');setReadError('');setLoading(true)
    if(!bridge.packageList){setError('当前引擎尚未提供技能包文件读取，请更新引擎后重试。');setLoading(false);return}
    void bridge.packageList({skillId}).then(value=>{if(gen!==generation.current)return;setListing(value);setPath(value.entries.find(item=>item.path==='SKILL.md')?.path??value.entries.find(item=>item.kind==='file')?.path??'')}).catch(cause=>{if(gen===generation.current)setError(skillPackageUserError(cause,'技能目录读取失败'))}).finally(()=>{if(gen===generation.current)setLoading(false)})
    return()=>{generation.current++;request.current++}
  },[skillId,bridge,refreshKey,reload])
  const offset=offsets[offsets.length-1]??0
  useEffect(()=>{
    const seq=++request.current
    setContent(undefined);setReadError('');setReading(false)
    if(!path||!listing||listing.skillId!==skillId)return
    if(!bridge.packageRead){setReadError('当前引擎尚未提供技能文件内容读取。');return}
    setReading(true)
    void bridge.packageRead({skillId,path,offset,limit:16384,expectedRevision:listing.revision}).then(value=>{if(seq===request.current)setContent(value)}).catch(cause=>{if(seq===request.current)setReadError(skillPackageUserError(cause,'技能文件读取失败'))}).finally(()=>{if(seq===request.current)setReading(false)})
    return()=>{request.current++}
  },[skillId,path,offset,listing?.revision,bridge])
  const more=async()=>{
    if(!listing?.nextCursor||!bridge.packageList||loading)return
    const gen=generation.current,snapshot=listing
    setLoading(true);setError('')
    try{
      const page=await bridge.packageList({skillId,cursor:snapshot.nextCursor})
      if(gen!==generation.current)return
      if(page.revision!==snapshot.revision){setError('技能文件已更新，请刷新目录后继续查看。');return}
      setListing({...page,entries:[...snapshot.entries,...page.entries.filter(item=>!snapshot.entries.some(old=>old.path===item.path))]})
    }catch(cause){if(gen===generation.current)setError(skillPackageUserError(cause,'更多文件读取失败'))}
    finally{if(gen===generation.current)setLoading(false)}
  }
  const tree=useMemo(()=>skillPackageTree(listing?.entries??[]),[listing?.entries])
  return <section className="skill-package-panel" aria-label="技能包文件">
    <header><b>目录与文件</b><button type="button" disabled={loading} onClick={()=>setReload(value=>value+1)}>刷新目录</button></header>
    {listing&&<code className="skill-package-root" title={listing.rootPath}>{listing.rootPath}</code>}
    {error&&<p role="alert">{error}</p>}
    {loading&&!listing?<p role="status">正在读取技能目录…</p>:listing&&<>
      <ul className="skill-package-tree" aria-label="当前技能目录">{tree.map(node=><PackageNode key={node.path} node={node} selected={path} onSelect={next=>{setPath(next);setOffsets([0])}}/>)}</ul>
      {!listing.entries.length&&<p className="skill-package-note">当前技能包没有可读取的文件。</p>}
      {listing.nextCursor&&<button type="button" disabled={loading} onClick={()=>void more()}>{loading?'读取中…':'更多文件'}</button>}
    </>}
    {path&&<div className="skill-package-preview"><header><b title={path}>{path}</b></header>
      {reading?<p role="status">正在读取文件…</p>:readError?<p role="alert">{readError}</p>:content?.encoding==='binary'?<p className="skill-package-note">这是二进制文件（{content.size} 字节），无法作为文本展示。</p>:content&&<pre tabIndex={0} aria-label={`文件内容 ${path}`}>{content.content||'（空文件）'}</pre>}
      {(offset>0||content&&!content.eof)&&<nav aria-label="技能文件分页"><button type="button" disabled={reading||offsets.length<2} onClick={()=>setOffsets(values=>values.slice(0,-1))}>上一段</button><span>{offset>0?`从第 ${offset+1} 字节开始`:'文件开头'}{content?` · 共 ${content.size} 字节`:''}</span><button type="button" disabled={reading||!content||content.eof||content.nextOffset<=offset} onClick={()=>content&&setOffsets(values=>[...values,content.nextOffset])}>下一段</button></nav>}
    </div>}
  </section>
}
