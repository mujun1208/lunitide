import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { moonVisualMode, strandsSpeaking, STRANDS_THINKING } from './moonVisual'

// Speaking sits on the thinking seat. Only the inner mouth speeds up —
// no zoom, no 46vmin grow, no CSS scale that pulls the wave off-center.
describe('speaking moon stays on the thinking seat (mouth only)', () => {
  it('drives the glass strands renderer while thinking and speaking', () => {
    expect(moonVisualMode('thinking')).toBe('glass')
    expect(moonVisualMode('speaking')).toBe('glass')
    expect(moonVisualMode('idle')).toBe('orb')
  })

  it('keeps the same plasma scale as thinking and only raises mouth speed', () => {
    for (const gain of [0, 0.2, 0.6, 1]) {
      expect(strandsSpeaking(gain).scale).toBe(STRANDS_THINKING.scale)
      expect(strandsSpeaking(gain).glass).toBe(true)
    }
    expect(STRANDS_THINKING.scale).toBe(1.15)
    expect(strandsSpeaking(0.6).amplitude).toBeGreaterThan(STRANDS_THINKING.amplitude)
    expect(strandsSpeaking(0.6).speed).toBeGreaterThan(STRANDS_THINKING.speed)
  })

  const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')

  it('keeps the speaking box on the thinking seat and does not CSS-zoom strands', () => {
    expect(css).toMatch(/\.companion-moon\{[^}]*width:34vmin;height:34vmin[^}]*margin-inline:auto/)
    expect(css).toMatch(/\.companion-moon\.state-speaking\{width:34vmin;height:34vmin\}/)
    expect(css).toMatch(/\.companion-moon\[data-visual="webgl"\]\.state-speaking \.companion-moon-strands\.is-on\{transform:none;transform-origin:center center\}/)
    expect(css).not.toMatch(/\.companion-moon\.state-speaking\{width:46vmin/)
    expect(css).not.toMatch(/state-speaking \.companion-moon-strands\.is-on\{transform:scale\(/)
  })

  it('does not expand the speaking halo beyond the thinking seat', () => {
    expect(css).toMatch(/\.companion-moon\.state-speaking \.companion-moon-halo-wave\{inset:-92%;animation:none;opacity:0\}/)
  })

  it('keeps speaking and thinking the same size at the small-viewport breakpoint', () => {
    expect(css).toMatch(/@media\(max-width:900px\)\{\.companion-moon,\.companion-moon\.state-speaking,\.companion-moon-slot\{width:44vmin;height:44vmin\}/)
  })
})
