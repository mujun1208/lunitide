import { expect, it } from 'vitest'
import { loadSkillIndexCache, REMOTE_SKILL_SOURCES, searchLocalRemoteSkills, searchRemoteSkills, searchRemoteSkillsLive } from './remoteSkillIndex'

it('only searches named sources and keeps the GitHub repo for import approval', () => {
  const hits = searchRemoteSkills('docx')
  expect(hits[0]?.repo).toBe('https://github.com/anthropics/skills')
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

it('falls back to the bundled catalog when GitHub is unavailable', async () => {
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
