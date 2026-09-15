import { expect, it } from 'vitest'
import { workspaceDownloadEnabled, workspaceDownloadIsText } from './workspaceDownload'

it('treats office and pdf names as binary even when extracted text exists', () => {
  expect(workspaceDownloadIsText('application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'notes.docx')).toBe(false)
  expect(workspaceDownloadEnabled({
    name: 'notes.docx',
    mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    parsedText: 'extracted paragraphs',
  })).toBe(false)
  expect(workspaceDownloadEnabled({
    name: 'notes.docx',
    mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    parsedText: 'extracted paragraphs',
    contentBase64: 'UEsB',
  })).toBe(true)
})

it('allows text downloads from parsed or local text', () => {
  expect(workspaceDownloadEnabled({ name: 'notes.txt', mime: 'text/plain', parsedText: 'hello' })).toBe(true)
  expect(workspaceDownloadEnabled({ name: 'readme.md', localText: '# hi' })).toBe(true)
  expect(workspaceDownloadEnabled({ name: 'notes.docx', localText: 'extracted' })).toBe(false)
})
