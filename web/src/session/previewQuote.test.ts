import { describe, expect, it } from 'vitest'
import { composerWithPreviewQuote } from './previewQuote'

describe('browser selection in the chat composer', () => {
  it('puts the selected page text into an empty composer', () => {
    expect(composerWithPreviewQuote('', '销售额 5,648 万')).toBe('> 销售额 5,648 万\n')
  })

  it('keeps what was already typed and adds the selection under it', () => {
    expect(composerWithPreviewQuote('看一下这一段\n', '新建商机')).toBe('看一下这一段\n\n> 新建商机\n')
  })
})
