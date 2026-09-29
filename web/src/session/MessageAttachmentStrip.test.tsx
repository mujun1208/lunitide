import{cleanup,render,screen,fireEvent,waitFor}from'@testing-library/react'
import{afterEach,expect,it,vi}from'vitest'
import{forgetAttachmentPreview,rememberAttachmentPreview}from'./attachments'
import type {AttachmentBridge} from '../bridge/client'
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
 rememberAttachmentPreview(ID,file,'data:image/png;base64,AQID')
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

it('loads a persisted webp after restart and opens the real image when clicked',async()=>{
 const get=vi.fn().mockResolvedValue({attachmentId:ID,originalName:'shot.webp',mime:'image/webp',contentBase64:'UklGRg=='}),attachments={get} as unknown as AttachmentBridge
 render(<MessageAttachmentStrip mentions={[{id:ID,label:'shot.webp'}]} attachments={attachments}/>)
 const thumb=await screen.findByRole('img',{name:'shot.webp'})
 expect(thumb.getAttribute('src')).toBe('data:image/webp;base64,UklGRg==')
 fireEvent.click(screen.getByRole('button',{name:'查看附件 shot.webp'}))
 expect(await screen.findByRole('dialog')).toHaveAccessibleName('查看附件 shot.webp')
 await waitFor(()=>expect(screen.getAllByRole('img',{name:'shot.webp'})).toHaveLength(2))
 fireEvent.click(screen.getByRole('button',{name:'关闭附件预览'}))
 expect(screen.queryByRole('dialog')).toBeNull()
})
it('does not show raw English attachment read failures',async()=>{
  const get=vi.fn().mockRejectedValue(new Error('Failed to fetch')),attachments={get} as unknown as AttachmentBridge
  render(<MessageAttachmentStrip mentions={[{id:OTHER,label:'notes.md'}]} attachments={attachments}/>)
  fireEvent.click(screen.getByRole('button',{name:'查看附件 notes.md'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('附件读取失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('opens a text file and permits retry after a preview failure',async()=>{
 const get=vi.fn().mockRejectedValueOnce(new Error('暂时不可用')).mockResolvedValue({attachmentId:OTHER,originalName:'notes.md',mime:'text/plain',parsedText:'# 原文件内容'}),attachments={get} as unknown as AttachmentBridge
 render(<MessageAttachmentStrip mentions={[{id:OTHER,label:'notes.md'}]} attachments={attachments}/>)
 fireEvent.click(screen.getByRole('button',{name:'查看附件 notes.md'}))
 expect(await screen.findByRole('alert')).toHaveTextContent('暂时不可用')
 fireEvent.click(screen.getByRole('button',{name:'重试'}))
 expect(await screen.findByText('# 原文件内容')).toBeInTheDocument()
})

it('opens a docx with the original program instead of the inline viewer',async()=>{
 const open=vi.fn().mockResolvedValue({opened:'C:\\Temp\\lunitide-attachment-open\\'+ID+'\\需求.docx'}),get=vi.fn(),attachments={open,get} as unknown as AttachmentBridge
 render(<MessageAttachmentStrip mentions={[{id:ID,label:'需求.docx'}]} attachments={attachments}/>)
 fireEvent.click(screen.getByRole('button',{name:'查看附件 需求.docx'}))
 await waitFor(()=>expect(open).toHaveBeenCalledWith({attachmentId:ID}))
 expect(screen.queryByRole('dialog')).toBeNull()
 expect(get).not.toHaveBeenCalled()
})

it('falls back to the inline viewer when the original program cannot open',async()=>{
 const open=vi.fn().mockRejectedValue(new Error('无法打开文件')),get=vi.fn().mockResolvedValue({attachmentId:ID,originalName:'需求.docx',mime:'application/vnd.openxmlformats-officedocument.wordprocessingml.document',parsedText:'提取出的正文'}),attachments={open,get} as unknown as AttachmentBridge
 render(<MessageAttachmentStrip mentions={[{id:ID,label:'需求.docx'}]} attachments={attachments}/>)
 fireEvent.click(screen.getByRole('button',{name:'查看附件 需求.docx'}))
 expect(await screen.findByRole('dialog')).toHaveAccessibleName('查看附件 需求.docx')
 await waitFor(()=>expect(open).toHaveBeenCalled())
 expect(await screen.findByText('提取出的正文')).toBeInTheDocument()
})

it('offers opening the original program from the viewer header for documents only',async()=>{
 const open=vi.fn().mockRejectedValueOnce(new Error('无法打开文件')).mockResolvedValue({opened:'x'}),get=vi.fn().mockResolvedValue({attachmentId:ID,originalName:'需求.docx',mime:'application/vnd.openxmlformats-officedocument.wordprocessingml.document',parsedText:'提取出的正文'}),attachments={open,get} as unknown as AttachmentBridge
 render(<MessageAttachmentStrip mentions={[{id:ID,label:'需求.docx'}]} attachments={attachments}/>)
 fireEvent.click(screen.getByRole('button',{name:'查看附件 需求.docx'}))
 expect(await screen.findByRole('dialog')).toHaveAccessibleName('查看附件 需求.docx')
 fireEvent.click(screen.getByRole('button',{name:'原程序打开'}))
 await waitFor(()=>expect(open).toHaveBeenCalledTimes(2))
 const getMd=vi.fn().mockResolvedValue({attachmentId:OTHER,originalName:'notes.md',mime:'text/plain',parsedText:'# 原文件内容'}),attachmentsMd={get:getMd} as unknown as AttachmentBridge
 fireEvent.click(screen.getByRole('button',{name:'关闭附件预览'}))
 cleanup()
 render(<MessageAttachmentStrip mentions={[{id:OTHER,label:'notes.md'}]} attachments={attachmentsMd}/>)
 fireEvent.click(screen.getByRole('button',{name:'查看附件 notes.md'}))
 expect(await screen.findByRole('dialog')).toHaveAccessibleName('查看附件 notes.md')
 expect(screen.queryByRole('button',{name:'原程序打开'})).toBeNull()
})
