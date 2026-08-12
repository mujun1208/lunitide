export type ContextRef={type:'attachment'|'skillResult';id:string}
export type SkillInvocation={skillId:string;input:string}
export type ParsedComposer={text:string;contextRefs:ContextRef[];skillInvocation?:SkillInvocation}
const U='[0-7][0-9A-HJKMNP-TV-Z]{25}'
const ATT=new RegExp(`\\[attachment:(${U})\\|([^\\]\\r\\n]+)\\]`,'g')
const SKILL=new RegExp(`^\\s*\\[skill:(${U})\\|([^\\]\\r\\n]+)\\](?:\\s+([\\s\\S]*))?$`)
export function attachmentToken(id:string,label:string){return`[attachment:${id}|${label.replace(/[\]|\r\n]/g,' ')}]`}
export function skillToken(id:string,label:string){return`[skill:${id}|${label.replace(/[\]|\r\n]/g,' ')}]`}
export function parseComposer(source:string):ParsedComposer{
 const refs:ContextRef[]=[];let text=source.replace(ATT,(_,id:string)=>{refs.push({type:'attachment',id});return''}).replace(/\s{2,}/g,' ').trim()
 const match=SKILL.exec(source);if(match)return{text:(match[3]??'').trim(),contextRefs:refs,skillInvocation:{skillId:match[1],input:(match[3]??'').trim()||match[2]}}
 return{text,contextRefs:refs}
}
