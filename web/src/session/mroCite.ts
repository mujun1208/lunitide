export type MroCite = {
  docType?: string
  revision: string
  locator: string
  quote: string
  expertName: string
}

export type MroCiteView = {
  cites: MroCite[]
  discarded?: number
  gate?: string
  restored?: boolean
}

const MARK = /<!--mro-cite:([\s\S]*?)-->/

export function stripMroCiteMarker(text: string): string {
  return text.replace(MARK, '').replace(/\s+$/u, '')
}

export function parseMroCite(text: string): {visible: string; view?: MroCiteView} {
  const visible = stripMroCiteMarker(text)
  const match = MARK.exec(text)
  if (match) {
    try {
      const raw = JSON.parse(match[1]) as Partial<MroCiteView>
      const view: MroCiteView = {
        cites: Array.isArray(raw.cites) ? raw.cites.filter(isCite) : [],
        discarded: typeof raw.discarded === 'number' ? raw.discarded : undefined,
        gate: typeof raw.gate === 'string' ? raw.gate : undefined,
        restored: raw.restored === true,
      }
      if (view.cites.length || view.gate || view.discarded || view.restored) {
        return {visible, view}
      }
    } catch {
      // fall through to infer
    }
  }
  return {visible, view: inferMroCite(visible)}
}

function isCite(value: unknown): value is MroCite {
  if (!value || typeof value !== 'object') return false
  const row = value as Record<string, unknown>
  return typeof row.revision === 'string' && typeof row.locator === 'string' && typeof row.quote === 'string' && typeof row.expertName === 'string'
}

function inferMroCite(text: string): MroCiteView | undefined {
  const advisory = /辅助建议|不构成放行/.test(text)
  if (!advisory) return undefined
  const ungrounded = /件号|扭矩/.test(text) && !/修订/.test(text)
  return {cites: [], gate: ungrounded ? 'ungrounded' : 'advisory'}
}
