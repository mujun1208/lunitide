import{create}from'zustand'
import type{ChatTarget}from'./appTypes'

// A-02.3 sessionStore (F-07 §3): single source of truth for the personal-chat
// list state that App and useAppNavigation both mutate — `localChats` (the
// optimistic sidebar list), `deletedChatIds` (tombstones that hide a row until
// the backend list catches up) and `draftSessionIds` (sessions created but not
// yet sent, shown as drafts). Extracted from App's useState so the
// register/delete/activity closures in useAppNavigation keep their existing
// React.Dispatch setter props while the state lives in one store (props
// contract unchanged — see the SetState value-or-updater shim below).
//
// Like navStore, this list decides what the sidebar renders across mounts, so
// the singleton MUST be reset synchronously per App mount (see
// resetSessionStore + the useState initializer in App): an effect-time reset
// would let a stale localChats/deleted/draft set from a previous mount leak
// into the first paint.

export type LocalChat=ChatTarget&{pending?:boolean}

type SetState<T>=T|((prev:T)=>T)
function resolve<T>(next:SetState<T>,prev:T):T{
 return typeof next==='function'?(next as(p:T)=>T)(prev):next
}

export interface SessionState{
 localChats:LocalChat[]
 deletedChatIds:Set<string>
 draftSessionIds:Set<string>
 setLocalChats:(next:SetState<LocalChat[]>)=>void
 setDeletedChatIds:(next:SetState<Set<string>>)=>void
 setDraftSessionIds:(next:SetState<Set<string>>)=>void
}

type SessionData=Pick<SessionState,'localChats'|'deletedChatIds'|'draftSessionIds'>

function initialSession():SessionData{
 return{localChats:[],deletedChatIds:new Set<string>(),draftSessionIds:new Set<string>()}
}

export const useSessionStore=create<SessionState>((set,get)=>({
 ...initialSession(),
 setLocalChats:next=>set({localChats:resolve(next,get().localChats)}),
 setDeletedChatIds:next=>set({deletedChatIds:resolve(next,get().deletedChatIds)}),
 setDraftSessionIds:next=>set({draftSessionIds:resolve(next,get().draftSessionIds)}),
}))

// resetSessionStore restores the singleton to its initial empty lists. App runs
// it via a useState initializer so every mount starts synchronously with no
// local chats / tombstones / drafts — matching the previous per-mount useState
// defaults and keeping the store from leaking a list across test mounts
// (afterEach unmounts, next mount resets before any selector reads).
export function resetSessionStore(){
 useSessionStore.setState(initialSession())
}