import{createMutationAttempt,type ProjectBridge}from'../bridge/client'
import type{ProjectDTO}from'../generated/bridge'
import{isColleagueChatTitle,isPlaceholderChatTitle}from'../session/sessionTitle'

export const PERSONAL_CHAT_PROJECT='⁣月汐·普通对话'
export const PERSONAL_CHAT_PROJECT_ID_KEY='lunitide:personal-chat-project-id'
export {validMessageText as validPrompt} from '../session/messageLimits'
export const isOrdinarySidebarChat=(title:string)=>!isPlaceholderChatTitle(title)&&!isColleagueChatTitle(title)
export const formatBytes=(n:number)=>n<1024?`${n} B`:n<1048576?`${(n/1024).toFixed(1)} KiB`:`${(n/1048576).toFixed(1)} MiB`
export const findPersonalProject=(items:ProjectDTO[]):ProjectDTO|undefined=>{const savedId=localStorage.getItem(PERSONAL_CHAT_PROJECT_ID_KEY);return items.find(item=>item.id===savedId)??items.find(item=>item.name===PERSONAL_CHAT_PROJECT)??items.find(item=>item.name.startsWith('\u2063'))}
export const seededUnit=(index:number,salt:number):number=>{const value=Math.sin((index+1)*12.9898+salt*78.233)*43758.5453;return value-Math.floor(value)}
export async function ensurePersonalProject(projects:ProjectBridge):Promise<ProjectDTO>{
 const items=(await projects.list()).items,existing=findPersonalProject(items)
 if(existing){localStorage.setItem(PERSONAL_CHAT_PROJECT_ID_KEY,existing.id);return existing}
 const payload={name:PERSONAL_CHAT_PROJECT},attempt=createMutationAttempt('project.create',payload)
 const created=await projects.create(attempt.payload,{attempt});localStorage.setItem(PERSONAL_CHAT_PROJECT_ID_KEY,created.id);return created
}