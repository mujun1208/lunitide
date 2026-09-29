import{create}from'zustand'
import type{WorkbenchRail}from'../expert/expertIds'

// A-02.4 mroStore (F-07 §3): single source of truth for the MRO workbench
// gating state that App derives from experts.list and the expert-center
// "open workbench" action — `mroEnabled` (ops workbench installed+enabled),
// `mroExpertId` (the enabled mro-expert id, '' when absent), `opsExpertIds`
// (catalogItemId -> expertId map for ask/bulletin routing) and
// `mroInitialRail` (which rail the workbench opens on). Extracted from App's
// useState so the experts.list effect and the ExpertCenterPage onOpenWorkbench
// closure keep their existing setter shapes while the state lives in one store
// (props contract unchanged — see the SetState value-or-updater shim below).
//
// Like navStore/sessionStore, this gating decides what the MRO route renders
// across mounts, so the singleton MUST be reset synchronously per App mount
// (see resetMroStore + the useState initializer in App): an effect-time reset
// would let a stale enabled/expert map from a previous mount leak into the
// first paint before experts.list resolves.

type SetState<T>=T|((prev:T)=>T)
function resolve<T>(next:SetState<T>,prev:T):T{
 return typeof next==='function'?(next as(p:T)=>T)(prev):next
}

export interface MroState{
 mroEnabled:boolean
 mroExpertId:string
 opsExpertIds:Record<string,string>
 mroInitialRail:WorkbenchRail
 setMroEnabled:(next:SetState<boolean>)=>void
 setMroExpertId:(next:SetState<string>)=>void
 setOpsExpertIds:(next:SetState<Record<string,string>>)=>void
 setMroInitialRail:(next:SetState<WorkbenchRail>)=>void
}

type MroData=Pick<MroState,'mroEnabled'|'mroExpertId'|'opsExpertIds'|'mroInitialRail'>

function initialMro():MroData{
 return{mroEnabled:false,mroExpertId:'',opsExpertIds:{},mroInitialRail:'manuals'}
}

export const useMroStore=create<MroState>((set,get)=>({
 ...initialMro(),
 setMroEnabled:next=>set({mroEnabled:resolve(next,get().mroEnabled)}),
 setMroExpertId:next=>set({mroExpertId:resolve(next,get().mroExpertId)}),
 setOpsExpertIds:next=>set({opsExpertIds:resolve(next,get().opsExpertIds)}),
 setMroInitialRail:next=>set({mroInitialRail:resolve(next,get().mroInitialRail)}),
}))

// resetMroStore restores the singleton to its initial disabled/empty gating.
// App runs it via a useState initializer so every mount starts synchronously
// with the workbench gated off and no expert map — matching the previous
// per-mount useState defaults and keeping the store from leaking gating across
// test mounts (afterEach unmounts, next mount resets before any selector reads).
export function resetMroStore(){
 useMroStore.setState(initialMro())
}