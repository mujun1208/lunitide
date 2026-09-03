import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { DataSourcePanel, type DatasourcePanelApi, type DatasourceRow } from './DataSourcePanel'
import { composeDatasourceDsn } from './datasourceDsn'

afterEach(cleanup)

const row = (o: Partial<DatasourceRow> = {}): DatasourceRow => ({
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  name: '航司只读副本',
  kind: 'postgres',
  state: 'active',
  readonlyVerified: false,
  createdAt: '2026-09-03T00:00:00Z',
  ...o,
})

const api = (o: Partial<DatasourcePanelApi> = {}): DatasourcePanelApi => ({
  list: vi.fn().mockResolvedValue({ items: [row()] }),
  create: vi.fn().mockResolvedValue(row({ readonlyVerified: false })),
  probe: vi.fn().mockResolvedValue({ id: row().id, readonlyVerified: true }),
  browse: vi.fn().mockResolvedValue({ items: [{ name: 'inv' }] }),
  disable: vi.fn().mockResolvedValue({ id: row().id, state: 'disabled' as const }),
  ...o,
})

it('lists connections without echoing dsn or password', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <DataSourcePanel api={api()} />
    </LanguageProvider>,
  )
  expect(await screen.findByText('航司只读副本')).toBeInTheDocument()
  expect(screen.getByText('未探测')).toBeInTheDocument()
  expect(screen.queryByText('不可查询')).not.toBeInTheDocument()
  expect(screen.queryByDisplayValue(/postgres:\/\//)).not.toBeInTheDocument()
  expect(screen.queryByText(/s3cret/)).not.toBeInTheDocument()
  expect(screen.getByText(/自动创建/)).toBeInTheDocument()
})

it('shows 不可查询 for disabled rows and never renders a dsn field after save', async () => {
  const create = vi.fn().mockResolvedValue(row())
  render(
    <LanguageProvider value="zh-CN">
      <DataSourcePanel api={api({
        list: vi.fn()
          .mockResolvedValueOnce({ items: [row({ state: 'disabled', readonlyVerified: false, name: '实验库' })] })
          .mockResolvedValue({ items: [row()] }),
        create,
      })} />
    </LanguageProvider>,
  )
  expect(await screen.findByText('不可查询')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '添加 PostgreSQL' }))
  fireEvent.change(screen.getByLabelText('名称'), { target: { value: 'lab' } })
  fireEvent.change(screen.getByLabelText('主机'), { target: { value: 'db.internal' } })
  fireEvent.change(screen.getByLabelText('用户'), { target: { value: 'ro' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: 's3cret' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(create).toHaveBeenCalled())
  const payload = create.mock.calls[0][0] as { dsn: string }
  expect(payload.dsn).toContain('s3cret')
  expect(screen.queryByDisplayValue('s3cret')).not.toBeInTheDocument()
  expect(await screen.findByText('已保存 · 不回显')).toBeInTheDocument()
})

it('composes postgres dsn without putting it in list copy', () => {
  const dsn = composeDatasourceDsn('postgres', {
    host: 'db.internal', port: '5432', database: 'ops', user: 'ro', password: 's3cret', ssl: true,
  })
  expect(dsn).toBe('postgres://ro:s3cret@db.internal:5432/ops?sslmode=require')
})

it('adds a local MySQL source from only user and password with the fixed database', async () => {
  const create = vi.fn().mockResolvedValue(row({ kind: 'mysql' }))
  render(
    <LanguageProvider value="zh-CN">
      <DataSourcePanel api={api({ create })} />
    </LanguageProvider>,
  )
  await screen.findByText('航司只读副本')
  fireEvent.click(screen.getByRole('button', { name: '添加 MySQL' }))
  fireEvent.change(screen.getByLabelText('用户'), { target: { value: 'root' } })
  fireEvent.change(screen.getByLabelText('密码'), { target: { value: '128128' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(create).toHaveBeenCalled())
  const payload = create.mock.calls[0][0] as { name: string; kind: string; dsn: string }
  expect(payload.kind).toBe('mysql')
  expect(payload.dsn).toContain('@tcp(127.0.0.1:3306)/lunitide')
  expect(payload.dsn).toContain('tls=preferred')
  expect(payload.dsn).toContain('allowPublicKeyRetrieval=true')
  expect(payload.name).toBe('lunitide@127.0.0.1')
})
