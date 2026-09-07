import { describe, expect, it } from 'vitest'
import { sessionMessageAtItems,atMenuPlaceholder, filterComposerAtItems, insertComposerAtPick } from './composerAtMenu'

describe('composerAtMenu', () => {
  it('labels each @ source', () => {
    expect(atMenuPlaceholder('attachment')).toBe('附件')
    expect(atMenuPlaceholder('expert')).toBe('已挂载专家')
    expect(atMenuPlaceholder('member')).toBe('同事')
    expect(atMenuPlaceholder('message')).toBe('对话消息')
  })

  it('inserts expert and colleague tokens', () => {
    expect(insertComposerAtPick('请 @', { kind: 'expert', id: '01ARZ3NDEKTSV4RRFFQ69G5FAC', label: 'PPT专家' })).toBe('请 [引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAC] ')
    expect(insertComposerAtPick('请 @P', { kind: 'member', id: '01ARZ3NDEKTSV4RRFFQ69G5FAD', label: 'PPT专家' })).toBe('请 @PPT专家 ')
  })

  it('inserts a message token', () => {
    expect(insertComposerAtPick('参考 @', { kind: 'message', id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', label: '我：上一句' })).toBe('参考 [message:01ARZ3NDEKTSV4RRFFQ69G5FAV|我：上一句] ')
  })

  it('filters by label', () => {
    const items = [
      { kind: 'expert' as const, id: 'a', label: 'PPT专家' },
      { kind: 'member' as const, id: 'b', label: '安全工程师' },
    ]
    expect(filterComposerAtItems(items, 'ppt').map(item => item.label)).toEqual(['PPT专家'])
  })
})
it('references only this session’s real messages and their durable artifacts',()=>{
 const messages=[{id:'one',sessionId:'here',role:'assistant',text:'本轮完整回复',artifacts:[{path:'reports/result.docx'}]},{id:'other',sessionId:'elsewhere',role:'user',text:'别人的内容'},{id:'tool',sessionId:'here',role:'tool',text:'内部结果'}] as unknown as Parameters<typeof sessionMessageAtItems>[0]
 const items=sessionMessageAtItems(messages,'here')
 expect(items).toEqual([{kind:'message',id:'one',label:'月汐：本轮完整回复'},{kind:'artifact',id:'one',label:'产物：reports/result.docx'}])
 expect(insertComposerAtPick('请参考 @',items[1])).toBe('请参考 [message:one|产物：reports/result.docx] ')
})
