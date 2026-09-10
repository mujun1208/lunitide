import {act,cleanup,renderHook,waitFor} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import type {SkillBridge} from '../bridge/client'
import type {SkillDTO} from '../generated/bridge'
import {useSessionSkillArtifacts} from './SessionSkillArtifacts'

afterEach(cleanup)
const skill=(id:string,sessionId?:string)=>({id,manifestJson:JSON.stringify({originSessionId:sessionId}),status:'draft'}) as SkillDTO

it('does not expose raw English list failures',async()=>{
  const bridge={list:vi.fn().mockRejectedValue(new Error('Failed to fetch')),get:vi.fn()} as unknown as SkillBridge
  const {result}=renderHook(()=>useSessionSkillArtifacts('session-a',bridge,[]))
  await waitFor(()=>expect(result.current.error).toBe('技能产物暂时无法读取'))
  expect(result.current.error).not.toContain('Failed to fetch')
})

it('restores only skills with exact creation-session provenance, including no legacy guesses',async()=>{
  const bridge={list:vi.fn().mockResolvedValue({items:[skill('mine','session-a'),skill('another','session-b'),skill('legacy')]}),get:vi.fn()} as unknown as SkillBridge
  const {result}=renderHook(()=>useSessionSkillArtifacts('session-a',bridge,[]))
  await waitFor(()=>expect(result.current.items.map(item=>item.id)).toEqual(['mine']))
  expect(bridge.list).toHaveBeenCalledWith({sourceSessionId:'session-a'})
})

it('loads a newly created ID returned by the real tool receipt and discards late session results',async()=>{
  let finish!:(value:{items:SkillDTO[]})=>void
  const bridge={list:vi.fn().mockImplementation(({sourceSessionId})=>sourceSessionId==='a'?new Promise<{items:SkillDTO[]}>(resolve=>{finish=resolve}):Promise.resolve({items:[]})),get:vi.fn().mockResolvedValue(skill('new-id'))} as unknown as SkillBridge
  const {result,rerender}=renderHook(({sessionId,liveIds})=>useSessionSkillArtifacts(sessionId,bridge,liveIds),{initialProps:{sessionId:'a',liveIds:[] as string[]}})
  rerender({sessionId:'b',liveIds:['new-id']})
  await waitFor(()=>expect(result.current.items.map(item=>item.id)).toEqual(['new-id']))
  await act(async()=>finish({items:[skill('old','a')]}))
  expect(result.current.items.map(item=>item.id)).toEqual(['new-id'])
  expect(bridge.get).toHaveBeenCalledWith({id:'new-id'})
})
