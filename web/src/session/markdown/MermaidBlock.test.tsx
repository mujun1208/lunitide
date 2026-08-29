import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { mermaidInitConfig, mermaidThemeVariables, mountMermaidSvg } from './MermaidBlock'
import { pinConversationScroll } from '../streamScroll'
import { MERMAID_MAX_HEIGHT_CSS, prepareMermaidSource, resetMermaidEngineForTests } from './tideMermaid'

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

it('still draws A–J when prose leaks into the mermaid fence and does not show NODE_STRING', async () => {
  mermaid.render.mockImplementation(async (_id: string, src: string) => {
    if (/第二轮检索|无法执行|desktop\s*=\s*true|NODE_STRING/.test(src)) {
      throw new Error("Parse error on line 11: ... Expecting 'SEMI', got 'NODE_STRING'")
    }
    expect(src).toContain('A["封面<br/>穆军 · IT公司副总经理"]')
    expect(src).toContain('J["致谢<br/>联系方式与邀请"]')
    return {
      svg: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 640 80"><g class="node"><text>封面</text></g><g class="node"><text>致谢</text></g></svg>',
    }
  })
  render(<MermaidBlock source={LEAKED_PPT_FENCE} />)
  await waitFor(() => expect(document.querySelector('.mermaid-host svg .node')).not.toBeNull())
  expect(screen.queryByText(/Parse error/)).toBeNull()
  expect(screen.queryByText(/NODE_STRING/)).toBeNull()
  expect(screen.queryByText(/图表未能渲染/)).toBeNull()
  expect(document.querySelector('.mermaid-host')?.textContent).toMatch(/封面/)
  expect((document.querySelector('.mermaid-host svg') as HTMLElement | null)?.style.maxHeight).toBe(MERMAID_MAX_HEIGHT_CSS)
})

it('does not call scrollTo on mermaid layout complete when follow is paused', async () => {
  mermaid.render.mockResolvedValue({ svg: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 80 40"/>' })
  const scrollTo = vi.fn()
  const box = { scrollHeight: 900, clientHeight: 300, scrollTop: 120, scrollTo }
  render(
    <MermaidBlock
      source={'flowchart TD\nA-->B'}
      onLayout={() => pinConversationScroll(box, { userFollowPaused: true })}
    />,
  )
  await waitFor(() => expect(document.querySelector('.mermaid-host svg')).not.toBeNull())
  expect(scrollTo).not.toHaveBeenCalled()
})
