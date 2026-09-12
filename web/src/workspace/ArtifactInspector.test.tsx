import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { artifactReviewBridge, sessionFolderBridge } from '../bridge/client'
import { ArtifactInspector, ArtifactPreviewContent } from './ArtifactInspector'
import type { WorkspaceArtifactPreviewResult } from '../generated/bridge'

vi.mock('../bridge/client', () => ({artifactReviewBridge:{preview:vi.fn()},sessionFolderBridge:{open:vi.fn()}}))
const sessionId='01ARZ3NDEKTSV4RRFFQ69G5FAV'
afterEach(cleanup)
beforeEach(()=>{vi.resetAllMocks();vi.mocked(sessionFolderBridge.open).mockResolvedValue({opened:'E:/项目/报告.docx'})})

it('does not show raw English preview or open failures',async()=>{
  vi.mocked(artifactReviewBridge.preview).mockRejectedValue(new Error('Failed to fetch'))
  render(<ArtifactInspector sessionId={sessionId} path="notes.md" onClose={vi.fn()}/>)
  expect(await screen.findByRole('alert')).toHaveTextContent('文件预览失败')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
  cleanup()
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'text',path:'notes.md',content:'正文',size:2})
  vi.mocked(sessionFolderBridge.open).mockRejectedValue(new Error('Failed to fetch'))
  render(<ArtifactInspector sessionId={sessionId} path="notes.md" onClose={vi.fn()}/>)
  await screen.findByText('正文')
  fireEvent.click(screen.getByRole('button',{name:'本机打开'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('无法打开文件')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('displays a host preview and opens the same file or folder without guessing Desktop', async()=>{
  const path='E:/项目/报告.docx'
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'docx',path,content:'报告正文',size:1234,absolutePath:path})
  render(<ArtifactInspector sessionId={sessionId} path={path} onClose={vi.fn()}/> )
  expect(await screen.findByText('报告正文')).toBeInTheDocument()
  expect(screen.getByTitle(path)).toBeInTheDocument()
  expect(screen.queryByText(path)).toBeNull()
  fireEvent.click(screen.getByRole('button',{name:'本机打开'}))
  await waitFor(()=>expect(sessionFolderBridge.open).toHaveBeenLastCalledWith({sessionId,relativePath:path,reveal:false}))
  await waitFor(()=>expect(screen.getByRole('button',{name:'所在文件夹'})).not.toBeDisabled())
  fireEvent.click(screen.getByRole('button',{name:'所在文件夹'}))
  await waitFor(()=>expect(sessionFolderBridge.open).toHaveBeenLastCalledWith({sessionId,relativePath:path,reveal:true}))
})

it('discards a late preview after switching to a different artifact',async()=>{
  let resolveOld!:(value:WorkspaceArtifactPreviewResult)=>void
  vi.mocked(artifactReviewBridge.preview).mockImplementation(({path})=>path==='old.docx'?new Promise(resolve=>{resolveOld=resolve}):Promise.resolve({kind:'docx',path,content:'最新文件',size:1}))
  const {rerender}=render(<ArtifactInspector sessionId={sessionId} path="old.docx" onClose={vi.fn()}/> )
  rerender(<ArtifactInspector sessionId={sessionId} path="new.docx" onClose={vi.fn()}/> )
  await screen.findByText('最新文件')
  await act(async()=>resolveOld({kind:'docx',path:'old.docx',content:'旧文件',size:1}))
  expect(screen.queryByText('旧文件')).not.toBeInTheDocument()
  expect(screen.getByText('最新文件')).toBeInTheDocument()
})

it('isolates generated HTML from scripts and network access',async()=>{
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'html',path:'page.html',content:'<script>fetch("https://external.invalid")</script><h1>页面</h1>',size:20})
  render(<ArtifactInspector sessionId={sessionId} path="page.html" onClose={vi.fn()}/> )
  const frame=await screen.findByTitle('产物预览 page.html')
  expect(frame).toHaveAttribute('sandbox','')
  expect(frame).toHaveAttribute('referrerpolicy','no-referrer')
  expect(frame.getAttribute('srcdoc')).toContain("connect-src 'none'")
})

it('keeps PDF native opening available and recovers from opening failures',async()=>{
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'pdf',path:'report.pdf',content:'',size:1024,notice:'请用本机软件打开查看完整内容'})
  vi.mocked(sessionFolderBridge.open).mockRejectedValueOnce(new Error('本机软件不可用'))
  render(<ArtifactInspector sessionId={sessionId} path="report.pdf" onClose={vi.fn()}/> )
  await screen.findByText('请用本机软件打开查看完整内容')
  fireEvent.click(screen.getByRole('button',{name:'本机打开'}))
  expect(await screen.findByRole('alert')).toHaveTextContent('本机软件不可用')
  expect(screen.getByRole('button',{name:'本机打开'})).not.toBeDisabled()
  fireEvent.click(screen.getByRole('button',{name:'本机打开'}))
  await waitFor(()=>expect(sessionFolderBridge.open).toHaveBeenCalledTimes(2))
})

it('rejects a remote URL disguised as an image preview',async()=>{
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'image',path:'capture.png',content:'https://outside.invalid/image.png',size:100})
  render(<ArtifactInspector sessionId={sessionId} path="capture.png" onClose={vi.fn()}/> )
  expect(await screen.findByRole('alert')).toHaveTextContent('图片预览格式无效')
  expect(screen.queryByRole('img')).toBeNull()
})

it('uses a slim icon chrome and toggles expand without a second confirmation dialog',async()=>{
  const onToggleExpand=vi.fn()
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'html',path:'page.html',content:'<h1>页面</h1>',size:20})
  render(<ArtifactInspector sessionId={sessionId} path="page.html" onClose={vi.fn()} onToggleExpand={onToggleExpand}/>)
  await screen.findByTitle('产物预览 page.html')
  expect(document.querySelector('.artifact-inspector-chrome')).not.toBeNull()
  expect(document.querySelectorAll('.artifact-icon-btn').length).toBeGreaterThanOrEqual(3)
  expect(screen.queryByRole('button',{name:'刷新预览'})).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button',{name:'放大预览'}))
  expect(onToggleExpand).toHaveBeenCalledOnce()
  expect(screen.queryByRole('dialog')).toBeNull()
})

it('renders markdown and source files instead of a raw dump',async()=>{
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'text',path:'填报信息.md',content:'# 软件著作权登记信息表\n\n软件全称：演示',size:40})
  render(<ArtifactInspector sessionId={sessionId} path="填报信息.md" onClose={vi.fn()}/>)
  expect(await screen.findByRole('heading',{name:'软件著作权登记信息表'})).toBeInTheDocument()
  cleanup()
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'text',path:'project.config.json',content:'{"appid":"1"}',size:14})
  render(<ArtifactInspector sessionId={sessionId} path="project.config.json" onClose={vi.fn()}/>)
  expect(await screen.findByLabelText('源代码预览')).toBeInTheDocument()
  expect(screen.getByText('{"appid":"1"}')).toBeInTheDocument()
})

it('presents Word as a reading page',async()=>{
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'docx',path:'周报.docx',content:'本周完成闭环验收。',size:20})
  render(<ArtifactInspector sessionId={sessionId} path="周报.docx" onClose={vi.fn()}/>)
  const paper=await screen.findByLabelText('文档预览')
  expect(paper).toHaveTextContent('本周完成闭环验收。')
})

it('keeps weekly-report PDF notices readable when bytes are not embedded',()=>{
  render(<ArtifactPreviewContent preview={{kind:'pdf',path:'周报.pdf',content:'请用本机软件打开查看完整内容',size:0}}/>)
  expect(screen.getByText('请用本机软件打开查看完整内容')).toBeInTheDocument()
  expect(screen.queryByTitle('产物预览 周报.pdf')).toBeNull()
})

it('keeps weekly-report sheet notices readable when grid JSON is missing',()=>{
  render(<ArtifactPreviewContent preview={{kind:'xlsx',path:'周报.xlsx',content:'请用本机软件打开查看完整内容',size:0}}/>)
  expect(screen.getByText('请用本机软件打开查看完整内容')).toBeInTheDocument()
  expect(screen.queryByText('表格预览格式无效，请用本机软件打开')).toBeNull()
})

it('keeps weekly-report image notices readable when data URL is missing',()=>{
  render(<ArtifactPreviewContent preview={{kind:'image',path:'封面.png',content:'请用本机软件打开查看完整内容',size:0}}/>)
  expect(screen.getByText('请用本机软件打开查看完整内容')).toBeInTheDocument()
  expect(screen.queryByText('图片预览格式无效，请用本机软件打开')).toBeNull()
})

it('embeds compact PDF bytes instead of a native-open banner',async()=>{
  const create=vi.spyOn(URL,'createObjectURL').mockReturnValue('blob:pdf-preview')
  const revoke=vi.spyOn(URL,'revokeObjectURL').mockImplementation(()=>{})
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'pdf',path:'说明.pdf',content:'JVBERi0xLjQKMTAw',size:20,notice:'请用本机软件打开查看完整内容'})
  render(<ArtifactInspector sessionId={sessionId} path="说明.pdf" onClose={vi.fn()}/>)
  const frame=await screen.findByTitle('产物预览 说明.pdf')
  expect(frame).toHaveAttribute('src','blob:pdf-preview')
  expect(screen.queryByText('请用本机软件打开查看完整内容')).toBeNull()
  cleanup()
  create.mockRestore()
  revoke.mockRestore()
})
