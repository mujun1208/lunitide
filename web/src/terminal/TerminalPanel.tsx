import React,{useEffect,useRef,useState}from'react'
import{Terminal}from'@xterm/xterm'
import{FitAddon}from'@xterm/addon-fit'
import'@xterm/xterm/css/xterm.css'
import{getTerminalBridge,type TerminalBridge,type TerminalSession}from'../bridge/client'

type ToolActivity={callId:string;name:string;status:string;summary?:string}
const commandLine=(activity:ToolActivity)=>{
 const summary=(activity.summary??'').trim()
 if(summary.startsWith('$ '))return summary
 if(summary)return `$ ${summary}`
 return `$ ${activity.name}`
}
export function TerminalPanel({projectId,sessionId,bridge,toolActivities=[],executionMode='auto-edit'}:{projectId:string;sessionId:string;bridge?:TerminalBridge;toolActivities?:ToolActivity[];executionMode?:'approval'|'auto-edit'|'full-access'}){
 const host=useRef<HTMLDivElement>(null),active=useRef<TerminalSession|undefined>(undefined),term=useRef<Terminal|undefined>(undefined),replay=useRef(new Map<string,{len:number;done:boolean}>()),interactive=useRef(false),[status,setStatus]=useState<'idle'|'starting'|'running'|'exited'|'error'>('idle'),[error,setError]=useState('')
 const disposeTerm=()=>{term.current?.dispose();term.current=undefined;if(host.current)host.current.textContent='';replay.current.clear()}
 useEffect(()=>()=>{active.current?.dispose();disposeTerm();active.current=undefined},[projectId,sessionId])
 const attach=(opts:{cursorBlink:boolean;disableStdin:boolean})=>{
  if(!host.current||term.current)return
  const terminal=new Terminal({cursorBlink:opts.cursorBlink,disableStdin:opts.disableStdin,convertEol:true,theme:{background:'#000000',foreground:'#cccccc'}})
  const fit=new FitAddon()
  term.current=terminal
  terminal.loadAddon(fit)
  terminal.open(host.current)
  fit.fit()
  const observer=typeof ResizeObserver==='undefined'?undefined:new ResizeObserver(()=>fit.fit())
  if(observer)observer.observe(host.current)
  const prevDispose=terminal.dispose.bind(terminal)
  terminal.dispose=()=>{observer?.disconnect();prevDispose()}
  return terminal
 }
 const start=async()=>{
  if(active.current||!host.current)return
  setStatus('starting');setError('');interactive.current=true;disposeTerm()
  const terminal=attach({cursorBlink:true,disableStdin:false})
  if(!terminal){setStatus('error');setError('终端启动失败');return}
  try{
   const api=bridge??getTerminalBridge()
   const session=await api.start({projectId,sessionId,cols:terminal.cols,rows:terminal.rows},event=>{
    if(event.type==='output')terminal.write(event.data)
    else{terminal.writeln(`\r\n[进程已退出：${event.exitCode}]`);active.current=undefined;setStatus('exited')}
   })
   active.current=session
   terminal.onData(data=>void session.input(data))
   const resize=()=>{const addonHost=host.current;if(!addonHost)return;void session.resize(terminal.cols,terminal.rows)}
   window.addEventListener('resize',resize)
   setStatus('running')
  }catch(e){
   terminal.dispose();term.current=undefined;interactive.current=false;setStatus('error');setError(e instanceof Error?e.message:'终端启动失败')
  }
 }
 useEffect(()=>{
  if(interactive.current||status==='starting'||status==='running')return
  if(!host.current||toolActivities.length===0)return
  if(!term.current)attach({cursorBlink:false,disableStdin:true})
  const terminal=term.current
  if(!terminal)return
  for(const activity of toolActivities){
   let rec=replay.current.get(activity.callId)
   if(!rec){
    terminal.writeln(`\x1b[36m${commandLine(activity)}\x1b[0m`)
    rec={len:0,done:false}
    replay.current.set(activity.callId,rec)
   }
   const summary=activity.summary??''
   const output=summary.startsWith('$ ')? '':summary
   if(output.length>rec.len){
    terminal.write(output.slice(rec.len).replace(/\n/g,'\r\n'))
    rec.len=output.length
   }
   if(!rec.done&&(activity.status==='tool_completed'||activity.status==='failed'||activity.status==='rejected')){
    terminal.writeln(`\r\n\x1b[32m[${activity.status==='tool_completed'?'已完成':'失败'}]\x1b[0m`)
    rec.done=true
   }
  }
 },[toolActivities,status,projectId,sessionId])
 const close=async()=>{const session=active.current;active.current=undefined;interactive.current=false;if(session)await session.close().catch(()=>{});disposeTerm();setStatus('idle')}
 const waiting=status==='idle'&&toolActivities.length===0
 return <section className="terminal-panel" aria-label="终端面板"><div className="terminal-tabstrip" role="tablist" aria-label="终端会话"><div className="terminal-tab" role="tab" aria-selected="true"><span aria-hidden="true">›_</span><b>PowerShell</b></div><button type="button" aria-label="新建终端" disabled>＋</button><span className="terminal-sandbox-badge">{executionMode==='full-access'?'完全访问':executionMode==='approval'?'手动审批':'自动审批'}</span></div><div className="terminal-toolbar"><div className="terminal-actions"><button type="button" disabled={status==='starting'||status==='running'} onClick={()=>void start()}>启动交互终端</button><button type="button" disabled={status!=='running'} onClick={()=>void close()}>关闭终端</button></div><span role="status" data-status={status}><i aria-hidden="true"/>{status==='running'?'运行中':status==='starting'?'启动中…':status==='exited'?'已退出':toolActivities.length?'只读回放':'未启动'}</span></div>{toolActivities.length>0&&<ol className="terminal-activities" aria-label="命令调用情况">{toolActivities.map(activity=><li key={activity.callId}><b>{activity.name}</b><span>{activity.status}</span>{activity.summary&&<code>{activity.summary}</code>}</li>)}</ol>}{error&&<p role="alert">{error}</p>}<div className="terminal-viewport"><div className="terminal-watermark" aria-hidden="true">LUNITIDE TERMINAL</div><div className="terminal-host" ref={host}/>{waiting&&<div className="terminal-empty"><span aria-hidden="true">›_</span><b>等待命令执行</b><p>助手通过 command.run 执行的命令会逐步显示在这里（只读）。需要自己操作时再启动交互终端，不会提升权限。</p></div>}</div></section>
}
