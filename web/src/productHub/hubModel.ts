import { PAGE_ATLAS, isHubPage } from './hubCatalog'
import type { HubChange, HubEdge, HubFinding, HubNode, HubOverview } from './productHubTypes'

export const NODE_TYPES = [
  'Product', 'Domain', 'Module', 'Feature', 'Expert', 'Skill', 'Plugin',
  'Mcp', 'McpTool', 'Chain', 'Step', 'Capability', 'Scenario',
] as const

export const HUB_TYPES = new Set(['Product', 'Domain', 'Module'])

export const DOMAIN_META: Record<string, { zh: string; en: string }> = {
  dialog: { zh: '对话体验', en: 'DIALOG' },
  office: { zh: '业务工作台', en: 'OFFICE' },
  assets: { zh: '资产与智能', en: 'ASSETS' },
  execution: { zh: '执行与控制', en: 'EXECUTION' },
  foundation: { zh: '底座与治理', en: 'FOUNDATION' },
}

export const DOMAIN_ORDER = ['dialog', 'office', 'assets', 'execution', 'foundation'] as const

export type AssetStat = { key: string; label: string; n: number; delta?: number }

export type HubModuleRow = {
  id: string
  name: string
  shortKey: string
  domain: string
  summary: string
  features: HubNode[]
}

export type DomainCardModel = {
  id: string
  name: string
  en: string
  modules: number
  cards: number
  passed: number
  total: number
  moduleNames: string[]
}

export function isFeatureLike(node: HubNode): boolean {
  return node.type === 'Feature' || (node.type === 'Scenario' && node.stable_key.startsWith('landscape.'))
}

export function isLandscape(node: HubNode): boolean {
  return node.stable_key.startsWith('landscape.') && (node.type === 'Feature' || node.type === 'Scenario')
}

export function domainKey(node: HubNode): string {
  if (node.domain) return node.domain
  const fromId = node.id.match(/^domain\.([^.]+)/)
  if (fromId) return fromId[1]
  const fromKey = node.stable_key.match(/^(?:domain|module|feature)\.([^.]+)/)
  return fromKey?.[1] ?? 'foundation'
}

export function moduleSlug(node: HubNode): string {
  if (node.module) return node.module
  if (node.type === 'Module') return node.stable_key.replace(/^module\.[^.]+\./, '') || node.stable_key
  const feature = node.stable_key.match(/^feature\.[^.]+\.([^.]+)/)
  if (feature) return feature[1]
  const moduleKey = node.stable_key.match(/^module\.[^.]+\.([^.]+)/)
  return moduleKey?.[1] ?? ''
}

export function countByType(nodes: HubNode[], type: string): number {
  return nodes.reduce((n, node) => n + (node.type === type ? 1 : 0), 0)
}

export function assetStats(nodes: HubNode[], overview?: HubOverview): AssetStat[] {
  const features = countByType(nodes, 'Feature')
  const chains = countByType(nodes, 'Chain') || features
  const extra = overview?.assetCounts ?? {}
  const pick = (key: string, fallback: number, delta?: number): { n: number; delta?: number } => {
    const hit = extra[key]
    return { n: hit?.n ?? fallback, delta: hit?.delta ?? delta }
  }
  return [
    { key: 'Expert', label: '专家', ...pick('Expert', countByType(nodes, 'Expert')) },
    { key: 'Skill', label: '技能', ...pick('Skill', countByType(nodes, 'Skill')) },
    { key: 'Plugin', label: '插件', ...pick('Plugin', countByType(nodes, 'Plugin')) },
    { key: 'Mcp', label: 'MCP', ...pick('Mcp', countByType(nodes, 'Mcp')) },
    { key: 'McpTool', label: 'MCP工具', ...pick('McpTool', countByType(nodes, 'McpTool')) },
    { key: 'Capability', label: '能力', ...pick('Capability', countByType(nodes, 'Capability')) },
    { key: 'Feature', label: '功能卡', ...pick('Feature', features || overview?.cardCount || 0, overview?.added) },
    { key: 'Chain', label: '链路', ...pick('Chain', chains, overview?.updated) },
    { key: 'Scenario', label: '场景', ...pick('Scenario', countByType(nodes, 'Scenario')) },
    { key: 'Memory', label: '记忆事实', ...pick('Memory', 0) },
  ]
}

export function moduleRows(nodes: HubNode[], edges: HubEdge[]): HubModuleRow[] {
  const features = nodes.filter(node => isFeatureLike(node) && !isLandscape(node))
  const modules = nodes.filter(node => node.type === 'Module')
  const childIds = new Map<string, string[]>()
  for (const edge of edges) {
    if (edge.rel !== 'contains') continue
    const list = childIds.get(edge.from) ?? []
    list.push(edge.to)
    childIds.set(edge.from, list)
  }
  const featureById = new Map(features.map(node => [node.id, node]))
  const claimed = new Set<string>()
  if (modules.length > 0) {
    return modules.map(mod => {
      const slug = moduleSlug(mod)
      const kids = (childIds.get(mod.id) ?? []).flatMap(id => {
        const hit = featureById.get(id) ?? features.find(node => node.stable_key === id)
        return hit ? [hit] : []
      })
      const bySlug = features.filter(node => {
        if (claimed.has(node.id) || kids.some(kid => kid.id === node.id)) return false
        return moduleSlug(node) === slug && (!node.domain || domainKey(node) === domainKey(mod))
      })
      let listed = [...kids, ...bySlug]
      if (listed.length === 0) {
        const siblings = modules.filter(item => domainKey(item) === domainKey(mod))
        if (siblings.length === 1) {
          listed = features.filter(node => domainKey(node) === domainKey(mod) && !claimed.has(node.id))
        }
      }
      listed.forEach(node => claimed.add(node.id))
      return {
        id: mod.id,
        name: mod.name,
        shortKey: slug,
        domain: domainKey(mod),
        summary: listed.map(node => node.name).slice(0, 3).join(' / '),
        features: listed,
      }
    })
  }
  const grouped = new Map<string, HubNode[]>()
  for (const node of features) {
    const key = domainKey(node)
    const list = grouped.get(key) ?? []
    list.push(node)
    grouped.set(key, list)
  }
  return [...grouped.entries()].map(([domain, list]) => ({
    id: `synth.${domain}`,
    name: DOMAIN_META[domain]?.zh ?? domain,
    shortKey: domain,
    domain,
    summary: list.map(node => node.name).slice(0, 3).join(' / '),
    features: list,
  }))
}

export function domainCards(overview: HubOverview | undefined, nodes: HubNode[], edges: HubEdge[], findings: HubFinding[]): DomainCardModel[] {
  const rows = moduleRows(nodes, edges)
  const openKeys = new Set(findings.filter(item => item.status === 'open' && (item.severity === 'error' || item.severity === 'warn')).map(item => item.stable_key))
  const fromOverview = new Map((overview?.domains ?? []).map(item => [item.id.replace(/^domain\./, ''), item]))
  return DOMAIN_ORDER.map(id => {
    const mods = rows.filter(row => row.domain === id)
    const cards = mods.reduce((n, row) => n + row.features.length, 0)
    const ov = fromOverview.get(id)
    const total = Math.max(cards, ov?.cards ?? 0, 1)
    const failed = mods.flatMap(row => row.features).filter(node => openKeys.has(node.stable_key)).length
    const meta = DOMAIN_META[id]
    return {
      id,
      name: ov?.name || meta?.zh || id,
      en: meta?.en ?? id.toUpperCase(),
      modules: ov?.modules ?? mods.length,
      cards: ov?.cards ?? cards,
      passed: Math.max(0, total - failed),
      total,
      moduleNames: mods.map(row => row.name),
    }
  })
}

export function formatSnapshot(iso?: string): string {
  if (!iso) return '—'
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const mm = String(date.getUTCMonth() + 1).padStart(2, '0')
  const dd = String(date.getUTCDate()).padStart(2, '0')
  const hh = String(date.getUTCHours()).padStart(2, '0')
  const mi = String(date.getUTCMinutes()).padStart(2, '0')
  return `${mm}-${dd} ${hh}:${mi}`
}

export function healthTone(score: number): string {
  if (score >= 90) return 'var(--ph-brand)'
  if (score >= 70) return 'var(--ph-cyan)'
  return 'var(--ph-warn)'
}

export function tagKind(tag: string): 'scenario' | 'capability' | 'entry' | 'status' {
  const raw = tag.toLocaleLowerCase()
  if (raw.startsWith('status') || raw.includes('状态') || raw.includes('核心')) return 'status'
  if (raw.startsWith('capability') || raw.includes('能力')) return 'capability'
  if (raw.startsWith('scenario') || raw.includes('场景') || raw.includes('娱乐')) return 'scenario'
  return 'entry'
}

export function probeLabel(passed: number, total: number): string {
  if (total <= 0) return '—'
  return passed >= total ? `${passed}/${total} ✓` : `${passed}/${total} !`
}

export function findingTone(severity: string): 'error' | 'warn' | 'info' {
  if (severity === 'error') return 'error'
  if (severity === 'warn' || severity === 'warning') return 'warn'
  return 'info'
}

export function changeKindLabel(kind: string, zh: boolean): string {
  if (kind === 'added') return zh ? '新增' : 'Added'
  if (kind === 'removed') return zh ? '弃用' : 'Removed'
  return zh ? '更新' : 'Updated'
}

export const TYPE_LABELS: Record<string, { zh: string; en: string }> = {
  Product: { zh: '产品', en: 'Product' },
  Domain: { zh: '能力域', en: 'Domain' },
  Module: { zh: '模块', en: 'Module' },
  Feature: { zh: '功能卡', en: 'Feature' },
  Expert: { zh: '专家', en: 'Expert' },
  Skill: { zh: '技能', en: 'Skill' },
  Plugin: { zh: '插件', en: 'Plugin' },
  Mcp: { zh: 'MCP', en: 'MCP' },
  McpTool: { zh: 'MCP 工具', en: 'MCP tool' },
  Chain: { zh: '链路', en: 'Chain' },
  Step: { zh: '步骤', en: 'Step' },
  Capability: { zh: '能力', en: 'Capability' },
  Scenario: { zh: '场景', en: 'Scenario' },
  体系: { zh: '体系结构', en: 'System' },
}

export function typeLabel(type: string, zh: boolean): string {
  const hit = TYPE_LABELS[type]
  return hit ? (zh ? hit.zh : hit.en) : type
}

export function matchesQuery(node: HubNode, query: string): boolean {
  const q = query.trim().toLocaleLowerCase()
  if (!q) return true
  if (q === '放歌' && node.stable_key.includes('music')) return true
  return node.name.toLocaleLowerCase().includes(q) || node.stable_key.toLocaleLowerCase().includes(q)
}

const SEARCHABLE = new Set(['Feature', 'Scenario', 'Module', 'Expert', 'Skill', 'Plugin', 'Domain', 'Mcp', 'Capability'])

export function searchHubNodes(nodes: HubNode[], query: string): HubNode[] {
  const q = query.trim().toLocaleLowerCase()
  if (q.length < 1) return []
  return nodes
    .flatMap(node => {
      if (!SEARCHABLE.has(node.type) && !isFeatureLike(node)) return []
      if (!matchesQuery(node, q)) return []
      const name = node.name.toLocaleLowerCase()
      const key = node.stable_key.toLocaleLowerCase()
      const score = name === q ? 0 : name.startsWith(q) ? 1 : key.startsWith(q) ? 2 : name.includes(q) ? 3 : 4
      return [{ node, score }]
    })
    .toSorted((a, b) => a.score - b.score || a.node.name.localeCompare(b.node.name, 'zh'))
    .slice(0, 10)
    .map(item => item.node)
}

export function changeImpactLabel(key: string, nodes: HubNode[]): string {
  if (isHubPage(key)) return PAGE_ATLAS[key].name
  const pageKey = key.startsWith('page.') ? key.slice(5) : ''
  if (isHubPage(pageKey)) return PAGE_ATLAS[pageKey].name
  return nodes.find(node => node.stable_key === key || node.id === key)?.name ?? key.split('.').slice(-1)[0]
}

export function splitLines(text: string): string[] {
  return text.split(/[；;\n]/).map(item => item.trim()).filter(Boolean)
}

export function versionLabel(editionId?: string): string {
  if (!editionId) return '—'
  const hit = editionId.match(/v?\d+\.\d+\.\d+(?:\.\d+)?/i)
  return hit?.[0] ?? editionId.slice(0, 18)
}

export const GRAPH_NODE_W = 148
export const GRAPH_NODE_H = 36
export const GRAPH_LAYER_GAP = 100
export const GRAPH_PAD = 40

export function wrapLabel(text: string, maxChars: number, maxLines = 2): string[] {
  const clean = text.trim()
  const width = Math.max(2, maxChars)
  if (!clean) return ['']
  if (clean.length <= width) return [clean]
  const lines: string[] = []
  let index = 0
  while (index < clean.length && lines.length < maxLines) {
    const last = lines.length === maxLines - 1
    const remain = clean.length - index
    if (remain <= width) {
      lines.push(clean.slice(index))
      break
    }
    const take = last ? width - 1 : width
    const slice = clean.slice(index, index + take)
    const br = Math.max(slice.lastIndexOf(' '), slice.lastIndexOf('·'), slice.lastIndexOf('.'), slice.lastIndexOf('/'), slice.lastIndexOf('-'))
    const cut = !last && br >= Math.ceil(take * 0.3) ? br + 1 : take
    const piece = clean.slice(index, index + cut).trim()
    lines.push(last ? `${piece}…` : piece)
    index += cut
    while (clean[index] === ' ') index += 1
  }
  return lines
}

export type GraphLayoutNode = {
  id: string
  key: string
  label: string
  type: string
  x: number
  y: number
}

export type GraphLayoutEdge = {
  from: string
  to: string
  rel: string
}

export type GraphLayout = {
  nodes: GraphLayoutNode[]
  edges: GraphLayoutEdge[]
  width: number
  height: number
  nodeW: number
  nodeH: number
  fitScale?: number
}

export function graphFitScale(
  size: { width: number; height: number },
  viewport: { w: number; h: number },
  pad = 20,
): number {
  const sx = Math.max(48, viewport.w - pad) / Math.max(1, size.width)
  const sy = Math.max(48, viewport.h - pad) / Math.max(1, size.height)
  return Math.min(sx, sy)
}

function outgoing(edges: HubEdge[], from: string, rel?: string): HubEdge[] {
  return edges.filter(edge => edge.from === from && (!rel || edge.rel === rel))
}

function layerXs(count: number, width: number): number[] {
  if (count <= 0) return []
  if (count === 1) return [width / 2]
  const usable = width - GRAPH_PAD * 2
  const step = usable / (count - 1)
  return Array.from({ length: count }, (_, i) => GRAPH_PAD + i * step)
}

export function graphLayout(
  nodes: HubNode[],
  edges: HubEdge[],
  opts: { domain?: string; type?: string; focusId?: string; viewport?: { w: number; h: number } },
): GraphLayout {
  const byId = new Map(nodes.map(node => [node.id, node]))
  const byKey = new Map(nodes.map(node => [node.stable_key, node]))
  const resolve = (id: string) => byId.get(id) ?? byKey.get(id)
  const inDomain = (node: HubNode) => !opts.domain || domainKey(node) === opts.domain
  const product = nodes.find(node => node.type === 'Product')
  const domains = nodes.filter(node => node.type === 'Domain' && inDomain(node))
    .toSorted((a, b) => DOMAIN_ORDER.indexOf(domainKey(a) as typeof DOMAIN_ORDER[number]) - DOMAIN_ORDER.indexOf(domainKey(b) as typeof DOMAIN_ORDER[number]))
  const focus = (opts.focusId ? resolve(opts.focusId) : undefined) ?? nodes.find(isFeatureLike)
  const systemOnly = opts.type === '体系'
  const typeFilter = opts.type && opts.type !== '全部类型' && opts.type !== '体系' ? opts.type : ''

  const sortZh = (list: HubNode[]) => list.toSorted((a, b) => a.name.localeCompare(b.name, 'zh'))
  const containedBy = (parent: HubNode) => {
    const kids = new Map<string, HubNode>()
    for (const edge of [...outgoing(edges, parent.id, 'contains'), ...outgoing(edges, parent.stable_key, 'contains')]) {
      const node = resolve(edge.to)
      if (node) kids.set(node.id, node)
    }
    return [...kids.values()]
  }

  const modules = sortZh(nodes.filter(node => node.type === 'Module' && inDomain(node)))
  const features = systemOnly ? [] : sortZh(nodes.filter(node => isFeatureLike(node) && inDomain(node) && (!typeFilter || typeFilter === 'Feature' || typeFilter === 'Scenario' || node.type === typeFilter)))
  const assets = sortZh(nodes.filter(node => (
    node.type === 'Expert' || node.type === 'Skill' || node.type === 'Plugin' || node.type === 'Mcp'
  ) && inDomain(node) && (!typeFilter || node.type === typeFilter || typeFilter === 'Module')))

  const moduleByDomain = new Map<string, HubNode[]>()
  const featureByDomain = new Map<string, HubNode[]>()
  const assetByDomain = new Map<string, HubNode[]>()
  for (const domain of domains) {
    moduleByDomain.set(domain.id, [])
    featureByDomain.set(domain.id, [])
    assetByDomain.set(domain.id, [])
  }
  const domainIdFor = (node: HubNode) => {
    const keyed = domains.find(domain => domainKey(domain) === domainKey(node))
    if (keyed) return keyed.id
    for (const domain of domains) {
      if (containedBy(domain).some(child => child.id === node.id)) return domain.id
    }
    return ''
  }
  for (const node of modules) {
    const id = domainIdFor(node)
    if (id) moduleByDomain.get(id)?.push(node)
  }
  for (const node of features) {
    const id = domainIdFor(node)
    if (id) featureByDomain.get(id)?.push(node)
  }
  for (const node of assets) {
    const id = domainIdFor(node)
    if (id && !modules.some(item => item.id === node.id)) assetByDomain.get(id)?.push(node)
  }

  const leaves: HubNode[] = []
  const leafSeen = new Set<string>()
  const pushLeaf = (node?: HubNode) => {
    if (!node || leafSeen.has(node.id) || !inDomain(node)) return
    if (systemOnly) return
    leafSeen.add(node.id)
    leaves.push(node)
  }
  if (focus && isFeatureLike(focus)) {
    for (const edge of [...outgoing(edges, focus.id), ...outgoing(edges, focus.stable_key)]) {
      if (edge.rel === 'contains') continue
      pushLeaf(resolve(edge.to))
    }
  }
  if (typeFilter && typeFilter !== 'Feature' && typeFilter !== 'Scenario' && typeFilter !== 'Module') {
    nodes.filter(node => node.type === typeFilter && inDomain(node)).forEach(node => pushLeaf(node))
  }

  const nodeW = 128
  const nodeH = 40
  const rowGap = 14
  const packGap = 10
  const colGap = 24
  const packCols = (count: number) => (count > 1 ? 2 : 1)
  const packWidth = (count: number, cols: number) => {
    const used = Math.min(Math.max(1, cols), Math.max(1, count))
    return used * nodeW + (used - 1) * packGap
  }
  const packHeight = (count: number, cols: number) => {
    if (count <= 0) return 0
    const rows = Math.ceil(count / Math.max(1, cols))
    return rows * nodeH + (rows - 1) * rowGap
  }

  const columns = (domains.length ? domains : [{ id: '_all', stable_key: '_all', type: 'Domain' as const, name: '' }]).map(domain => {
    const mods = domain.id === '_all' ? modules : [...(moduleByDomain.get(domain.id) ?? []), ...(assetByDomain.get(domain.id) ?? [])]
    const feats = domain.id === '_all' ? features : (featureByDomain.get(domain.id) ?? [])
    const mCols = packCols(mods.length)
    const fCols = packCols(feats.length)
    return {
      domain: domain.id === '_all' ? undefined : domain,
      mods,
      feats,
      mCols,
      fCols,
      innerW: Math.max(nodeW, packWidth(mods.length, mCols), packWidth(feats.length, fCols)),
      modH: packHeight(mods.length, mCols),
      featH: packHeight(feats.length, fCols),
    }
  })

  const maxModH = Math.max(0, ...columns.map(column => column.modH))
  const maxFeatH = Math.max(0, ...columns.map(column => column.featH))
  const band = (size: number) => size > 0 ? size + 26 : 0
  let cursorX = GRAPH_PAD
  const colLefts: number[] = []
  const colCenters: number[] = []
  for (const column of columns) {
    colLefts.push(cursorX)
    colCenters.push(cursorX + column.innerW / 2)
    cursorX += column.innerW + colGap
  }
  const width = Math.max(GRAPH_PAD * 2 + nodeW, cursorX - (columns.length ? colGap : 0) + GRAPH_PAD)
  let cursorY = GRAPH_PAD
  const productY = product ? cursorY + nodeH / 2 : 0
  if (product) cursorY += nodeH + 32
  const domainY = columns.some(column => column.domain) ? cursorY + nodeH / 2 : 0
  if (columns.some(column => column.domain)) cursorY += nodeH + 22
  const moduleTop = cursorY
  cursorY += band(maxModH)
  const featureTop = cursorY
  cursorY += band(maxFeatH)
  const leafY = leaves.length ? cursorY + nodeH / 2 : 0
  if (leaves.length) cursorY += nodeH + GRAPH_PAD
  else cursorY += GRAPH_PAD
  const height = Math.max(GRAPH_PAD * 2 + nodeH, cursorY)

  const placed: GraphLayoutNode[] = []
  const placedIds = new Set<string>()
  const place = (node: HubNode, x: number, y: number) => {
    if (placedIds.has(node.id)) return
    placedIds.add(node.id)
    placed.push({ id: node.id, key: node.stable_key, label: node.name, type: node.type, x, y })
  }
  const placePack = (items: HubNode[], left: number, top: number, cols: number, innerW: number) => {
    if (!items.length) return
    const used = Math.min(Math.max(1, cols), items.length)
    const packW = packWidth(items.length, used)
    const origin = left + (innerW - packW) / 2
    items.forEach((node, index) => {
      const col = index % used
      const row = Math.floor(index / used)
      place(node, origin + col * (nodeW + packGap) + nodeW / 2, top + nodeH / 2 + row * (nodeH + rowGap))
    })
  }

  if (product) place(product, width / 2, productY)
  columns.forEach((column, index) => {
    if (column.domain) place(column.domain, colCenters[index] ?? width / 2, domainY)
    placePack(column.mods, colLefts[index] ?? GRAPH_PAD, moduleTop, column.mCols, column.innerW)
    placePack(column.feats, colLefts[index] ?? GRAPH_PAD, featureTop, column.fCols, column.innerW)
  })
  if (leaves.length) {
    layerXs(leaves.length, width).forEach((x, index) => {
      const node = leaves[index]
      if (node && !placedIds.has(node.id)) place(node, x, leafY)
    })
  }

  const visible = new Set(placed.map(node => node.id))
  const laid: GraphLayoutEdge[] = []
  for (const edge of edges) {
    const from = resolve(edge.from)
    const to = resolve(edge.to)
    if (!from || !to || !visible.has(from.id) || !visible.has(to.id)) continue
    laid.push({ from: from.id, to: to.id, rel: edge.rel })
  }

  const vw = opts.viewport && opts.viewport.w >= 80 ? opts.viewport.w : 0
  const vh = opts.viewport && opts.viewport.h >= 80 ? opts.viewport.h : 0
  return {
    nodes: placed,
    edges: laid,
    width,
    height,
    nodeW,
    nodeH,
    fitScale: vw && vh ? graphFitScale({ width, height }, { w: vw, h: vh }) : 1,
  }
}

export function neighborIds(edges: HubEdge[], id: string): Set<string> {
  const next = new Set<string>([id])
  for (const edge of edges) {
    if (edge.from === id) next.add(edge.to)
    if (edge.to === id) next.add(edge.from)
  }
  return next
}

export const EXPERT_SECTIONS = [
  { key: 'persona', zh: '人格', en: 'PERSONA' },
  { key: 'knowledge', zh: '知识', en: 'KNOWLEDGE' },
  { key: 'skills', zh: '技能', en: 'SKILLS' },
  { key: 'style', zh: '风格', en: 'STYLE' },
  { key: 'constraints', zh: '约束', en: 'CONSTRAINTS' },
  { key: 'memory', zh: '记忆', en: 'MEMORY' },
] as const

export function expertHandbook(node: HubNode): Record<(typeof EXPERT_SECTIONS)[number]['key'], string> {
  if (node.stable_key.includes('companion') || node.name.includes('月伴')) {
    return {
      persona: '严谨机务工程师，安全第一',
      knowledge: 'AMM / TSM / IPC / SRM / SB 手册体系',
      skills: '手册检索 · 故障隔离 · 工卡解读',
      style: '结构化输出，引用章节号',
      constraints: '只依据现行有效手册版本',
      memory: '相位挂接 · 版本链 v1–v3',
    }
  }
  if (node.stable_key.includes('office') || node.name.includes('办公')) {
    return {
      persona: '冷静的办公协作者，先确认文件与权限',
      knowledge: '工作台任务、产物导出、会议纪要',
      skills: '打开文件 · 创建任务 · 导出产物',
      style: '先给路径和结果，再补操作细节',
      constraints: '不覆盖用户未确认的本地文件',
      memory: node.version || '办公会话版本链',
    }
  }
  return {
    persona: node.summary || `${node.name} 专家角色`,
    knowledge: '领域知识与活源目录',
    skills: '技能检索 · 卡解读',
    style: '结构化输出',
    constraints: '只依据有效手册版本',
    memory: node.version || '版本链待挂接',
  }
}

export type PluginRow = { id: string; name: string; provides: string; version: string; state: string }

export function pluginRows(nodes: HubNode[]): PluginRow[] {
  return nodes.filter(node => node.type === 'Plugin').map(node => ({
    id: node.id,
    name: node.name,
    provides: node.provides || node.summary || node.stable_key,
    version: node.version || 'v1.0',
    state: node.state || 'ready',
  }))
}

export const SETTING_GROUPS: Array<{ id: string; zh: string; n: number; domain?: string; types?: string[] }> = [
  { id: 'general', zh: '通用', n: 3, types: ['Product'] },
  { id: 'appearance', zh: '外观', n: 1, types: ['Product'] },
  { id: 'voice', zh: '语音', n: 2, domain: 'dialog', types: ['Capability'] },
  { id: 'models', zh: '模型供应', n: 4, domain: 'foundation', types: ['Capability'] },
  { id: 'routing', zh: '能力路由', n: 2, domain: 'foundation', types: ['Capability'] },
  { id: 'mcp', zh: 'MCP 连接', n: 2, types: ['Mcp', 'McpTool'] },
  { id: 'skills', zh: '技能管理', n: 1, types: ['Skill'] },
  { id: 'plugins', zh: '插件', n: 1, types: ['Plugin'] },
  { id: 'memory', zh: '记忆', n: 2, domain: 'assets' },
  { id: 'auto', zh: '自动化', n: 1, domain: 'office' },
  { id: 'browser', zh: '浏览器', n: 1, domain: 'execution' },
  { id: 'computer', zh: '电脑控制', n: 1, domain: 'execution', types: ['Capability'] },
  { id: 'channels', zh: '消息通道', n: 1, domain: 'execution' },
  { id: 'agents', zh: '子智能体', n: 1, domain: 'execution' },
  { id: 'gate', zh: '协作门禁', n: 1, domain: 'foundation' },
  { id: 'diag', zh: '诊断', n: 1, domain: 'foundation' },
  { id: 'update', zh: '更新', n: 1, domain: 'foundation' },
  { id: 'token', zh: 'Token 效率', n: 2, domain: 'foundation' },
]

export function settingCoverage(nodes: HubNode[]): { covered: number; total: number } {
  const n = Math.max(SETTING_GROUPS.length, nodes.filter(node => node.type === 'Module' || node.type === 'Capability').length)
  return { covered: SETTING_GROUPS.length, total: n }
}

const FIX_ACTION = /^(edit_manifest|rebuild|check_service|check_[a-z_]+)/

export function parseFixSteps(fix: string): Array<{ action: string; target: string; detail: string }> {
  const lines = fix.split(/\n+/).map(item => item.trim()).filter(Boolean)
  const source = lines.length > 1 ? lines : splitLines(fix)
  return source.map(line => {
    const cleaned = line.replace(/^[①②③④⑤⑥⑦⑧⑨⑩][.)、]?\s*/, '').replace(/^\d+[.)、]\s*/, '')
    const hit = cleaned.match(FIX_ACTION)
    if (!hit) return { action: 'note', target: '', detail: cleaned }
    const action = hit[1]
    const rest = cleaned.slice(action.length).replace(/^[:：\s]+/, '')
    const parts = rest.split(/[:：]\s*/)
    if (action === 'rebuild') {
      return { action, target: parts[0] || '快照重建', detail: parts.slice(1).join('：').trim() || parts[0] }
    }
    if (parts.length > 1) {
      return { action, target: parts[0].split(/\s/)[0] ?? '', detail: parts.slice(1).join('：').trim() }
    }
    const tokens = rest.split(/\s+/).filter(Boolean)
    return { action, target: tokens[0] ?? '', detail: tokens.slice(1).join(' ') }
  })
}

export function reportId(generatedAt?: string): string {
  const date = generatedAt ? new Date(generatedAt) : new Date('2026-09-19T09:15:00Z')
  if (Number.isNaN(date.getTime())) return 'DR-preview'
  const y = date.getUTCFullYear()
  const m = String(date.getUTCMonth() + 1).padStart(2, '0')
  const d = String(date.getUTCDate()).padStart(2, '0')
  return `DR-${y}${m}${d}-01`
}

export function changeTitle(item: HubChange, nodes: HubNode[]): string {
  if (item.title) return item.title
  return nodes.find(node => node.stable_key === item.stable_key)?.name ?? item.stable_key.split('.').slice(-1)[0]
}
