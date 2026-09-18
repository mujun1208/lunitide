import { expect, it } from 'vitest'
import { loadSkillIndexCache, REMOTE_SKILL_SOURCES, searchLocalRemoteSkills, searchRemoteSkills, searchRemoteSkillsLive } from './remoteSkillIndex'

it('only searches named sources and keeps the GitHub repo for import approval', () => {
  const hits = searchRemoteSkills('docx')
  expect(hits[0]?.repo).toBe('https://github.com/anthropics/skills')
  expect(hits[0]?.directory).toBe('skills/docx')
  expect(searchRemoteSkills('review').find(item => item.slug === 'review')?.directory).toBe('review')
  expect(searchRemoteSkills('周报').find(item => item.slug === 'weekly-report')).toEqual(expect.objectContaining({
    repo: 'https://github.com/anbeime/skill',
    directory: 'weekly-report',
    title: '周报',
  }))
  expect(searchRemoteSkills('')).toEqual([])
  expect(searchRemoteSkills('anbeime').some(item => item.sourceName === 'anbeime/skill')).toBe(true)
})

it('uses the last successful listing cache before the local 9 slugs', async () => {
  localStorage.setItem('lunitide:remote-skill-index', JSON.stringify({
    listings: [{ sourceId: 'anthropics-skills', skills: [{ slug: 'canvas-design', title: 'canvas-design', summary: 'cached' }] }],
  }))
  const original = globalThis.fetch
  globalThis.fetch = () => Promise.reject(new Error('offline')) as never
  try {
    const result = await searchRemoteSkillsLive('canvas')
    expect(result.fallback).toBe(true)
    expect(result.notice).toContain('缓存')
    expect(result.hits.some(item => item.slug === 'canvas-design')).toBe(true)
    expect(loadSkillIndexCache()[0]?.skills[0]?.slug).toBe('canvas-design')
  } finally {
    globalThis.fetch = original
    localStorage.removeItem('lunitide:remote-skill-index')
  }
})

it('keeps 周报 on live GitHub listings that only return directory names', async () => {
  localStorage.removeItem('lunitide:remote-skill-index')
  const original = globalThis.fetch
  const json = (rows: Array<{ name: string; type: string; path: string }>) =>
    Promise.resolve(new Response(JSON.stringify(rows), { status: 200 }))
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('anbeime')) return json([{ name: 'weekly-report', type: 'dir', path: 'weekly-report' }])
    if (url.includes('anthropics')) return json([{ name: 'docx', type: 'dir', path: 'skills/docx' }])
    return json([{ name: 'review', type: 'dir', path: 'review' }])
  }) as typeof fetch
  try {
    const named = await searchRemoteSkillsLive('周报')
    expect(named.fallback).toBe(false)
    expect(named.hits.find(item => item.slug === 'weekly-report')).toEqual(expect.objectContaining({
      title: '周报',
      directory: 'weekly-report',
      repo: 'https://github.com/anbeime/skill',
    }))
  } finally {
    globalThis.fetch = original
    localStorage.removeItem('lunitide:remote-skill-index')
  }
})

it('still offers bundled 周报 when the live listing omitted that directory', async () => {
  localStorage.removeItem('lunitide:remote-skill-index')
  const original = globalThis.fetch
  const json = (rows: Array<{ name: string; type: string; path: string }>) =>
    Promise.resolve(new Response(JSON.stringify(rows), { status: 200 }))
  globalThis.fetch = ((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('anbeime')) return json([{ name: 'writing', type: 'dir', path: 'writing' }])
    if (url.includes('anthropics')) return json([{ name: 'docx', type: 'dir', path: 'skills/docx' }])
    return json([{ name: 'review', type: 'dir', path: 'review' }])
  }) as typeof fetch
  try {
    const result = await searchRemoteSkillsLive('周报')
    expect(result.fallback).toBe(false)
    expect(result.hits.find(item => item.slug === 'weekly-report')).toEqual(expect.objectContaining({
      title: '周报',
      directory: 'weekly-report',
    }))
  } finally {
    globalThis.fetch = original
    localStorage.removeItem('lunitide:remote-skill-index')
  }
})

it('falls back to the bundled catalog when GitHub is unavailable', async () => {
  localStorage.removeItem('lunitide:remote-skill-index')
  const original = globalThis.fetch
  globalThis.fetch = () => Promise.reject(new Error('offline')) as never
  try {
    const result = await searchRemoteSkillsLive('docx')
    expect(result.fallback).toBe(true)
    expect(result.notice).toMatch(/本地技能目录（\d+ 条）/)
    expect(searchLocalRemoteSkills('docx').some(item => item.slug === 'docx')).toBe(true)
    expect(searchLocalRemoteSkills('canvas').some(item => item.slug === 'canvas-design')).toBe(true)
    expect(REMOTE_SKILL_SOURCES.reduce((n, source) => n + source.skills.length, 0)).toBeGreaterThan(9)
  } finally {
    globalThis.fetch = original
  }
})
