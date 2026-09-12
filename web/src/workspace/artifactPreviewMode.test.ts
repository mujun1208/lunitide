import { expect, it } from 'vitest'
import { artifactLooksLikePdfBytes, artifactPreviewIsReady, artifactViewMode, previewKindFromPath } from './artifactPreviewMode'

it('picks a Trae-style viewer for each artifact kind', () => {
  expect(artifactViewMode('html', 'index.html')).toBe('html')
  expect(artifactViewMode('docx', '周报.docx')).toBe('paper')
  expect(artifactViewMode('pptx', 'deck.pptx')).toBe('paper')
  expect(artifactViewMode('pdf', '说明.pdf')).toBe('pdf')
  expect(artifactViewMode('xlsx', '表.xlsx')).toBe('sheet')
  expect(artifactViewMode('image', 'shot.png')).toBe('image')
  expect(artifactViewMode('text', '填报信息.md')).toBe('markdown')
  expect(artifactViewMode('text', 'project.config.json')).toBe('code')
  expect(artifactViewMode('text', 'notes.txt')).toBe('text')
})

it('only treats compact base64 as inline PDF bytes', () => {
  expect(artifactLooksLikePdfBytes('JVBERi0xLjQKMTAw')).toBe(true)
  expect(artifactLooksLikePdfBytes('请用本机软件打开查看完整内容')).toBe(false)
})

it('maps a file path to the same preview kind the inspector uses', () => {
  expect(previewKindFromPath('index.html')).toBe('html')
  expect(previewKindFromPath('填报信息.md')).toBe('text')
  expect(previewKindFromPath('project.config.json')).toBe('text')
  expect(previewKindFromPath('周报.docx')).toBe('docx')
  expect(previewKindFromPath('说明.pdf')).toBe('pdf')
})

it('hides the native-open notice once a rendered preview is ready', () => {
  expect(artifactPreviewIsReady('pdf', '说明.pdf', 'JVBERi0xLjQKMTAw')).toBe(true)
  expect(artifactPreviewIsReady('pdf', '说明.pdf', '')).toBe(false)
  expect(artifactPreviewIsReady('text', '填报信息.md', '# 标题')).toBe(true)
})
