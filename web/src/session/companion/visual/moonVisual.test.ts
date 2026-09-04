import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { moonVisualMode, strandsSpeaking, STRANDS_THINKING } from './moonVisual'

// Deterministic stand-in for the 1080p / 1440p / 125% DPI eyeball pass: the
// "speaking moon must read as a big, centered ball" regression is encoded as a
// numeric zoom invariant plus the CSS size/centering contract, so a future tweak
// that shrinks or off-centers the speaking moon fails here instead of in review.
describe('speaking moon stays large and centered (visual acceptance lock)', () => {
  it('drives the glass strands renderer while thinking and speaking', () => {
    expect(moonVisualMode('thinking')).toBe('glass')
    expect(moonVisualMode('speaking')).toBe('glass')
    expect(moonVisualMode('idle')).toBe('orb')
  })

  it('keeps the speaking plasma zoomed in at least as much as thinking (lower uScale = bigger ball)', () => {
    // uScale is inverse: a smaller number zooms the plasma IN. Speaking must
    // never render a smaller ball than the thinking glass, at any gain.
    for (const gain of [0, 0.2, 0.6, 1]) {
      expect(strandsSpeaking(gain).scale).toBeLessThanOrEqual(STRANDS_THINKING.scale)
    }
    expect(strandsSpeaking(0.6).scale).toBeLessThanOrEqual(0.62)
    expect(strandsSpeaking(0.6).glass).toBe(true)
    expect(strandsSpeaking(0.6).amplitude).toBeGreaterThanOrEqual(1.45)
    expect(STRANDS_THINKING.scale).toBe(1.15)
  })

  const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')

  it('enlarges the speaking moon over the idle circle and centers it', () => {
    expect(css).toMatch(/\.companion-moon\{[^}]*width:34vmin;height:34vmin[^}]*margin-inline:auto/)
    expect(css).toMatch(/\.companion-moon\.state-speaking\{width:46vmin;height:46vmin\}/)
    expect(css).toMatch(/\.companion-moon\[data-visual="webgl"\]\.state-speaking \.companion-moon-strands\.is-on\{transform:scale\(1\.55\);transform-origin:center center\}/)
  })

  it('fills the inner disc without WebGL and stays filled when motion is reduced', () => {
    expect(css).toMatch(/\.companion-moon\.state-speaking \.companion-moon-halo-wave\{inset:-8%/)
    expect(css).toMatch(/prefers-reduced-motion:reduce[\s\S]*\.companion-moon\.state-speaking \.companion-moon-halo-wave\{animation:none;inset:0/)
  })

  it('keeps the speaking moon bigger than idle at the small-viewport breakpoint', () => {
    expect(css).toMatch(/@media\(max-width:900px\)\{\.companion-moon\{width:44vmin;height:44vmin\}\.companion-moon\.state-speaking\{width:56vmin;height:56vmin\}/)
  })
})
