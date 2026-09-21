import React,{useCallback,useRef,useState}from'react'
import{ConfirmDialog,Dialog}from'./Dialog'

/** The desktop shell runs with AreDefaultScriptDialogsEnabled=FALSE, so
 *  window.confirm returns false and window.prompt returns null without ever
 *  drawing anything: every action gated on them looked like a dead button.
 *  These hooks are the in-app replacement. They return a node to render plus an
 *  awaitable asker, so a page keeps its own dialog and needs no provider —
 *  standalone renders (and tests) behave exactly like the shipped app. */

type Ask={title:string;description:string;confirmLabel?:string;danger?:boolean}

export function useConfirmDialog():[React.JSX.Element|null,(ask:Ask)=>Promise<boolean>]{
 const[ask,setAsk]=useState<Ask|null>(null)
 const settle=useRef<((ok:boolean)=>void)|null>(null)
 const close=useCallback((ok:boolean)=>{const done=settle.current;settle.current=null;setAsk(null);done?.(ok)},[])
 const confirm=useCallback((next:Ask)=>{
  // A second ask supersedes the first; never leave the earlier await hanging.
  settle.current?.(false)
  return new Promise<boolean>(done=>{settle.current=done;setAsk(next)})
 },[])
 const node=ask?<ConfirmDialog open title={ask.title} description={ask.description} confirmLabel={ask.confirmLabel} danger={ask.danger??true} onCancel={()=>close(false)} onConfirm={()=>close(true)}/>:null
 return[node,confirm]
}

type AskText={title:string;description?:string;label:string;placeholder?:string;multiline?:boolean;confirmLabel?:string;initial?:string}

export function usePromptDialog():[React.JSX.Element|null,(ask:AskText)=>Promise<string|null>]{
 const[ask,setAsk]=useState<AskText|null>(null)
 const[value,setValue]=useState('')
 const settle=useRef<((text:string|null)=>void)|null>(null)
 const close=useCallback((text:string|null)=>{const done=settle.current;settle.current=null;setAsk(null);setValue('');done?.(text)},[])
 const prompt=useCallback((next:AskText)=>{
  settle.current?.(null)
  return new Promise<string|null>(done=>{settle.current=done;setValue(next.initial??'');setAsk(next)})
 },[])
 const node=ask?<Dialog open title={ask.title} description={ask.description} onClose={()=>close(null)}>
  <form className="editor-dialog" onSubmit={e=>{e.preventDefault();close(value)}}>
   <label className="wide">{ask.label}{ask.multiline?<textarea rows={6} value={value} placeholder={ask.placeholder} onChange={e=>setValue(e.target.value)}/>:<input value={value} placeholder={ask.placeholder} onChange={e=>setValue(e.target.value)}/>}</label>
   <div className="dialog-actions"><button type="button" onClick={()=>close(null)}>取消</button><button type="submit" className="primary">{ask.confirmLabel??'确定'}</button></div>
  </form>
 </Dialog>:null
 return[node,prompt]
}
