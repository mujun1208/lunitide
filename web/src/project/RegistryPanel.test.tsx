import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import type { ProjectDTO } from '../generated/bridge'
import { RegistryPanel } from './RegistryPanel'

afterEach(cleanup)

const project = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: 'Mall',
  projectCode: 'ITM00001',
  type: 'implementation',
  status: 'in_progress',
  version: 1,
} as ProjectDTO

it('does not wait for the development checklist before opening the registry', () => {
  render(<RegistryPanel project={project} phase={3} registryReady />)
  expect(screen.getByRole('tab', { name: '数据库' })).toBeInTheDocument()
  expect(screen.queryByText('需先完成开发阶段规范与检查清单')).not.toBeInTheDocument()
})

it('asks to materialize the tree when the registry is locked', () => {
  render(<RegistryPanel project={project} phase={3} registryReady={false} />)
  expect(screen.getByText('请先在需求阶段确认并物化目录，再配置数据库与接口。')).toBeInTheDocument()
  expect(screen.queryByText('前往开发阶段')).not.toBeInTheDocument()
})
