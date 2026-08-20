import { skillBridge, type SkillBridge } from '../bridge/client'
import type { SkillDTO } from '../generated/bridge'

export const EXPERT_MANAGER_NAME = 'expert-manager'
export const EXPERT_MANAGER_TEMPLATE_ID = 'expert-manager'
export const EXPERT_CREATE_PROMPT = '帮我创建一个 XXX 专家，擅长 XXXXX。我的经验是：[请补充你的行业背景、相关经验]'

export async function ensureExpertManager(skills: SkillBridge = skillBridge): Promise<SkillDTO | undefined> {
  const findPublished = async () =>
    (await skills.list({ status: 'published' })).items.find(item => item.name === EXPERT_MANAGER_NAME)

  const existing = await findPublished()
  if (existing) return existing

  try {
    const installed = await skills.install?.({ templateId: EXPERT_MANAGER_TEMPLATE_ID })
    if (installed?.skillId) {
      await skills.publish?.({ id: installed.skillId })
      return findPublished()
    }
  } catch {
    /* draft may already exist */
  }

  const draft = (await skills.list({ status: 'draft' })).items.find(item => item.name === EXPERT_MANAGER_NAME)
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
