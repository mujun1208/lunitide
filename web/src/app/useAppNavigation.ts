import type React from 'react'
import type { ChatTarget, LaunchTarget, Page } from './appTypes'

// Q-06: low-coupling navigation/chat-registry handlers extracted from App.tsx.
// These closures depend only on state setters (plus `target` for the delete
// short-circuit), so moving them here is behavior-equivalent. Higher-coupling
// async creators (startCompanion / start*Creation / expert flows) stay in
// App.tsx on purpose.
type LocalChat = ChatTarget & { pending?: boolean }

interface AppNavigationInput {
  target: LaunchTarget | undefined
  setModelManagerOpen: React.Dispatch<React.SetStateAction<boolean>>
  setTarget: React.Dispatch<React.SetStateAction<LaunchTarget | undefined>>
  setPage: React.Dispatch<React.SetStateAction<Page>>
  setDraftKey: React.Dispatch<React.SetStateAction<number>>
  setDrawer: React.Dispatch<React.SetStateAction<boolean>>
  setDeletedChatIds: React.Dispatch<React.SetStateAction<Set<string>>>
  setDraftSessionIds: React.Dispatch<React.SetStateAction<Set<string>>>
  setLocalChats: React.Dispatch<React.SetStateAction<LocalChat[]>>
  setPeopleFocus: React.Dispatch<React.SetStateAction<'chats' | 'contacts' | 'me'>>
  setPeoplePeerId: React.Dispatch<React.SetStateAction<string | undefined>>
  setPeoplePeerName: React.Dispatch<React.SetStateAction<string | undefined>>
}

interface AppNavigation {
  fresh: () => void
  handleSessionDeleted: (id: string) => void
  registerChat: (next: ChatTarget, pending?: boolean) => void
  engagePersonalChat: (next: ChatTarget) => void
  setChatActivity: (id: string, active: boolean) => void
  openPeople: (rail: 'chats' | 'contacts' | 'me') => void
}

export function useAppNavigation(input: AppNavigationInput): AppNavigation {
  const {
    target,
    setModelManagerOpen,
    setTarget,
    setPage,
    setDraftKey,
    setDrawer,
    setDeletedChatIds,
    setDraftSessionIds,
    setLocalChats,
    setPeopleFocus,
    setPeoplePeerId,
    setPeoplePeerName,
  } = input

  const fresh = (): void => {
    setModelManagerOpen(false)
    setTarget(undefined)
    setPage('home')
    setDraftKey(v => v + 1)
    setDrawer(false)
  }

  const handleSessionDeleted = (id: string): void => {
    setDeletedChatIds(values => new Set(values).add(id))
    setDraftSessionIds(values => {
      if (!values.has(id)) return values
      const copy = new Set(values)
      copy.delete(id)
      return copy
    })
    setLocalChats(values => values.filter(value => value.session.id !== id))
    if (target?.personal && target.session.id === id) fresh()
  }

  const registerChat = (next: ChatTarget, pending = true): void => {
    setDeletedChatIds(values => {
      if (!values.has(next.session.id)) return values
      const copy = new Set(values)
      copy.delete(next.session.id)
      return copy
    })
    setDraftSessionIds(values => {
      if (!values.has(next.session.id)) return values
      const copy = new Set(values)
      copy.delete(next.session.id)
      return copy
    })
    setLocalChats(values => [{ ...next, pending }, ...values.filter(value => value.session.id !== next.session.id)])
  }

  const engagePersonalChat = (next: ChatTarget): void => registerChat(next, false)

  const setChatActivity = (id: string, active: boolean): void =>
    setLocalChats(values => values.map(value => (value.session.id === id ? { ...value, pending: active } : value)))

  const openPeople = (rail: 'chats' | 'contacts' | 'me'): void => {
    setPeoplePeerId(undefined)
    setPeoplePeerName(undefined)
    setPeopleFocus(rail)
    setTarget(undefined)
    setPage('people')
    setDrawer(false)
  }

  return { fresh, handleSessionDeleted, registerChat, engagePersonalChat, setChatActivity, openPeople }
}