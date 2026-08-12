import React,{useEffect,useId,useRef}from'react'

const focusable='button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[href],[tabindex]:not([tabindex="-1"])'

export function Dialog({open,title,description,onClose,children,initialFocus}:{open:boolean;title:string;description?:string;onClose:()=>void;children:React.ReactNode;initialFocus?:React.RefObject<HTMLElement|null>}):React.JSX.Element|null{
 const panel=useRef<HTMLElement>(null),returnFocus=useRef<HTMLElement|null>(null),closeRef=useRef(onClose),initialRef=useRef(initialFocus)
 closeRef.current=onClose;initialRef.current=initialFocus
 const uid=useId(),titleId=`dialog-title-${uid}`,descriptionId=`dialog-description-${uid}`
 useEffect(()=>{if(!open)return;returnFocus.current=document.activeElement as HTMLElement;const frame=requestAnimationFrame(()=>{const preferred=initialRef.current?.current,fallback=panel.current?.querySelector<HTMLElement>(focusable);(preferred&&!preferred.matches(':disabled')?preferred:fallback??panel.current)?.focus()});const key=(e:KeyboardEvent)=>{if(e.key==='Escape'){e.preventDefault();closeRef.current();return}if(e.key!=='Tab'||!panel.current)return;const nodes=[...panel.current.querySelectorAll<HTMLElement>(focusable)],active=document.activeElement;if(!panel.current.contains(active)){e.preventDefault();(e.shiftKey?nodes[nodes.length-1]:nodes[0]??panel.current).focus();return}if(!nodes.length){e.preventDefault();panel.current.focus();return}const first=nodes[0],last=nodes[nodes.length-1];if(e.shiftKey&&active===first){e.preventDefault();last.focus()}else if(!e.shiftKey&&active===last){e.preventDefault();first.focus()}};document.addEventListener('keydown',key);return()=>{cancelAnimationFrame(frame);document.removeEventListener('keydown',key);requestAnimationFrame(()=>returnFocus.current?.focus())}},[open])
 if(!open)return null
 return <div className="modal-backdrop" onMouseDown={()=>closeRef.current()}><section ref={panel} tabIndex={-1} className="moon-dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} aria-describedby={description?descriptionId:undefined} onMouseDown={e=>e.stopPropagation()}><h2 id={titleId}>{title}</h2>{description&&<p id={descriptionId}>{description}</p>}{children}</section></div>
}

export function ConfirmDialog({open,title,description,confirmLabel='确认删除',busy=false,onCancel,onConfirm}:{open:boolean;title:string;description:string;confirmLabel?:string;busy?:boolean;onCancel:()=>void;onConfirm:()=>void}):React.JSX.Element|null{
 const cancel=useRef<HTMLButtonElement>(null)
 return <Dialog open={open} title={title} description={description} onClose={()=>{if(!busy)onCancel()}} initialFocus={cancel}><div className="dialog-actions"><button ref={cancel} disabled={busy} onClick={onCancel}>取消</button><button className="danger" disabled={busy} onClick={onConfirm}>{busy?'处理中…':confirmLabel}</button></div></Dialog>
}
