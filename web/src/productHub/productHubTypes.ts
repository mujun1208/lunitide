export const PRODUCT_HUB_TOKEN_KEY = 'lunitide:product-hub-token'

export type HubTab = 'overview' | 'graph' | 'anatomy' | 'diagnostics' | 'landscape'

export type HubStep = {
  index: number
  name: string
  detail: string
  description: string
}

export type HubFork = {
  name: string
  description?: string
  on_success?: string
  on_fail?: string
}

export type HubBranch = {
  type: string
  from_step: number
  name: string
  description: string
  retry?: HubFork
  fallback?: HubFork
}

export type HubCard = {
  stable_key: string
  name: string
  name_en: string
  domain: string
  module: string
  summary: string
  description: string
  attributes: {
    operations?: string[]
    tools?: string[]
    mcps?: string[]
    skills?: string[]
    capabilities?: string[]
  }
  methods: Array<{ type: string; entry: string; continuous?: string }>
  chain: { steps: HubStep[]; branches: HubBranch[] }
  chain_class?: string
  scaffold: { pages?: string[]; bridge?: string[]; settings?: string[]; runtime?: string[] }
  principle?: string
  logic?: string
  tech?: string
  analysis?: string
  tags?: string[]
  provenance: string
  source: string
  probe?: { passed: number; total: number }
  version: string
}

export type HubNode = {
  id: string
  stable_key: string
  type: string
  name: string
  domain?: string
  module?: string
  version?: string
  state?: string
  provides?: string
  summary?: string
}

export type HubEdge = {
  from: string
  to: string
  rel: string
}

export type HubFinding = {
  severity: string
  error_code: string
  stable_key: string
  title: string
  evidence: string
  root_cause: string
  fix: string
  verify: string
  status: string
  apply_prompt?: string
  plan?: string
  applied_at?: string
  skill_id?: string
  skill_name?: string
  skill_output?: string
}

export type HubChange = {
  stable_key: string
  kind: string
  title?: string
  summary?: string
  impacts?: string[]
}

// Optional exactly where the bridge schema is optional (product-hub.overview):
// an engine that omits a counter must not be a type error at the call site, or
// the page stops compiling the next time the schema is regenerated.
export type HubOverview = {
  product: string
  editionId: string
  generatedAt?: string
  cardCount: number
  healthScore: number
  added?: number
  updated?: number
  removed?: number
  probePassed?: number
  probeTotal?: number
  domains?: Array<{ id: string; name: string; modules: number; cards: number }>
  tags?: string[]
  assetCounts?: Record<string, { n: number; delta?: number }>
}

export function isHubCard(value: unknown): value is HubCard {
  return !!value && typeof value === 'object' && typeof (value as HubCard).stable_key === 'string' && typeof (value as HubCard).name === 'string'
}

export function readHubToken(): string {
  try {
    return sessionStorage.getItem(PRODUCT_HUB_TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

export function writeHubToken(token: string): void {
  try {
    if (token) sessionStorage.setItem(PRODUCT_HUB_TOKEN_KEY, token)
    else sessionStorage.removeItem(PRODUCT_HUB_TOKEN_KEY)
  } catch { /* private mode */ }
}

export function hubUserError(err: unknown, fallback: string): string {
  const detail = err instanceof Error ? err.message.trim() : ''
  return /[\u4e00-\u9fff]/.test(detail) ? detail : fallback
}
