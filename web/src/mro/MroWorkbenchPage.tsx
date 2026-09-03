import React, { useEffect, useMemo, useState } from 'react'
import { ConfirmDialog, Dialog } from '../ui/Dialog'
import { useZh } from '../i18n/language'
import { MroAskButton, openMroChat, type MroChatOpened } from './MroAskButton'
import { parseMroContext, type MroSessionContext } from './mroContext'

export type MroAircraft = { aircraftId: string; tailNo: string; msn: string; model: string; config: string }
export type MroManual = {
  manualId: string
  title: string
  docType: string
  revision: string
  status: string
  ata: string
  sectionCount: number
}

type Rail = 'manuals' | 'fault' | 'more'

export type StockBindInput = {
  connectionId: string
  tableMap: { schema: string; table: string; pnColumn: string; stationColumn?: string; qtyColumn?: string }
}

export type ChecklistCite = { revision?: string; ata?: string; locator?: string; quote?: string; expertName?: string }
export type ChecklistBuilt = { banner: string; steps: Array<{ n: number; text: string; revision: string; ata?: string; expertName?: string }> }
export type ManualRegisterInput = {
  title?: string
  docType: string
  revision: string
  status: string
  ata?: string
  documents: Array<{ documentId: string; partNo: number }>
}
export type ManualIngestResult = {
  collectionId: string
  documents: Array<{ documentId: string; version: number; indexState: string; preview?: string[]; failReason?: string }>
}
export type AuditRow = { id: string; action: string; resourceType: string; resourceId: string; createdAt: string }

// manualMediaType maps a manual filename to the media hint the ingest bridge
// expects. The backend re-detects by extension and magic bytes, so this is a
// coherent hint rather than the source of truth. Unknown extensions send no
// hint and let the backend sniff.
const MANUAL_MEDIA: Record<string, string> = {
  md: 'text/markdown',
  markdown: 'text/markdown',
  txt: 'text/plain',
  pdf: 'application/pdf',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}
export function manualMediaType(name: string): string | undefined {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  return MANUAL_MEDIA[ext]
}

export function MroWorkbenchPage({
  enabled, mroExpertId, initialRail = 'manuals',
  aircraftList, manualList, onUpsertAircraft, onAskOpened, openChat,
  verifiedConnections, existingStock, onBindStock,
  lastCites, onBuildChecklist, onRegisterManual, onIngestManual, auditList,
}: {
  enabled: boolean
  mroExpertId?: string
  initialRail?: Rail
  aircraftList?: () => Promise<{ items: MroAircraft[] }>
  manualList?: () => Promise<{ items: MroManual[] }>
  onUpsertAircraft?: (input: { tailNo: string; msn: string; model: string; config: string }) => Promise<MroAircraft>
  onAskOpened?: (opened: MroChatOpened) => void
  openChat?: typeof openMroChat
  verifiedConnections?: () => Promise<Array<{ id: string; name: string; kind: string }>>
  existingStock?: () => Promise<{ connectionId: string; tableMapJson: string } | null>
  onBindStock?: (input: StockBindInput) => Promise<void>
  lastCites?: ChecklistCite[]
  onBuildChecklist?: (input: { steps: string[]; cites: ChecklistCite[] }) => Promise<ChecklistBuilt>
  onRegisterManual?: (input: ManualRegisterInput) => Promise<MroManual>
  onIngestManual?: (input: { expertId: string; path: string; sourceLocator: string; mediaType?: string }) => Promise<ManualIngestResult>
  auditList?: () => Promise<{ items: AuditRow[] }>
}): React.JSX.Element {
  const zh = useZh()
  const [rail, setRail] = useState<Rail>(initialRail)
  const [moreOpen, setMoreOpen] = useState(initialRail === 'more')
  const [morePage, setMorePage] = useState<'checklist' | 'fleet' | 'datasource' | 'audit'>('checklist')
  const [aircraft, setAircraft] = useState<MroAircraft[]>([])
  const [manuals, setManuals] = useState<MroManual[]>([])
  const [tailNo, setTailNo] = useState('')
  const [asOf, setAsOf] = useState(() => new Date().toISOString().slice(0, 10))
  const [manualId, setManualId] = useState('')
  const [symptoms, setSymptoms] = useState('')
  const [registerOpen, setRegisterOpen] = useState(false)
  const [draft, setDraft] = useState({ tailNo: '', msn: '', model: '', config: '' })
  const [error, setError] = useState('')
  const [verified, setVerified] = useState<Array<{ id: string; name: string; kind: string }>>([])
  const [stock, setStock] = useState({ connectionId: '', schema: '', table: '', pnColumn: '', stationColumn: '', qtyColumn: '' })
  const [bindMsg, setBindMsg] = useState('')
  const [importOpen, setImportOpen] = useState(false)
  const [uncontrolledOpen, setUncontrolledOpen] = useState(false)
  const [manualDraft, setManualDraft] = useState({ title: '', docType: 'AMM', revision: '', status: 'controlled', ata: '', tail: '' })
  const [manualFile, setManualFile] = useState<{ path: string; name: string } | null>(null)
  const [importPreview, setImportPreview] = useState<string[]>([])
  const [checklistDraft, setChecklistDraft] = useState('')
  const [auditItems, setAuditItems] = useState<AuditRow[]>([])

  useEffect(() => {
    let alive = true
    void Promise.all([
      aircraftList?.().catch(() => ({ items: [] as MroAircraft[] })) ?? Promise.resolve({ items: [] as MroAircraft[] }),
      manualList?.().catch(() => ({ items: [] as MroManual[] })) ?? Promise.resolve({ items: [] as MroManual[] }),
    ]).then(([a, m]) => {
      if (!alive) return
      setAircraft(a.items)
      setManuals(m.items)
      if (!tailNo && a.items[0]) setTailNo(a.items[0].tailNo)
      if (!manualId && m.items[0]) setManualId(m.items[0].manualId)
    })
    void Promise.all([
      verifiedConnections?.().catch(() => [] as Array<{ id: string; name: string; kind: string }>) ?? Promise.resolve([] as Array<{ id: string; name: string; kind: string }>),
      existingStock?.().catch(() => null) ?? Promise.resolve(null),
    ]).then(([conns, bound]) => {
      if (!alive) return
      setVerified(conns)
      if (bound) {
        let map: Record<string, string> = {}
        try { map = JSON.parse(bound.tableMapJson) as Record<string, string> } catch { /* ignore */ }
        setStock({
          connectionId: bound.connectionId,
          schema: map.schema ?? '',
          table: map.table ?? '',
          pnColumn: map.pnColumn ?? '',
          stationColumn: map.stationColumn ?? '',
          qtyColumn: map.qtyColumn ?? '',
        })
      } else if (conns[0]) {
        setStock(v => ({ ...v, connectionId: conns[0].id }))
      }
    })
    void (auditList?.().catch(() => ({ items: [] as AuditRow[] })) ?? Promise.resolve({ items: [] as AuditRow[] })).then(result => {
      if (alive) setAuditItems(result.items)
    })
    return () => { alive = false }
    // Load once on mount; later upserts update local state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const context: MroSessionContext = useMemo(() => parseMroContext({
    tailNo: tailNo || 'B-0000',
    asOf,
    manualIds: manualId ? [manualId] : [],
    pack: 'mro.v1',
    scenario: rail === 'fault' ? 'fault' : 'manual',
  }) ?? { tailNo: 'B-0000', asOf: '1970-01-01', manualIds: [], pack: 'mro.v1', scenario: 'manual' }, [tailNo, asOf, manualId, rail])

  const empty = aircraft.length === 0 && manuals.length === 0

  const buildImportLocator = () => {
    const params = new URLSearchParams()
    params.set('status', manualDraft.status)
    if (manualDraft.ata.trim()) params.set('ata', manualDraft.ata.trim())
    if (manualDraft.tail.trim()) params.set('tail', manualDraft.tail.trim())
    return `mro://${manualDraft.docType}/${manualDraft.revision.trim()}?${params.toString()}`
  }

  const finishImport = async () => {
    const expertId = (mroExpertId ?? '').trim()
    if (!onIngestManual || !onRegisterManual) return
    if (!expertId) { setError(zh ? '未挂载机务专家，无法导入手册' : 'No MRO expert mounted'); return }
    if (!manualFile) { setError(zh ? '请选择手册文件' : 'Choose a manual file first'); return }
    setError('')
    setImportPreview([])
    try {
      const mediaType = manualMediaType(manualFile.name)
      const ingested = await onIngestManual({ expertId, path: manualFile.path, sourceLocator: buildImportLocator(), mediaType })
      const failed = ingested.documents.find(d => d.indexState === 'failed')
      if (failed) {
        setError((zh ? '解析失败：' : 'Parse failed: ') + (failed.failReason ?? (zh ? '未产出可检索正文' : 'no searchable body')))
        return
      }
      const docs = ingested.documents.filter(d => d.documentId).map((d, i) => ({ documentId: d.documentId, partNo: i + 1 }))
      if (docs.length === 0) { setError(zh ? '未产出可登记的文档' : 'No registrable document produced'); return }
      const row = await onRegisterManual({
        title: manualDraft.title,
        docType: manualDraft.docType,
        revision: manualDraft.revision,
        status: manualDraft.status,
        ata: manualDraft.ata,
        documents: docs,
      })
      setManuals(values => [...values.filter(item => item.manualId !== row.manualId), row])
      setManualId(row.manualId)
      setImportPreview(ingested.documents.find(d => d.preview && d.preview.length)?.preview ?? [])
      setImportOpen(false)
      setUncontrolledOpen(false)
      setManualDraft({ title: '', docType: 'AMM', revision: '', status: 'controlled', ata: '', tail: '' })
      setManualFile(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '导入失败' : 'Could not import manual'))
    }
  }

  const startImport = () => {
    if (manualDraft.status === 'uncontrolled') {
      setUncontrolledOpen(true)
      return
    }
    void finishImport()
  }

  const downloadChecklist = async () => {
    if (!onBuildChecklist) return
    const steps = checklistDraft.split('\n').map(line => line.trim()).filter(Boolean)
    setError('')
    try {
      const built = await onBuildChecklist({ steps, cites: lastCites ?? [] })
      if (!built.steps.length) {
        setError(zh ? '没有带引用的步骤可下载' : 'No cited steps to download')
        return
      }
      const blob = new Blob([JSON.stringify(built, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = 'mro-checklist.json'
      link.click()
      URL.revokeObjectURL(url)
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '检查单生成失败' : 'Could not build checklist'))
    }
  }

  const submitAircraft = async () => {
    if (!onUpsertAircraft) return
    setError('')
    try {
      const row = await onUpsertAircraft(draft)
      setAircraft(values => [...values.filter(item => item.aircraftId !== row.aircraftId), row])
      setTailNo(row.tailNo)
      setRegisterOpen(false)
      setDraft({ tailNo: '', msn: '', model: '', config: '' })
    } catch (e) {
      setError(e instanceof Error ? e.message : (zh ? '登记失败' : 'Could not register tail'))
    }
  }

  if (!enabled) {
    return (
      <main className="skill-center mro-workbench-page">
        <header className="mro-top">
          <h1 className="view-title">{zh ? '机务工作台' : 'MRO workbench'}</h1>
        </header>
        <p className="mro-enable-hint">{zh ? '先启用航空机务专家' : 'Enable the aviation MRO expert first'}</p>
      </main>
    )
  }

  return (
    <main className="skill-center mro-workbench-page">
      <header className="mro-top">
        <div>
          <h1 className="view-title">{zh ? '机务工作台' : 'MRO workbench'}</h1>
        </div>
        <small>{zh ? '辅助建议，不构成放行' : 'Advisory only. Not a release to service.'}</small>
      </header>
      <div className="mro-context-bar">
        <label>
          <span>{zh ? '机尾' : 'Tail'}</span>
          <select value={tailNo} onChange={e => {
            if (e.target.value === '__register__') { setRegisterOpen(true); return }
            setTailNo(e.target.value)
          }} aria-label={zh ? '机尾' : 'Tail'}>
            <option value="">{zh ? '选择机尾' : 'Select tail'}</option>
            {aircraft.map(item => <option key={item.aircraftId} value={item.tailNo}>{item.tailNo}</option>)}
            <option value="__register__">{zh ? '登记机尾…' : 'Register tail…'}</option>
          </select>
        </label>
        <label>
          <span>{zh ? '日期' : 'Date'}</span>
          <input type="date" value={asOf} onChange={e => setAsOf(e.target.value)} aria-label={zh ? '日期' : 'Date'} />
        </label>
        <label>
          <span>{zh ? '手册' : 'Manual'}</span>
          <select value={manualId} onChange={e => setManualId(e.target.value)} aria-label={zh ? '手册' : 'Manual'}>
            <option value="">{zh ? '选择手册' : 'Select manual'}</option>
            {manuals.map(item => <option key={item.manualId} value={item.manualId}>{item.title || item.docType} {item.revision}</option>)}
          </select>
        </label>
        <MroAskButton
          mroExpertId={mroExpertId ?? ''}
          context={{ ...context, scenario: rail === 'fault' ? 'fault' : 'manual' }}
          prompt={rail === 'fault' ? symptoms : undefined}
          onOpened={onAskOpened}
          openChat={openChat}
        />
      </div>
      <div className="mro-body">
        <nav className="mro-rail" aria-label={zh ? '机务分区' : 'MRO sections'}>
          <button type="button" aria-selected={rail === 'manuals'} onClick={() => setRail('manuals')}>{zh ? '手册' : 'Manuals'}</button>
          <button type="button" aria-selected={rail === 'fault'} onClick={() => setRail('fault')}>{zh ? '排故' : 'Fault'}</button>
          <button type="button" aria-expanded={moreOpen} aria-selected={rail === 'more'} onClick={() => { setMoreOpen(v => !v); setRail('more') }}>{zh ? '更多' : 'More'}</button>
          {moreOpen && rail === 'more' && (
            <div className="mro-more">
              <button type="button" aria-selected={morePage === 'checklist'} onClick={() => setMorePage('checklist')}>{zh ? '检查单' : 'Checklist'}</button>
              <button type="button" aria-selected={morePage === 'fleet'} onClick={() => { setMorePage('fleet'); setRegisterOpen(true) }}>{zh ? '机队' : 'Fleet'}</button>
              <button type="button" aria-selected={morePage === 'datasource'} onClick={() => setMorePage('datasource')}>{zh ? '数据源' : 'Data sources'}</button>
              <button type="button" aria-selected={morePage === 'audit'} onClick={() => setMorePage('audit')}>{zh ? '审计' : 'Audit'}</button>
            </div>
          )}
        </nav>
        <section>
          {rail === 'fault' ? (
            <div className="mro-fault">
              <label>
                <span>{zh ? '症状' : 'Symptoms'}</span>
                <textarea value={symptoms} onChange={e => setSymptoms(e.target.value)} rows={6} placeholder={zh ? '起落架收放异常……' : 'Landing-gear retraction anomaly…'} />
              </label>
              <p className="mro-fault-meta">{zh ? `当前机尾 ${tailNo || '—'} · ${asOf}` : `Tail ${tailNo || '—'} · ${asOf}`}</p>
              <p className="mro-fault-hint">{zh ? '按 症状 → 候选故障 → 原因 → 任务 → 件号 组织，并标置信度低/中/高。每条关键句要有修订版引用。' : 'Organize as symptom → candidate → cause → task → PN, with low/medium/high confidence. Every key sentence needs a revision cite.'}</p>
            </div>
          ) : rail === 'more' && morePage === 'checklist' ? (
            <div className="mro-checklist">
              {!(lastCites && lastCites.length) ? (
                <div className="mro-empty">
                  <p>{zh ? '在带受控引用的机务回答下方，点击「下载检查单 JSON」。' : 'Open a grounded MRO answer and use “Download checklist JSON” beneath it.'}</p>
                </div>
              ) : (
                <>
                  <label>
                    <span>{zh ? '已引用步骤（一行一步）' : 'Cited steps (one per line)'}</span>
                    <textarea value={checklistDraft} onChange={e => setChecklistDraft(e.target.value)} rows={6} aria-label={zh ? '检查单步骤' : 'Checklist steps'} />
                  </label>
                  <button type="button" className="primary" disabled={!checklistDraft.trim()} onClick={() => void downloadChecklist()}>{zh ? '下载 JSON' : 'Download JSON'}</button>
                </>
              )}
            </div>
          ) : rail === 'more' && morePage === 'audit' ? (
            auditItems.length === 0 ? (
              <div className="mro-empty">
                <p>{zh ? '还没有可回放的机务审计。' : 'No MRO audit events to replay yet.'}</p>
              </div>
            ) : (
              <ol className="mro-audit-list" aria-label={zh ? '审计回放' : 'Audit replay'}>
                {auditItems.map(item => (
                  <li key={item.id}>
                    <b>{item.action}</b>
                    <small>{item.resourceType} · {item.createdAt}</small>
                  </li>
                ))}
              </ol>
            )
          ) : rail === 'more' && morePage === 'datasource' ? (
            <div className="mro-datasource">
              {verified.length === 0 ? (
                <div className="mro-empty">
                  <p>{zh ? '先在设置 → 数据源探测连接' : 'Probe a connection in Settings → Data sources first.'}</p>
                </div>
              ) : (
                <form className="mro-bind" onSubmit={async e => {
                  e.preventDefault()
                  if (!onBindStock) return
                  setError('')
                  try {
                    await onBindStock({
                      connectionId: stock.connectionId,
                      tableMap: {
                        schema: stock.schema, table: stock.table, pnColumn: stock.pnColumn,
                        ...(stock.stationColumn ? { stationColumn: stock.stationColumn } : {}),
                        ...(stock.qtyColumn ? { qtyColumn: stock.qtyColumn } : {}),
                      },
                    })
                    setBindMsg(zh ? '库存表已绑定' : 'Stock table bound')
                  } catch (err) {
                    setError(err instanceof Error ? err.message : (zh ? '绑定失败' : 'Bind failed'))
                  }
                }}>
                  <p>{zh ? '把已探测连接映射到库存表。不手写 SQL。' : 'Map a probed connection to the stock table. No handwritten SQL.'}</p>
                  <label>
                    <span>{zh ? '连接' : 'Connection'}</span>
                    <select value={stock.connectionId} onChange={e => setStock(v => ({ ...v, connectionId: e.target.value }))} aria-label={zh ? '连接' : 'Connection'}>
                      {verified.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
                    </select>
                  </label>
                  <label><span>schema</span><input value={stock.schema} onChange={e => setStock(v => ({ ...v, schema: e.target.value }))} list="mro-ds-schema" /></label>
                  <label><span>{zh ? '表' : 'Table'}</span><input value={stock.table} onChange={e => setStock(v => ({ ...v, table: e.target.value }))} list="mro-ds-table" /></label>
                  <label><span>PN</span><input value={stock.pnColumn} onChange={e => setStock(v => ({ ...v, pnColumn: e.target.value }))} list="mro-ds-col" /></label>
                  <label><span>{zh ? '工位' : 'Station'}</span><input value={stock.stationColumn} onChange={e => setStock(v => ({ ...v, stationColumn: e.target.value }))} /></label>
                  <label><span>{zh ? '数量' : 'Qty'}</span><input value={stock.qtyColumn} onChange={e => setStock(v => ({ ...v, qtyColumn: e.target.value }))} /></label>
                  {bindMsg && <p role="status">{bindMsg}</p>}
                  {error && <p role="alert">{error}</p>}
                  <button className="primary" disabled={!stock.connectionId || !stock.schema.trim() || !stock.table.trim() || !stock.pnColumn.trim()}>{zh ? '绑定库存' : 'Bind stock'}</button>
                </form>
              )}
            </div>
          ) : empty ? (
            <div className="mro-empty">
              <b>{zh ? '从一本手册或一个机尾开始' : 'Start from a manual or a tail number'}</b>
              <p>{zh ? '工作台只做适用性与引用。放行仍由持证人员做出。' : 'The workbench only checks effectivity and cites. Release stays with licensed personnel.'}</p>
              <div className="mro-empty-actions">
                <button type="button" onClick={() => setImportOpen(true)}>{zh ? '导入手册' : 'Import manual'}</button>
                <button type="button" onClick={() => setRegisterOpen(true)}>{zh ? '登记机尾' : 'Register tail'}</button>
              </div>
            </div>
          ) : (
            <>
            <div className="mro-empty-actions">
              <button type="button" onClick={() => setImportOpen(true)}>{zh ? '导入手册' : 'Import manual'}</button>
            </div>
            {importPreview.length > 0 && (
              <ul className="mro-import-preview" aria-label={zh ? '解析预览' : 'Parse preview'}>
                {importPreview.map((block, i) => <li key={i}>{block}</li>)}
              </ul>
            )}
            <table className="mro-manual-table">
              <thead>
                <tr>
                  <th>{zh ? '名称' : 'Title'}</th>
                  <th>{zh ? '类型' : 'Type'}</th>
                  <th>{zh ? '修订' : 'Rev'}</th>
                  <th>{zh ? '受控' : 'Status'}</th>
                  <th>ATA</th>
                  <th>{zh ? '分段' : 'Parts'}</th>
                </tr>
              </thead>
              <tbody>
                {manuals.map(item => (
                  <tr key={item.manualId}>
                    <td>{item.title || item.docType}</td>
                    <td>{item.docType}</td>
                    <td>{item.revision}</td>
                    <td>{item.status}</td>
                    <td>{item.ata}</td>
                    <td>{item.sectionCount}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            </>
          )}
        </section>
      </div>
      <Dialog open={importOpen} title={zh ? '导入手册' : 'Import manual'} onClose={() => setImportOpen(false)}>
        <form onSubmit={e => { e.preventDefault(); startImport() }}>
          <p className="mro-import-hint">{zh ? '选择 Markdown / 纯文本 / PDF / Word / Excel / PPT 手册。导入即解析入库并写入修订/ATA/受控/机尾时效。' : 'Pick a Markdown, text, PDF, Word, Excel or PowerPoint manual. Import parses, indexes and stamps revision/ATA/status/tail effectivity.'}</p>
          <label>{zh ? '手册文件' : 'Manual file'}
            <input type="file" accept=".md,.markdown,.txt,.pdf,.docx,.pptx,.xlsx,application/pdf" aria-label={zh ? '手册文件' : 'Manual file'} onChange={e => {
              const f = e.target.files?.[0] as (File & { path?: string }) | undefined
              if (!f) { setManualFile(null); return }
              if (!f.path) { setError(zh ? '当前环境拿不到本地路径，请把文件放到工作区后再试。' : 'No local path for this file. Put it in the workspace first.'); return }
              setError('')
              setManualFile({ path: f.path, name: f.name })
              if (!manualDraft.title.trim()) setManualDraft(v => ({ ...v, title: f.name }))
            }} />
          </label>
          <label>{zh ? '名称' : 'Title'}<input value={manualDraft.title} maxLength={256} onChange={e => setManualDraft(v => ({ ...v, title: e.target.value }))} aria-label={zh ? '手册名称' : 'Manual title'} /></label>
          <label>{zh ? '类型' : 'Type'}
            <select value={manualDraft.docType} onChange={e => setManualDraft(v => ({ ...v, docType: e.target.value }))} aria-label={zh ? '手册类型' : 'Manual type'}>
              {['AMM', 'IPC', 'TSM', 'FIM', 'WDM', 'CMM', 'MEL', 'SB', 'AD', 'EO', 'POLICY'].map(item => <option key={item} value={item}>{item}</option>)}
            </select>
          </label>
          <label>{zh ? '修订' : 'Rev'}<input value={manualDraft.revision} maxLength={64} onChange={e => setManualDraft(v => ({ ...v, revision: e.target.value }))} aria-label={zh ? '修订' : 'Revision'} /></label>
          <label>{zh ? '受控' : 'Status'}
            <select value={manualDraft.status} onChange={e => setManualDraft(v => ({ ...v, status: e.target.value }))} aria-label={zh ? '受控状态' : 'Control status'}>
              <option value="controlled">{zh ? '受控' : 'Controlled'}</option>
              <option value="uncontrolled">{zh ? '未受控' : 'Uncontrolled'}</option>
              <option value="superseded">{zh ? '已替代' : 'Superseded'}</option>
            </select>
          </label>
          <label>ATA<input value={manualDraft.ata} maxLength={16} onChange={e => setManualDraft(v => ({ ...v, ata: e.target.value }))} /></label>
          <label>{zh ? '机尾时效（可选）' : 'Tail effectivity (optional)'}<input value={manualDraft.tail} maxLength={32} onChange={e => setManualDraft(v => ({ ...v, tail: e.target.value }))} aria-label={zh ? '机尾时效' : 'Tail effectivity'} /></label>
          {error && <p role="alert">{error}</p>}
          <div className="dialog-actions">
            <button type="button" onClick={() => setImportOpen(false)}>{zh ? '取消' : 'Cancel'}</button>
            <button className="primary" disabled={!manualDraft.revision.trim() || !manualFile}>{zh ? '导入' : 'Import'}</button>
          </div>
        </form>
      </Dialog>
      <ConfirmDialog
        open={uncontrolledOpen}
        title={zh ? '未受控手册' : 'Uncontrolled manual'}
        description={zh ? '未受控手册仅供参考，回答前会再次确认' : 'Uncontrolled manuals are reference only. Chat will confirm again before using them.'}
        confirmLabel={zh ? '继续导入' : 'Import anyway'}
        danger={false}
        onCancel={() => setUncontrolledOpen(false)}
        onConfirm={() => void finishImport()}
      />
      <Dialog open={registerOpen} title={zh ? '登记机尾' : 'Register tail'} onClose={() => setRegisterOpen(false)}>
        <form onSubmit={e => { e.preventDefault(); void submitAircraft() }}>
          <label>{zh ? '机尾' : 'Tail'}<input value={draft.tailNo} maxLength={32} onChange={e => setDraft(v => ({ ...v, tailNo: e.target.value }))} /></label>
          <label>MSN<input value={draft.msn} maxLength={32} onChange={e => setDraft(v => ({ ...v, msn: e.target.value }))} /></label>
          <label>{zh ? '机型' : 'Model'}<input value={draft.model} maxLength={64} onChange={e => setDraft(v => ({ ...v, model: e.target.value }))} /></label>
          <label>{zh ? '构型' : 'Config'}<input value={draft.config} maxLength={128} onChange={e => setDraft(v => ({ ...v, config: e.target.value }))} /></label>
          {error && <p role="alert">{error}</p>}
          <div className="dialog-actions">
            <button type="button" onClick={() => setRegisterOpen(false)}>{zh ? '取消' : 'Cancel'}</button>
            <button className="primary" disabled={!draft.tailNo.trim() || !draft.model.trim()}>{zh ? '保存' : 'Save'}</button>
          </div>
        </form>
      </Dialog>
    </main>
  )
}
