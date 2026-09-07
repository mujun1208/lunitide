import {cleanup,render,screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {afterEach,expect,it,vi} from 'vitest'
import {ChatFollowUps,suggestChatFollowUps} from './ChatFollowUps'
import {splitChatSuggestions} from './chatSuggestions'

afterEach(cleanup)
const answer=`已完成需求草案。

### 下一步建议
- 核对草案里的离线使用限制，列出需要补充的需求
- 按草案中的优先级拆分开发任务与验收标准`

it('uses exactly two distinct contextual suggestions produced with the answer',()=>{
  const {body,suggestions}=splitChatSuggestions(answer)
  expect(body).toBe('已完成需求草案。')
  expect(suggestChatFollowUps(answer)).toEqual(suggestions)
  expect(suggestions).toHaveLength(2)
  expect(suggestions[0]).toContain('离线使用限制')
})

it('keeps malformed suggestions and code examples in the original body',()=>{
  for (const text of ['内容\n### 下一步建议\n- 只有一个建议','```markdown\n'+answer+'\n```','内容\n### 下一步建议\n- 重复的建议内容\n- 重复的建议内容']) {
    expect(splitChatSuggestions(text)).toEqual({body:text,suggestions:[]})
  }
})

it('falls back to file-specific review requests without inventing style changes',()=>{
  const chips=suggestChatFollowUps('PPT 已写到桌面。需要我加一页联系方式吗？',[{kind:'pptx',path:'desktop/介绍.pptx',content:'',callId:'c1',toolName:'pptx.gen'}])
  expect(chips).toHaveLength(2)
  expect(chips.every(item=>item.includes('介绍.pptx'))).toBe(true)
  expect(chips.join(' ')).not.toMatch(/联系方式|深色|主题|精简一页|需要我/)
})

it('acknowledges unresolved failures instead of assuming success',()=>{
  expect(suggestChatFollowUps('无法读取文件，任务未完成。')).toEqual([
    '说明这次未完成的具体原因，以及继续处理所需的信息',
    '根据目前已确认的信息，给出可行的替代方案和取舍',
  ])
})

it('sends only the chosen next request after a click',async()=>{
  const onSelect=vi.fn(), user=userEvent.setup()
  render(<ChatFollowUps text={answer} onSelect={onSelect}/> )
  expect(onSelect).not.toHaveBeenCalled()
  const buttons=screen.getAllByRole('button')
  expect(buttons).toHaveLength(2)
  await user.click(buttons[1])
  expect(onSelect).toHaveBeenCalledExactlyOnceWith('按草案中的优先级拆分开发任务与验收标准')
})
