import { expect, it } from 'vitest'
import { ACCESS_MODES, composeHubPrompt, FREE_TEMPLATES, hubSceneToThreadScene, mergeEvents, parentWorkspacePath, pptDeckMissing, sceneBlurb, scenePrefix, shortWorkDir, statusLabel, taskElapsed, threadPptMissing, threadTitleFromPrompt, visibleHubArtifacts } from './agentHubCopy'

it('clips a create title to the first 200-rune line', () => {
  expect(threadTitleFromPrompt('第一行\n第二行')).toBe('第一行')
  expect(threadTitleFromPrompt('字'.repeat(220))).toBe('字'.repeat(200))
})

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
  expect(pptDeckMissing('kimi', prompt, [{ name: '参考.pptx', path: '参考.pptx', source: 'inbox' }, { name: 'notes.md', path: 'notes.md', source: 'changed' }])).toBe(true)
  expect(pptDeckMissing('kimi', prompt, [{ name: 'demo.pptx', path: 'demo.pptx', source: 'scan' }, { name: 'notes.md', path: 'notes.md', source: 'changed' }])).toBe(true)
  expect(pptDeckMissing('kimi', prompt, [{ name: 'demo.pptx', path: 'demo.pptx', source: 'changed' }])).toBe(false)
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

it('maps the write home scene to write_project and keeps the other thread scenes', () => {
  expect(hubSceneToThreadScene('write')).toBe('write_project')
  expect(hubSceneToThreadScene('fix')).toBe('fix')
  expect(hubSceneToThreadScene('ppt')).toBe('ppt')
  expect(hubSceneToThreadScene('free')).toBe('free')
})

it('keeps the spec §8 permission chip labels', () => {
  expect(ACCESS_MODES.map(item => [item.id, item.zh])).toEqual([
    ['approval', '手动'],
    ['auto-edit', '自动'],
    ['full-access', '完全访问'],
  ])
})

it('walks one workspace folder up', () => {
  expect(parentWorkspacePath('src/pkg')).toBe('src')
  expect(parentWorkspacePath('src')).toBe('')
})

it('says a thread PPT scene finished without a produced deck', () => {
  expect(threadPptMissing('ppt', [{ name: 'notes.md', path: 'notes.md', source: 'scan' }])).toBe(true)
  expect(threadPptMissing('ppt', [{ name: '参考.pptx', path: '.agenthub-inbox/参考.pptx', source: 'scan' }])).toBe(true)
  expect(threadPptMissing('ppt', [{ name: 'demo.pptx', path: 'demo.pptx', source: 'scan' }])).toBe(false)
  expect(threadPptMissing('ppt', [{ name: 'demo.pptx', path: 'demo.pptx', source: 'export' }])).toBe(false)
  expect(threadPptMissing('free', [])).toBe(false)
})

it('labels thread statuses and keeps task labels', () => {
  expect(statusLabel('waiting_user', true)).toBe('等你回答')
  expect(statusLabel('faulted', true)).toBe('失败')
  expect(statusLabel('idle', true)).toBe('空闲')
  expect(statusLabel('running', true)).toBe('进行中')
})

it('keeps the spec §8 scene blurbs and leaves 自由 empty', () => {
  expect(sceneBlurb('write_project')).toBe('在你选的文件夹里按你的规则创建子目录并写文件。不要把已有文件挪到别处。')
  expect(sceneBlurb('fix')).toBe('在此仓库根内检索和修改。已有文件保持原路径。新文件按已有结构和你的规则放置。')
  expect(sceneBlurb('ppt')).toBe('用 Kimi 自己的技能做文稿。pptx 写在工作区；指定了导出目录则完成时复制过去。')
  expect(sceneBlurb('free')).toBe('')
})
