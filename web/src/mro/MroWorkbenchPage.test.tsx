import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { LanguageProvider } from '../i18n/language'
import { MroWorkbenchPage, manualMediaType } from './MroWorkbenchPage'

afterEach(cleanup)

it('maps manual filenames to the ingest media hint', () => {
  expect(manualMediaType('amm.pdf')).toBe('application/pdf')
  expect(manualMediaType('amm.docx')).toContain('wordprocessingml')
  expect(manualMediaType('ipc.xlsx')).toContain('spreadsheetml')
  expect(manualMediaType('deck.pptx')).toContain('presentationml')
  expect(manualMediaType('notes.md')).toBe('text/markdown')
  expect(manualMediaType('notes.TXT')).toBe('text/plain')
  expect(manualMediaType('scan.tiff')).toBeUndefined()
})

function fileWithPath(name: string, path: string, body = '# ATA 32\n\nGear retraction fault isolation.'): File {
  const file = new File([body], name, { type: name.endsWith('.md') ? 'text/markdown' : 'text/plain' })
  Object.defineProperty(file, 'path', { value: path })
  return file
}

it('shows the empty-state copy and advisory header', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        mroExpertId="01ARZ3NDEKTSV4RRFFQ69G5FAX"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
      />
    </LanguageProvider>,
  )
  expect(await screen.findByRole('heading', { name: '机务工作台' })).toBeInTheDocument()
  expect(screen.getByText('辅助建议，不构成放行')).toBeInTheDocument()
  expect(screen.getByText('从一本手册或一个机尾开始')).toBeInTheDocument()
  expect(screen.getByText('工作台只做适用性与引用。放行仍由持证人员做出。')).toBeInTheDocument()
  expect(screen.queryByText('先建机尾或先导入一本手册')).not.toBeInTheDocument()
  expect(screen.getByRole('button', { name: '导入手册' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '登记机尾' })).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '问月汐' })).toHaveClass('primary')
})

it('binds a probed connection to the stock table without a DSN field', async () => {
  const onBindStock = vi.fn().mockResolvedValue(undefined)
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="more"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        verifiedConnections={async () => [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: '航司只读副本', kind: 'postgres' }]}
        onBindStock={onBindStock}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '数据源' }))
  expect(await screen.findByLabelText('连接')).toBeInTheDocument()
  expect(screen.queryByText('先在设置 → 数据源探测连接')).not.toBeInTheDocument()
  expect(screen.queryByText(/postgres:\/\//)).not.toBeInTheDocument()
  fireEvent.change(screen.getByLabelText('连接'), { target: { value: '01ARZ3NDEKTSV4RRFFQ69G5FAV' } })
  fireEvent.change(screen.getByLabelText('schema'), { target: { value: 'inv' } })
  fireEvent.change(screen.getByLabelText('表'), { target: { value: 'stock' } })
  fireEvent.change(screen.getByLabelText('PN'), { target: { value: 'part_no' } })
  fireEvent.click(screen.getByRole('button', { name: '绑定库存' }))
  expect(onBindStock).toHaveBeenCalledWith({
    connectionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    tableMap: { schema: 'inv', table: 'stock', pnColumn: 'part_no' },
  })
})

it('keeps the fault page as a real symptom workspace', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="fault"
        aircraftList={async () => ({ items: [{ aircraftId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', tailNo: 'B-0000', msn: '', model: 'A320', config: '' }] })}
        manualList={async () => ({ items: [] })}
      />
    </LanguageProvider>,
  )
  expect(await screen.findByText('症状')).toBeInTheDocument()
  expect(screen.getByText(/当前机尾 B-0000/)).toBeInTheDocument()
  expect(screen.getByText(/症状 → 候选故障 → 原因 → 任务 → 件号/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: '问月汐' })).toHaveClass('primary')
})

it('asks the user to enable the aviation expert first', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled={false} />
    </LanguageProvider>,
  )
  expect(await screen.findByText('先启用航空机务维修专家')).toBeInTheDocument()
  expect(screen.queryByText('从一本手册或一个机尾开始')).not.toBeInTheDocument()
})

it('keeps more-rail pages as honest empty states', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="more"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
      />
    </LanguageProvider>,
  )
  expect(await screen.findByText('在带受控引用的机务回答下方，点击「下载检查单 JSON」。')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '数据源' }))
  expect(await screen.findByText('先在设置 → 数据源探测连接')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '审计' }))
  expect(screen.getByText('还没有可回放的机务审计。')).toBeInTheDocument()
})

it('imports a manual by ingesting the file then registering the real document', async () => {
  const onIngestManual = vi.fn().mockResolvedValue({
    collectionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    documents: [{ documentId: '01ARZ3NDEKTSV4RRFFQ69G5FCC', version: 1, indexState: 'ready', preview: ['ATA 32 gear retraction fault isolation.'] }],
  })
  const onRegisterManual = vi.fn().mockResolvedValue({
    manualId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', title: 'AMM', docType: 'AMM', revision: '42', status: 'controlled', ata: '32', sectionCount: 1,
  })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled mroExpertId="01ARZ3NDEKTSV4RRFFQ69G5FAX" aircraftList={async () => ({ items: [] })} manualList={async () => ({ items: [] })} onIngestManual={onIngestManual} onRegisterManual={onRegisterManual} />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '导入手册' }))
  fireEvent.change(screen.getByLabelText('手册文件'), { target: { files: [fileWithPath('amm.md', '/ws/amm.md')] } })
  fireEvent.change(screen.getByLabelText('修订'), { target: { value: '42' } })
  fireEvent.change(screen.getByLabelText('ATA'), { target: { value: '32' } })
  fireEvent.change(screen.getByLabelText('机尾时效'), { target: { value: 'B-1234' } })
  fireEvent.click(screen.getByRole('button', { name: '导入' }))
  await waitFor(() => expect(onIngestManual).toHaveBeenCalledWith(expect.objectContaining({
    expertId: '01ARZ3NDEKTSV4RRFFQ69G5FAX', path: '/ws/amm.md',
    sourceLocator: expect.stringContaining('mro://AMM/42'),
  })))
  const locator = onIngestManual.mock.calls[0][0].sourceLocator as string
  expect(locator).toContain('status=controlled')
  expect(locator).toContain('ata=32')
  expect(locator).toContain('tail=B-1234')
  await waitFor(() => expect(onRegisterManual).toHaveBeenCalledWith(expect.objectContaining({
    documents: [{ documentId: '01ARZ3NDEKTSV4RRFFQ69G5FCC', partNo: 1 }],
  })))
  expect(await screen.findByText('ATA 32 gear retraction fault isolation.')).toBeInTheDocument()
})

it('confirms before ingesting an uncontrolled manual and stamps status in the locator', async () => {
  const onIngestManual = vi.fn().mockResolvedValue({
    collectionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    documents: [{ documentId: '01ARZ3NDEKTSV4RRFFQ69G5FBB', version: 1, indexState: 'ready' }],
  })
  const onRegisterManual = vi.fn().mockResolvedValue({
    manualId: '01ARZ3NDEKTSV4RRFFQ69G5FAV', title: 'AMM draft', docType: 'AMM', revision: '42', status: 'uncontrolled', ata: '32', sectionCount: 1,
  })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled mroExpertId="01ARZ3NDEKTSV4RRFFQ69G5FAX" aircraftList={async () => ({ items: [] })} manualList={async () => ({ items: [] })} onIngestManual={onIngestManual} onRegisterManual={onRegisterManual} />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '导入手册' }))
  fireEvent.change(screen.getByLabelText('手册文件'), { target: { files: [fileWithPath('amm.md', '/ws/amm.md')] } })
  fireEvent.change(screen.getByLabelText('修订'), { target: { value: '42' } })
  fireEvent.change(screen.getByLabelText('受控状态'), { target: { value: 'uncontrolled' } })
  fireEvent.click(screen.getByRole('button', { name: '导入' }))
  expect(onIngestManual).not.toHaveBeenCalled()
  expect(onRegisterManual).not.toHaveBeenCalled()
  expect(screen.getByText('未受控手册仅供参考，回答前会再次确认')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '继续导入' }))
  await waitFor(() => expect(onIngestManual).toHaveBeenCalledWith(expect.objectContaining({ sourceLocator: expect.stringContaining('status=uncontrolled') })))
  await waitFor(() => expect(onRegisterManual).toHaveBeenCalledWith(expect.objectContaining({ status: 'uncontrolled', revision: '42' })))
})

it('shows the parse failure reason and does not register on a failed ingest', async () => {
  const onIngestManual = vi.fn().mockResolvedValue({
    collectionId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
    documents: [{ documentId: '01ARZ3NDEKTSV4RRFFQ69G5FDD', version: 1, indexState: 'failed', failReason: '无法抽出正文：parse function not configured' }],
  })
  const onRegisterManual = vi.fn()
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage enabled mroExpertId="01ARZ3NDEKTSV4RRFFQ69G5FAX" aircraftList={async () => ({ items: [] })} manualList={async () => ({ items: [] })} onIngestManual={onIngestManual} onRegisterManual={onRegisterManual} />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '导入手册' }))
  fireEvent.change(screen.getByLabelText('手册文件'), { target: { files: [fileWithPath('scan.pdf', '/ws/scan.pdf', 'binary')] } })
  fireEvent.change(screen.getByLabelText('修订'), { target: { value: '42' } })
  fireEvent.click(screen.getByRole('button', { name: '导入' }))
  await waitFor(() => expect(onIngestManual).toHaveBeenCalled())
  expect(await screen.findByText(/parse function not configured/)).toBeInTheDocument()
  expect(onRegisterManual).not.toHaveBeenCalled()
})

it('downloads cited checklist JSON and drops nothing silently when cites exist', async () => {
  const onBuildChecklist = vi.fn().mockResolvedValue({
    banner: '辅助建议，不构成放行',
    steps: [{ n: 1, text: '隔离液压', revision: '42', ata: '32' }],
  })
  const createObjectURL = vi.fn().mockReturnValue('blob:checklist')
  const revoke = vi.fn()
  vi.stubGlobal('URL', { createObjectURL, revokeObjectURL: revoke })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => undefined)
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="more"
        lastCites={[{ revision: '42', ata: '32' }]}
        onBuildChecklist={onBuildChecklist}
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
      />
    </LanguageProvider>,
  )
  fireEvent.change(await screen.findByLabelText('检查单步骤'), { target: { value: '隔离液压\n无引用步骤' } })
  fireEvent.click(screen.getByRole('button', { name: '下载 JSON' }))
  await waitFor(() => expect(onBuildChecklist).toHaveBeenCalledWith({
    steps: ['隔离液压', '无引用步骤'],
    cites: [{ revision: '42', ata: '32' }],
  }))
  expect(createObjectURL).toHaveBeenCalled()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

it('replays real audit rows instead of a stub table', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="more"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        auditList={async () => ({ items: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', action: 'kb.document.upsert', resourceType: 'kb_document', resourceId: '01ARZ3NDEKTSV4RRFFQ69G5FAW', createdAt: '2026-09-03T00:00:00Z' }] })}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '审计' }))
  expect(await screen.findByText('kb.document.upsert')).toBeInTheDocument()
  expect(screen.queryByText('还没有可回放的机务审计。')).not.toBeInTheDocument()
})
