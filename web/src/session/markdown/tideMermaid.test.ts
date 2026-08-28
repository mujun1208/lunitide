import { afterEach, expect, it } from 'vitest'
import {
  TIDE_PALETTE_FALLBACK,
  fitMermaidSvg,
  mountMermaidSvg,
  prepareMermaidSource,
  readTidePalette,
  tideMermaidConfig,
  tideMermaidThemeCSS,
} from './tideMermaid'

afterEach(() => {
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.style.cssText = ''
})

it('maps --tide tokens into mermaid theme so nodes and edges are not near-black', () => {
  const cfg = tideMermaidConfig(TIDE_PALETTE_FALLBACK)
  expect(cfg.theme).toBe('base')
  expect(cfg.securityLevel).toBe('antiscript')
  expect(cfg.flowchart.useMaxWidth).toBe(false)
  expect(cfg.flowchart.htmlLabels).toBe(false)
  expect(cfg.flowchart.curve).toBe('linear')
  expect(cfg.flowchart.defaultRenderer).toBe('dagre-wrapper')
  expect(cfg.themeVariables.primaryBorderColor).toBe(TIDE_PALETTE_FALLBACK.tide1)
  expect(cfg.themeVariables.lineColor).toBe(TIDE_PALETTE_FALLBACK.tide2)
  expect(cfg.themeVariables.primaryTextColor).toBe(TIDE_PALETTE_FALLBACK.ink)
  expect(cfg.themeVariables.background).toBe(TIDE_PALETTE_FALLBACK.bg)
  expect(cfg.themeVariables.primaryColor).not.toBe('#1f2020')
  expect(cfg.themeVariables.primaryColor).not.toBe(TIDE_PALETTE_FALLBACK.bg)
  expect(cfg.themeCSS).toContain('cluster rect')
  expect(tideMermaidThemeCSS()).toContain('stroke-dasharray')
})

it('reads live --tide tokens and treats data-theme=light as a light diagram', () => {
  document.documentElement.setAttribute('data-theme', 'light')
  document.documentElement.style.setProperty('--tide1', '#008ac5')
  document.documentElement.style.setProperty('--ink', '#14243b')
  const palette = readTidePalette(document.documentElement)
  expect(palette.dark).toBe(false)
  expect(palette.tide1).toBe('#008ac5')
  expect(palette.ink).toBe('#14243b')
  const cfg = tideMermaidConfig(palette)
  expect(cfg.darkMode).toBe(false)
  expect(cfg.themeVariables.primaryBorderColor).toBe('#008ac5')
})

it('strips mermaid init directives that would override the Tide theme', () => {
  const source = prepareMermaidSource('%%{init: {"theme":"dark", "themeVariables":{"primaryColor":"#000"}}}%%\nflowchart TD\nA-->B')
  expect(source.startsWith('flowchart')).toBe(true)
  expect(source).not.toContain('init')
})

it('fits mermaid SVG so inline height cannot leave a huge empty band', () => {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  svg.setAttribute('width', '100%')
  svg.setAttribute('height', '1600')
  svg.setAttribute('viewBox', '0 0 400 200')
  svg.style.height = '1600px'
  fitMermaidSvg(svg)
  expect(svg.getAttribute('height')).toBeNull()
  expect(svg.getAttribute('width')).toBeNull()
  expect(svg.style.height).toBe('auto')
  expect(svg.style.maxWidth).toBe('100%')
  expect(svg.getAttribute('preserveAspectRatio')).toBe('xMidYMid meet')
})

it('mounts mermaid SVG through the parser and fits height', () => {
  const host = document.createElement('div')
  mountMermaidSvg(host, '<svg xmlns="http://www.w3.org/2000/svg" width="100%" height="1400"><g class="node"><rect/></g></svg>')
  const svg = host.querySelector('svg')
  expect(svg?.querySelector('.node rect')).not.toBeNull()
  expect(svg?.getAttribute('height')).toBeNull()
  expect(() => mountMermaidSvg(host, '<div>nope</div>')).toThrow(/无法解析/)
})
