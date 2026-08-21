import{describe,expect,it}from'vitest'
import{expertInitial,readMountedExperts,writeMountedExperts}from'./sessionExperts'

describe('sessionExperts',()=>{
 it('round-trips mounted experts in localStorage',()=>{
  const sessionId='01ARZ3NDEKTSV4RRFFQ69G5FAA'
  writeMountedExperts(sessionId,[{expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name:'安全工程师',division:'security'}])
  expect(readMountedExperts(sessionId)).toEqual([{expertId:'01ARZ3NDEKTSV4RRFFQ69G5FAB',name:'安全工程师',division:'security'}])
  expect(expertInitial('安全工程师')).toBe('安')
 })
})
