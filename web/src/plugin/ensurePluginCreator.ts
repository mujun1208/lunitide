import{skillBridge,type SkillBridge}from'../bridge/client'
import type{SkillDTO}from'../generated/bridge'

export const PLUGIN_CREATOR_NAME='plugin-creator'
export const PLUGIN_CREATOR_TEMPLATE_ID='plugin-creator'
export const PLUGIN_CREATE_PROMPT='帮我创建一个可以实现「……」的插件。说明它要增强哪项能力、需要哪些权限，创建成功后告诉我去插件页安装使用。'

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
