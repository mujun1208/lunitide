import { expect, it } from 'vitest'
import { expertSections, graphFitScale, graphLayout, moduleRows, parseFixSteps, pluginRows, searchHubNodes, settingCoverage, settingRows, wrapLabel } from './hubModel'
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

it('lists every plugin and keeps a stored version instead of inventing one', () => {
  const rows = pluginRows([
    { id: 'plugin.llm', stable_key: 'plugin.llm', type: 'Plugin', name: '插件：LLM', summary: '在插件页启用或使用「LLM」。' },
    { id: 'plugin.git', stable_key: 'plugin.git', type: 'Plugin', name: '插件：Git', summary: '在插件页启用或使用「Git」。' },
    { id: 'plugin.ocr', stable_key: 'plugin.ocr-router', type: 'Plugin', name: 'ocr.router', provides: 'OCR 模型路由', version: 'v1.0', state: 'degraded' },
  ])
  expect(rows.map(row => row.name)).toEqual(['插件：LLM', '插件：Git', 'ocr.router'])
  expect(rows[0].provides).toBe('在插件页启用或使用「LLM」。')
  expect(rows[0].version).toBe('—')
  expect(rows[0].state).toBe('—')
  expect(rows[2].version).toBe('v1.0')
  expect(rows[2].state).toBe('degraded')
})

it('reads anatomy settings from the stored setting cards', () => {
  const nodes: HubNode[] = [
    { id: 'feature.foundation.settings.canvas', stable_key: 'feature.foundation.settings.canvas', type: 'Feature', name: '画布设置' },
    { id: 'feature.foundation.settings.general', stable_key: 'feature.foundation.settings.general', type: 'Feature', name: '常规设置' },
    { id: 'feature.dialog.page.home', stable_key: 'feature.dialog.page.home', type: 'Feature', name: '首页' },
  ]
  expect(settingRows(nodes).map(row => row.name)).toEqual(['常规设置', '画布设置'])
  expect(settingCoverage(nodes)).toEqual({ covered: 2, total: 2 })
})

it('uses the stored expert summary once', () => {
  const sections = expertSections({ id: 'e', stable_key: 'expert.feature.assets.expert.try', type: 'Expert', name: '试用专家', summary: '打开专家试用会话。' })
  expect(sections.map(section => section.text)).toEqual(['打开专家试用会话。'])
  const companion = expertSections({ id: 'c', stable_key: 'expert.companion', type: 'Expert', name: '月伴', summary: '唤醒后用语音连续对话' })
  expect(companion.map(section => section.text).join('')).not.toContain('AMM')
})

it('keeps each stored expert section distinct', () => {
  const sections = expertSections({
    id: 'e', stable_key: 'expert.try', type: 'Expert', name: '试用专家',
    summary: '试用专家。',
    description: '打开专家试用会话。',
    principle: '链路族 asset-invoke。',
    logic: '1.选择专家',
    tech: '工具 session.experts.set',
    analysis: '来源 verbs。',
  })
  expect(sections.map(section => section.zh)).toEqual(['简介', '功能描述', '原理', '逻辑', '技术', '总结分析'])
  expect(new Set(sections.map(section => section.text)).size).toBe(sections.length)
})
