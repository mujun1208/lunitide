import{describe,expect,it}from'vitest'
import{attachmentToken,parseComposer,skillToken}from'./composerParser'
const A='01ARZ3NDEKTSV4RRFFQ69G5FAV',S='01ARZ3NDEKTSV4RRFFQ69G5FAA'
describe('composer parser',()=>{it('round trips stable ids',()=>expect(parseComposer(`${attachmentToken(A,'a.md')} explain`)).toEqual({text:'explain',contextRefs:[{type:'attachment',id:A}]}));it('parses an anchored skill only',()=>expect(parseComposer(`${skillToken(S,'sum')} hello`)).toMatchObject({skillInvocation:{skillId:S,input:'hello'}}));it.each(['https://x/y','C:/work/a','foo/bar','mail@host'])('does not mistake ordinary text %s',text=>expect(parseComposer(text)).toEqual({text,contextRefs:[]}))})
