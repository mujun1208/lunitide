// workspaceSession.ts keeps the user-chosen local folder inside one chat.
// Opening another conversation starts unbound; picking a folder in this
// session is remembered until the renderer reloads.
const boundSessions = new Set<string>()

export function sessionWorkspaceBound(sessionId: string): boolean {
  return boundSessions.has(sessionId)
}

export function markSessionWorkspaceBound(sessionId: string): void {
  if (sessionId) boundSessions.add(sessionId)
}

export function resetSessionWorkspaceBindings(): void {
  boundSessions.clear()
}
