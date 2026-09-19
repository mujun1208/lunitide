import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, test } from 'vitest'
import { MeetingNotesDoc } from './MeetingNotesDoc'

afterEach(() => {
  cleanup()
})

test('renders attendee chips, topic table and owned todos', () => {
  render(
    <MeetingNotesDoc
      startedAt="2026-09-19T11:16:00.000Z"
      durationLabel="1:00:00"
      summary={`参会：敏民、绍辉\n\n## 背景\n确认个人知识库定位。\n\n## 议题：个人与组织知识库\n- 个人知识库对个人开放\n\n### 对照\n| 个人 | 组织 |\n| --- | --- |\n| 可上传 | 需审核 |\n\n## 决议\n- 个人库先上线`}
      actions="- 绍辉：补齐报价（截止：周五）"
      emptySummaryHint="尚未生成摘要。"
      emptyActionsHint="这场没有抽出可执行待办。"
    />,
  )
  expect(screen.getByLabelText('参会人')).toHaveTextContent('敏民')
  expect(screen.getByRole('heading', { name: '个人与组织知识库' })).toBeInTheDocument()
  expect(screen.getByRole('columnheader', { name: '个人' })).toBeInTheDocument()
  expect(screen.getByRole('cell', { name: '可上传' })).toBeInTheDocument()
  expect(screen.getByText('补齐报价')).toBeInTheDocument()
  expect(screen.getByText('绍辉 · 截止 周五')).toBeInTheDocument()
})
