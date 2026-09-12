import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { artifactOpenRelativePath, ChatArtifactCards, filterChatDeliverables, isChatDeliverableArtifact } from './ChatArtifactCards'
import { OFFICE_STUDIO_OPEN_EVENT, type OfficeOpenRequest } from '../officeStudio/officeNavigation'

vi.mock('../bridge/client', () => ({
  sessionFolderBridge: { open: vi.fn() },
}))
afterEach(cleanup)

it('does not show raw English artifact open failures', async () => {
  const { sessionFolderBridge } = await import('../bridge/client')
  vi.mocked(sessionFolderBridge.open).mockRejectedValue(new Error('Failed to fetch'))
  const onError = vi.fn()
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[{
    kind: 'md', path: 'notes.md', content: '', callId: 'call-1', toolName: 'workspace.write',
  }]} onError={onError} />)
  fireEvent.click(screen.getByText('notes.md'))
  await vi.waitFor(() => expect(onError).toHaveBeenCalledWith('无法打开产物文件'))
  expect(onError).not.toHaveBeenCalledWith('Failed to fetch')
})

it('labels image artifacts as screenshots the user can open', () => {
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[{
    kind: 'image', path: 'screen-capture-20260826.png', content: '', callId: 'call-1', toolName: 'cc.screen_capture',
  }]} />)
  expect(screen.getByRole('listitem')).toHaveTextContent('截图 · 点击打开')
  expect(screen.getByText('screen-capture-20260826.png')).toBeInTheDocument()
})

it('shows a Trae-style changed-file card for office.generate Word', () => {
  expect(isChatDeliverableArtifact({ toolName: 'office.generate', kind: 'docx', path: 'office/周报-abc123.docx' })).toBe(true)
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[{
    kind: 'docx', path: 'office/周报-abc123.docx', content: '', callId: 'gen-1', toolName: 'office.generate',
  }]} />)
  const card = screen.getByRole('listitem')
  expect(card).toHaveTextContent('1个文件已更改')
  expect(card).toHaveTextContent('周报-abc123.docx')
  expect(card).toHaveTextContent('点击打开')
})

it('hides intermediate web search and fetch HTML from deliverable cards', () => {
  expect(isChatDeliverableArtifact({ toolName: 'web.search', kind: 'html', path: 'search.html' })).toBe(false)
  expect(isChatDeliverableArtifact({ toolName: 'web.fetch', kind: 'html', path: 'fetch.html' })).toBe(false)
  expect(isChatDeliverableArtifact({ toolName: 'pptx.gen', kind: 'pptx', path: 'deck.pptx' })).toBe(true)
  expect(isChatDeliverableArtifact({ toolName: 'workspace.write', kind: 'pptx', path: 'desktop/介绍.pptx' })).toBe(true)
  expect(isChatDeliverableArtifact({ toolName: 'workspace.write', kind: 'md', path: '周报/周报_2026-W37.md' })).toBe(true)
  expect(isChatDeliverableArtifact({ toolName: 'workspace.edit', kind: 'txt', path: 'notes.txt' })).toBe(true)
  expect(artifactOpenRelativePath(String.raw`C:\Users\mujun\Desktop\介绍.pptx`)).toBe('C:/Users/mujun/Desktop/介绍.pptx')
  expect(artifactOpenRelativePath(String.raw`E:\项目\介绍.pptx`)).toBe('E:/项目/介绍.pptx')
  const visible = filterChatDeliverables([
    { kind: 'html', path: 'search.html', content: '', callId: 's1', toolName: 'web.search' },
    { kind: 'html', path: 'fetch.html', content: '', callId: 'f1', toolName: 'web.fetch' },
    { kind: 'pptx', path: 'deck.pptx', content: '', callId: 'p1', toolName: 'pptx.gen' },
  ])
  expect(visible).toHaveLength(1)
  expect(visible[0]?.path).toBe('deck.pptx')
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[
    { kind: 'html', path: 'search.html', content: '', callId: 's1', toolName: 'web.search' },
    { kind: 'pptx', path: 'deck.pptx', content: '', callId: 'p1', toolName: 'pptx.gen' },
  ]} />)
  expect(screen.queryByText('search.html')).toBeNull()
  expect(screen.getByText('deck.pptx')).toBeInTheDocument()
})

it('shows markdown skill files as chat cards without opening Office Studio', () => {
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[{
    kind: 'md', path: '周报/周报_2026-W37.md', content: '', callId: 'write-1', toolName: 'workspace.write',
  }]} />)
  expect(screen.getByRole('listitem')).toHaveTextContent('Markdown · 点击打开')
  expect(screen.getByText('周报_2026-W37.md')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '在办公工作台查看' })).toBeNull()
})

it('opens the inspector with the exact historical artifact path', () => {
  const onInspect = vi.fn()
  const artifact = {kind:'docx' as const,path:String.raw`E:\项目\报告.docx`,content:'',callId:'persisted-1',toolName:'docx.gen'}
  render(<ChatArtifactCards sessionId="01ARZ3NDEKTSV4RRFFQ69G5FAV" artifacts={[artifact]} onInspect={onInspect}/>)
  fireEvent.click(screen.getByText('报告.docx'))
  expect(onInspect).toHaveBeenCalledWith(artifact)
})

it('focuses the current office task instead of remounting the workbench', () => {
  const focuses: Array<{ taskId: string; path: string }> = []
  const onFocus = (event: Event) => {
    focuses.push((event as CustomEvent<{ taskId: string; path: string }>).detail)
  }
  const opened: OfficeOpenRequest[] = []
  const onOpen = (event: Event) => {
    opened.push((event as CustomEvent<OfficeOpenRequest>).detail)
    opened.at(-1)?.finish()
  }
  window.addEventListener('lunitide:office-artifact-focus', onFocus)
  window.addEventListener(OFFICE_STUDIO_OPEN_EVENT, onOpen)
  try {
    render(<ChatArtifactCards officeTaskId="01ARZ3NDEKTSV4RRFFQ69G5FA0" sessionId="same-session" artifacts={[{kind:'pptx',path:'节奏图.pptx',content:'',callId:'call',toolName:'pptx.gen'}]}/>)
    fireEvent.click(screen.getByText('节奏图.pptx'))
    fireEvent.click(screen.getByRole('button', { name: '在办公工作台查看' }))
    expect(focuses).toEqual([
      { taskId: '01ARZ3NDEKTSV4RRFFQ69G5FA0', path: '节奏图.pptx' },
      { taskId: '01ARZ3NDEKTSV4RRFFQ69G5FA0', path: '节奏图.pptx' },
    ])
    expect(opened).toEqual([])
  } finally {
    window.removeEventListener('lunitide:office-artifact-focus', onFocus)
    window.removeEventListener(OFFICE_STUDIO_OPEN_EVENT, onOpen)
  }
})

it('opens Office artifacts in the workbench with the original session and reports feature errors in chat', async () => {
  const onError = vi.fn()
  let requested: OfficeOpenRequest | undefined
  window.addEventListener(OFFICE_STUDIO_OPEN_EVENT, event => {
    requested = (event as CustomEvent<OfficeOpenRequest>).detail
    requested.finish('办公工作台暂未启用')
  }, { once: true })
  render(<ChatArtifactCards sessionId="same-session" artifacts={[{kind:'docx',path:'报告.docx',content:'',callId:'call',toolName:'docx.gen'}]} onError={onError}/>)
  fireEvent.click(screen.getByRole('button', { name: '在办公工作台查看' }))
  await vi.waitFor(() => expect(onError).toHaveBeenCalledWith('办公工作台暂未启用'))
  expect(requested?.sessionId).toBe('same-session')
  expect(requested?.path).toBe('报告.docx')
})
