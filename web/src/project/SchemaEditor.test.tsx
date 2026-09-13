import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import type { ProjectDTO } from '../generated/bridge'
import { SchemaEditor } from './SchemaEditor'
import { projectFactoryApi } from './projectFactoryApi'

vi.mock('./projectFactoryApi', () => ({
  projectFactoryApi: {
    schemaGet: vi.fn(),
    schemaPut: vi.fn(),
    schemaMaterialize: vi.fn(),
    schemaVerify: vi.fn(),
  },
}))

afterEach(cleanup)

const project = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: '商场',
  projectCode: 'ITM00001',
  type: 'implementation',
  status: 'in_progress',
  dbStatus: 'none',
  version: 1,
} as ProjectDTO

it('blocks save when the schema has no tables', async () => {
  vi.mocked(projectFactoryApi.schemaGet).mockResolvedValue({
    schema: { version: 1, dialect: 'sqlite', tables: [] },
    dbPath: '',
    dbStatus: 'none',
  })
  render(<SchemaEditor project={project} />)
  expect(await screen.findByRole('status')).toHaveTextContent('至少需要一张表才能保存或核齐')
  expect(screen.getByRole('button', { name: '保存模式' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '物化建表' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '核齐库表' })).toBeDisabled()
})
