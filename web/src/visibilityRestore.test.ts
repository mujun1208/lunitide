import { afterEach, describe, expect, test, vi } from 'vitest'
import {
  decideRestore,
  inspectSurface,
  installVisibilityRestore,
  isWhiteColor,
  kickRepaint,
  type RestoreAction,
  type SurfaceSnapshot,
} from './visibilityRestore'

afterEach(() => {
  document.body.innerHTML = ''
  document.body.style.background = ''
  document.body.style.color = ''
  document.documentElement.removeAttribute('data-theme')
})

function snapshot(overrides: Partial<SurfaceSnapshot> = {}): SurfaceSnapshot {
  return { hidden: false, rootEmpty: false, bodyMissing: false, stuckWhite: false, ...overrides }
}

describe('decideRestore', () => {
  test('does nothing while the document is still hidden', () => {
    expect(decideRestore(snapshot({ hidden: true, rootEmpty: true }), true)).toBe('ok')
  })

  test('does not reload on an ordinary visible frame', () => {
    expect(decideRestore(snapshot(), false)).toBe('ok')
  })

  test('reloads only when the React root is gone after a reveal', () => {
    expect(decideRestore(snapshot({ rootEmpty: true }), true)).toBe('reload')
    expect(decideRestore(snapshot({ bodyMissing: true }), true)).toBe('reload')
  })

  test('repaints a living tree after hidden→visible so compositor hangs recover without killing audio', () => {
    expect(decideRestore(snapshot(), true)).toBe('repaint')
    expect(decideRestore(snapshot({ stuckWhite: true }), true)).toBe('repaint')
  })
})

describe('isWhiteColor', () => {
  test('detects the compositor-miss whites', () => {
    expect(isWhiteColor('#fff')).toBe(true)
    expect(isWhiteColor('rgb(255, 255, 255)')).toBe(true)
    expect(isWhiteColor('var(--bg)')).toBe(false)
    expect(isWhiteColor('rgb(3, 6, 12)')).toBe(false)
  })
})

describe('inspectSurface', () => {
  test('treats an empty #root as blank', () => {
    document.body.innerHTML = '<div id="root"></div>'
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    expect(inspectSurface(document).rootEmpty).toBe(true)
  })

  test('treats a mounted tree as present', () => {
    document.body.innerHTML = '<div id="root"><main>月汐</main></div>'
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    expect(inspectSurface(document).rootEmpty).toBe(false)
  })
})

describe('kickRepaint', () => {
  test('resets a stuck white body to the dark --bg token and notifies listeners', () => {
    document.body.innerHTML = '<div id="root"><span>ok</span></div>'
    document.body.style.background = '#ffffff'
    document.documentElement.removeAttribute('data-theme')
    const seen: string[] = []
    window.addEventListener('lunitide:surface-restore', () => seen.push('restore'))
    kickRepaint(document)
    expect(document.body.style.background).toBe('var(--bg)')
    expect(seen).toEqual(['restore'])
  })
})

describe('installVisibilityRestore', () => {
  let uninstall: (() => void) | undefined

  afterEach(() => {
    uninstall?.()
    uninstall = undefined
    document.body.innerHTML = ''
    document.body.style.background = ''
    vi.unstubAllGlobals()
  })

  test('host helper returns reload without navigating when the root is empty', () => {
    document.body.innerHTML = '<div id="root"></div>'
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    const reload = vi.fn()
    uninstall = installVisibilityRestore({ reload, clock: { now: () => 10_000 }, bootGuardMs: 0 })
    const action = window.__lunitideRestoreSurface?.()
    expect(action).toBe('reload')
    expect(reload).not.toHaveBeenCalled()
  })

  test('hidden→visible with a living tree repaints and does not reload', () => {
    document.body.innerHTML = '<div id="root"><main>月汐</main></div>'
    let hidden = true
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (hidden ? 'hidden' : 'visible') })
    const reload = vi.fn()
    const actions: RestoreAction[] = []
    uninstall = installVisibilityRestore({
      reload,
      clock: { now: () => 10_000 },
      bootGuardMs: 0,
    })
    document.dispatchEvent(new Event('visibilitychange'))
    hidden = false
    document.dispatchEvent(new Event('visibilitychange'))
    actions.push(window.__lunitideRestoreSurface?.() ?? 'ok')
    expect(reload).not.toHaveBeenCalled()
    expect(actions[0]).toBe('repaint')
  })

  test('visibilitychange reloads an empty root after boot, with a cooldown', () => {
    document.body.innerHTML = '<div id="root"></div>'
    let hidden = true
    let now = 5_000
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (hidden ? 'hidden' : 'visible') })
    const reload = vi.fn()
    uninstall = installVisibilityRestore({
      reload,
      clock: { now: () => now },
      bootGuardMs: 0,
      cooldownMs: 30_000,
    })
    document.dispatchEvent(new Event('visibilitychange'))
    hidden = false
    document.dispatchEvent(new Event('visibilitychange'))
    expect(reload).toHaveBeenCalledOnce()
    now = 10_000
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    hidden = false
    document.dispatchEvent(new Event('visibilitychange'))
    expect(reload).toHaveBeenCalledOnce()
  })

  test('webglcontextlost kicks a CSS reflow without reloading', () => {
    document.body.innerHTML = '<div id="root"><main>月汐</main></div>'
    const reload = vi.fn()
    const seen: string[] = []
    window.addEventListener('lunitide:surface-restore', () => seen.push('restore'))
    uninstall = installVisibilityRestore({ reload, clock: { now: () => 10_000 }, bootGuardMs: 0 })
    document.dispatchEvent(new Event('webglcontextlost', { bubbles: true }))
    expect(reload).not.toHaveBeenCalled()
    expect(seen).toEqual(['restore'])
  })
})

describe('kickRepaint re-entry', () => {
  test('a restore listener that calls kickRepaint again does not overflow', () => {
    document.body.innerHTML = '<div id="root"><span>ok</span></div>'
    let nested = 0
    const onRestore = () => {
      nested += 1
      kickRepaint(document)
    }
    window.addEventListener('lunitide:surface-restore', onRestore)
    kickRepaint(document)
    window.removeEventListener('lunitide:surface-restore', onRestore)
    expect(nested).toBe(1)
  })
})
