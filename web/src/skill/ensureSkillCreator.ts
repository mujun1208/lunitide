import { skillBridge, type SkillBridge } from '../bridge/client'
import type { SkillDTO } from '../generated/bridge'

export const SKILL_CREATOR_NAME = 'skill-creator'
export const SKILL_CREATOR_TEMPLATE_ID = 'skill-creator'
export const SKILL_CREATE_PROMPT = '请帮我创建一个可以实现「……」的skill'

export async function ensureSkillCreator(skills: SkillBridge = skillBridge): Promise<SkillDTO | undefined> {
  const findPublished = async () =>
    (await skills.list({ status: 'published' })).items.find(item => item.name === SKILL_CREATOR_NAME)

  const existing = await findPublished()
  if (existing) return existing

  try {
    const installed = await skills.install?.({ templateId: SKILL_CREATOR_TEMPLATE_ID })
    if (installed?.skillId) {
      await skills.publish?.({ id: installed.skillId })
      return findPublished()
    }
  } catch {
    /* draft may already exist */
  }

  const draft = (await skills.list({ status: 'draft' })).items.find(item => item.name === SKILL_CREATOR_NAME)
  if (draft) {
    try {
      await skills.publish?.({ id: draft.id })
    } catch {
      /* publish gate may block in tests */
    }
    return findPublished()
  }

  return findPublished()
}
