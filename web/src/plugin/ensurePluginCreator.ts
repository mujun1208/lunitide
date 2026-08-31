import{skillBridge,type SkillBridge}from'../bridge/client'
import type{SkillDTO}from'../generated/bridge'

export const PLUGIN_CREATOR_NAME='plugin-creator'
export const PLUGIN_CREATOR_TEMPLATE_ID='plugin-creator'
export const PLUGIN_CREATE_PROMPT='帮我创建一个能力包。说明要组合哪些技能模板、MCP 预置和工具门闸。调用 plugin.create 时在 manifest 里写 skills、mcpPresetIds、toolGates。创建成功后告诉我去能力包页查看。不会执行外部脚本。'

export async function ensurePluginCreator(skills:SkillBridge=skillBridge):Promise<SkillDTO|undefined>{
 const findPublished=async()=>(await skills.list({status:'published'})).items.find(item=>item.name===PLUGIN_CREATOR_NAME)
 const existing=await findPublished()
 if(existing)return existing
 try{
  const installed=await skills.install?.({templateId:PLUGIN_CREATOR_TEMPLATE_ID})
  if(installed?.skillId){await skills.publish?.({id:installed.skillId});return findPublished()}
 }catch{/* draft may already exist */}
 const draft=(await skills.list({status:'draft'})).items.find(item=>item.name===PLUGIN_CREATOR_NAME)
 if(draft){
  try{await skills.publish?.({id:draft.id})}catch{/* publish gate may block in tests */}
  return findPublished()
 }
 return findPublished()
}
