import{create}from'zustand'
import type{SettingsCategory}from'../settings/settingsNav'
import type{LaunchTarget,Page}from'./appTypes'

// A-02.2 navStore (F-07 §3): single source of truth for the navigation state
// that decides App's top-level render branch — `page`/`target` (personal chat vs
// project workbench vs launch), plus the drawer/collapse/settings-category/
// catalog-focus UI that several components read and write. Extracted from App's
// useState so useAppNavigation and MroWorkbenchRoute keep their existing setter
// props while the state itself lives in one store (props contract unchanged).
//
// Unlike themeStore, this state selects the whole component subtree, so the
// singleton MUST be reset synchronously per App mount (see resetNavStore + the
// useState initializer in App): an effect-time reset would let a stale target
// from a previous mount render the wrong branch on first paint.

export type CatalogFocus={kind:'skill'|'expert'|'plugin';id?:string}|undefined

type SetState<T>=T|((prev:T)=>T)
function resolve<T>(next:SetState<T>,prev:T):T{
 return typeof next==='function'?(next as(p:T)=>T)(prev):next
}

export interface NavState{
 page:Page
 target:LaunchTarget|undefined
 drawer:boolean
 sidebarCollapsed:boolean
 settingsCategory:SettingsCategory
 catalogFocus:CatalogFocus
 setPage:(next:SetState<Page>)=>void
 setTarget:(next:SetState<LaunchTarget|undefined>)=>void
 setDrawer:(next:SetState<boolean>)=>void
 setSidebarCollapsed:(next:SetState<boolean>)=>void
 setSettingsCategory:(next:SetState<SettingsCategory>)=>void
 setCatalogFocus:(next:SetState<CatalogFocus>)=>void
}

type NavData=Pick<NavState,'page'|'target'|'drawer'|'sidebarCollapsed'|'settingsCategory'|'catalogFocus'>

function initialNav():NavData{
 return{page:'home',target:undefined,drawer:false,sidebarCollapsed:false,settingsCategory:'general',catalogFocus:undefined}
}

export const useNavStore=create<NavState>((set,get)=>({
 ...initialNav(),
 setPage:next=>set({page:resolve(next,get().page)}),
 setTarget:next=>set({target:resolve(next,get().target)}),
 setDrawer:next=>set({drawer:resolve(next,get().drawer)}),
 setSidebarCollapsed:next=>set({sidebarCollapsed:resolve(next,get().sidebarCollapsed)}),
 setSettingsCategory:next=>set({settingsCategory:resolve(next,get().settingsCategory)}),
 setCatalogFocus:next=>set({catalogFocus:resolve(next,get().catalogFocus)}),
}))

// resetNavStore restores the singleton to its initial navigation state. App runs
// it via a useState initializer so every mount starts synchronously on 'home'
// with no target — matching the previous per-mount useState defaults and keeping
// the store from leaking a page/target across test mounts (afterEach unmounts,
// next mount resets before any selector reads).
export function resetNavStore(){
 useNavStore.setState(initialNav())
}