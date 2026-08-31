export type TurnInspectHint = {
  status?: string
  persistFailed?: boolean
  persistDraft?: string
}

export function bannersFromTurnState(opts: {
  live: boolean
  storedPersist?: { draft?: string }
  localTurn?: { status?: string }
  server?: TurnInspectHint
}): { persistFailed: boolean; persistDraft?: string; resume: boolean } {
  if (opts.live) return { persistFailed: false, resume: false }
  const persistFailed = opts.server
    ? Boolean(opts.server.persistFailed)
    : opts.storedPersist !== undefined
  const persistDraft = (opts.server?.persistDraft || opts.storedPersist?.draft || '').trim() || undefined
  const status = opts.server ? opts.server.status : opts.localTurn?.status
  const resume = !persistFailed && (status === 'interrupted' || status === 'running')
  return { persistFailed, persistDraft, resume }
}
