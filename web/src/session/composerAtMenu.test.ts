import { describe, expect, it } from 'vitest'
import { atMenuPlaceholder, filterComposerAtItems, insertComposerAtPick } from './composerAtMenu'

describe('composerAtMenu', () => {
  it('labels each @ source', () => {
    expect(atMenuPlaceholder('attachment')).toBe('附件')
    expect(atMenuPlaceholder('expert')).toBe('已挂载专家')
    expect(atMenuPlaceholder('member')).toBe('同事')
  })

  it('inserts expert and colleague tokens', () => {
    expect(insertComposerAtPick('请 @', { kind: 'expert', id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', label: 'PPT专家' })).toBe('请 [引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAC] ')
    expect(insertComposerAtPick('请 @P', { kind: 'member', id: '01ARZ3NDEKTSV4RRFFQ69G5FAD', label: 'PPT专家' })).toBe('请 @PPT专家 ')
  })

  it('filters by label', () => {
    const items = [
      { kind: 'expert' as const, id: 'a', label: 'PPT专家' },
      { kind: 'member' as const, id: 'b', label: '安全工程师' },
    ]
    expect(filterComposerAtItems(items, 'ppt').map(item => item.label)).toEqual(['PPT专家'])
  })
})