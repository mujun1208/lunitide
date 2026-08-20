export type ContextRef={type:'attachment'|'skillResult';id:string}
export type ParsedComposer={text:string;contextRefs:ContextRef[]}
const U='[0-7][0-9A-HJKMNP-TV-Z]{25}'
const ATT=new RegExp(`\\[attachment:(${U})\\|([^\\]\\r\\n]+)\\]`,'g')
// Skill / expert references ride as chips in the composer (not inline text); the send
// path prefixes the persisted message with `[引用技能 name|id]` / `[引用专家 name|id]`
// so the model sees them and can invoke the skill or follow the expert persona.
const SKILL_REF=/^\[引用技能 ([^\]|\r\n]+)\|([0-9A-HJKMNP-TV-Z]{26})\]\r?\n?/
const EXPERT_REF=/^\[引用专家 ([^\]|\r\n]+)\|([0-9A-HJKMNP-TV-Z]{26})\]\r?\n?/
const cleanLabel=(label:string)=>label.replace(/[\]|\r\n]/g,' ')
export function attachmentToken(id:string,label:string){return`[attachment:${id}|${cleanLabel(label)}]`}
export function skillRefPrefix(skills:Array<{displayName:string;id:string}>):string{return skills.map(s=>`[引用技能 ${cleanLabel(s.displayName)}|${s.id}]`).join('\n')+(skills.length?'\n':'')}
export function expertRefPrefix(experts:Array<{name:string;expertId:string}>):string{return experts.map(e=>`[引用专家 ${cleanLabel(e.name)}|${e.expertId}]`).join('\n')+(experts.length?'\n':'')}
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
 const refs:ContextRef[]=[];let text=source.replace(ATT,(_,id:string)=>{refs.push({type:'attachment',id});return''}).replace(/\s{2,}/g,' ').trim()
 return{text,contextRefs:refs}
}
