import type { MroChecklist, MroChecklistCite } from '../bridge/client'
import { mroBridge } from '../bridge/client'
import type { MroCite } from './mroCite'

/** Recover the ATA chapter stamped in an mro:// locator so a step keeps its context. */
export function ataFromLocator(locator: string): string {
  const at = locator.indexOf('?')
  if (at < 0) return ''
  try {
    return (new URLSearchParams(locator.slice(at + 1)).get('ata') ?? '').trim()
  } catch {
    return ''
  }
}

/**
 * Map grounded cites onto the checklist.build contract: every cited quote is one
 * step, index-aligned with its cite so the backend keeps only sourced lines. An
 * empty quote is skipped so an advisory/ungrounded answer yields no fake steps.
 */
export function checklistPayloadFromCites(cites: MroCite[]): { steps: string[]; cites: MroChecklistCite[] } {
  const steps: string[] = []
  const rows: MroChecklistCite[] = []
  for (const cite of cites) {
    const quote = (cite.quote ?? '').trim()
    if (!quote) continue
    steps.push(quote)
    const row: MroChecklistCite = { revision: cite.revision, locator: cite.locator, quote, expertName: cite.expertName }
    const ata = ataFromLocator(cite.locator ?? '')
    if (ata) row.ata = ata
    rows.push(row)
  }
  return { steps, cites: rows }
}

/**
 * Build and download a cited checklist from the cites under one answer. Returns
 * false (without touching the DOM) when there is nothing sourced to download so
 * the caller can surface an honest empty note instead of an empty file.
 */
export async function downloadCitedChecklist(
  cites: MroCite[],
  build: (payload: { steps: string[]; cites: MroChecklistCite[] }) => Promise<MroChecklist> = mroBridge.checklistBuild,
): Promise<boolean> {
  const payload = checklistPayloadFromCites(cites)
  if (!payload.steps.length) return false
  const built = await build(payload)
  if (!built.steps.length) return false
  triggerJsonDownload(built, 'mro-checklist.json')
  return true
}

function triggerJsonDownload(data: unknown, filename: string): void {
  if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function' || typeof document === 'undefined') return
  const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}
