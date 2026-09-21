import { expect, it } from 'vitest'
import { parseMeetingNotesDoc } from './notesDoc'

const summary = `参会：张伟、李娜

## 背景

明确知识库分级口径。

## 议题：知识库分级

个人库目前不做标准化。

- 个人库不做标准化
- 组织库要走审核

### 思考

- 个人库直接进生产会带入未审核内容
- 先判分级再决定入库路径

### 三类知识库差异

| 类型 | 审核 |
| --- | --- |
| 个人 | 否 |
| 组织 | 是 |

### 入库流程

\`\`\`mermaid
flowchart LR
  n1["接收内容"]
  n2["判定分级"]
  n3["驳回并反馈"]
  n1 --> n2
  n2 -->|"不合规"| n3
\`\`\`
`

it('separates transcript facts, the thinking record, the table and the flow', () => {
  const doc = parseMeetingNotesDoc(summary, '')
  expect(doc.attendees).toEqual(['张伟', '李娜'])
  const topic = doc.sections.find(s => s.heading === '知识库分级')
  expect(topic).toBeDefined()
  // Facts stay in bullets; the thinking record must not be mixed in with them,
  // or the notes claim the meeting said things it only concluded.
  expect(topic!.bullets).toEqual(['个人库不做标准化', '组织库要走审核'])
  expect(topic!.reasoning).toEqual(['个人库直接进生产会带入未审核内容', '先判分级再决定入库路径'])
  expect(topic!.table?.caption).toBe('三类知识库差异')
  expect(topic!.table?.rows).toEqual([['个人', '否'], ['组织', '是']])
  expect(topic!.diagram?.caption).toBe('入库流程')
  expect(topic!.diagram?.code).toContain('flowchart LR')
  expect(topic!.diagram?.code).toContain('不合规')
  // The fence must never survive as prose; that is what a raw dump looks like.
  expect(topic!.paragraphs.join('\n')).not.toContain('flowchart')
  expect(topic!.paragraphs).toContain('个人库目前不做标准化。')
})

it('leaves reasoning and diagram empty when the notes carry neither', () => {
  const doc = parseMeetingNotesDoc('## 议题：闲聊\n\n- 确认下周继续\n', '')
  const topic = doc.sections[0]!
  expect(topic.reasoning).toEqual([])
  expect(topic.diagram).toBeUndefined()
  expect(topic.bullets).toEqual(['确认下周继续'])
})

it('does not let an unclosed fence swallow the rest of the document', () => {
  const doc = parseMeetingNotesDoc('## 议题：甲\n\n```mermaid\nflowchart LR\n  n1["甲"]\n', '')
  expect(doc.sections).toHaveLength(1)
  expect(doc.sections[0]!.paragraphs.join('\n')).toContain('flowchart LR')
})
