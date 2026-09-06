import{describe,expect,it,beforeEach}from'vitest'
import{useSessionStore,resetSessionStore}from'./sessionStore'
import type{ChatTarget}from'./appTypes'

function chat(id:string):ChatTarget{
 return{project:{id:`p-${id}`}as ChatTarget['project'],session:{id}as ChatTarget['session'],personal:true}
}

describe('sessionStore',()=>{
 beforeEach(()=>resetSessionStore())

 it('starts empty',()=>{
  const s=useSessionStore.getState()
  expect(s.localChats).toEqual([])
  expect(s.deletedChatIds.size).toBe(0)
  expect(s.draftSessionIds.size).toBe(0)
 })

 it('sets values directly',()=>{
  useSessionStore.getState().setLocalChats([{...chat('a'),pending:true}])
  useSessionStore.getState().setDeletedChatIds(new Set(['x']))
  useSessionStore.getState().setDraftSessionIds(new Set(['d']))
  const s=useSessionStore.getState()
  expect(s.localChats.map(c=>c.session.id)).toEqual(['a'])
  expect(s.deletedChatIds.has('x')).toBe(true)
  expect(s.draftSessionIds.has('d')).toBe(true)
 })

 it('supports functional updaters',()=>{
  useSessionStore.getState().setLocalChats(prev=>[chat('a'),...prev])
  useSessionStore.getState().setLocalChats(prev=>[chat('b'),...prev])
  expect(useSessionStore.getState().localChats.map(c=>c.session.id)).toEqual(['b','a'])
  useSessionStore.getState().setDraftSessionIds(prev=>new Set(prev).add('d1'))
  useSessionStore.getState().setDraftSessionIds(prev=>new Set(prev).add('d2'))
  expect([...useSessionStore.getState().draftSessionIds].sort()).toEqual(['d1','d2'])
 })

 it('resetSessionStore clears every field',()=>{
  useSessionStore.getState().setLocalChats([chat('a')])
  useSessionStore.getState().setDeletedChatIds(new Set(['x']))
  useSessionStore.getState().setDraftSessionIds(new Set(['d']))
  resetSessionStore()
  const s=useSessionStore.getState()
  expect(s.localChats).toEqual([])
  expect(s.deletedChatIds.size).toBe(0)
  expect(s.draftSessionIds.size).toBe(0)
 })
})