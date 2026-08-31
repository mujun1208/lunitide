import { expect, it } from 'vitest'
import { excerptFromMessages, parseCreatedArtifactId, saveAsSkillPrompt } from './catalogCreated'

it('parses a skill create id from the tool summary', () => {
  expect(parseCreatedArtifactId('技能「周报」已创建（id=01ARZ3NDEKTSV4RRFFQ69G5FAA，status=draft）。')).toBe('01ARZ3NDEKTSV4RRFFQ69G5FAA')
  expect(parseCreatedArtifactId('已创建能力包「演示」（id=pack-ppt，state=enabled）。')).toBe('pack-ppt')
  expect(parseCreatedArtifactId('没有编号')).toBeUndefined()
})

it('builds a confirm-first save-as-skill prompt from the last turns', () => {
  const excerpt = excerptFromMessages([
    { role: 'user', text: '把验收清单写成可复用约定' },
    { role: 'assistant', text: '可以按六段画像整理。' },
  ])
  const prompt = saveAsSkillPrompt(excerpt)
  expect(prompt).toContain('不要自动发布')
  expect(prompt).toContain('把验收清单写成可复用约定')
})
