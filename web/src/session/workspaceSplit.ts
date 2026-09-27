/** How far the right workspace can be dragged, and when that drag covers the chat. */

export function workspaceDragLimit(viewport: number): number {
  const width = Number.isFinite(viewport) ? Math.round(viewport) : 0
  return Math.max(360, width - 48)
}

export function workspaceCoversChat(width: number, viewport: number): boolean {
  if (!Number.isFinite(width) || !Number.isFinite(viewport) || viewport <= 760) return false
  return width >= viewport - 320
}

export function workspaceWidthAfterCover(viewport: number): number {
  const width = Number.isFinite(viewport) ? viewport : 0
  return Math.min(720, Math.max(360, Math.round(width * 0.42)))
}
