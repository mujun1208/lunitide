import { expect, it } from 'vitest'
import { bannersFromTurnState } from './sessionResume'

it('does not treat a trailing user message as resume', () => {
  expect(bannersFromTurnState({ live: false, storedPersist: undefined, localTurn: undefined })).toEqual({
    persistFailed: false,
    persistDraft: undefined,
    resume: false,
  })
})

it('uses the server checkpoint for persist versus resume', () => {
  expect(bannersFromTurnState({
    live: false,
    storedPersist: undefined,
    server: { status: 'completed', persistFailed: true, persistDraft: '已经生成但没落库' },
  })).toEqual({ persistFailed: true, persistDraft: '已经生成但没落库', resume: false })
  expect(bannersFromTurnState({
    live: false,
    localTurn: { status: 'running' },
    server: { status: 'interrupted', persistFailed: false, persistDraft: '' },
  })).toEqual({ persistFailed: false, persistDraft: undefined, resume: true })
})

it('falls back to local persist and active-turn only when inspect is missing', () => {
  expect(bannersFromTurnState({
    live: false,
    storedPersist: { draft: '本地草稿' },
    localTurn: { status: 'interrupted' },
  })).toEqual({ persistFailed: true, persistDraft: '本地草稿', resume: false })
  expect(bannersFromTurnState({
    live: false,
    localTurn: { status: 'interrupted' },
  })).toEqual({ persistFailed: false, persistDraft: undefined, resume: true })
})

it('restores a running draft without marking persist-failed', () => {
  expect(bannersFromTurnState({
    live: false,
    server: { status: 'running', persistFailed: false, persistDraft: '流到一半还没写完' },
  })).toEqual({ persistFailed: false, persistDraft: '流到一半还没写完', resume: true })
})

it('hides both banners while a live stream is attached', () => {
  expect(bannersFromTurnState({
    live: true,
    storedPersist: { draft: 'x' },
    server: { status: 'running', persistFailed: true, persistDraft: 'x' },
  })).toEqual({ persistFailed: false, resume: false })
})
