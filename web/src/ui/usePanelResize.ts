import{useCallback,useEffect,useRef,useState,type PointerEvent as ReactPointerEvent}from'react'

type ResizeOptions={storageKey:string;initial:number;min:number;max:()=>number;reverse?:boolean;axis?:'x'|'y'}

export function usePanelResize({storageKey,initial,min,max,reverse=false,axis='x'}:ResizeOptions):[number,(event:ReactPointerEvent<HTMLDivElement>)=>void,(value:number)=>void]{
 const maxRef=useRef(max);maxRef.current=max
 const clamp=useCallback((value:number)=>Math.round(Math.min(Math.max(min,value),Math.max(min,maxRef.current()))),[min]),cleanupRef=useRef<()=>void>(()=>{})
 const[size,setSize]=useState(()=>{let saved=Number.NaN;try{const raw=localStorage.getItem(storageKey);if(raw!==null)saved=Number(raw)}catch{/* Optional view preference. */}return Math.round(Math.min(Math.max(min,Number.isFinite(saved)?saved:initial),Math.max(min,max())))})
 const persist=useCallback((value:number)=>{try{localStorage.setItem(storageKey,String(value))}catch{/* Optional view preference. */}},[storageKey])
 const resizeTo=useCallback((value:number)=>{const next=clamp(value);setSize(next);persist(next)},[clamp,persist])
 useEffect(()=>{const resize=()=>setSize(value=>clamp(value));window.addEventListener('resize',resize);return()=>{window.removeEventListener('resize',resize);cleanupRef.current()}},[clamp])
 const start=useCallback((event:ReactPointerEvent<HTMLDivElement>)=>{
  if(event.button!==0)return
  cleanupRef.current()
  event.preventDefault()
  const handle=event.currentTarget,pointerId=event.pointerId
  try{handle.setPointerCapture(pointerId)}catch{/* Synthetic events may not support capture. */}
  const origin=axis==='y'?event.clientY:event.clientX,originSize=size,body=document.body,previousCursor=body.style.cursor,previousSelection=body.style.userSelect
  body.style.cursor=axis==='y'?'row-resize':'col-resize';body.style.userSelect='none'
  const move=(next:PointerEvent)=>{const point=axis==='y'?next.clientY:next.clientX;const delta=(point-origin)*(reverse?-1:1);setSize(clamp(originSize+delta))}
  const stop=()=>{body.style.cursor=previousCursor;body.style.userSelect=previousSelection;window.removeEventListener('pointermove',move);window.removeEventListener('pointerup',stop);window.removeEventListener('pointercancel',stop);window.removeEventListener('blur',stop);handle.removeEventListener('lostpointercapture',stop);try{if(handle.hasPointerCapture(pointerId))handle.releasePointerCapture(pointerId)}catch{/* The handle may already be detached. */}setSize(value=>{persist(value);return value});cleanupRef.current=()=>{}}
  cleanupRef.current=stop;window.addEventListener('pointermove',move);window.addEventListener('pointerup',stop,{once:true});window.addEventListener('pointercancel',stop,{once:true});window.addEventListener('blur',stop,{once:true});handle.addEventListener('lostpointercapture',stop,{once:true})
 },[axis,clamp,persist,reverse,size])
 return[size,start,resizeTo]
}
