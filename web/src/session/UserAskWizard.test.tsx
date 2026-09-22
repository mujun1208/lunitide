import {cleanup, render, screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {afterEach, expect, it, vi} from 'vitest'
import {UserAskWizard} from './UserAskWizard'
import type {UserAskPack} from './userAsk'

afterEach(cleanup)

const pack: UserAskPack = {
  title: '需求边界',
  questions: [
    {
      id: 'deploy',
      prompt: '部署方式',
      options: [
        {id: 'k8s', label: '容器化'},
        {id: 'vm', label: '虚拟机'},
      ],
    },
    {
      id: 'db',
      prompt: '数据库',
      options: [
        {id: 'pg', label: 'PostgreSQL'},
        {id: 'mysql', label: 'MySQL'},
      ],
    },
  ],
}

it('walks questions one at a time, lets the user go back, and submits once', async () => {
  const user = userEvent.setup()
  const onSubmit = vi.fn()
  render(<UserAskWizard pack={pack} onSubmit={onSubmit} />)
  expect(screen.getByText('问题 1 / 2')).toBeInTheDocument()
  expect(screen.queryByText('数据库')).toBeNull()
  await user.click(screen.getByRole('radio', {name: /容器化/}))
  await user.click(screen.getByRole('button', {name: '下一题'}))
  expect(screen.getByText('问题 2 / 2')).toBeInTheDocument()
  await user.click(screen.getByRole('button', {name: '上一步'}))
  expect(screen.getByRole('radio', {name: /容器化/})).toHaveAttribute('aria-checked', 'true')
  await user.click(screen.getByRole('button', {name: '下一题'}))
  await user.click(screen.getByRole('radio', {name: /其他/}))
  await user.type(screen.getByLabelText('其他选项说明'), '已有 TiDB')
  await user.click(screen.getByRole('button', {name: '提交决策'}))
  expect(onSubmit).toHaveBeenCalledOnce()
  expect(onSubmit.mock.calls[0][0]).toContain('1. 部署方式：容器化')
  expect(onSubmit.mock.calls[0][0]).toContain('2. 数据库：其他 — 已有 TiDB')
})

it('preselects the recommended option and keeps the other field', async () => {
  const user = userEvent.setup()
  const onSubmit = vi.fn()
  const decided: UserAskPack = {
    title: '交付方式',
    questions: [{
      id: 'ship',
      prompt: '这份周报怎么交',
      options: [
        {id: 'mail', label: '发邮件', detail: '今晚就能送到'},
        {id: 'deck', label: '做成演示', recommended: true, detail: '适合会上讲'},
      ],
    }],
  }
  render(<UserAskWizard pack={decided} onSubmit={onSubmit} />)
  expect(screen.getByText('推荐')).toBeInTheDocument()
  expect(screen.getByText('适合会上讲')).toBeInTheDocument()
  expect(screen.getByRole('radio', {name: /做成演示/})).toHaveAttribute('aria-checked', 'true')
  await user.click(screen.getByRole('button', {name: '提交决策'}))
  expect(onSubmit).toHaveBeenCalledOnce()
  expect(onSubmit.mock.calls[0][0]).toContain('1. 这份周报怎么交：做成演示（推荐）')
})
