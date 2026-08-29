import{cleanup,render,screen}from'@testing-library/react'
import{afterEach,expect,it}from'vitest'
import{forgetAttachmentPreview,rememberAttachmentPreview}from'./attachments'
import{attachmentToken}from'./composerParser'
import{MessageAttachmentStrip}from'./MessageAttachmentStrip'

const ID='01ARZ3NDEKTSV4RRFFQ69G5FAV'
const OTHER='01ARZ3NDEKTSV4RRFFQ69G5FAA'

afterEach(()=>{
 cleanup()
 forgetAttachmentPreview(ID)
 forgetAttachmentPreview(OTHER)
 try{sessionStorage.clear();localStorage.clear()}catch{/* jsdom */}
})

it('renders an image when a preview is remembered',()=>{
 const bytes=new Uint8Array([1,2,3]),file=new File([bytes],'shot.png',{type:'image/png'})
 rememberAttachmentPreview(ID,file)
 render(<MessageAttachmentStrip mentions={[{id:ID,label:'shot.png'}]}/>)
 expect(screen.getByRole('img',{name:'shot.png'})).toBeInTheDocument()
})

it('falls back to a labeled chip when only the filename is known',()=>{
 render(<MessageAttachmentStrip mentions={[{id:OTHER,label:'notes.md'}]}/>)
 expect(screen.getByText(/notes\.md/)).toBeInTheDocument()
 expect(screen.queryByRole('img')).toBeNull()
})

it('keeps the attachment token helper stable',()=>{
 expect(attachmentToken(ID,'shot.png')).toBe(`[attachment:${ID}|shot.png]`)
})
