import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { mermaidInitConfig, mermaidThemeVariables, mountMermaidSvg } from './MermaidBlock'
import { prepareMermaidSource, resetMermaidEngineForTests } from './tideMermaid'

const mermaid = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(),
}))

vi.mock('mermaid', () => ({ default: mermaid }))

import { MermaidBlock } from './MermaidBlock'

afterEach(() => {
  cleanup()
  resetMermaidEngineForTests()
  mermaid.initialize.mockReset()
  mermaid.render.mockReset()
})

it('uses SVG labels and Tide fills so nodes are not naked text on black', () => {
  const theme = mermaidThemeVariables()
  const config = mermaidInitConfig()
  expect(config.theme).toBe('base')
  expect(config.securityLevel).toBe('antiscript')
  expect(config.flowchart.htmlLabels).toBe(false)
  expect(config.flowchart.useMaxWidth).toBe(false)
  expect(config.flowchart.curve).toBe('linear')
  expect(theme.primaryBorderColor).toBe('#3bd6ff')
  expect(theme.lineColor).toBe('#7f5bff')
  expect(theme.primaryTextColor).toBe('#eaf3ff')
  expect(theme.mainBkg).toBe('#0d1118')
  expect(theme.primaryColor).not.toBe('#000')
})

it('mounts parsed SVG instead of innerHTML and rejects junk', () => {
  const host = document.createElement('div')
  mountMermaidSvg(host, '<svg xmlns="http://www.w3.org/2000/svg" height="900"><rect width="10" height="10"/></svg>')
  const svg = host.querySelector('svg')
  expect(svg?.querySelector('rect')).not.toBeNull()
  expect(svg?.getAttribute('height')).toBeNull()
  expect(svg?.style.height).toBe('auto')
  expect(() => mountMermaidSvg(host, '<not-svg></not-svg>')).toThrow(/无法解析/)
})

it('falls back to source when mermaid.render fails instead of throwing', async () => {
  mermaid.render.mockRejectedValue(new Error('parse failed'))
  render(<MermaidBlock source={'flowchart TD\nA-->B'} onCopy={vi.fn()} />)
  expect(await screen.findByText(/图表未能渲染：parse failed/)).toBeInTheDocument()
  expect(screen.getByText(/flowchart TD/)).toBeInTheDocument()
  expect(document.querySelector('.mermaid-host')).toHaveAttribute('hidden')
})

it('renders a flowchart SVG into the host', async () => {
  mermaid.render.mockResolvedValue({
    svg: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 240 80" height="900"><g class="node"><rect width="72" height="36"/></g><g class="edgePath"><path class="path" d="M0 0 L24 24"/></g></svg>',
  })
  render(<MermaidBlock source={'flowchart TD\nsubgraph layer["层"]\nA["节点"]-->B["核心"]\nend\n'} />)
  await waitFor(() => expect(document.querySelector('.mermaid-host svg .node rect')).not.toBeNull())
  expect(document.querySelector('.mermaid-host svg .edgePath .path')).not.toBeNull()
  expect(document.querySelector('.mermaid-host svg')?.getAttribute('height')).toBeNull()
  expect(mermaid.initialize).toHaveBeenCalled()
})

it('copies mermaid source from the toolbar', async () => {
  mermaid.render.mockResolvedValue({ svg: '<svg xmlns="http://www.w3.org/2000/svg"/>' })
  const onCopy = vi.fn()
  render(<MermaidBlock source={'flowchart TD\nA-->B'} onCopy={onCopy} />)
  fireEvent.click(screen.getByRole('button', { name: '复制 Mermaid 源码' }))
  await waitFor(() => expect(onCopy).toHaveBeenCalledWith('flowchart TD\nA-->B'))
})

it('renders the PPT structure graph even when mermaid returns HTML <br> inside SVG', async () => {
  const graph = `flowchart LR
    A[封面<br/>穆俊 · IT公司副总] --> B[关于我<br/>基本档案]
    B --> C[技术出身的管理者<br/>十年IT经验]
    C --> D[职业履历<br/>从工程师到管理者]
    D --> E[能力四维<br/>技术/团队/交付/经营]
    E --> F[管理实践<br/>实干派业绩]
    F --> G[愿景收尾<br/>深耕IT · 欢迎交流]`
  mermaid.render.mockImplementation(async (_id: string, src: string) => {
    expect(src).toBe(prepareMermaidSource(graph))
    expect(src).toContain('A["封面<br/>穆俊 · IT公司副总"]')
    return {
      svg: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 80"><g class="node"><foreignObject width="160" height="48"><div>封面<br>穆俊 · IT公司副总</div></foreignObject></g></svg>',
    }
  })
  render(<MermaidBlock source={graph} />)
  await waitFor(() => expect(document.querySelector('.mermaid-host svg .node')).not.toBeNull())
  expect(screen.queryByText(/无法解析/)).toBeNull()
  expect(document.querySelector('.mermaid-host svg')?.textContent).toMatch(/封面/)
})

it('serializes concurrent mermaid.render calls so overlapping diagrams cannot crash the host', async () => {
  let active = 0
  let maxActive = 0
  mermaid.render.mockImplementation(async () => {
    active++
    maxActive = Math.max(maxActive, active)
    await Promise.resolve()
    active--
    return { svg: '<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>' }
  })
  render(
    <>
      <MermaidBlock source={'flowchart TD\nA-->B'} />
      <MermaidBlock source={'flowchart TD\nC-->D'} />
    </>,
  )
  await waitFor(() => expect(document.querySelectorAll('.mermaid-host svg').length).toBe(2))
  expect(maxActive).toBe(1)
})
