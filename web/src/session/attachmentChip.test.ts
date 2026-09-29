import {expect, it} from 'vitest'
import {attachmentChipMeta} from './attachmentChip'

it('shows the extension and size without a percent', () => {
  expect(attachmentChipMeta('支点互动CRM系统需求说明.docx', 69222)).toBe('DOCX · 67.6 KB')
  expect(attachmentChipMeta('notes.txt', 3)).toBe('TXT · 3 B')
  expect(attachmentChipMeta('clip.png', 1024 * 1024)).toBe('PNG · 1.0 MB')
  expect(attachmentChipMeta('README', 12)).toBe('FILE · 12 B')
})
