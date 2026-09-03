import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { ExpertKnowledgePanel, knowledgeMediaType } from './ExpertKnowledgePanel'

afterEach(cleanup)

it('routes office and pdf files to their OOXML/pdf media hint even when File.type is empty', () => {
  expect(knowledgeMediaType('report.xlsx', '')).toBe('application/vnd.openxmlformats-officedocument.spreadsheetml.sheet')
  expect(knowledgeMediaType('deck.pptx', '')).toBe('application/vnd.openxmlformats-officedocument.presentationml.presentation')
  expect(knowledgeMediaType('manual.docx', '')).toBe('application/vnd.openxmlformats-officedocument.wordprocessingml.document')
  expect(knowledgeMediaType('scan.pdf', '')).toBe('application/pdf')
  expect(knowledgeMediaType('notes.md', '')).toBe('text/markdown')
  expect(knowledgeMediaType('data.bin', '')).toBe('text/plain')
  expect(knowledgeMediaType('data.bin', 'application/octet-stream')).toBe('application/octet-stream')
})

const expertId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'

it('shows knowledge counts when the expert has a collection', async () => {
  const knowledgeGet = vi.fn().mockResolvedValue({
    collectionId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    documentCount: 12, readyCount: 10, chunkCount: 480, nodeCount: 0, memoryCount: 3, missing: false,
  })
  render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} /></LanguageProvider>)
  expect(await screen.findByText(/480/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '把文件交给此专家' })).toBeInTheDocument()
})

it('shows the first three parse preview blocks after a ready ingest', async () => {
  const knowledgeGet = vi.fn().mockResolvedValue({
    collectionId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    documentCount: 1, readyCount: 1, chunkCount: 3, nodeCount: 0, memoryCount: 0, missing: false,
  })
  const upsertDocument = vi.fn().mockResolvedValue({
    indexState: 'ready',
    preview: ['ATA 32 isolation', 'retract the gear', 'third block', 'ignored'],
  })
  render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} upsertDocument={upsertDocument} /></LanguageProvider>)
  await screen.findByRole('button', { name: '把文件交给此专家' })
  const file = mockLocalFile('# ATA', 'amm.md', 'text/markdown', 'E:\\manuals\\amm.md')
  const input = document.querySelector('input[type="file"]') as HTMLInputElement
  Object.defineProperty(input, 'files', { value: [file], configurable: true })
  fireEvent.change(input)
  expect(await screen.findByLabelText('解析预览')).toBeInTheDocument()
  expect(screen.getByText('ATA 32 isolation')).toBeInTheDocument()
  expect(screen.getByText('third block')).toBeInTheDocument()
  expect(screen.queryByText('ignored')).toBeNull()
})

it('shows a red fail reason when index_state is failed', async () => {
  const knowledgeGet = vi.fn().mockResolvedValue({
    collectionId: '01ARZ3NDEKTSV4RRFFQ69G5FAW',
    documentCount: 0, readyCount: 0, chunkCount: 0, nodeCount: 0, memoryCount: 0, missing: false,
  })
  const upsertDocument = vi.fn().mockResolvedValue({
    indexState: 'failed',
    failReason: 'parse function not configured',
  })
  render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} upsertDocument={upsertDocument} /></LanguageProvider>)
  await screen.findByRole('button', { name: '把文件交给此专家' })
  const file = mockLocalFile('%PDF', 'scan.pdf', 'application/pdf', 'E:\\manuals\\scan.pdf')
  const input = document.querySelector('input[type="file"]') as HTMLInputElement
  Object.defineProperty(input, 'files', { value: [file], configurable: true })
  fireEvent.change(input)
  const alert = await screen.findByRole('alert')
  expect(alert).toHaveTextContent('无法抽出正文')
  expect(alert).toHaveTextContent('parse function not configured')
  expect(alert).toHaveClass('is-failed')
})

it('hides the file button on a persona card without collectionId', async () => {
  const knowledgeGet = vi.fn().mockResolvedValue({
    collectionId: '', documentCount: 0, readyCount: 0, chunkCount: 0, nodeCount: 0, memoryCount: 0, missing: true,
  })
  render(<LanguageProvider value="zh-CN"><ExpertKnowledgePanel expertId={expertId} knowledgeGet={knowledgeGet} /></LanguageProvider>)
  expect(await screen.findByText(/人设卡不建知识库/)).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: '把文件交给此专家' })).toBeNull()
})

function mockLocalFile(text: string, name: string, type: string, path: string): File {
  const file = new File([text], name, { type })
  Object.defineProperty(file, 'path', { value: path })
  Object.defineProperty(file, 'arrayBuffer', {
    value: async () => new TextEncoder().encode(text).buffer,
  })
  return file
}
