import { expect, it } from 'vitest'
import { composeHubPrompt, FREE_TEMPLATES, mergeEvents, pptDeckMissing, scenePrefix, shortWorkDir, taskElapsed, visibleHubArtifacts } from './agentHubCopy'

it('keeps the weekly-report Markdown template and never offers generating a weekly Office file', () => {
  expect(FREE_TEMPLATES.map(item => item.zh)).toContain('写周报 Markdown')
  expect(FREE_TEMPLATES.map(item => item.zh).join(' ')).not.toContain('生成周报')
})

it('prefixes shortcuts but leaves free prompts clean until inbox files exist', () => {
  expect(scenePrefix('free', 'E:/hub', '总结这些材料')).toBe('总结这些材料')
  expect(scenePrefix('ppt', 'E:/hub', '做两页')).toContain('【场景：做 PPT】')
  expect(composeHubPrompt('free', 'E:/hub', '总结这些材料', [{ name: '纪要.pdf', path: '纪要.pdf', size: 12 }])).toContain('.agenthub-inbox')
  expect(composeHubPrompt('free', 'E:/hub', '总结这些材料', [{ name: '纪要.pdf', path: '纪要.pdf', size: 12 }])).not.toContain('【场景：')
})

it('says a PPT shortcut finished without a deck until a pptx appears', () => {
  const prompt = scenePrefix('ppt', 'E:/hub', '做两页')
  expect(pptDeckMissing('kimi', prompt, [{ name: 'notes.md', path: 'notes.md' }])).toBe(true)
  expect(pptDeckMissing('kimi', prompt, [{ name: 'demo.pptx', path: 'demo.pptx' }])).toBe(false)
  expect(pptDeckMissing('codex', prompt, [])).toBe(false)
  expect(pptDeckMissing('kimi', '总结这些材料', [])).toBe(false)
})

it('hides scan and outside artifacts until asked', () => {
  const items = [
    { source: 'inbox' },
    { source: 'changed' },
    { source: 'scan' },
    { source: 'outside' },
  ]
  expect(visibleHubArtifacts(items, false).map(item => item.source)).toEqual(['inbox', 'changed'])
  expect(visibleHubArtifacts(items, true).map(item => item.source)).toEqual(['inbox', 'changed', 'scan'])
})

it('dedupes timeline events by seq while keeping order', () => {
  expect(mergeEvents([{ seq: 1 }, { seq: 2 }], [{ seq: 2 }, { seq: 3 }])).toEqual([{ seq: 1 }, { seq: 2 }, { seq: 3 }])
})

it('shortens work dirs and formats elapsed time', () => {
  expect(shortWorkDir('E:/Trae-Work-Projects/lunitide')).toBe('Trae-Work-Projects/lunitide')
  expect(taskElapsed({ startedAt: '2026-09-12T00:00:00Z', finishedAt: '2026-09-12T00:01:05Z' }, Date.parse('2026-09-12T00:02:00Z'))).toBe('1m 5s')
})
