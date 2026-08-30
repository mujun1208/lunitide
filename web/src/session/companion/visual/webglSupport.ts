// One-shot probe so jsdom tests and reduce-motion never construct a Renderer.
// Aurora / Strands are GLSL 300 es, so WebGL2 is required.
// MUST be cached: useCompanionEnter re-renders every frame, and each
// getContext('webgl2') would leak a GPU context until canvases go blank.
let webgl2Cached: boolean | undefined

export function prefersCompanionStillVisual(): boolean {
  if (typeof document !== 'undefined' && document.documentElement.classList.contains('reduce-motion')) return true
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
}

export function canUseCompanionWebgl(): boolean {
  if (webgl2Cached !== undefined) return webgl2Cached
  if (typeof document === 'undefined') return false
  if (prefersCompanionStillVisual()) {
    webgl2Cached = false
    return false
  }
  try {
    const canvas = document.createElement('canvas')
    const gl = canvas.getContext('webgl2')
    webgl2Cached = Boolean(gl)
    gl?.getExtension('WEBGL_lose_context')?.loseContext()
    return webgl2Cached
  } catch {
    webgl2Cached = false
    return false
  }
}
