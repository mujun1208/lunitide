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
        initialRail="parts"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        verifiedConnections={async () => [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', name: '航司只读副本', kind: 'postgres' }]}
        onBindStock={onBindStock}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: '库存来源' }))
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
  expect(await screen.findByText('先启用至少一位机务同事专家')).toBeInTheDocument()
  expect(screen.queryByText('从一本手册或一个机尾开始')).not.toBeInTheDocument()
})

it('promotes the six domains to first-class nav and keeps honest empty states', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="checklist"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
      />
    </LanguageProvider>,
  )
  // Utilities group keeps checklist/audit; the four ops domains are first-class now.
  expect(await screen.findByText('在带受控引用的机务回答下方，点击「下载检查单 JSON」。')).toBeInTheDocument()
  for (const name of ['手册', '排故', '到期', '工具化工品', '航材', '计划', '检查单', '审计', '机队']) {
    expect(screen.getByRole('button', { name })).toBeInTheDocument()
  }
  fireEvent.click(screen.getByRole('button', { name: '审计' }))
  expect(screen.getByText('还没有可回放的机务审计。')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '到期' }))
  expect(screen.getByText(/还没有到期项/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '工具化工品' }))
  expect(screen.getByText(/还没有工具台账/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '航材' }))
  expect(screen.getByText(/还没有本机航材台账/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '计划' }))
  expect(screen.getByText(/还没有工作包/)).toBeInTheDocument()
})

it('exposes domain sub-tabs for due, tools, parts and plan', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="parts"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
      />
    </LanguageProvider>,
  )
  for (const name of ['库存', '替代件', 'AOG', '采购草稿', '库存来源']) {
    expect(await screen.findByRole('tab', { name })).toBeInTheDocument()
  }
  fireEvent.click(screen.getByRole('button', { name: '计划' }))
  for (const name of ['工作包', '间隔规则', '窗口与约束']) {
    expect(screen.getByRole('tab', { name })).toBeInTheDocument()
  }
})

it('renders due missing utilization as 未录入 and blocks overdue checkout', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="due"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        dueItems={[{ id: 'd1', kind: 'FH', usedMissing: true, limitValue: 100, state: 'missing', label: '未录入' }]}
        tools={[{ id: 't1', toolNo: 'TW-1', sn: 'SN1', location: 'bay', holder: '', calibDue: '2020-01-01', status: 'overdue', checkoutBlocked: '校准过期' }]}
      />
    </LanguageProvider>,
  )
  expect((await screen.findAllByText('未录入')).length).toBeGreaterThanOrEqual(1)
  fireEvent.click(screen.getByRole('button', { name: '工具化工品' }))
  expect(screen.getByRole('button', { name: '借出' })).toBeDisabled()
  expect(screen.getByRole('button', { name: '借出' })).toHaveAttribute('title', '校准过期')
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

it('shows publish todos and opens bulletin Ask with lot and two experts', async () => {
  const onPublishSchedule = vi.fn().mockResolvedValue({
    todos: [
      { id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', kind: 'kit_staging', ref: 'wp1', status: 'open' },
      { id: '01ARZ3NDEKTSV4RRFFQ69G5FAB', kind: 'parts_request', ref: 'wp1', status: 'open' },
    ],
  })
  const onBulletinChain = vi.fn().mockResolvedValue({ tails: ['B-9'], note: 'draft' })
  const openChat = vi.fn().mockResolvedValue({ sessionId: 's1', project: {}, session: {}, prompt: '' })
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="plan"
        mroExpertId="01ARZ3NDEKTSV4RRFFQ69G5FAX"
        opsExpertIds={{
          'tooling-chemical-expert': '01ARZ3NDEKTSV4RRFFQ69G5FAC',
          'uas-airworthiness-expert': '01ARZ3NDEKTSV4RRFFQ69G5FAD',
        }}
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        workPackages={[{ id: 'wp1', title: 'C检草稿', sources: ['标准卡', 'AD/SB', 'MEL', '未关闭项'] }]}
        lots={[{ id: 'lot1', lotNo: 'M-1', qty: 1, expires: '2027-01-01', tails: ['B-9'] }]}
        onPublishSchedule={onPublishSchedule}
        onBulletinChain={onBulletinChain}
        openChat={openChat}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '发布' }))
  await waitFor(() => expect(onPublishSchedule).toHaveBeenCalledWith('wp1'))
  fireEvent.click(screen.getByRole('button', { name: '工具化工品' }))
  expect(await screen.findByText(/套件待办/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '航材' }))
  expect(await screen.findByText(/航材待办/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '工具化工品' }))
  fireEvent.click(screen.getByRole('tab', { name: '批次' }))
  fireEvent.click(screen.getByRole('button', { name: '质量通报串查' }))
  await waitFor(() => expect(onBulletinChain).toHaveBeenCalledWith('lot1'))
  await waitFor(() => expect(openChat).toHaveBeenCalledWith(expect.objectContaining({
    mroExpertId: '01ARZ3NDEKTSV4RRFFQ69G5FAC',
    extraExpertIds: ['01ARZ3NDEKTSV4RRFFQ69G5FAD'],
    prompt: expect.stringContaining('M-1'),
    context: expect.objectContaining({ lot: 'M-1', scenario: 'tools' }),
  })))
  vi.restoreAllMocks()
})

it('persists kit shortage as a parts todo through the write path', async () => {
  const onAddPartsTodo = vi.fn().mockResolvedValue({ ok: true, id: '01ARZ3NDEKTSV4RRFFQ69G5FAA' })
  const todoList = vi.fn().mockResolvedValue({ items: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAA', kind: 'parts_request', ref: 'kit1', status: 'open', detail: 'SEAL-1' }] })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="tools"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        kits={[{ id: 'kit1', name: 'C检套件', missing: ['SEAL-1'] }]}
        onAddPartsTodo={onAddPartsTodo}
        todoList={todoList}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: '套件' }))
  fireEvent.click(screen.getByRole('button', { name: '转航材待办' }))
  await waitFor(() => expect(onAddPartsTodo).toHaveBeenCalledWith({ kitId: 'kit1', detail: 'SEAL-1' }))
  fireEvent.click(screen.getByRole('button', { name: '航材' }))
  expect(await screen.findByText(/航材待办/)).toBeInTheDocument()
})

it('confirms a PIREP draft from the due rail', async () => {
  const onConfirmPirep = vi.fn().mockResolvedValue({ ok: true })
  const pirepListFn = vi.fn()
    .mockResolvedValueOnce({ items: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', tailNo: 'B-1', body: 'vibration', state: 'draft', createdAt: '2026-09-04' }] })
    .mockResolvedValue({ items: [{ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', tailNo: 'B-1', body: 'vibration', state: 'confirmed', createdAt: '2026-09-04' }] })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="due"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        pirepListFn={pirepListFn}
        onConfirmPirep={onConfirmPirep}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: 'PIREP' }))
  fireEvent.click(await screen.findByRole('button', { name: '确认转缺陷' }))
  await waitFor(() => expect(onConfirmPirep).toHaveBeenCalledWith({ id: '01ARZ3NDEKTSV4RRFFQ69G5FAV', state: 'confirmed' }))
})

it('turns kit shortage into a parts todo', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="tools"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        kits={[{ id: 'kit1', name: 'C检套件', missing: ['SEAL-1'] }]}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: '套件' }))
  fireEvent.click(screen.getByRole('button', { name: '转航材待办' }))
  fireEvent.click(screen.getByRole('button', { name: '航材' }))
  expect(await screen.findByText(/航材待办/)).toBeInTheDocument()
  expect(screen.getByText(/kit1/)).toBeInTheDocument()
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

it('adds a tool through the inline quick form', async () => {
  const onAddTool = vi.fn().mockResolvedValue({ ok: true })
  const toolList = vi.fn().mockResolvedValue({ items: [] })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="tools"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        onAddTool={onAddTool}
        toolList={toolList}
      />
    </LanguageProvider>,
  )
  fireEvent.click((await screen.findAllByRole('button', { name: '登记工具' }))[0])
  fireEvent.change(await screen.findByLabelText('工具号'), { target: { value: 'TW-9' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(onAddTool).toHaveBeenCalledWith(expect.objectContaining({ toolNo: 'TW-9' })))
})

it('records utilization and refreshes due rows from the returned recompute', async () => {
  const onRecordUtil = vi.fn().mockResolvedValue({ items: [{ id: 'd1', kind: 'FH', usedValue: 10, state: 'ok', label: '正常' }] })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="due"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        dueItems={[]}
        onRecordUtil={onRecordUtil}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('button', { name: '录利用率' }))
  fireEvent.change(await screen.findByLabelText('范围 ID'), { target: { value: 'B-1' } })
  fireEvent.change(screen.getByLabelText('小时'), { target: { value: '10' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(onRecordUtil).toHaveBeenCalledWith(expect.objectContaining({ scopeId: 'B-1', hours: 10 })))
})

it('intakes an AOG paste into a draft case without auto-purchasing', async () => {
  const onIntakeAog = vi.fn().mockResolvedValue({ ok: true })
  const aogListFn = vi.fn().mockResolvedValue({ items: [] })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="parts"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        onIntakeAog={onIntakeAog}
        aogListFn={aogListFn}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: 'AOG' }))
  fireEvent.click(screen.getAllByRole('button', { name: 'AOG 抽取' })[0])
  fireEvent.change(await screen.findByLabelText('粘贴文本'), { target: { value: 'B-1234 AOG PN 3G2000-1 数量2' } })
  expect(await screen.findByText(/预览 · 机尾 B-1234 · PN 3G2000-1 · 数量 2/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '确认入案' }))
  await waitFor(() => expect(onIntakeAog).toHaveBeenCalledWith({ text: 'B-1234 AOG PN 3G2000-1 数量2' }))
})

it('lists interval rules and adds a new one from the plan rail', async () => {
  const intervalListFn = vi.fn().mockResolvedValue({ items: [{ taskKey: 'MLG-DET', intervalValue: 500, unit: 'FH', version: '2', sourceCite: 'MPD 32-11' }] })
  const onAddInterval = vi.fn().mockResolvedValue({ ok: true })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="plan"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        intervalListFn={intervalListFn}
        onAddInterval={onAddInterval}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: '间隔规则' }))
  expect(await screen.findByText('MLG-DET')).toBeInTheDocument()
  expect(screen.getByText('2')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '登记间隔' }))
  fireEvent.change(await screen.findByLabelText('任务号'), { target: { value: 'NLG-INSP' } })
  fireEvent.change(screen.getByLabelText('间隔值'), { target: { value: '750' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(onAddInterval).toHaveBeenCalledWith(expect.objectContaining({ taskKey: 'NLG-INSP', intervalValue: 750, unit: 'FH' })))
})

it('resolves component genealogy and registers a new component', async () => {
  const componentListFn = vi.fn().mockResolvedValue({ items: [{ id: 'c1', sn: 'SN-1', pn: 'PN-1', lifeCount: 3, installed: true, events: [{ kind: 'install', occurredAt: '2026-01-01', note: '' }] }] })
  const onAddComponent = vi.fn().mockResolvedValue({ ok: true })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="due"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        componentListFn={componentListFn}
        onAddComponent={onAddComponent}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: '部件履历' }))
  expect(await screen.findByText('SN-1')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '登记部件' }))
  fireEvent.change(await screen.findByLabelText('SN'), { target: { value: 'SN-9' } })
  fireEvent.change(screen.getByLabelText('PN'), { target: { value: 'PN-9' } })
  fireEvent.click(screen.getByRole('button', { name: '保存' }))
  await waitFor(() => expect(onAddComponent).toHaveBeenCalledWith(expect.objectContaining({ sn: 'SN-9', pn: 'PN-9' })))
})

it('badges every C1–C7 constraint and flags the violated ones', async () => {
  const constraintList = vi.fn().mockResolvedValue({ violations: [{ code: 'C2', detail: '技能组工时超出 avionic' }] })
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="plan"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        constraintList={constraintList}
      />
    </LanguageProvider>,
  )
  fireEvent.click(await screen.findByRole('tab', { name: '窗口与约束' }))
  // The full C1–C7 legend renders; only the violated code repeats in the detail list.
  expect(await screen.findByText('C1')).toBeInTheDocument()
  expect(screen.getByText('C7')).toBeInTheDocument()
  expect(await screen.findByText('技能组工时超出 avionic')).toBeInTheDocument()
  expect(screen.getAllByText('C2').length).toBeGreaterThanOrEqual(2)
})

it('filters calendar dues by the as-of date and badges expired lots', async () => {
  render(
    <LanguageProvider value="zh-CN">
      <MroWorkbenchPage
        enabled
        initialRail="due"
        aircraftList={async () => ({ items: [] })}
        manualList={async () => ({ items: [] })}
        dueItems={[
          { id: 'd1', kind: 'CAL', scope: 'B-1', dueAt: '2026-09-01', state: 'overdue', label: 'overdue' },
          { id: 'd2', kind: 'CAL', scope: 'B-1', dueAt: '2026-12-01', state: 'ok', label: 'ok' },
        ]}
        lots={[{ id: 'lot1', lotNo: 'M-EXP', qty: 1, expires: '2026-01-01', tails: [] }]}
      />
    </LanguageProvider>,
  )
  fireEvent.change(await screen.findByLabelText('日期'), { target: { value: '2026-09-04' } })
  expect(await screen.findByText('2026-09-01')).toBeInTheDocument()
  expect(screen.queryByText('2026-12-01')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '工具化工品' }))
  fireEvent.click(await screen.findByRole('tab', { name: '批次' }))
  expect(await screen.findByText(/2026-01-01/)).toBeInTheDocument()
  expect(screen.getByText(/过期/)).toBeInTheDocument()
})
