import {cleanup, render, screen} from '@testing-library/react'
import {afterEach, expect, it} from 'vitest'
import {latestTodoSummary, parseTaskSteps, TaskStepsFromSummary} from './TaskSteps'

afterEach(cleanup)

const rendered = `3 todo(s) stored
1. [x] (completed|medium) 确认读者
2. [ ] (in_progress|high) 写出大纲
3. [ ] (pending|low) 交给用户`

it('reads the rendered checklist and JSON arguments', () => {
  expect(parseTaskSteps(rendered).map(step => [step.status, step.content])).toEqual([
    ['completed', '确认读者'],
    ['in_progress', '写出大纲'],
    ['pending', '交给用户'],
  ])
  expect(parseTaskSteps('{"todos":[{"content":"先查资料","status":"in_progress"},{"content":"再写"}]}').map(step => step.content)).toEqual(['先查资料', '再写'])
  expect(parseTaskSteps('搜索：天气')).toEqual([])
  expect(latestTodoSummary([
    {name: 'todo.write', summary: '1 todo(s) stored\n1. [ ] (pending|medium) 旧步骤'},
    {name: 'web.search', summary: 'ok'},
    {name: 'todo.write', summary: rendered},
  ])).toBe(rendered)
})

it('shows the current step in the typed chat', () => {
  render(<TaskStepsFromSummary summary={rendered} />)
  expect(screen.getByRole('list', {name: '任务步骤'})).toBeInTheDocument()
  expect(screen.getByText('写出大纲').closest('li')).toHaveClass('now')
  expect(screen.getByText('确认读者').closest('li')).toHaveClass('done')
})
