import{afterEach,describe,expect,it}from'vitest'
import{useNavStore,resetNavStore}from'./navStore'
import type{LaunchTarget}from'./appTypes'

// A-02.2: navStore is a singleton, so every case resets it the way App does on
// mount. These lock in the value + function-updater setter contract (matching
// React.Dispatch<SetStateAction<T>>) and the synchronous per-mount reset that
// keeps a stale page/target from leaking across App mounts.
afterEach(()=>resetNavStore())

const fakeTarget=(id:string):LaunchTarget=>({project:{id,name:'p',projectCode:'ITM00000',type:'implementation',status:'active',createdAt:'',updatedAt:'',version:1},session:{id,projectId:id,title:'t',status:'active',pinned:false,createdAt:'',updatedAt:'',version:1},personal:true})

describe('navStore',()=>{
 it('starts on home with no target and default ui state',()=>{
  const s=useNavStore.getState()
  expect(s.page).toBe('home')
  expect(s.target).toBeUndefined()
  expect(s.drawer).toBe(false)
  expect(s.sidebarCollapsed).toBe(false)
  expect(s.settingsCategory).toBe('general')
  expect(s.catalogFocus).toBeUndefined()
 })

 it('sets values directly',()=>{
  useNavStore.getState().setPage('settings')
  useNavStore.getState().setSettingsCategory('personal')
  useNavStore.getState().setCatalogFocus({kind:'expert',id:'x'})
  const s=useNavStore.getState()
  expect(s.page).toBe('settings')
  expect(s.settingsCategory).toBe('personal')
  expect(s.catalogFocus).toEqual({kind:'expert',id:'x'})
 })

 it('supports the React function-updater setter form',()=>{
  const t=fakeTarget('01ARZ3NDEKTSV4RRFFQ69G5FAV')
  useNavStore.getState().setTarget(t)
  useNavStore.getState().setTarget(prev=>prev?.personal?{...prev,noAutoSend:true}:prev)
  expect(useNavStore.getState().target).toEqual({...t,noAutoSend:true})
  useNavStore.getState().setDrawer(prev=>!prev)
  expect(useNavStore.getState().drawer).toBe(true)
 })

 it('resets every field back to initial navigation state',()=>{
  const s=useNavStore.getState()
  s.setPage('mcp');s.setTarget(fakeTarget('01ARZ3NDEKTSV4RRFFQ69G5FAW'));s.setDrawer(true);s.setSidebarCollapsed(true);s.setSettingsCategory('meetings');s.setCatalogFocus({kind:'skill'})
  resetNavStore()
  const after=useNavStore.getState()
  expect(after.page).toBe('home')
  expect(after.target).toBeUndefined()
  expect(after.drawer).toBe(false)
  expect(after.sidebarCollapsed).toBe(false)
  expect(after.settingsCategory).toBe('general')
  expect(after.catalogFocus).toBeUndefined()
 })
})