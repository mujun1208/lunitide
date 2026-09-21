import { expect, it } from 'vitest'
import { graphFitScale, graphLayout, moduleRows, parseFixSteps, searchHubNodes, wrapLabel } from './hubModel'
import type { HubEdge, HubNode } from './productHubTypes'

it('ranks searchable hub nodes by name before stable key', () => {
  const nodes: HubNode[] = [
    { id: 'feature.dialog.music', stable_key: 'feature.dialog.music.open-player', type: 'Feature', name: '打开音乐播放软件', domain: 'dialog' },
    { id: 'module.execution.computer', stable_key: 'module.execution.computer', type: 'Module', name: '电脑控制', domain: 'execution' },
    { id: 'product.lunitide', stable_key: 'product.lunitide', type: 'Product', name: '月汐' },
  ]
  expect(searchHubNodes(nodes, '打开').map(node => node.name)).toEqual(['打开音乐播放软件'])
  expect(searchHubNodes(nodes, '电脑').map(node => node.type)).toEqual(['Module'])
  expect(searchHubNodes(nodes, '月汐')).toEqual([])
})

it('wraps long graph labels onto two lines instead of a single ellipsis', () => {
  expect(wrapLabel('computer.control', 9, 2)).toEqual(['computer.', 'control'])
  expect(wrapLabel('llm.intent', 9, 2)).toEqual(['llm.', 'intent'])
  expect(wrapLabel('打开音乐播放软件', 8, 2)).toEqual(['打开音乐播放软件'])
})

it('places every module and feature in domain columns instead of sampling one child', () => {
  const nodes: HubNode[] = [
    { id: 'product.lunitide', stable_key: 'product.lunitide', type: 'Product', name: '月汐' },
    { id: 'domain.dialog', stable_key: 'domain.dialog', type: 'Domain', name: '对话体验', domain: 'dialog' },
    { id: 'domain.office', stable_key: 'domain.office', type: 'Domain', name: '业务工作台', domain: 'office' },
    { id: 'module.dialog.companion', stable_key: 'module.dialog.companion', type: 'Module', name: '月伴语音对话', domain: 'dialog' },
    { id: 'module.dialog.chat', stable_key: 'module.dialog.chat', type: 'Module', name: '打字聊天对话', domain: 'dialog' },
    { id: 'module.office.desk', stable_key: 'module.office.desk', type: 'Module', name: '办公工作台', domain: 'office' },
    { id: 'feature.dialog.music', stable_key: 'feature.dialog.music.open-player', type: 'Feature', name: '打开音乐播放软件', domain: 'dialog' },
    { id: 'feature.dialog.chat.type', stable_key: 'feature.dialog.chat.type', type: 'Feature', name: '打字聊天', domain: 'dialog' },
    { id: 'cap.asr', stable_key: 'capability.stt.asr', type: 'Capability', name: 'stt.asr', domain: 'dialog' },
  ]
  const edges: HubEdge[] = [
    { from: 'product.lunitide', to: 'domain.dialog', rel: 'contains' },
    { from: 'product.lunitide', to: 'domain.office', rel: 'contains' },
    { from: 'domain.dialog', to: 'module.dialog.companion', rel: 'contains' },
    { from: 'domain.dialog', to: 'module.dialog.chat', rel: 'contains' },
    { from: 'domain.office', to: 'module.office.desk', rel: 'contains' },
    { from: 'module.dialog.companion', to: 'feature.dialog.music', rel: 'contains' },
    { from: 'module.dialog.chat', to: 'feature.dialog.chat.type', rel: 'contains' },
    { from: 'feature.dialog.music', to: 'cap.asr', rel: 'uses' },
  ]
  const layout = graphLayout(nodes, edges, { focusId: 'feature.dialog.music' })
  expect(layout.nodes.map(node => node.label)).toEqual(expect.arrayContaining([
    '月伴语音对话', '打字聊天对话', '办公工作台', '打开音乐播放软件', '打字聊天', 'stt.asr',
  ]))
  const chat = layout.nodes.find(node => node.label === '打字聊天对话')
  const companion = layout.nodes.find(node => node.label === '月伴语音对话')
  expect(chat && companion && Math.abs(chat.x - companion.x) > 20).toBe(true)
})

it('sizes the graph from content and reports a fit scale for the viewport', () => {
  const nodes: HubNode[] = [
    { id: 'product.lunitide', stable_key: 'product.lunitide', type: 'Product', name: '月汐' },
    { id: 'domain.dialog', stable_key: 'domain.dialog', type: 'Domain', name: '对话体验', domain: 'dialog' },
    { id: 'feature.dialog.music', stable_key: 'feature.dialog.music.open-player', type: 'Feature', name: '打开音乐播放软件', domain: 'dialog' },
  ]
  const layout = graphLayout(nodes, [], { focusId: 'feature.dialog.music', viewport: { w: 1200, h: 640 } })
  expect(layout.width).toBeLessThan(1200)
  expect(layout.height).toBeLessThan(640)
  expect(graphFitScale(layout, { w: 400, h: 240 })).toBeLessThan(1)
  expect(layout.fitScale).toBeGreaterThan(1)
})

it('assigns feature cards to modules by contains edge or module slug', () => {
  const nodes: HubNode[] = [
    { id: 'module.dialog.chat', stable_key: 'module.dialog.chat', type: 'Module', name: '打字聊天对话', domain: 'dialog' },
    { id: 'module.dialog.companion', stable_key: 'module.dialog.companion', type: 'Module', name: '月伴语音对话', domain: 'dialog' },
    { id: 'feature.dialog.chat.type', stable_key: 'feature.dialog.chat.type', type: 'Feature', name: '打字聊天', domain: 'dialog' },
    { id: 'feature.dialog.music', stable_key: 'feature.dialog.music.open-player', type: 'Feature', name: '打开音乐', domain: 'dialog', module: 'companion' },
  ]
  const rows = moduleRows(nodes, [])
  expect(rows.find(row => row.id === 'module.dialog.chat')?.features.map(node => node.name)).toEqual(['打字聊天'])
  expect(rows.find(row => row.id === 'module.dialog.companion')?.features.map(node => node.name)).toEqual(['打开音乐'])
})

it('parses circled diagnostic fix steps without leftover markers', () => {
  const steps = parseFixSteps('① edit_manifest manifest.json：将 skillpack.computer-ops 替换为 skillpack.computer-ops-v2\n② rebuild 快照重建\n③ check_service computer-ops-v2')
  expect(steps).toEqual([
    { action: 'edit_manifest', target: 'manifest.json', detail: '将 skillpack.computer-ops 替换为 skillpack.computer-ops-v2' },
    { action: 'rebuild', target: '快照重建', detail: '快照重建' },
    { action: 'check_service', target: 'computer-ops-v2', detail: '' },
  ])
})
