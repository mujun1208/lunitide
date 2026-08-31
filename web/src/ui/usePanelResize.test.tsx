import{act,cleanup,render,screen}from'@testing-library/react'
import{afterEach,beforeEach,describe,expect,it}from'vitest'
import{usePanelResize}from'./usePanelResize'

function Harness({reverse=false,axis='x'}:{reverse?:boolean;axis?:'x'|'y'}){
 const[size,start]=usePanelResize({storageKey:'panel-test',initial:300,min:200,max:()=>500,reverse,axis})
 return <><output>{size}</output><div role="separator" onPointerDown={start}/></>
}

describe('usePanelResize',()=>{
 beforeEach(()=>localStorage.clear())
 afterEach(cleanup)
 const pointer=(target:EventTarget,type:string,clientX:number,button=0,clientY=0)=>act(()=>{const event=new Event(type,{bubbles:true});Object.defineProperties(event,{clientX:{value:clientX},clientY:{value:clientY},button:{value:button}});target.dispatchEvent(event)})
 it('resizes forward within bounds and persists on release',()=>{
  render(<Harness/>);const handle=screen.getByRole('separator')
  pointer(handle,'pointerdown',300);pointer(window,'pointermove',460);expect(screen.getByText('460')).toBeInTheDocument()
  pointer(window,'pointermove',900);expect(screen.getByText('500')).toBeInTheDocument();pointer(window,'pointerup',900)
  expect(localStorage.getItem('panel-test')).toBe('500')
 })
 it('resizes a right panel in reverse and respects its minimum',()=>{
  render(<Harness reverse/>);const handle=screen.getByRole('separator')
  pointer(handle,'pointerdown',300);pointer(window,'pointermove',380);expect(screen.getByText('220')).toBeInTheDocument()
  pointer(window,'pointermove',500);expect(screen.getByText('200')).toBeInTheDocument();pointer(window,'pointerup',500)
 })
 it('resizes a bottom composer upward on the y axis',()=>{
  render(<Harness axis="y" reverse/>);const handle=screen.getByRole('separator')
  pointer(handle,'pointerdown',0,0,400);pointer(window,'pointermove',0,0,280);expect(screen.getByText('420')).toBeInTheDocument()
  pointer(window,'pointermove',0,0,0);expect(screen.getByText('500')).toBeInTheDocument();pointer(window,'pointerup',0,0,0)
  expect(localStorage.getItem('panel-test')).toBe('500')
 })
 it('clamps restored widths and cleans up a cancelled drag',()=>{
  localStorage.setItem('panel-test','9999');render(<Harness/>);expect(screen.getByText('500')).toBeInTheDocument()
  pointer(screen.getByRole('separator'),'pointerdown',500);expect(document.body.style.userSelect).toBe('none')
  pointer(window,'pointercancel',500);expect(document.body.style.userSelect).toBe('');expect(document.body.style.cursor).toBe('')
 })
})
