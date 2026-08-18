export type ContextRef={type:'attachment'|'skillResult';id:string}
export type ParsedComposer={text:string;contextRefs:ContextRef[]}
const U='[0-7][0-9A-HJKMNP-TV-Z]{25}'
const ATT=new RegExp(`\\[attachment:(${U})\\|([^\\]\\r\\n]+)\\]`,'g')
// Skill references ride as chips in the composer (not inline text); the send
// path prefixes the persisted message with `[引用技能 name|id]` lines so the
// model sees them and can call the skill.invoke tool during the chat stream.
const SKILL_REF=/^\[引用技能 ([^\]|\r\n]+)\|([0-9A-HJKMNP-TV-Z]{26})\]\r?\n?/gm
const cleanLabel=(label:string)=>label.replace(/[\]|\r\n]/g,' ')
export function attachmentToken(id:string,label:string){return`[attachment:${id}|${cleanLabel(label)}]`}
export function skillRefPrefix(skills:Array<{displayName:string;id:string}>):string{return skills.map(s=>`[引用技能 ${cleanLabel(s.displayName)}|${s.id}]`).join('\n')+(skills.length?'\n':'')}
export function splitSkillRefs(text:string):{names:string[];text:string}{
 const names:string[]=[];let rest=text,m:RegExpExecArray|null
 SKILL_REF.lastIndex=0
 while((m=SKILL_REF.exec(rest))!==null&&m.index===0){names.push(m[1]);rest=rest.slice(m[0].length);SKILL_REF.lastIndex=0}
 return{names,text:rest}
}
export function parseComposer(source:string):ParsedComposer{
 const refs:ContextRef[]=[];let text=source.replace(ATT,(_,id:string)=>{refs.push({type:'attachment',id});return''}).replace(/\s{2,}/g,' ').trim()
 return{text,contextRefs:refs}
}
