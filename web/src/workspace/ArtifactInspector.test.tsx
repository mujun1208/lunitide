import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { artifactReviewBridge, sessionFolderBridge } from '../bridge/client'
import { ArtifactInspector } from './ArtifactInspector'
import type { WorkspaceArtifactPreviewResult } from '../generated/bridge'

vi.mock('../bridge/client', () => ({artifactReviewBridge:{preview:vi.fn()},sessionFolderBridge:{open:vi.fn()}}))
const sessionId='01ARZ3NDEKTSV4RRFFQ69G5FAV'
afterEach(cleanup)
beforeEach(()=>{vi.resetAllMocks();vi.mocked(sessionFolderBridge.open).mockResolvedValue({opened:'E:/项目/报告.docx'})})

it('displays a host preview and opens the same file or folder without guessing Desktop', async()=>{
  const path='E:/项目/报告.docx'
  vi.mocked(artifactReviewBridge.preview).mockResolvedValue({kind:'docx',path,content:'报告正文',size:1234,absolutePath:path})
  render(<ArtifactInspector sessionId={sessionId} path={path} onClose={vi.fn()}/> )
  expect(await screen.findByText('报告正文')).toBeInTheDocument()
  expect(screen.getByText(path)).toBeInTheDocument()
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
