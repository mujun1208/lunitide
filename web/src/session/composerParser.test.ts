import{describe,expect,it}from'vitest'
import{attachmentToken,expertRefPrefix,parseComposer,skillRefPrefix,splitLeadingRefs,splitSkillRefs}from'./composerParser'
const A='01ARZ3NDEKTSV4RRFFQ69G5FAV',S='01ARZ3NDEKTSV4RRFFQ69G5FAA'
describe('composer parser',()=>{it('round trips stable ids',()=>expect(parseComposer(`${attachmentToken(A,'a.md')} explain`)).toEqual({text:'explain',contextRefs:[{type:'attachment',id:A}]}));it.each(['https://x/y','C:/work/a','foo/bar','mail@host'])('does not mistake ordinary text %s',text=>expect(parseComposer(text)).toEqual({text,contextRefs:[]}))})
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
  it('splits leading expert refs with skills',()=>{
    const split=splitLeadingRefs(`[引用技能 摘要|${S}]\n[引用专家 安全工程师|${A}]\n请审查这段设计`)
    expect(split).toEqual({skills:['摘要'],experts:['安全工程师'],text:'请审查这段设计'})
  })
})
