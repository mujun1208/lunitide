import React, { useEffect, useMemo, useState } from 'react'
import { type ExpertBridge, type ProjectBridge, type SessionBridge } from '../bridge/client'
import { ConfirmDialog, Dialog } from '../ui/Dialog'
import { useZh } from '../i18n/language'
import { bulletinExpertIds, resolveAskExpertId } from '../expert/expertIds'
import { MRO_RAIL_GROUPS, MRO_RAIL_LEAF_LABELS, type MroRailLeaf, railGroupExpanded, readRailOpen, writeRailOpen } from './mroRailGroups'
import { MroAskButton, openMroChat, type MroChatOpened } from './MroAskButton'
import { parseMroContext, type MroSessionContext } from './mroContext'
import { parseAogPaste } from './aogPaste'
import { QuickForm, type QuickFormSpec } from './MroForms'

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

// Six first-class domains + a small utilities group.
type Rail = 'manuals' | 'fault' | 'due' | 'tools' | 'parts' | 'plan' | 'checklist' | 'audit' | 'fleet'
type DueTab = 'list' | 'components' | 'pirep' | 'triggers'
type ToolTab = 'tools' | 'lots' | 'kits'
type PartTab = 'stock' | 'alternates' | 'aog' | 'po' | 'source'
type PlanTab = 'wp' | 'intervals' | 'schedule'

export type MroDueRow = { id: string; kind: string; scope?: string; limitValue?: number; usedValue?: number; usedMissing?: boolean; dueAt?: string; remaining?: number; state: string; label: string }
export type MroToolRow = { id: string; toolNo: string; sn?: string; location?: string; holder?: string; calibDue?: string; status?: string; checkoutBlocked?: string }
export type MroLotRow = { id: string; lotNo: string; parentLotId?: string; qty?: number; expires?: string; tails: string[] }
export type MroKitRow = { id: string; name: string; missing: string[] }
export type MroStockRow = { pn: string; qty: number; source: string }
export type MroAlternateRow = { pnFrom: string; pnTo: string; certOk: boolean; effectivity?: string; qty?: number; accepted: boolean }
export type MroWorkPackageRow = { id: string; title: string; sources: string[]; hours?: number }
export type MroOpsTodo = { id: string; kind: string; ref: string; status: string; detail?: string }
export type MroComponentRow = { id: string; sn: string; pn: string; lifeCount: number; installed: boolean; tailNo?: string; events: Array<{ kind: string; occurredAt: string; note?: string }> }
export type MroPirepRow = { id: string; tailNo: string; body: string; state: string; createdAt: string }
export type MroAogRow = { id: string; tailNo: string; pn?: string; qty?: string; note?: string; state: string }
export type MroPoRow = { id: string; pn: string; qty?: string; price?: string; state: string }
export type MroTriggerRow = { scopeId: string; kind: string; state: string; action: string; category?: string }
export type MroIntervalRow = { taskKey: string; intervalValue: number; unit: string; version?: string; sourceCite?: string }

// FormKind selects which QuickForm spec the single dialog renders.
type FormKind =
  | 'tool' | 'lot' | 'lot-use' | 'chem-issue' | 'kit'
  | 'due' | 'util' | 'component' | 'life' | 'pirep'
  | 'stock' | 'alternate' | 'aog' | 'po'
  | 'wp' | 'interval' | 'interval-propose' | 'schedule' | 'capacity'

function normalizeInitialRail(initial?: string): { rail: Rail; part: PartTab } {
  switch (initial) {
    case 'fault': return { rail: 'fault', part: 'stock' }
    case 'due': case 'tools': case 'parts': case 'plan':
    case 'checklist': case 'audit': case 'fleet':
      return { rail: initial, part: 'stock' }
    case 'datasource': return { rail: 'parts', part: 'source' }
    case 'more': return { rail: 'checklist', part: 'stock' }
    default: return { rail: 'manuals', part: 'stock' }
  }
}

function askScenario(rail: Rail): MroSessionContext['scenario'] {
  switch (rail) {
    case 'fault': return 'fault'
    case 'checklist': return 'checklist'
    case 'due': return 'due'
    case 'tools': return 'tools'
    case 'parts': return 'parts'
    case 'plan': return 'plan'
    default: return 'manual'
  }
}

function dueBadgeClass(state: string): string {
  if (state === 'overdue') return 'is-err'
  if (state === 'missing') return 'is-warn'
  return 'is-ok'
}

// C1–C7 are the deterministic schedule constraints checked by the engine. The
// legend renders every code as a pass/fail badge; codes present in the current
// violations flip to is-err. The engine never solves or auto-shifts.
const CONSTRAINT_CODES: Array<{ code: string; zh: string; en: string }> = [
  { code: 'C1', zh: '窗口晚于超限到期', en: 'Window after overdue due' },
  { code: 'C2', zh: '技能组工时超载', en: 'Skill capacity overload' },
  { code: 'C3', zh: '机尾已停场/AOG', en: 'Tail grounded / AOG' },
  { code: 'C4', zh: '同机尾窗口重叠', en: 'Same-tail window overlap' },
  { code: 'C5', zh: '套件缺件', en: 'Kit shortage' },
  { code: 'C6', zh: '长周期件无库存', en: 'Long-lead part no stock' },
  { code: 'C7', zh: '间隔缺来源引用', en: 'Interval missing source cite' },
]

function toolBadgeClass(status?: string, blocked?: string, calibDue?: string, asOf?: string): string {
  if (blocked || status === 'overdue' || (calibDue && asOf && calibDue < asOf)) return 'is-err'
  if (status === 'out' || (calibDue && asOf && calibDue === asOf)) return 'is-warn'
  return 'is-ok'
}

function ymd(value?: string): string {
  return (value ?? '').trim().slice(0, 10)
}

function onOrBefore(value: string | undefined, asOf: string): boolean {
  const day = ymd(value)
  return !day || day <= asOf
}

function Badge({ tone, children }: { tone: string; children: React.ReactNode }): React.JSX.Element {
  return <span className={`mro-badge ${tone}`}>{children}</span>
}

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
  enabled, mroExpertId, opsExpertIds, initialRail = 'manuals',
  aircraftList, manualList, onUpsertAircraft, onAskOpened, openChat,
  verifiedConnections, existingStock, onBindStock,
  lastCites, onBuildChecklist, onRegisterManual, onIngestManual, auditList,
  dueItems, tools, lots, kits, partsStock, alternates, workPackages, opsTodos,
  dueList, toolList, lotList, kitList, partsList, planList, todoList, constraintList,
  onCheckoutTool, onPublishSchedule, onBulletinChain,
  componentListFn, pirepListFn, aogListFn, poListFn, triggerListFn, intervalListFn,
  onAddTool, onReturnTool, onAddDue, onRecordUtil, onAddLot, onRecordUse, onDefineKit,
  onAddStock, onAddAlternate, onBuildWp, onAddInterval, onProposeInterval, onAddSchedule, onSetCapacity,
  onAddComponent, onLifeEvent, onDraftPirep, onIntakeAog, onDraftPo,
  onIssueChem, onAddPartsTodo, onConfirmPirep, onConfirmAog, onConfirmPo,
}: {
  enabled: boolean
  mroExpertId?: string
  opsExpertIds?: Record<string, string>
  initialRail?: string
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
  dueItems?: MroDueRow[]
  tools?: MroToolRow[]
  lots?: MroLotRow[]
  kits?: MroKitRow[]
  partsStock?: MroStockRow[]
  alternates?: MroAlternateRow[]
  workPackages?: MroWorkPackageRow[]
  opsTodos?: MroOpsTodo[]
  dueList?: () => Promise<{ items: MroDueRow[] }>
  toolList?: () => Promise<{ items: MroToolRow[] }>
  lotList?: () => Promise<{ items: MroLotRow[] }>
  kitList?: () => Promise<{ items: MroKitRow[] }>
  partsList?: () => Promise<{ items: MroStockRow[]; alternates?: MroAlternateRow[] }>
  planList?: () => Promise<{ items: MroWorkPackageRow[] }>
  todoList?: () => Promise<{ items: MroOpsTodo[] }>
  constraintList?: () => Promise<{ violations: Array<{ code: string; detail: string }> }>
  onCheckoutTool?: (id: string) => Promise<{ ok: boolean; reason?: string }>
  onPublishSchedule?: (id: string) => Promise<{ todos: MroOpsTodo[] }>
  onBulletinChain?: (lotId: string) => Promise<{ tails: string[]; note: string }>
  componentListFn?: () => Promise<{ items: MroComponentRow[] }>
  pirepListFn?: () => Promise<{ items: MroPirepRow[] }>
  aogListFn?: () => Promise<{ items: MroAogRow[] }>
  poListFn?: () => Promise<{ items: MroPoRow[] }>
  triggerListFn?: () => Promise<{ items: MroTriggerRow[] }>
  intervalListFn?: () => Promise<{ items: MroIntervalRow[] }>
  onAddTool?: (input: { toolNo: string; sn?: string; location?: string; calibDue?: string }) => Promise<{ ok: boolean }>
  onReturnTool?: (id: string) => Promise<{ ok: boolean }>
  onAddDue?: (input: { scopeId: string; kind: string; limitValue?: number; dueAt?: string; source?: string }) => Promise<{ ok: boolean }>
  onRecordUtil?: (input: { scopeId: string; hours?: number; cycles?: number; batteryCycles?: number }) => Promise<{ items: MroDueRow[] }>
  onAddLot?: (input: { lotNo: string; parentLotId?: string; qty?: number; expires?: string; sdsDoc?: string }) => Promise<{ ok: boolean }>
  onRecordUse?: (input: { lotId: string; tailNo?: string; wo?: string; tech?: string }) => Promise<{ ok: boolean }>
  onDefineKit?: (input: { name: string; items?: Array<{ pn: string; required?: number; onHand?: number }> }) => Promise<{ ok: boolean }>
  onAddStock?: (input: { pn: string; qty: number; source?: string }) => Promise<{ ok: boolean }>
  onAddAlternate?: (input: { pnFrom: string; pnTo: string; certOk: boolean; effectivity?: string }) => Promise<{ ok: boolean }>
  onBuildWp?: (input: { title?: string; cards?: string[]; ads?: string[]; mels?: string[]; open?: string[] }) => Promise<{ ok: boolean }>
  onAddInterval?: (input: { taskKey: string; intervalValue: number; unit: string; sourceCite?: string }) => Promise<{ ok: boolean }>
  onProposeInterval?: (input: { taskKey: string; mpdCite: string; fleetCite: string }) => Promise<{ ok: boolean }>
  onAddSchedule?: (input: { tailNo: string; checkName: string; startOn?: string; endOn?: string; hours?: number; skill?: string }) => Promise<{ ok: boolean }>
  onSetCapacity?: (input: { skill: string; hours: number }) => Promise<{ ok: boolean }>
  onAddComponent?: (input: { sn: string; pn: string; life?: number }) => Promise<{ ok: boolean }>
  onLifeEvent?: (input: { componentId: string; kind: string; occurredAt: string; note?: string }) => Promise<{ ok: boolean }>
  onDraftPirep?: (input: { tailNo: string; body: string }) => Promise<{ ok: boolean }>
  onIntakeAog?: (input: { text: string }) => Promise<{ ok: boolean }>
  onDraftPo?: (input: { pn: string; qty?: string; price?: string }) => Promise<{ ok: boolean }>
  onIssueChem?: (input: { lotId: string; qty: number; tailNo?: string; wo?: string; tech?: string }) => Promise<{ ok: boolean }>
  onAddPartsTodo?: (input: { kitId: string; detail?: string }) => Promise<{ ok: boolean; id?: string }>
  onConfirmPirep?: (input: { id: string; state: 'confirmed' | 'rejected' }) => Promise<{ ok: boolean }>
  onConfirmAog?: (input: { id: string; state: 'confirmed' | 'rejected' }) => Promise<{ ok: boolean }>
  onConfirmPo?: (input: { id: string; state: 'confirmed' | 'rejected' }) => Promise<{ ok: boolean }>
}): React.JSX.Element {
  const zh = useZh()
  const initial = normalizeInitialRail(initialRail)
  const [rail, setRail] = useState<Rail>(initial.rail)
  const [railOpen, setRailOpen] = useState<Record<string, boolean>>(readRailOpen)
  const [dueTab, setDueTab] = useState<DueTab>('list')
  const [toolTab, setToolTab] = useState<ToolTab>('tools')
  const [partTab, setPartTab] = useState<PartTab>(initial.part)
  const [planTab, setPlanTab] = useState<PlanTab>('wp')
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
  const [dueRows, setDueRows] = useState<MroDueRow[]>(dueItems ?? [])
  const [toolRows, setToolRows] = useState<MroToolRow[]>(tools ?? [])
  const [lotRows, setLotRows] = useState<MroLotRow[]>(lots ?? [])
  const [kitRows, setKitRows] = useState<MroKitRow[]>(kits ?? [])
  const [stockRows, setStockRows] = useState<MroStockRow[]>(partsStock ?? [])
  const [altRows, setAltRows] = useState<MroAlternateRow[]>(alternates ?? [])
  const [planRows, setPlanRows] = useState<MroWorkPackageRow[]>(workPackages ?? [])
  const [todoRows, setTodoRows] = useState<MroOpsTodo[]>(opsTodos ?? [])
  const [violations, setViolations] = useState<Array<{ code: string; detail: string }>>([])
  const [componentRows, setComponentRows] = useState<MroComponentRow[]>([])
  const [pirepRows, setPirepRows] = useState<MroPirepRow[]>([])
  const [aogRows, setAogRows] = useState<MroAogRow[]>([])
  const [poRows, setPoRows] = useState<MroPoRow[]>([])
  const [triggerRows, setTriggerRows] = useState<MroTriggerRow[]>([])
  const [intervalRows, setIntervalRows] = useState<MroIntervalRow[]>([])
  const [formKind, setFormKind] = useState<FormKind | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let alive = true
    void Promise.all([
      aircraftList?.().catch(() => ({ items: [] as MroAircraft[] })) ?? Promise.resolve({ items: [] as MroAircraft[] }),
      manualList?.().catch(() => ({ items: [] as MroManual[] })) ?? Promise.resolve({ items: [] as MroManual[] }),
    ]).then(([a, m]) => {
      if (!alive) return
      setAircraft(a.items)
      setManuals(m.items)
      if (a.items[0]) setTailNo(current => current || a.items[0].tailNo)
      if (m.items[0]) setManualId(current => current || m.items[0].manualId)
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
    const jobs: Array<Promise<unknown>> = []
    if (!dueItems && dueList) jobs.push(dueList().then(r => { if (alive) setDueRows(r.items) }).catch(() => undefined))
    if (!tools && toolList) jobs.push(toolList().then(r => { if (alive) setToolRows(r.items) }).catch(() => undefined))
    if (!lots && lotList) jobs.push(lotList().then(r => { if (alive) setLotRows(r.items) }).catch(() => undefined))
    if (!kits && kitList) jobs.push(kitList().then(r => { if (alive) setKitRows(r.items) }).catch(() => undefined))
    if (!partsStock && partsList) jobs.push(partsList().then(r => { if (alive) { setStockRows(r.items); if (r.alternates) setAltRows(r.alternates) } }).catch(() => undefined))
    if (!workPackages && planList) jobs.push(planList().then(r => { if (alive) setPlanRows(r.items) }).catch(() => undefined))
    if (!opsTodos && todoList) jobs.push(todoList().then(r => { if (alive) setTodoRows(r.items) }).catch(() => undefined))
    if (constraintList) jobs.push(constraintList().then(r => { if (alive) setViolations(r.violations) }).catch(() => undefined))
    if (componentListFn) jobs.push(componentListFn().then(r => { if (alive) setComponentRows(r.items) }).catch(() => undefined))
    if (pirepListFn) jobs.push(pirepListFn().then(r => { if (alive) setPirepRows(r.items) }).catch(() => undefined))
    if (aogListFn) jobs.push(aogListFn().then(r => { if (alive) setAogRows(r.items) }).catch(() => undefined))
    if (poListFn) jobs.push(poListFn().then(r => { if (alive) setPoRows(r.items) }).catch(() => undefined))
    if (triggerListFn) jobs.push(triggerListFn().then(r => { if (alive) setTriggerRows(r.items) }).catch(() => undefined))
    if (intervalListFn) jobs.push(intervalListFn().then(r => { if (alive) setIntervalRows(r.items) }).catch(() => undefined))
    if (jobs.length) {
      setLoading(true)
      void Promise.all(jobs).then(() => { if (alive) setLoading(false) })
    }
    return () => { alive = false }
    // Load once on mount; later upserts update local state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const scenario = askScenario(rail)
  const currentModel = aircraft.find(item => item.tailNo === tailNo)?.model
  const askExpertId = resolveAskExpertId(scenario ?? 'manual', opsExpertIds ?? {}, mroExpertId ?? '', currentModel)
  const context: MroSessionContext = useMemo(() => parseMroContext({
    tailNo: tailNo || 'B-0000',
    asOf,
    manualIds: manualId ? [manualId] : [],
    pack: 'mro.v1',
    scenario,
  }) ?? { tailNo: 'B-0000', asOf: '1970-01-01', manualIds: [], pack: 'mro.v1', scenario: 'manual' }, [tailNo, asOf, manualId, scenario])

  const empty = aircraft.length === 0 && manuals.length === 0
  const kitTodos = todoRows.filter(t => t.kind === 'kit_staging')
  const partsTodos = todoRows.filter(t => t.kind === 'parts_request')
  const matchTail = (scope?: string) => !tailNo || !scope || scope === tailNo || scope.includes(tailNo)
  const visibleDue = dueRows.filter(item => matchTail(item.scope) && onOrBefore(item.dueAt, asOf))
  const visibleLots = lotRows.filter(lot => !tailNo || lot.tails.length === 0 || lot.tails.includes(tailNo))
  const visibleComponents = componentRows
    .filter(c => matchTail(c.tailNo) || c.events.some(e => e.note?.includes(tailNo)))
    .map(c => ({ ...c, events: c.events.filter(e => onOrBefore(e.occurredAt, asOf)) }))
  const visiblePireps = pirepRows.filter(p => matchTail(p.tailNo) && onOrBefore(p.createdAt, asOf))
  const visibleAog = aogRows.filter(a => matchTail(a.tailNo))
  const visibleTriggers = triggerRows.filter(t => !t.scopeId || matchTail(t.scopeId))

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

  const runBulletin = (lot: MroLotRow) => {
    void (async () => {
      const chain = onBulletinChain ? await onBulletinChain(lot.id) : { tails: lot.tails, note: '' }
      const ids = bulletinExpertIds(opsExpertIds ?? {}, mroExpertId ?? '')
      if (!openChat || !ids[0]) return
      const opened = await openChat({
        ensureProject: async () => { throw new Error('ensureProject missing') },
        projects: {} as ProjectBridge,
        sessions: {} as SessionBridge,
        experts: {} as Pick<ExpertBridge, 'sessionMountSet' | 'mount'>,
        mroExpertId: ids[0],
        extraExpertIds: ids.slice(1),
        context: { ...context, scenario: 'tools', lot: lot.lotNo, expertCatalogId: 'tooling-chemical-expert' },
        prompt: `质量通报串查 批次 ${lot.lotNo}${chain.tails?.length ? ` 机尾 ${chain.tails.join(',')}` : ''}`,
      })
      onAskOpened?.(opened)
    })()
  }

  const addKitTodo = (kit: MroKitRow) => {
    const detail = kit.missing.join(',')
    if (onAddPartsTodo) {
      void onAddPartsTodo({ kitId: kit.id, detail }).then(async result => {
        if (todoList) {
          const listed = await todoList()
          setTodoRows(listed.items)
          return
        }
        setTodoRows(rows => rows.some(t => t.kind === 'parts_request' && t.ref === kit.id)
          ? rows
          : [...rows, { id: result.id || `kit-${kit.id}`, kind: 'parts_request', ref: kit.id, status: 'open', detail }])
      })
      return
    }
    setTodoRows(rows => rows.some(t => t.kind === 'parts_request' && t.ref === kit.id)
      ? rows
      : [...rows, { id: `kit-${kit.id}`, kind: 'parts_request', ref: kit.id, status: 'open', detail }])
  }

  // Refetch helpers keep a domain list fresh after an inline write.
  const refetchDue = async () => { if (dueList) { const r = await dueList(); setDueRows(r.items) } }
  const refetchTools = async () => { if (toolList) { const r = await toolList(); setToolRows(r.items) } }
  const refetchLots = async () => { if (lotList) { const r = await lotList(); setLotRows(r.items) } }
  const refetchKits = async () => { if (kitList) { const r = await kitList(); setKitRows(r.items) } }
  const refetchParts = async () => { if (partsList) { const r = await partsList(); setStockRows(r.items); if (r.alternates) setAltRows(r.alternates) } }
  const refetchPlan = async () => { if (planList) { const r = await planList(); setPlanRows(r.items) } }
  const refetchConstraints = async () => { if (constraintList) { const r = await constraintList(); setViolations(r.violations) } }
  const refetchComponents = async () => { if (componentListFn) { const r = await componentListFn(); setComponentRows(r.items) } }
  const refetchPireps = async () => { if (pirepListFn) { const r = await pirepListFn(); setPirepRows(r.items) } }
  const refetchAog = async () => { if (aogListFn) { const r = await aogListFn(); setAogRows(r.items) } }
  const refetchPo = async () => { if (poListFn) { const r = await poListFn(); setPoRows(r.items) } }
  const refetchTriggers = async () => { if (triggerListFn) { const r = await triggerListFn(); setTriggerRows(r.items) } }
  const refetchIntervals = async () => { if (intervalListFn) { const r = await intervalListFn(); setIntervalRows(r.items) } }

  const num = (v?: string): number | undefined => { const n = Number(v); return v && Number.isFinite(n) ? n : undefined }
  const csv = (s: string): string[] => s.split(/[\n,]/).map(x => x.trim()).filter(Boolean)
  const parseKitItems = (s: string): Array<{ pn: string; required?: number; onHand?: number }> =>
    s.split('\n').map(line => line.trim()).filter(Boolean).map(line => {
      const [pn, req, on] = line.split(',').map(x => x.trim())
      return { pn, required: req ? Number(req) : undefined, onHand: on ? Number(on) : undefined }
    }).filter(it => it.pn)

  const closeForm = () => setFormKind(null)
  const addBtn = (kind: FormKind, label: string) => (
    <button type="button" onClick={() => setFormKind(kind)}>{label}</button>
  )

  // formSpec builds the active QuickForm from current state. Every submit calls
  // the injected bridge action then refetches just that domain's list.
  const formSpec = (kind: FormKind): QuickFormSpec | null => {
    switch (kind) {
      case 'tool': return {
        title: zh ? '登记工具' : 'Add tool',
        fields: [
          { name: 'toolNo', label: zh ? '工具号' : 'Tool no', required: true },
          { name: 'sn', label: 'SN' },
          { name: 'location', label: zh ? '库位' : 'Location' },
          { name: 'calibDue', label: zh ? '校准到期' : 'Calibration due', kind: 'date', hint: zh ? '过期不能借出' : 'Overdue blocks checkout' },
        ],
        submit: async v => { if (onAddTool) { await onAddTool({ toolNo: v.toolNo.trim(), sn: v.sn.trim() || undefined, location: v.location.trim() || undefined, calibDue: v.calibDue || undefined }); await refetchTools() } },
      }
      case 'lot': return {
        title: zh ? '登记批次' : 'Add lot',
        fields: [
          { name: 'lotNo', label: zh ? '批号' : 'Lot no', required: true },
          { name: 'parentLotId', label: zh ? '母批 ID' : 'Parent lot ID' },
          { name: 'qty', label: zh ? '数量' : 'Qty', kind: 'number' },
          { name: 'expires', label: zh ? '有效期' : 'Expires', kind: 'date' },
          { name: 'sdsDoc', label: 'SDS', placeholder: zh ? '安全数据表引用' : 'Safety data sheet ref' },
        ],
        submit: async v => { if (onAddLot) { await onAddLot({ lotNo: v.lotNo.trim(), parentLotId: v.parentLotId.trim() || undefined, qty: num(v.qty), expires: v.expires || undefined, sdsDoc: v.sdsDoc.trim() || undefined }); await refetchLots() } },
      }
      case 'lot-use': return {
        title: zh ? '登记使用' : 'Record use',
        fields: [
          { name: 'lotId', label: zh ? '批次' : 'Lot', kind: 'select', required: true, options: lotRows.map(l => ({ value: l.id, label: l.lotNo })) },
          { name: 'tailNo', label: zh ? '机尾' : 'Tail' },
          { name: 'wo', label: zh ? '工单' : 'Work order' },
          { name: 'tech', label: zh ? '技工' : 'Tech' },
        ],
        submit: async v => { if (onRecordUse) { await onRecordUse({ lotId: v.lotId, tailNo: v.tailNo.trim() || undefined, wo: v.wo.trim() || undefined, tech: v.tech.trim() || undefined }); await refetchLots() } },
      }
      case 'chem-issue': return {
        title: zh ? '发放批次' : 'Issue lot',
        intro: zh ? '从母批拆出子批并写使用记录。不编造库存。' : 'Splits a child lot from the parent and records the use. Never invents stock.',
        fields: [
          { name: 'lotId', label: zh ? '母批' : 'Parent lot', kind: 'select', required: true, options: lotRows.map(l => ({ value: l.id, label: `${l.lotNo} (${l.qty ?? 0})` })) },
          { name: 'qty', label: zh ? '发放数量' : 'Qty', kind: 'number', required: true },
          { name: 'tailNo', label: zh ? '机尾' : 'Tail' },
          { name: 'wo', label: zh ? '工单' : 'Work order' },
          { name: 'tech', label: zh ? '技工' : 'Tech' },
        ],
        submit: async v => { if (onIssueChem) { await onIssueChem({ lotId: v.lotId, qty: num(v.qty) ?? 0, tailNo: v.tailNo.trim() || undefined, wo: v.wo.trim() || undefined, tech: v.tech.trim() || undefined }); await refetchLots() } },
      }
      case 'kit': return {
        title: zh ? '定义套件' : 'Define kit',
        fields: [
          { name: 'name', label: zh ? '套件名' : 'Kit name', required: true },
          { name: 'items', label: zh ? '模板项' : 'Template items', kind: 'textarea', placeholder: 'SEAL-1,2,0', hint: zh ? '每行：PN,需求,在库' : 'Per line: PN,required,onHand' },
        ],
        submit: async v => { if (onDefineKit) { await onDefineKit({ name: v.name.trim(), items: parseKitItems(v.items) }); await refetchKits() } },
      }
      case 'due': return {
        title: zh ? '登记到期项' : 'Add due item',
        fields: [
          { name: 'scopeId', label: zh ? '范围 ID（机尾/部件）' : 'Scope ID (tail/part)', required: true },
          { name: 'kind', label: zh ? '种类' : 'Kind', kind: 'select', required: true, options: ['FH', 'FC', 'BC', 'CAL', 'LLP'].map(k => ({ value: k, label: k })) },
          { name: 'limitValue', label: zh ? '限制值' : 'Limit', kind: 'number' },
          { name: 'dueAt', label: zh ? '日历到期' : 'Due date', kind: 'date' },
          { name: 'source', label: zh ? '来源引用' : 'Source cite' },
        ],
        submit: async v => { if (onAddDue) { await onAddDue({ scopeId: v.scopeId.trim(), kind: v.kind, limitValue: num(v.limitValue), dueAt: v.dueAt || undefined, source: v.source.trim() || undefined }); await refetchDue(); await refetchTriggers() } },
      }
      case 'util': return {
        title: zh ? '录入利用率' : 'Record utilization',
        intro: zh ? '按范围累加飞行小时/循环/电池循环，随后重算到期。缺数据不写 0。' : 'Adds hours/cycles per scope then recomputes due. Never writes zeros.',
        fields: [
          { name: 'scopeId', label: zh ? '范围 ID' : 'Scope ID' },
          { name: 'hours', label: zh ? '小时' : 'Hours', kind: 'number' },
          { name: 'cycles', label: zh ? '循环' : 'Cycles', kind: 'number' },
          { name: 'batteryCycles', label: zh ? '电池循环' : 'Battery cycles', kind: 'number' },
          { name: 'csv', label: 'CSV', kind: 'textarea', hint: zh ? '可选，每行：范围,小时,循环,电池' : 'Optional, per line: scope,hours,cycles,battery' },
        ],
        submit: async v => {
          if (!onRecordUtil) return
          const lines = v.csv.split('\n').map(x => x.trim()).filter(Boolean)
          if (lines.length) {
            let last: { items: MroDueRow[] } | undefined
            for (const line of lines) {
              const [scope, h, c, b] = line.split(',').map(x => x.trim())
              if (!scope) continue
              last = await onRecordUtil({ scopeId: scope, hours: num(h), cycles: num(c), batteryCycles: num(b) })
            }
            if (last?.items) setDueRows(last.items)
          } else {
            if (!v.scopeId.trim()) throw new Error(zh ? '请填写范围 ID 或 CSV' : 'Scope ID or CSV required')
            const r = await onRecordUtil({ scopeId: v.scopeId.trim(), hours: num(v.hours), cycles: num(v.cycles), batteryCycles: num(v.batteryCycles) })
            if (r?.items) setDueRows(r.items)
          }
          await refetchTriggers()
        },
      }
      case 'component': return {
        title: zh ? '登记部件' : 'Add component',
        fields: [
          { name: 'sn', label: 'SN', required: true },
          { name: 'pn', label: 'PN', required: true },
          { name: 'life', label: zh ? '起始寿命' : 'Life count', kind: 'number' },
        ],
        submit: async v => { if (onAddComponent) { await onAddComponent({ sn: v.sn.trim(), pn: v.pn.trim(), life: num(v.life) }); await refetchComponents() } },
      }
      case 'life': return {
        title: zh ? '登记履历事件' : 'Record life event',
        fields: [
          { name: 'componentId', label: zh ? '部件' : 'Component', kind: 'select', required: true, options: componentRows.map(c => ({ value: c.id, label: `${c.sn} · ${c.pn}` })) },
          { name: 'kind', label: zh ? '事件' : 'Event', kind: 'select', required: true, options: [['install', zh ? '装机' : 'install'], ['remove', zh ? '拆下' : 'remove'], ['transfer', zh ? '转移' : 'transfer'], ['repair', zh ? '修理' : 'repair'], ['scrap', zh ? '报废' : 'scrap']].map(([value, label]) => ({ value, label })) },
          { name: 'occurredAt', label: zh ? '发生时间' : 'Occurred at', kind: 'date', required: true },
          { name: 'note', label: zh ? '备注' : 'Note' },
        ],
        submit: async v => { if (onLifeEvent) { await onLifeEvent({ componentId: v.componentId, kind: v.kind, occurredAt: v.occurredAt, note: v.note.trim() || undefined }); await refetchComponents() } },
      }
      case 'pirep': return {
        title: zh ? 'PIREP 草稿' : 'PIREP draft',
        fields: [
          { name: 'tailNo', label: zh ? '机尾' : 'Tail', required: true },
          { name: 'body', label: zh ? '内容' : 'Body', kind: 'textarea', required: true },
        ],
        submit: async v => { if (onDraftPirep) { await onDraftPirep({ tailNo: v.tailNo.trim(), body: v.body.trim() }); await refetchPireps() } },
      }
      case 'stock': return {
        title: zh ? '登记库存' : 'Add stock',
        fields: [
          { name: 'pn', label: 'PN', required: true },
          { name: 'qty', label: zh ? '数量' : 'Qty', kind: 'number', required: true },
          { name: 'source', label: zh ? '来源' : 'Source' },
        ],
        submit: async v => { if (onAddStock) { await onAddStock({ pn: v.pn.trim(), qty: num(v.qty) ?? 0, source: v.source.trim() || undefined }); await refetchParts() } },
      }
      case 'alternate': return {
        title: zh ? '登记替代件' : 'Add alternate',
        fields: [
          { name: 'pnFrom', label: zh ? '原件 PN' : 'From PN', required: true },
          { name: 'pnTo', label: zh ? '替代 PN' : 'To PN', required: true },
          { name: 'certOk', label: zh ? '认证有效' : 'Cert valid', kind: 'checkbox' },
          { name: 'effectivity', label: zh ? '适用构型' : 'Effectivity' },
        ],
        submit: async v => { if (onAddAlternate) { await onAddAlternate({ pnFrom: v.pnFrom.trim(), pnTo: v.pnTo.trim(), certOk: v.certOk === 'true', effectivity: v.effectivity.trim() || undefined }); await refetchParts() } },
      }
      case 'aog': return {
        title: zh ? 'AOG 抽取入案' : 'AOG intake',
        submitLabel: zh ? '确认入案' : 'Confirm intake',
        intro: zh ? '粘贴 AOG 邮件/群消息，先预览机尾/PN/数量，再确认写成草稿。永不自动下单。' : 'Paste an AOG note, preview tail/PN/qty, then confirm a draft. Never auto-purchases.',
        fields: [
          { name: 'text', label: zh ? '粘贴文本' : 'Paste text', kind: 'textarea', required: true, placeholder: zh ? 'B-1234 AOG 需要 PN 3G2000-1 数量2' : 'B-1234 AOG need PN 3G2000-1 qty 2' },
        ],
        preview: v => {
          const d = parseAogPaste(v.text ?? '')
          if (!d.tailNo && !d.pn && !d.qty) return null
          return (
            <p className="mro-import-preview" role="status">
              {zh ? '预览' : 'Preview'} · {zh ? '机尾' : 'tail'} {d.tailNo || '—'} · PN {d.pn || '—'} · {zh ? '数量' : 'qty'} {d.qty || '—'}
            </p>
          )
        },
        submit: async v => {
          const d = parseAogPaste(v.text.trim())
          if (!d.tailNo) throw new Error(zh ? '无法抽出机尾，请写成 B-1234 或 机尾: B-1234' : 'Could not extract a tail. Use B-1234 or tail: B-1234')
          if (onIntakeAog) { await onIntakeAog({ text: v.text.trim() }); await refetchAog() }
        },
      }
      case 'po': return {
        title: zh ? '采购草稿' : 'PO draft',
        intro: zh ? '草稿仅为建议，下单与确认仍由人工完成。' : 'Drafts are advisory; ordering stays human.',
        fields: [
          { name: 'pn', label: 'PN', required: true },
          { name: 'qty', label: zh ? '数量' : 'Qty' },
          { name: 'price', label: zh ? '价格' : 'Price' },
        ],
        submit: async v => { if (onDraftPo) { await onDraftPo({ pn: v.pn.trim(), qty: v.qty.trim() || undefined, price: v.price.trim() || undefined }); await refetchPo() } },
      }
      case 'wp': return {
        title: zh ? '组装工作包' : 'Build work package',
        intro: zh ? '把标准卡/AD·SB/MEL/未关闭项合并为工作包。间隔数字只来自间隔规则。' : 'Merge cards/AD·SB/MEL/open items. Interval numbers come only from rules.',
        fields: [
          { name: 'title', label: zh ? '标题' : 'Title' },
          { name: 'cards', label: zh ? '标准卡' : 'Cards', kind: 'textarea', hint: zh ? '逗号或换行分隔' : 'Comma or newline separated' },
          { name: 'ads', label: 'AD/SB', kind: 'textarea' },
          { name: 'mels', label: 'MEL', kind: 'textarea' },
          { name: 'open', label: zh ? '未关闭项' : 'Open items', kind: 'textarea' },
        ],
        submit: async v => { if (onBuildWp) { await onBuildWp({ title: v.title.trim() || undefined, cards: csv(v.cards), ads: csv(v.ads), mels: csv(v.mels), open: csv(v.open) }); await refetchPlan() } },
      }
      case 'interval': return {
        title: zh ? '登记间隔规则' : 'Add interval rule',
        intro: zh ? '到期数字的唯一来源。缺来源引用会触发 C7 但不拒绝录入。' : 'The only source of interval numbers. A missing cite trips C7 but is not rejected.',
        fields: [
          { name: 'taskKey', label: zh ? '任务号' : 'Task key', required: true },
          { name: 'intervalValue', label: zh ? '间隔值' : 'Interval', kind: 'number', required: true },
          { name: 'unit', label: zh ? '单位' : 'Unit', kind: 'select', required: true, options: ['FH', 'FC', 'DY', 'MO'].map(u => ({ value: u, label: u })) },
          { name: 'sourceCite', label: zh ? '来源引用' : 'Source cite' },
        ],
        submit: async v => { if (onAddInterval) { await onAddInterval({ taskKey: v.taskKey.trim(), intervalValue: num(v.intervalValue) ?? 0, unit: v.unit, sourceCite: v.sourceCite.trim() || undefined }); await refetchIntervals() } },
      }
      case 'interval-propose': return {
        title: zh ? '间隔复审草案' : 'Propose interval change',
        fields: [
          { name: 'taskKey', label: zh ? '任务号' : 'Task key', required: true },
          { name: 'mpdCite', label: 'MPD', required: true },
          { name: 'fleetCite', label: zh ? '机队依据' : 'Fleet cite', required: true },
        ],
        submit: async v => { if (onProposeInterval) { await onProposeInterval({ taskKey: v.taskKey.trim(), mpdCite: v.mpdCite.trim(), fleetCite: v.fleetCite.trim() }) } },
      }
      case 'schedule': return {
        title: zh ? '登记维修窗口' : 'Add maintenance window',
        fields: [
          { name: 'tailNo', label: zh ? '机尾' : 'Tail', required: true },
          { name: 'checkName', label: zh ? '检查名' : 'Check', required: true },
          { name: 'startOn', label: zh ? '开始' : 'Start', kind: 'date' },
          { name: 'endOn', label: zh ? '结束' : 'End', kind: 'date' },
          { name: 'hours', label: zh ? '工时' : 'Hours', kind: 'number' },
          { name: 'skill', label: zh ? '技能组' : 'Skill' },
        ],
        submit: async v => { if (onAddSchedule) { await onAddSchedule({ tailNo: v.tailNo.trim(), checkName: v.checkName.trim(), startOn: v.startOn || undefined, endOn: v.endOn || undefined, hours: num(v.hours), skill: v.skill.trim() || undefined }); await refetchConstraints() } },
      }
      case 'capacity': return {
        title: zh ? '设置机位工时' : 'Set capacity',
        fields: [
          { name: 'skill', label: zh ? '技能组' : 'Skill', required: true },
          { name: 'hours', label: zh ? '可用工时' : 'Available hours', kind: 'number', required: true },
        ],
        submit: async v => { if (onSetCapacity) { await onSetCapacity({ skill: v.skill.trim(), hours: num(v.hours) ?? 0 }); await refetchConstraints() } },
      }
      default: return null
    }
  }

  if (!enabled) {
    return (
      <main className="skill-center mro-workbench-page">
        <header className="mro-top">
          <h1 className="view-title">{zh ? '机务工作台' : 'MRO workbench'}</h1>
        </header>
        <p className="mro-enable-hint">{zh ? '先启用至少一位机务同事专家' : 'Enable at least one MRO colleague first'}</p>
      </main>
    )
  }

  const railButton = (id: Rail, label: string) => (
    <button type="button" key={id} aria-selected={rail === id} onClick={() => {
      setRail(id)
      if (id === 'fleet') setRegisterOpen(true)
    }}>{label}</button>
  )

  const toggleRailGroup = (id: string) => {
    setRailOpen(prev => {
      const next = { ...prev, [id]: !railGroupExpanded(id, rail, prev) }
      writeRailOpen(next)
      return next
    })
  }

  const subTabs = <T extends string>(tabs: Array<[T, string]>, value: T, onChange: (v: T) => void) => (
    <div className="mro-subtabs" role="tablist">
      {tabs.map(([id, label]) => (
        <button key={id} type="button" role="tab" aria-selected={value === id} onClick={() => onChange(id)}>{label}</button>
      ))}
    </div>
  )

  const stateHints = (
    <>
      {loading && <p role="status" className="mro-loading">{zh ? '加载中…' : 'Loading…'}</p>}
      {error && <p role="alert">{error}</p>}
    </>
  )

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
          mroExpertId={askExpertId}
          context={{ ...context, scenario }}
          prompt={rail === 'fault' ? symptoms : undefined}
          onOpened={onAskOpened}
          openChat={openChat}
        />
      </div>
      <div className="mro-body">
        <nav className="mro-rail" aria-label={zh ? '机务分区' : 'MRO sections'}>
          {MRO_RAIL_GROUPS.map(group => {
            const expanded = railGroupExpanded(group.id, rail, railOpen)
            return (
              <div key={group.id} className={`mro-rail-fold${expanded ? ' is-open' : ''}`}>
                <button
                  type="button"
                  className="mro-rail-group-btn"
                  aria-expanded={expanded}
                  aria-controls={`mro-rail-${group.id}`}
                  aria-label={`${zh ? group.label : group.labelEn}分组`}
                  onClick={() => toggleRailGroup(group.id)}
                >
                  {zh ? group.label : group.labelEn}
                </button>
                {expanded && (
                  <div id={`mro-rail-${group.id}`} className="mro-rail-leaves">
                    {group.rails.map(leaf => railButton(leaf as Rail, zh ? MRO_RAIL_LEAF_LABELS[leaf as MroRailLeaf].zh : MRO_RAIL_LEAF_LABELS[leaf as MroRailLeaf].en))}
                  </div>
                )}
              </div>
            )
          })}
        </nav>
        <section>
          {rail === 'fault' ? (
            <div className="mro-domain">
              <div className="mro-fault">
                <label>
                  <span>{zh ? '症状' : 'Symptoms'}</span>
                  <textarea value={symptoms} onChange={e => setSymptoms(e.target.value)} rows={6} placeholder={zh ? '起落架收放异常……' : 'Landing-gear retraction anomaly…'} />
                </label>
                <p className="mro-fault-meta">{zh ? `当前机尾 ${tailNo || '—'} · ${asOf}` : `Tail ${tailNo || '—'} · ${asOf}`}</p>
                <p className="mro-fault-hint">{zh ? '按 症状 → 候选故障 → 原因 → 任务 → 件号 组织，并标置信度低/中/高。每条关键句要有修订版引用。' : 'Organize as symptom → candidate → cause → task → PN, with low/medium/high confidence. Every key sentence needs a revision cite.'}</p>
              </div>
            </div>
          ) : rail === 'checklist' ? (
            <div className="mro-domain">
              <div className="mro-domain-head"><h2 className="view-subtitle">{zh ? '检查单' : 'Checklist'}</h2></div>
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
            </div>
          ) : rail === 'audit' ? (
            <div className="mro-domain">
              <div className="mro-domain-head"><h2 className="view-subtitle">{zh ? '审计' : 'Audit'}</h2></div>
              {auditItems.length === 0 ? (
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
              )}
            </div>
          ) : rail === 'due' ? (
            <div className="mro-domain" aria-label={zh ? '到期与履历' : 'Due and history'}>
              <div className="mro-domain-head">
                <h2 className="view-subtitle">{zh ? '到期与履历' : 'Due & history'}</h2>
                <span className="mro-count">{dueRows.length}</span>
                <div className="mro-domain-actions">
                  {dueTab === 'list' && onAddDue && addBtn('due', zh ? '登记到期项' : 'Add due')}
                  {dueTab === 'list' && onRecordUtil && addBtn('util', zh ? '录利用率' : 'Record util')}
                  {dueTab === 'components' && onAddComponent && addBtn('component', zh ? '登记部件' : 'Add component')}
                  {dueTab === 'components' && onLifeEvent && componentRows.length > 0 && addBtn('life', zh ? '履历事件' : 'Life event')}
                  {dueTab === 'pirep' && onDraftPirep && addBtn('pirep', zh ? 'PIREP 草稿' : 'PIREP draft')}
                </div>
              </div>
              {subTabs<DueTab>([
                ['list', zh ? '到期' : 'Due'],
                ['components', zh ? '部件履历' : 'Components'],
                ['pirep', 'PIREP'],
                ['triggers', zh ? '触发器' : 'Triggers'],
              ], dueTab, setDueTab)}
              <div className="mro-domain-body mro-ops-rail">
                {stateHints}
                {dueTab === 'list' ? (
                  !(visibleDue && visibleDue.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有到期项。先手录利用率或导入飞行小时 CSV。数字由到期引擎计算，缺数据写「未录入」而不是 0。' : 'No due items yet. Record utilization or import a flight-hour CSV. The due engine never invents zeros.'}</p>
                      {onAddDue && <button type="button" className="primary" onClick={() => setFormKind('due')}>{zh ? '登记到期项' : 'Add due'}</button>}
                    </div>
                  ) : (
                    <table className="mro-manual-table">
                      <thead><tr><th>{zh ? '种类' : 'Kind'}</th><th>{zh ? '范围' : 'Scope'}</th><th>{zh ? '已用' : 'Used'}</th><th>{zh ? '限制' : 'Limit'}</th><th>{zh ? '到期' : 'Due'}</th><th>{zh ? '剩余' : 'Left'}</th><th>{zh ? '状态' : 'State'}</th></tr></thead>
                      <tbody>
                        {visibleDue.map(item => (
                          <tr key={item.id}>
                            <td>{item.kind}</td>
                            <td>{item.scope || '—'}</td>
                            <td>{item.usedMissing ? <Badge tone="is-warn">{zh ? '未录入' : 'missing'}</Badge> : item.usedValue}</td>
                            <td>{item.limitValue ?? '—'}</td>
                            <td>{item.dueAt || '—'}</td>
                            <td>{item.usedMissing ? '—' : (item.remaining ?? '—')}</td>
                            <td><Badge tone={dueBadgeClass(item.state)}>{item.label || item.state}</Badge></td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )
                ) : dueTab === 'components' ? (
                  !(visibleComponents && visibleComponents.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有部件履历。登记部件并记录装机/拆下/转移/报废，谱系按时间解析安装状态。' : 'No component history yet. Register components and log install/remove events; genealogy resolves install state.'}</p>
                      {onAddComponent && <button type="button" className="primary" onClick={() => setFormKind('component')}>{zh ? '登记部件' : 'Add component'}</button>}
                    </div>
                  ) : (
                    <ul className="mro-genealogy-list" aria-label={zh ? '部件谱系' : 'Component genealogy'}>
                      {visibleComponents.map(c => (
                        <li key={c.id}>
                          <div className="mro-genealogy-head">
                            <b>{c.sn}</b> · {c.pn}{c.tailNo ? ` · ${c.tailNo}` : ''} · {c.installed ? <Badge tone="is-ok">{zh ? '在装' : 'installed'}</Badge> : <Badge tone="is-warn">{zh ? '离位' : 'off'}</Badge>}
                          </div>
                          {c.events.length > 0 && (
                            <ol className="mro-life-events">
                              {c.events.map((e, i) => <li key={i}>{e.occurredAt} · {e.kind}{e.note ? ` · ${e.note}` : ''}</li>)}
                            </ol>
                          )}
                        </li>
                      ))}
                    </ul>
                  )
                ) : dueTab === 'pirep' ? (
                  !(visiblePireps && visiblePireps.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有 PIREP 草稿。抽取飞行/操作报告为草稿，人工确认后再流转。' : 'No PIREP drafts yet. Draft pilot/operator reports for human confirmation.'}</p>
                      {onDraftPirep && <button type="button" className="primary" onClick={() => setFormKind('pirep')}>{zh ? 'PIREP 草稿' : 'PIREP draft'}</button>}
                    </div>
                  ) : (
                    <ul className="mro-pirep-list" aria-label="PIREP">
                      {visiblePireps.map(p => (
                        <li key={p.id}>
                          <span>{p.tailNo} · {p.body}</span>
                          <Badge tone={p.state === 'confirmed' ? 'is-ok' : p.state === 'rejected' ? 'is-err' : 'is-warn'}>{p.state}</Badge>
                          {p.state === 'draft' && onConfirmPirep && (
                            <>
                              <button type="button" onClick={() => void onConfirmPirep({ id: p.id, state: 'confirmed' }).then(refetchPireps)}>{zh ? '确认转缺陷' : 'Confirm'}</button>
                              <button type="button" onClick={() => void onConfirmPirep({ id: p.id, state: 'rejected' }).then(refetchPireps)}>{zh ? '驳回' : 'Reject'}</button>
                            </>
                          )}
                        </li>
                      ))}
                    </ul>
                  )
                ) : (
                  !(visibleTriggers && visibleTriggers.length) ? (
                    <div className="mro-empty"><p>{zh ? '没有触发器。到期引擎判为到期/超期时，这里生成「安排检查或换件」的人工触发，从不自动动作。' : 'No triggers. Due/overdue scopes surface a human review trigger, never an automatic action.'}</p></div>
                  ) : (
                    <ul className="mro-constraint-legend" aria-label={zh ? '五类触发器' : 'Five trigger types'}>
                      {visibleTriggers.map((t, i) => (
                        <li key={`${t.kind}-${i}`}>
                          <Badge tone={dueBadgeClass(t.state)}>{t.category || t.kind}</Badge>
                          <span>{t.action}</span>
                        </li>
                      ))}
                    </ul>
                  )
                )}
              </div>
            </div>
          ) : rail === 'tools' ? (
            <div className="mro-domain" aria-label={zh ? '工具化工品' : 'Tools and chemicals'}>
              <div className="mro-domain-head">
                <h2 className="view-subtitle">{zh ? '工具化工品' : 'Tools & chemicals'}</h2>
                <div className="mro-domain-actions">
                  {toolTab === 'tools' && onAddTool && addBtn('tool', zh ? '登记工具' : 'Add tool')}
                  {toolTab === 'lots' && onAddLot && addBtn('lot', zh ? '登记批次' : 'Add lot')}
                  {toolTab === 'lots' && onIssueChem && lotRows.length > 0 && addBtn('chem-issue', zh ? '发放' : 'Issue')}
                  {toolTab === 'lots' && onRecordUse && lotRows.length > 0 && addBtn('lot-use', zh ? '登记使用' : 'Record use')}
                  {toolTab === 'kits' && onDefineKit && addBtn('kit', zh ? '定义套件' : 'Define kit')}
                </div>
              </div>
              {subTabs<ToolTab>([
                ['tools', zh ? '工具' : 'Tools'],
                ['lots', zh ? '批次' : 'Lots'],
                ['kits', zh ? '套件' : 'Kits'],
              ], toolTab, setToolTab)}
              <div className="mro-domain-body mro-ops-rail">
                {stateHints}
                {kitTodos.map(t => <p key={t.id} role="status" className="mro-todo-flag">{zh ? '套件待办' : 'Kit todo'} · {t.ref}</p>)}
                {toolTab === 'tools' ? (
                  !(toolRows && toolRows.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有工具台账。先手录工具号/校准到期。校准过期不能借出。' : 'No tool ledger yet. Record a tool first. Overdue calibration blocks checkout.'}</p>
                      {onAddTool && <button type="button" className="primary" onClick={() => setFormKind('tool')}>{zh ? '登记工具' : 'Add tool'}</button>}
                    </div>
                  ) : (
                    <table className="mro-manual-table">
                      <thead><tr><th>{zh ? '工具号' : 'Tool'}</th><th>SN</th><th>{zh ? '位置' : 'Loc'}</th><th>{zh ? '借用人' : 'Holder'}</th><th>{zh ? '校准到期' : 'Calib'}</th><th>{zh ? '状态' : 'Status'}</th><th /></tr></thead>
                      <tbody>
                        {toolRows.map(item => (
                          <tr key={item.id}>
                            <td>{item.toolNo}</td>
                            <td>{item.sn}</td>
                            <td>{item.location || '—'}</td>
                            <td>{item.holder || '—'}</td>
                            <td>{item.calibDue}</td>
                            <td><Badge tone={toolBadgeClass(item.status, item.checkoutBlocked, item.calibDue, asOf)}>{item.status}</Badge></td>
                            <td>
                              <button
                                type="button"
                                disabled={!!item.checkoutBlocked}
                                title={item.checkoutBlocked}
                                onClick={() => { if (onCheckoutTool) void onCheckoutTool(item.id).then(refetchTools) }}
                              >{zh ? '借出' : 'Checkout'}</button>
                              {onReturnTool && (item.status === 'out' || item.holder) && (
                                <button type="button" onClick={() => void onReturnTool(item.id).then(refetchTools)}>{zh ? '归还' : 'Return'}</button>
                              )}
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )
                ) : toolTab === 'lots' ? (
                  !(visibleLots && visibleLots.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有化学品批次。登记母批与子批，追溯到机尾使用记录。' : 'No chemical lots yet. Record parent/child lots to trace tail usage.'}</p>
                      {onAddLot && <button type="button" className="primary" onClick={() => setFormKind('lot')}>{zh ? '登记批次' : 'Add lot'}</button>}
                    </div>
                  ) : (
                    <ul className="mro-lot-list" aria-label={zh ? '批次追溯' : 'Lot trace'}>
                      {visibleLots.map(lot => (
                        <li key={lot.id}>
                          <span>
                            {lot.lotNo}{lot.parentLotId ? ` ← ${lot.parentLotId}` : ''} · {zh ? '机尾' : 'tails'} {lot.tails.join(', ') || '—'}
                            {lot.expires ? (
                              <Badge tone={lot.expires < asOf ? 'is-err' : lot.expires === asOf ? 'is-warn' : 'is-ok'}>
                                {lot.expires}{lot.expires < asOf ? (zh ? ' 过期' : ' expired') : ''}
                              </Badge>
                            ) : null}
                          </span>
                          <button type="button" onClick={() => runBulletin(lot)}>{zh ? '质量通报串查' : 'Bulletin chain'}</button>
                        </li>
                      ))}
                    </ul>
                  )
                ) : (
                  <div className="mro-kit-list">
                    {!(kitRows && kitRows.length) ? (
                      <div className="mro-empty">
                        <p>{zh ? '还没有套件。定义套件后比对模板需求与在库，列出缺件。' : 'No kits yet. Define a kit to compare required vs on-hand and list shortages.'}</p>
                        {onDefineKit && <button type="button" className="primary" onClick={() => setFormKind('kit')}>{zh ? '定义套件' : 'Define kit'}</button>}
                      </div>
                    ) : (
                      kitRows.map(kit => (
                        <p key={kit.id}>
                          {kit.name}{kit.missing.length ? <> · <Badge tone="is-warn">{zh ? '缺件' : 'missing'} {kit.missing.join(', ')}</Badge></> : ''}
                          {kit.missing.length > 0 && (
                            <button type="button" onClick={() => addKitTodo(kit)}>{zh ? '转航材待办' : 'To parts todo'}</button>
                          )}
                        </p>
                      ))
                    )}
                  </div>
                )}
              </div>
            </div>
          ) : rail === 'parts' ? (
            <div className="mro-domain" aria-label={zh ? '航材' : 'Parts'}>
              <div className="mro-domain-head">
                <h2 className="view-subtitle">{zh ? '航材' : 'Parts'}</h2>
                <div className="mro-domain-actions">
                  {partTab === 'stock' && onAddStock && addBtn('stock', zh ? '登记库存' : 'Add stock')}
                  {partTab === 'alternates' && onAddAlternate && addBtn('alternate', zh ? '登记替代件' : 'Add alternate')}
                  {partTab === 'aog' && onIntakeAog && addBtn('aog', zh ? 'AOG 抽取' : 'AOG intake')}
                  {partTab === 'po' && onDraftPo && addBtn('po', zh ? '采购草稿' : 'PO draft')}
                </div>
              </div>
              {subTabs<PartTab>([
                ['stock', zh ? '库存' : 'Stock'],
                ['alternates', zh ? '替代件' : 'Alternates'],
                ['aog', 'AOG'],
                ['po', zh ? '采购草稿' : 'PO drafts'],
                ['source', zh ? '库存来源' : 'Source'],
              ], partTab, setPartTab)}
              <div className="mro-domain-body mro-ops-rail">
                {stateHints}
                {partsTodos.map(t => <p key={t.id} role="status" className="mro-todo-flag">{zh ? '航材待办' : 'Parts todo'} · {t.ref}</p>)}
                {partTab === 'stock' ? (
                  !(stockRows && stockRows.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有本机航材台账。未绑定数据源时诚实降级为「未绑定库存」，不编造数量。' : 'No local parts ledger. Without a bound datasource this rail says stock is unbound and never invents qty.'}</p>
                      {onAddStock && <button type="button" className="primary" onClick={() => setFormKind('stock')}>{zh ? '登记库存' : 'Add stock'}</button>}
                    </div>
                  ) : (
                    <table className="mro-manual-table">
                      <thead><tr><th>PN</th><th>{zh ? '数量' : 'Qty'}</th><th>{zh ? '来源' : 'Source'}</th></tr></thead>
                      <tbody>{stockRows.map(row => <tr key={row.pn}><td>{row.pn}</td><td>{row.qty}</td><td>{row.source}</td></tr>)}</tbody>
                    </table>
                  )
                ) : partTab === 'alternates' ? (
                  !(altRows && altRows.length) ? (
                    <div className="mro-empty"><p>{zh ? '还没有替代件。替代件必须同时满足认证有效、适用构型、库存可用，否则降级询价。' : 'No alternates yet. An alternate must pass cert, effectivity and stock, else it degrades to quote-only.'}</p></div>
                  ) : (
                    <table className="mro-manual-table">
                      <thead><tr><th>{zh ? '原件' : 'From'}</th><th>{zh ? '替代' : 'To'}</th><th>{zh ? '状态' : 'State'}</th></tr></thead>
                      <tbody>{altRows.map(row => (
                        <tr key={`${row.pnFrom}-${row.pnTo}`}>
                          <td>{row.pnFrom}</td><td>{row.pnTo}</td>
                          <td>{row.accepted ? <Badge tone="is-ok">{zh ? '可用' : 'accepted'}</Badge> : <Badge tone="is-warn">{zh ? '降级询价' : 'quote only'}</Badge>}</td>
                        </tr>
                      ))}</tbody>
                    </table>
                  )
                ) : partTab === 'aog' ? (
                  !(visibleAog && visibleAog.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有 AOG 案子。粘贴 AOG 通知抽取机尾/PN/数量为草稿，永不自动下单。' : 'No AOG cases yet. Paste an AOG note to draft tail/PN/qty. Never auto-purchases.'}</p>
                      {onIntakeAog && <button type="button" className="primary" onClick={() => setFormKind('aog')}>{zh ? 'AOG 抽取' : 'AOG intake'}</button>}
                    </div>
                  ) : (
                    <table className="mro-manual-table">
                      <thead><tr><th>{zh ? '机尾' : 'Tail'}</th><th>PN</th><th>{zh ? '数量' : 'Qty'}</th><th>{zh ? '状态' : 'State'}</th><th /></tr></thead>
                      <tbody>{visibleAog.map(a => (
                        <tr key={a.id}>
                          <td>{a.tailNo}</td><td>{a.pn || '—'}</td><td>{a.qty || '—'}</td>
                          <td><Badge tone={a.state === 'confirmed' ? 'is-ok' : a.state === 'rejected' ? 'is-err' : 'is-warn'}>{a.state}</Badge></td>
                          <td>
                            {a.state === 'draft' && onConfirmAog && (
                              <>
                                <button type="button" onClick={() => void onConfirmAog({ id: a.id, state: 'confirmed' }).then(refetchAog)}>{zh ? '确认入案' : 'Confirm'}</button>
                                <button type="button" onClick={() => void onConfirmAog({ id: a.id, state: 'rejected' }).then(refetchAog)}>{zh ? '驳回' : 'Reject'}</button>
                              </>
                            )}
                            {onDraftPo && a.pn && <button type="button" onClick={() => { void onDraftPo({ pn: a.pn || '', qty: a.qty }).then(refetchPo); setPartTab('po') }}>{zh ? '生成采购草稿' : 'Draft PO'}</button>}
                          </td>
                        </tr>
                      ))}</tbody>
                    </table>
                  )
                ) : partTab === 'po' ? (
                  !(poRows && poRows.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有采购草稿。草稿只是建议，下单与确认仍由人工完成。' : 'No PO drafts yet. Drafts are advisory; ordering stays human.'}</p>
                      {onDraftPo && <button type="button" className="primary" onClick={() => setFormKind('po')}>{zh ? '采购草稿' : 'PO draft'}</button>}
                    </div>
                  ) : (
                    <table className="mro-manual-table">
                      <thead><tr><th>PN</th><th>{zh ? '数量' : 'Qty'}</th><th>{zh ? '价格' : 'Price'}</th><th>{zh ? '状态' : 'State'}</th><th /></tr></thead>
                      <tbody>{poRows.map(p => (
                        <tr key={p.id}>
                          <td>{p.pn}</td><td>{p.qty || '—'}</td><td>{p.price || '—'}</td>
                          <td><Badge tone={p.state === 'confirmed' ? 'is-ok' : p.state === 'rejected' ? 'is-err' : 'is-warn'}>{p.state}</Badge></td>
                          <td>
                            {p.state === 'draft' && onConfirmPo && (
                              <>
                                <button type="button" onClick={() => void onConfirmPo({ id: p.id, state: 'confirmed' }).then(refetchPo)}>{zh ? '确认' : 'Confirm'}</button>
                                <button type="button" onClick={() => void onConfirmPo({ id: p.id, state: 'rejected' }).then(refetchPo)}>{zh ? '驳回' : 'Reject'}</button>
                              </>
                            )}
                          </td>
                        </tr>
                      ))}</tbody>
                    </table>
                  )
                ) : partTab === 'source' ? (
                  <div className="mro-datasource">
                    {verified.length === 0 ? (
                      <div className="mro-empty"><p>{zh ? '先在设置 → 数据源探测连接' : 'Probe a connection in Settings → Data sources first.'}</p></div>
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
                        <label><span>schema</span><input value={stock.schema} onChange={e => setStock(v => ({ ...v, schema: e.target.value }))} /></label>
                        <label><span>{zh ? '表' : 'Table'}</span><input value={stock.table} onChange={e => setStock(v => ({ ...v, table: e.target.value }))} /></label>
                        <label><span>PN</span><input value={stock.pnColumn} onChange={e => setStock(v => ({ ...v, pnColumn: e.target.value }))} /></label>
                        <label><span>{zh ? '工位' : 'Station'}</span><input value={stock.stationColumn} onChange={e => setStock(v => ({ ...v, stationColumn: e.target.value }))} /></label>
                        <label><span>{zh ? '数量' : 'Qty'}</span><input value={stock.qtyColumn} onChange={e => setStock(v => ({ ...v, qtyColumn: e.target.value }))} /></label>
                        {bindMsg && <p role="status">{bindMsg}</p>}
                        <button className="primary" disabled={!stock.connectionId || !stock.schema.trim() || !stock.table.trim() || !stock.pnColumn.trim()}>{zh ? '绑定库存' : 'Bind stock'}</button>
                      </form>
                    )}
                  </div>
                ) : null}
              </div>
            </div>
          ) : rail === 'plan' ? (
            <div className="mro-domain" aria-label={zh ? '维修计划' : 'Maintenance plan'}>
              <div className="mro-domain-head">
                <h2 className="view-subtitle">{zh ? '维修计划' : 'Maintenance plan'}</h2>
                <span className="mro-count">{planRows.length}</span>
                <div className="mro-domain-actions">
                  {planTab === 'wp' && onBuildWp && addBtn('wp', zh ? '组装工作包' : 'Build WP')}
                  {planTab === 'intervals' && onAddInterval && addBtn('interval', zh ? '登记间隔' : 'Add interval')}
                  {planTab === 'intervals' && onProposeInterval && addBtn('interval-propose', zh ? '复审草案' : 'Propose')}
                  {planTab === 'schedule' && onAddSchedule && addBtn('schedule', zh ? '登记窗口' : 'Add window')}
                  {planTab === 'schedule' && onSetCapacity && addBtn('capacity', zh ? '设置工时' : 'Set capacity')}
                  {planTab === 'schedule' && constraintList && <button type="button" onClick={() => void refetchConstraints()}>{zh ? '运行检查' : 'Run check'}</button>}
                </div>
              </div>
              {subTabs<PlanTab>([
                ['wp', zh ? '工作包' : 'Work packages'],
                ['intervals', zh ? '间隔规则' : 'Intervals'],
                ['schedule', zh ? '窗口与约束' : 'Schedule'],
              ], planTab, setPlanTab)}
              <div className="mro-domain-body mro-ops-rail">
                {stateHints}
                {planTab === 'wp' ? (
                  !(planRows && planRows.length) ? (
                    <div className="mro-empty">
                      <p>{zh ? '还没有工作包。间隔数字只来自 interval_rules；发布需确认，发布后只生成套件/航材待办。' : 'No work packages yet. Interval numbers come only from interval_rules. Publish asks for confirmation and only writes kit/parts todos.'}</p>
                      {onBuildWp && <button type="button" className="primary" onClick={() => setFormKind('wp')}>{zh ? '组装工作包' : 'Build WP'}</button>}
                    </div>
                  ) : (
                    <ul className="mro-wp-list">
                      {planRows.map(pkg => (
                        <li key={pkg.id}>
                          <span>{pkg.title} · {pkg.sources.join(' + ')}</span>
                          {onPublishSchedule && <button type="button" onClick={() => { if (window.confirm(zh ? '确认发布此工作包？只生成待办，不写生产库。' : 'Publish this package? This only writes todos, not the production MRO.')) void onPublishSchedule(pkg.id).then(result => { if (result.todos) setTodoRows(result.todos) }) }}>{zh ? '发布' : 'Publish'}</button>}
                        </li>
                      ))}
                    </ul>
                  )
                ) : planTab === 'intervals' ? (
                  !(intervalRows && intervalRows.length) ? (
                    <div className="mro-empty"><p>{zh ? '还没有间隔规则。到期数字只来自这里；缺来源引用会触发 C7。' : 'No interval rules yet. Interval numbers come only from here; a missing cite trips C7.'}</p></div>
                  ) : (
                    <table className="mro-manual-table">
                      <thead><tr><th>{zh ? '任务' : 'Task'}</th><th>{zh ? '间隔' : 'Interval'}</th><th>{zh ? '单位' : 'Unit'}</th><th>{zh ? '版本' : 'Ver'}</th><th>{zh ? '来源' : 'Cite'}</th></tr></thead>
                      <tbody>{intervalRows.map((r, i) => <tr key={i}><td>{r.taskKey}</td><td>{r.intervalValue}</td><td>{r.unit}</td><td>{r.version || '1'}</td><td>{r.sourceCite ? r.sourceCite : <Badge tone="is-warn">{zh ? '缺引用' : 'no cite'}</Badge>}</td></tr>)}</tbody>
                    </table>
                  )
                ) : planTab === 'schedule' ? (
                  <div className="mro-constraints">
                    <ul className="mro-constraint-legend" aria-label={zh ? 'C1–C7 约束' : 'C1–C7 constraints'}>
                      {CONSTRAINT_CODES.map(item => {
                        const failed = violations.some(v => v.code === item.code)
                        return (
                          <li key={item.code}>
                            <Badge tone={failed ? 'is-err' : 'is-ok'}>{item.code}</Badge>
                            <span>{zh ? item.zh : item.en}</span>
                          </li>
                        )
                      })}
                    </ul>
                    {violations.length > 0 ? (
                      <ul className="mro-violations" aria-label={zh ? '约束违反' : 'Constraint violations'}>
                        {violations.map(item => <li key={item.code}><Badge tone="is-err">{item.code}</Badge> {item.detail}</li>)}
                      </ul>
                    ) : (
                      <p className="mro-constraint-ok" role="status">{zh ? 'C1–C7 全部通过或暂无数据。录入机位工时与窗口后自动校验。' : 'All C1–C7 pass or no data yet. Record capacity and windows to auto-check.'}</p>
                    )}
                  </div>
                ) : null}
              </div>
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
            <div className="mro-domain">
              <div className="mro-domain-head">
                <h2 className="view-subtitle">{zh ? '手册' : 'Manuals'}</h2>
                <span className="mro-count">{manuals.length}</span>
                <div className="mro-domain-actions">
                  <button type="button" onClick={() => setImportOpen(true)}>{zh ? '导入手册' : 'Import manual'}</button>
                </div>
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
            </div>
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
      <QuickForm spec={formKind ? formSpec(formKind) : null} onClose={closeForm} />
    </main>
  )
}
