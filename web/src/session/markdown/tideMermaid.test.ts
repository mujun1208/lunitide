import { afterEach, expect, it } from 'vitest'
import {
  MERMAID_MAX_HEIGHT_CSS,
  TIDE_PALETTE_FALLBACK,
  fitMermaidSvg,
  mountMermaidSvg,
  prepareMermaidSource,
  readTidePalette,
  recoverMermaidSource,
  tideMermaidConfig,
  tideMermaidThemeCSS,
  trimMermaidFenceLeak,
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
  expect(svg.style.width).toBe('auto')
  expect(svg.style.maxWidth).toBe('100%')
  expect(svg.style.maxHeight).toBe(MERMAID_MAX_HEIGHT_CSS)
  expect(svg.style.objectFit).toBe('contain')
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

const PPT_STRUCTURE_GRAPH = `flowchart LR
    A[封面<br/>穆俊 · IT公司副总] --> B[关于我<br/>基本档案]
    B --> C[技术出身的管理者<br/>十年IT经验]
    C --> D[职业履历<br/>从工程师到管理者]
    D --> E[能力四维<br/>技术/团队/交付/经营]
    E --> F[管理实践<br/>实干派业绩]
    F --> G[愿景收尾<br/>深耕IT · 欢迎交流]`

it('quotes the PPT structure graph so Chinese labels, <br/>, · and / stay in one node', () => {
  const prepared = prepareMermaidSource(PPT_STRUCTURE_GRAPH)
  expect(prepared).toContain('A["封面<br/>穆俊 · IT公司副总"]')
  expect(prepared).toContain('B["关于我<br/>基本档案"]')
  expect(prepared).toContain('E["能力四维<br/>技术/团队/交付/经营"]')
  expect(prepared).toContain('G["愿景收尾<br/>深耕IT · 欢迎交流"]')
  expect(prepared).not.toContain('A[封面')
  expect(prepared).not.toContain('E[能力四维')
})

const LEAKED_PPT_FENCE = `flowchart TD
    A["封面<br/>穆军 · IT公司副总经理"] --> B["目录<br/>六段式导读"]
    B --> C["关于我<br/>35岁 · 15年从业"]
    C --> D["职业历程<br/>从技术到管理的15年"]
    D --> E["核心能力<br/>技术+管理双轮驱动"]
    E --> F["管理理念<br/>三个坚持"]
    F --> G["代表成果<br/>可量化战绩"]
    G --> H["个人特质<br/>性格与成长关键词"]
    H --> I["愿景规划<br/>未来三年的目标"]
    I --> J["致谢<br/>联系方式与邀请"]
第二轮检索结果对个人信息类 PPT 帮助有限（这类 PPT 的事实来自本人而非公开网络），我已按流水线完成两轮检索并据此收录结构。现在定稿...无法执行。模型结果不完整。写到桌面请用对应 *.gen 工具并设 desktop=true，不要用 command.run。`

it('trims leaked Chinese prose and desktop=true closed-loop tails from a mermaid fence', () => {
  const prepared = prepareMermaidSource(LEAKED_PPT_FENCE)
  expect(prepared).toContain('A["封面<br/>穆军 · IT公司副总经理"]')
  expect(prepared).toContain('J["致谢<br/>联系方式与邀请"]')
  expect(prepared).not.toContain('第二轮检索')
  expect(prepared).not.toContain('无法执行')
  expect(prepared).not.toContain('desktop=true')
  expect(prepared).not.toContain('NODE_STRING')
  expect(recoverMermaidSource(LEAKED_PPT_FENCE)).toContain('I --> J[')
  expect(trimMermaidFenceLeak(`${LEAKED_PPT_FENCE}\n\`\`\`\nextra`)).not.toContain('extra')
})

it('mounts mermaid SVG that XML would reject because of HTML <br> in foreignObject', () => {
  const host = document.createElement('div')
  const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 80"><g class="node"><foreignObject width="120" height="40"><div>封面<br>穆俊 · IT公司副总</div></foreignObject></g></svg>'
  expect(() => new DOMParser().parseFromString(svg, 'image/svg+xml').querySelector('parsererror')).not.toBeUndefined()
  mountMermaidSvg(host, svg)
  expect(host.querySelector('svg .node')).not.toBeNull()
  expect(host.querySelector('svg')?.textContent).toMatch(/封面/)
})
