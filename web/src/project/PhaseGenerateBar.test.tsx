import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { PhaseGenerateBar } from './PhaseGenerateBar'

const generate = vi.fn().mockResolvedValue({ generated: 9 })
const interviewGet = vi.fn().mockResolvedValue({ interview: { phases: {} } })
const interviewSave = vi.fn().mockResolvedValue({ interview: { phases: {} } })

vi.mock('./projectFactoryApi', () => ({
  projectFactoryApi: {
    generate: (...args: unknown[]) => generate(...args),
    interviewGet: (...args: unknown[]) => interviewGet(...args),
    interviewSave: (...args: unknown[]) => interviewSave(...args),
  },
}))

afterEach(() => {
  cleanup()
  generate.mockClear()
  interviewGet.mockClear()
  interviewSave.mockClear()
})

it('shows the unfinished-question-bank banner and sends overwriteDrafts', async () => {
  const user = userEvent.setup()
  render(<PhaseGenerateBar projectId="01ARZ3NDEKTSV4RRFFQ69G5FAV" phase={1} />)
  expect(await screen.findByText('本题库未答完，按已答 + 骨架生成，请人审。')).toBeInTheDocument()
  await user.click(screen.getByLabelText('覆盖草稿'))
  await user.click(screen.getByRole('button', { name: '生成本阶段交付物' }))
  expect(generate).toHaveBeenCalledWith({
    projectId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    phase: 1,
    overwriteDrafts: true,
  })
})

it('lists asset-module names and only binds after confirm', async () => {
  const user = userEvent.setup()
  const onBindTemplates = vi.fn().mockResolvedValue(undefined)
  render(
    <PhaseGenerateBar
      projectId="01ARZ3NDEKTSV4RRFFQ69G5FAV"
      phase={1}
      docs={[{ key: 'biz_req_analysis', title: '业务需求分析报告' }]}
      items={[]}
      templatesByDoc={new Map([['biz_req_analysis', [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FA2', name: '商场需求分析', updatedAt: '2026-09-01T00:00:00Z' }]]])}
      onBindTemplates={onBindTemplates}
    />,
  )
  await user.click(await screen.findByRole('button', { name: '套用资产模块' }))
  expect(await screen.findAllByText(/商场需求分析/)).not.toHaveLength(0)
  expect(onBindTemplates).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: '确认套用' }))
  expect(onBindTemplates).toHaveBeenCalledWith([
    { key: 'biz_req_analysis', title: '业务需求分析报告', templateId: '01ARZ3NDEKTSV4RRFFQ69G5FA2', name: '商场需求分析' },
  ])
})
