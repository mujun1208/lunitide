import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { isolatedHTML } from './isolatedHTML'

/** An HTML artifact is previewed in an iframe with srcdoc. srcdoc is a local
 *  scheme, so the frame INHERITS the embedder's Content-Security-Policy, and
 *  CSP composes by intersection: whatever web/index.html forbids, the frame's
 *  own policy cannot grant. That is how every HTML preview came to render as
 *  raw unstyled markup — the frame asked for inline styles and the shell said
 *  style-src 'self'. These two policies have to be read together, so they are
 *  asserted together. */

const shellPolicy = (() => {
  const html = readFileSync(join(__dirname, '..', '..', 'index.html'), 'utf8')
  const match = /<meta http-equiv="Content-Security-Policy" content="([^"]+)"/.exec(html)
  if (!match) throw new Error('web/index.html lost its Content-Security-Policy meta tag')
  return match[1]
})()

function directive(policy: string, name: string): string[] {
  const found = policy.split(';').map(part => part.trim()).find(part => part === name || part.startsWith(name + ' '))
  return found ? found.split(/\s+/).slice(1) : []
}

describe('artifact preview policy', () => {
  const framePolicy = (() => {
    const match = /content="([^"]+)"/.exec(isolatedHTML('<p>x</p>'))
    if (!match) throw new Error('isolatedHTML stopped emitting a policy')
    return match[1]
  })()

  it('lets a preview style itself, because the frame inherits the shell policy', () => {
    // The frame asks for inline styles; the shell has to permit them or the
    // intersection drops every <style> block and style= attribute.
    expect(directive(framePolicy, 'style-src')).toContain("'unsafe-inline'")
    expect(directive(shellPolicy, 'style-src')).toContain("'unsafe-inline'")
  })

  it('lets a preview show its data: fonts and blob: images', () => {
    expect(directive(shellPolicy, 'font-src')).toContain('data:')
    expect(directive(shellPolicy, 'img-src')).toContain('data:')
    expect(directive(shellPolicy, 'img-src')).toContain('blob:')
  })

  it('keeps previews inert: no inline script may ever be granted', () => {
    // Styling a preview must not make it executable. The frame denies
    // everything by default and the shell never allows inline script, so the
    // intersection can only ever be "no script".
    expect(directive(framePolicy, 'default-src')).toEqual(["'none'"])
    expect(directive(framePolicy, 'script-src')).toEqual([])
    expect(directive(shellPolicy, 'script-src')).toEqual(["'self'"])
  })

  it('keeps previews offline and unable to navigate the shell', () => {
    expect(directive(framePolicy, 'connect-src')).toEqual(["'none'"])
    expect(directive(framePolicy, 'form-action')).toEqual(["'none'"])
    expect(directive(framePolicy, 'base-uri')).toEqual(["'none'"])
  })

  it('lets the shell frame the preview origin, or nothing interactive can load', () => {
    // The interactive preview is a real cross-origin document, not srcdoc, so the
    // shell must name its origin in frame-src. Without this the frame is blocked
    // outright and the panel is blank.
    expect(directive(shellPolicy, 'frame-src')).toContain('https://preview.lunitide.local')
  })

  it('does not let the preview origin bleed into the shell itself', () => {
    // Framing it is the whole permission. The shell must not also be willing to
    // load script, connect to, or inherit anything from that origin.
    for (const name of ['script-src', 'connect-src', 'default-src', 'style-src']) {
      expect(directive(shellPolicy, name).join(' ')).not.toContain('preview.lunitide.local')
    }
  })

  it('puts the policy before the artifact so it governs the whole document', () => {
    const html = isolatedHTML('<style>p{color:red}</style><p>hi</p>')
    expect(html.indexOf('Content-Security-Policy')).toBeLessThan(html.indexOf('<style>'))
  })
})
