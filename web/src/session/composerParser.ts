export type ContextRef={type:'attachment'|'skillResult';id:string}
export type AttachmentMention={id:string;label:string}
export type ParsedComposer={text:string;contextRefs:ContextRef[]}
const U='[0-7][0-9A-HJKMNP-TV-Z]{25}'
const ATT=new RegExp(`\\[attachment:(${U})\\|([^\\]\\r\\n]+)\\]`,'g')
const SKILL_REF=/^\[引用技能 ([^\]|\r\n]+)\|([0-9A-HJKMNP-TV-Z]{26})\]\r?\n?/
const EXPERT_REF=/^\[引用专家 ([^\]|\r\n]+)\|([0-9A-HJKMNP-TV-Z]{26})\]\r?\n?/
const cleanLabel=(label:string)=>label.replace(/[\]|\r\n]/g,' ')
export function attachmentToken(id:string,label:string){return`[attachment:${id}|${cleanLabel(label)}]`}
export function parseAttachmentMentions(source:string):{mentions:AttachmentMention[];text:string}{
 const mentions:AttachmentMention[]=[]
 const text=source.replace(ATT,(_,id:string,label:string)=>{mentions.push({id,label});return''}).replace(/\s{2,}/g,' ').trim()
 return{mentions,text}
}
export function embedPendingAttachments(prompt:string,extraIds:readonly string[]=[],labels:Record<string,string>={}):string{
 const have=new Set(parseComposer(prompt).contextRefs.map(ref=>ref.id))
 const prefix=extraIds.filter(id=>id&&!have.has(id)).map(id=>attachmentToken(id,labels[id]||'附件'))
 return prefix.length?`${prefix.join(' ')} ${prompt}`.trim():prompt
}
export function skillRefPrefix(skills:Array<{displayName:string;id:string}>):string{return skills.map(s=>`[引用技能 ${cleanLabel(s.displayName)}|${s.id}]`).join('\n')+(skills.length?'\n':'')}
export function expertRefPrefix(experts:Array<{name:string;expertId:string}>):string{return experts.map(e=>`[引用专家 ${cleanLabel(e.name)}|${e.expertId}]`).join('\n')+(experts.length?'\n':'')}
export function composeChatPrompt(text:string,skills:Array<{displayName:string;id:string}>=[],experts:Array<{name:string;expertId:string}>=[],includeExperts=true):string{
 const withSkills=skills.length?skillRefPrefix(skills)+text:text
 return includeExperts?expertRefPrefix(experts)+withSkills:withSkills
}
export function splitLeadingRefs(text:string):{skills:string[];experts:string[];text:string}{
 const skills:string[]=[],experts:string[]=[];let rest=text
 for(;;){
  const skill=rest.match(SKILL_REF)
  if(skill){skills.push(skill[1]);rest=rest.slice(skill[0].length);continue}
  const expert=rest.match(EXPERT_REF)
  if(expert){experts.push(expert[1]);rest=rest.slice(expert[0].length);continue}
  break
 }
 return{skills,experts,text:rest}
}
export function splitSkillRefs(text:string):{names:string[];text:string}{
 const split=splitLeadingRefs(text)
 return{names:split.skills,text:split.text}
}
export function parseComposer(source:string):ParsedComposer{
 const{mentions,text}=parseAttachmentMentions(source)
 return{text,contextRefs:mentions.map(item=>({type:'attachment' as const,id:item.id}))}
}
export function userBubbleParts(source:string):{skills:string[];experts:string[];mentions:AttachmentMention[];text:string}{
 const refs=splitLeadingRefs(source)
 const attachments=parseAttachmentMentions(refs.text)
 return{skills:refs.skills,experts:refs.experts,mentions:attachments.mentions,text:attachments.text}
}
