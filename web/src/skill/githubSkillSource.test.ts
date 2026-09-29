import { expect, it, vi } from 'vitest'
import { parseGithubSkillSource, pinGithubSkillUrl, resolveGithubCommit, skillSourceDirectory } from './githubSkillSource'

const sha = '0123456789abcdef0123456789abcdef01234567'

it('accepts pasted GitHub URLs without https, with www, query, or a SKILL.md blob path', () => {
  expect(parseGithubSkillSource('github.com/mattpocock/skills')).toEqual({
    owner: 'mattpocock', repo: 'skills', commit: '', directory: '', ref: 'HEAD',
  })
  expect(parseGithubSkillSource('https://www.github.com/mattpocock/skills/?tab=readme-ov-file')).toEqual({
    owner: 'mattpocock', repo: 'skills', commit: '', directory: '', ref: 'HEAD',
  })
  expect(parseGithubSkillSource('https://github.com/mattpocock/skills/blob/main/review/SKILL.md')).toEqual({
    owner: 'mattpocock', repo: 'skills', commit: '', directory: 'review', ref: 'main',
  })
  expect(parseGithubSkillSource('git@github.com:anthropics/skills.git')).toEqual({
    owner: 'anthropics', repo: 'skills', commit: '', directory: '', ref: 'HEAD',
  })
  expect(pinGithubSkillUrl('mattpocock', 'skills', sha, 'review')).toBe(
    `https://github.com/mattpocock/skills/tree/${sha}/review`,
  )
})

it('keeps a tree URL already pinned to the same 40-character SHA', () => {
  const url = `https://github.com/anthropics/skills/tree/${sha}/skills/docx`
  expect(parseGithubSkillSource(url)).toEqual({
    owner: 'anthropics', repo: 'skills', commit: sha, directory: 'skills/docx', ref: sha,
  })
})

it('treats a branch tree path as a directory plus git ref, not a commit', () => {
  expect(parseGithubSkillSource('https://github.com/mattpocock/skills/tree/main/review')).toEqual({
    owner: 'mattpocock', repo: 'skills', commit: '', directory: 'review', ref: 'main',
  })
})

it('resolves HEAD to a lowercase 40-character SHA', async () => {
  const fetchImpl = vi.fn(async () => new Response(JSON.stringify({ sha: sha.toUpperCase() }), { status: 200 }))
  await expect(resolveGithubCommit('mattpocock', 'skills', 'HEAD', fetchImpl)).resolves.toBe(sha)
  expect(fetchImpl).toHaveBeenCalledWith(
    'https://api.github.com/repos/mattpocock/skills/commits/HEAD',
    { headers: { Accept: 'application/vnd.github+json' } },
  )
})

it('rejects a missing GitHub commit instead of returning an empty SHA', async () => {
  const fetchImpl = vi.fn(async () => new Response('{}', { status: 404 }))
  await expect(resolveGithubCommit('mattpocock', 'skills', 'HEAD', fetchImpl)).rejects.toThrow(/无法读取仓库当前提交/)
})

it('derives the skill subdirectory from a GitHub contents URL', () => {
  expect(skillSourceDirectory('https://api.github.com/repos/mattpocock/skills/contents', 'review')).toBe('review')
  expect(skillSourceDirectory('https://api.github.com/repos/anthropics/skills/contents/skills', 'docx')).toBe('skills/docx')
  expect(skillSourceDirectory('https://api.github.com/repos/anthropics/skills/contents/skills', 'docx', 'skills/docx')).toBe('skills/docx')
})
