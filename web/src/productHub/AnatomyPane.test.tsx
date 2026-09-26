import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import { AnatomyPane } from './AnatomyPane'
import type { HubNode } from './productHubTypes'

afterEach(() => cleanup())

it('shows stored plugins and settings without a fixed gap claim', () => {
  const nodes: HubNode[] = [
    { id: 'plugin.llm', stable_key: 'plugin.llm', type: 'Plugin', name: '插件：LLM', summary: '在插件页启用或使用「LLM」。' },
    { id: 'plugin.git', stable_key: 'plugin.git', type: 'Plugin', name: '插件：Git', summary: '在插件页启用或使用「Git」。' },
    { id: 'feature.foundation.settings.canvas', stable_key: 'feature.foundation.settings.canvas', type: 'Feature', name: '画布设置' },
    { id: 'expert.try', stable_key: 'expert.try', type: 'Expert', name: '试用专家', summary: '打开专家试用会话。' },
  ]
  render(<AnatomyPane nodes={nodes} edges={[]} zh onOpen={() => undefined} />)
  expect(screen.getByText('插件：LLM')).toBeTruthy()
  expect(screen.getByText('插件：Git')).toBeTruthy()
  expect(screen.getByText('画布设置')).toBeTruthy()
  expect(screen.getByText('本版设置 1 项')).toBeTruthy()
  expect(screen.queryByText(/0 缺项/)).toBeNull()
  expect(screen.queryByText(/AMM/)).toBeNull()
  expect(screen.getAllByText('打开专家试用会话。').length).toBeGreaterThan(0)
  expect(screen.queryByText(/v3/)).toBeNull()
  expect(screen.queryByText('v1.0')).toBeNull()
})

it('lists every linked node and does not add unlinked neighbors', () => {
  const nodes: HubNode[] = [
    { id: 'expert.try', stable_key: 'expert.try', type: 'Expert', name: '试用专家', summary: '试用专家。' },
    ...Array.from({ length: 9 }, (_, i) => ({
      id: `feature.linked.${i}`,
      stable_key: `feature.linked.${i}`,
      type: 'Feature',
      name: `已连接${i}`,
      domain: 'assets',
    })),
    { id: 'feature.unlinked', stable_key: 'feature.unlinked', type: 'Feature', name: '未连接', domain: 'assets' },
  ]
  const edges = nodes.filter(node => node.id.startsWith('feature.linked.')).map(node => ({
    from: 'expert.try', to: node.id, rel: 'uses',
  }))
  render(<AnatomyPane nodes={nodes} edges={edges} zh onOpen={() => undefined} />)
  for (let i = 0; i < 9; i += 1) expect(screen.getByText(`已连接${i}`)).toBeTruthy()
  expect(screen.queryByText('未连接')).toBeNull()
})
