// visibilityRestore.ts recovers a blank WebView2 surface after minimize,
// resize, or sleep. Chromium can keep the document alive while DWM/GPU
// stops compositing; hidden→visible then reflows. Reload is last resort
// and only when #root is actually empty, so companion audio and people
// P2P stay up on a compositor miss.

export type RestoreAction = 'ok' | 'repaint' | 'reload'

export interface SurfaceSnapshot {
  hidden: boolean
  rootEmpty: boolean
  bodyMissing: boolean
  stuckWhite: boolean
}

export interface RestoreClock {
  now(): number
}

declare global {
  interface Window {
    __lunitideRestoreSurface?: () => RestoreAction
  }
}

export function isWhiteColor(value: string): boolean {
  const v = value.trim().toLowerCase().replace(/\s+/g, '')
  return v === '#fff' || v === '#ffffff' || v === 'white' || v === 'rgb(255,255,255)' || v === 'rgba(255,255,255,1)'
}

export function inspectSurface(doc: Document): SurfaceSnapshot {
  const body = doc.body
  const root = doc.getElementById('root')
  const hidden = doc.visibilityState === 'hidden'
  const rootEmpty = !root || root.childElementCount === 0
  const bodyMissing = body == null
  let stuckWhite = false
  if (body && !hidden && doc.documentElement.getAttribute('data-theme') !== 'light') {
    const computed = doc.defaultView?.getComputedStyle(body).backgroundColor ?? ''
    stuckWhite = isWhiteColor(computed) || isWhiteColor(body.style.background)
  }
  return { hidden, rootEmpty, bodyMissing, stuckWhite }
}

export function decideRestore(snapshot: SurfaceSnapshot, previousHidden: boolean): RestoreAction {
  if (snapshot.hidden) return 'ok'
  if (!previousHidden) return 'ok'
  if (snapshot.bodyMissing || snapshot.rootEmpty) return 'reload'
  return 'repaint'
}

let kickingRepaint = false

export function kickRepaint(doc: Document): void {
  if (kickingRepaint) return
  kickingRepaint = true
  try {
    const body = doc.body
    if (!body) return
    if (doc.documentElement.getAttribute('data-theme') !== 'light') {
      const computed = doc.defaultView?.getComputedStyle(body).backgroundColor ?? ''
      if (isWhiteColor(computed) || isWhiteColor(body.style.background)) {
        body.style.background = 'var(--bg)'
        body.style.color = 'var(--ink)'
      }
    }
    void body.offsetHeight
    doc.defaultView?.dispatchEvent(new Event('lunitide:surface-restore'))
  } finally {
    kickingRepaint = false
  }
}

export function installVisibilityRestore(options?: {
  document?: Document
  reload?: () => void
  clock?: RestoreClock
  cooldownMs?: number
  bootGuardMs?: number
}): () => void {
  const doc = options?.document ?? document
  const win = doc.defaultView
  if (!win) return () => {}
  const cooldownMs = options?.cooldownMs ?? 30_000
  const bootGuardMs = options?.bootGuardMs ?? 2_000
  const clock = options?.clock ?? { now: () => Date.now() }
  const reload = options?.reload ?? (() => { win.location.reload() })
  const startedAt = clock.now()
  let lastReloadAt = 0
  let previousHidden = doc.visibilityState !== 'visible'

  const allowed = (): boolean => {
    const t = clock.now()
    if (t - startedAt < bootGuardMs) return false
    if (lastReloadAt !== 0 && t - lastReloadAt < cooldownMs) return false
    return true
  }

  const run = (force: boolean, performReload: boolean): RestoreAction => {
    const snap = inspectSurface(doc)
    const action = decideRestore(snap, force || previousHidden)
    previousHidden = snap.hidden
    if (action === 'repaint') kickRepaint(doc)
    if (action === 'reload' && performReload && allowed()) {
      lastReloadAt = clock.now()
      reload()
    }
    return action
  }

  const onVisibility = (): void => { run(false, true) }
  const onPageShow = (event: Event): void => {
    if ((event as PageTransitionEvent).persisted) run(true, true)
  }

  doc.addEventListener('visibilitychange', onVisibility)
  win.addEventListener('pageshow', onPageShow)
  const onWebGLLost = (): void => { kickRepaint(doc) }
  doc.addEventListener('webglcontextlost', onWebGLLost, true)
  win.__lunitideRestoreSurface = () => run(true, false)

  return () => {
    doc.removeEventListener('visibilitychange', onVisibility)
    win.removeEventListener('pageshow', onPageShow)
    doc.removeEventListener('webglcontextlost', onWebGLLost, true)
    delete win.__lunitideRestoreSurface
  }
}
