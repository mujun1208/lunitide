import{describe,expect,it,beforeEach}from'vitest'
import{useMroStore,resetMroStore}from'./mroStore'

describe('mroStore',()=>{
 beforeEach(()=>resetMroStore())

 it('starts gated off with empty gating',()=>{
  const s=useMroStore.getState()
  expect(s.mroEnabled).toBe(false)
  expect(s.mroExpertId).toBe('')
  expect(s.opsExpertIds).toEqual({})
  expect(s.mroInitialRail).toBe('manuals')
 })

 it('sets values directly',()=>{
  useMroStore.getState().setMroEnabled(true)
  useMroStore.getState().setMroExpertId('exp-1')
  useMroStore.getState().setOpsExpertIds({'mro-expert':'exp-1'})
  useMroStore.getState().setMroInitialRail('parts')
  const s=useMroStore.getState()
  expect(s.mroEnabled).toBe(true)
  expect(s.mroExpertId).toBe('exp-1')
  expect(s.opsExpertIds).toEqual({'mro-expert':'exp-1'})
  expect(s.mroInitialRail).toBe('parts')
 })

 it('supports functional updaters',()=>{
  useMroStore.getState().setMroEnabled(prev=>!prev)
  expect(useMroStore.getState().mroEnabled).toBe(true)
  useMroStore.getState().setOpsExpertIds(prev=>({...prev,a:'1'}))
  useMroStore.getState().setOpsExpertIds(prev=>({...prev,b:'2'}))
  expect(useMroStore.getState().opsExpertIds).toEqual({a:'1',b:'2'})
 })

 it('resetMroStore clears every field',()=>{
  useMroStore.getState().setMroEnabled(true)
  useMroStore.getState().setMroExpertId('exp-1')
  useMroStore.getState().setOpsExpertIds({a:'1'})
  useMroStore.getState().setMroInitialRail('plan')
  resetMroStore()
  const s=useMroStore.getState()
  expect(s.mroEnabled).toBe(false)
  expect(s.mroExpertId).toBe('')
  expect(s.opsExpertIds).toEqual({})
  expect(s.mroInitialRail).toBe('manuals')
 })
})