import { expect, test } from 'vitest'
import { parseActionLines, parseMeetingNotesDoc } from './notesDoc'

test('parses Feishu-like sections, tables, attendees and owned actions', () => {
  const doc = parseMeetingNotesDoc(
    `参会：敏民、绍辉

## 背景
确认个人知识库定位。

## 议题：个人与组织知识库
- 个人知识库对个人开放
- 组织库需审核

### 对照
| 个人 | 组织 |
| --- | --- |
| 可上传 | 需审核 |

## 决议
- 个人库先上线

## 未决
- 收费口径`,
    '- 绍辉：补齐报价（截止：周五）\n- 复核权限',
  )
  expect(doc.attendees).toEqual(['敏民', '绍辉'])
  expect(doc.sections.map(section => section.kind)).toEqual(['background', 'topic', 'decision', 'open'])
  expect(doc.sections[1]?.heading).toBe('个人与组织知识库')
  expect(doc.sections[1]?.table?.headers).toEqual(['个人', '组织'])
  expect(doc.sections[1]?.table?.rows[0]).toEqual(['可上传', '需审核'])
  expect(doc.actions).toEqual([
    { owner: '绍辉', task: '补齐报价', due: '周五' },
    { owner: '', task: '复核权限', due: '' },
  ])
})

test('turns numbered legacy summaries into topic cards', () => {
  const doc = parseMeetingNotesDoc(
    '本次会议围绕产品体验展开。\n\n一、体验与稳定性\n优先修复截图发送。\n\n二、发布安排\n完成自动化验证后再验收。',
    '□ 验证截图发送',
  )
  expect(doc.sections.length).toBeGreaterThanOrEqual(2)
  expect(doc.sections.some(section => section.heading.includes('体验与稳定性'))).toBe(true)
  expect(parseActionLines('□ 验证截图发送')[0]?.task).toBe('验证截图发送')
})
