import{describe,expect,it}from'vitest'
import{attachmentToken,composeChatPrompt,embedPendingAttachments,expertRefPrefix,parseAttachmentMentions,parseComposer,skillRefPrefix,splitLeadingRefs,splitSkillRefs,userBubbleParts}from'./composerParser'
const A='01ARZ3NDEKTSV4RRFFQ69G5FAV',S='01ARZ3NDEKTSV4RRFFQ69G5FAA'
describe('composer parser',()=>{
 it('round trips stable ids',()=>expect(parseComposer(`${attachmentToken(A,'a.md')} explain`)).toEqual({text:'explain',contextRefs:[{type:'attachment',id:A}]}))
 it.each(['https://x/y','C:/work/a','foo/bar','mail@host'])('does not mistake ordinary text %s',text=>expect(parseComposer(text)).toEqual({text,contextRefs:[]}))
 it('keeps attachment labels for the chat bubble',()=>expect(parseAttachmentMentions(`${attachmentToken(A,'shot.png')} 看看这个图片`)).toEqual({mentions:[{id:A,label:'shot.png'}],text:'看看这个图片'}))
 it('embeds pending attachment ids that are not already in the prompt',()=>{
  expect(embedPendingAttachments('看看这个图片',[A],{[A]:'shot.png'})).toBe(`${attachmentToken(A,'shot.png')} 看看这个图片`)
  expect(embedPendingAttachments(`${attachmentToken(A,'shot.png')} 看看这个图片`,[A],{[A]:'shot.png'})).toBe(`${attachmentToken(A,'shot.png')} 看看这个图片`)
 })
 it('strips attachment tokens from the visible bubble text',()=>{
  expect(userBubbleParts(`${attachmentToken(A,'shot.png')} 看看这个图片`)).toEqual({skills:[],experts:[],mentions:[{id:A,label:'shot.png'}],text:'看看这个图片'})
 })
})
describe('skill reference prefix',()=>{
  it('builds a prefix from referenced skills',()=>expect(skillRefPrefix([{displayName:'摘要',id:S}])).toBe(`[引用技能 摘要|${S}]\n`))
  it('returns empty prefix without skills',()=>expect(skillRefPrefix([])).toBe(''))
  it('splits leading skill refs off a persisted message',()=>{
    const{names,text}=splitSkillRefs(`[引用技能 摘要|${S}]\n[引用技能 清单|${A}]\n正文的用户提问`)
    expect(names).toEqual(['摘要','清单']);expect(text).toBe('正文的用户提问')
  })
  it('keeps text intact when no refs lead',()=>expect(splitSkillRefs('普通消息 [引用技能 x|'+S+'] 在中间')).toEqual({names:[],text:'普通消息 [引用技能 x|'+S+'] 在中间'}))
})
describe('expert reference prefix',()=>{
  it('builds a prefix from referenced experts',()=>expect(expertRefPrefix([{name:'安全工程师',expertId:A}])).toBe(`[引用专家 安全工程师|${A}]\n`))
  it('prefixes selected chips onto a PM rethink so the engine cannot fan-out the catalog',()=>{
    const prompt=composeChatPrompt('重新思考，给出一个新的方案。',[],[{name:'安全工程师',expertId:A},{name:'AI 工程师',expertId:S}])
    expect(prompt.startsWith(`[引用专家 安全工程师|${A}]`)).toBe(true)
    expect(prompt).toContain('重新思考，给出一个新的方案。')
    expect(composeChatPrompt('继续',[],[{name:'PPT专家',expertId:A}],false)).toBe('继续')
    expect(composeChatPrompt('继续上次未完成的工作。',[],[{name:'PPT专家',expertId:A}],true)).toContain(`[引用专家 PPT专家|${A}]`)
  })
  it('splits leading expert refs with skills',()=>{
    const split=splitLeadingRefs(`[引用技能 摘要|${S}]\n[引用专家 安全工程师|${A}]\n请审查这段设计`)
    expect(split).toEqual({skills:['摘要'],experts:['安全工程师'],text:'请审查这段设计'})
  })
})
