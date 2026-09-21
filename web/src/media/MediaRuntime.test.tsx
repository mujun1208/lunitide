import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ActivityBridge, MediaBridge } from '../bridge/client'
import type { MediaAssetDTO, MediaOperationDTO, MediaSnapshotDTO } from '../generated/bridge'
import { LanguageProvider } from '../i18n/language'
import { resetNavStore } from '../app/navStore'
import { MediaRuntime, useMediaStore } from './MediaRuntime'

afterEach(() => {
  cleanup()
  resetNavStore()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

beforeEach(() => {
  const proto = window.HTMLMediaElement.prototype
  vi.spyOn(proto, 'play').mockResolvedValue(undefined)
  vi.spyOn(proto, 'pause').mockImplementation(() => {})
  vi.spyOn(proto, 'load').mockImplementation(() => {})
  const dispatch = proto.dispatchEvent
  vi.spyOn(proto, 'dispatchEvent').mockImplementation(function (this: HTMLMediaElement, ev: Event) {
    if (ev.type === 'error') return true
    return dispatch.call(this, ev)
  })
})

const sessionId = '01ARZ3NDEKTSV4RRFFQ69G5FAV'
const assetId = '01ARZ3NDEKTSV4RRFFQ69G5FAW'
const playbackUrl = 'https://media.lunitide.local/v1/assets/tokentokentoken12'

const idle: MediaSnapshotDTO = {
  mediaSessionId: sessionId,
  scopeKind: 'user',
  scopeId: null,
  origin: 'owned',
  phase: 'idle',
  verificationStatus: 'none',
  verificationSource: 'owned_runtime',
  assetId,
  playbackEpoch: 1,
  autoAdvance: true,
  positionMs: 0,
  durationMs: 0,
  volume: 80,
  muted: false,
  queueRevision: 1,
  revision: 1,
  updatedAt: '2026-01-01T00:00:00Z',
}

const asset: MediaAssetDTO = {
  assetId,
  sourceKind: 'user_selected',
  kind: 'audio',
  title: 'ringing_shortest.mp3',
  mime: 'audio/mpeg',
  size: 12,
  state: 'ready',
  revision: 1,
}

const playOp: MediaOperationDTO = {
  operationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  mediaSessionId: sessionId,
  parentOperationId: null,
  rootOperationId: '01ARZ3NDEKTSV4RRFFQ69G5FAX',
  action: 'play',
  phase: 'dispatching',
  verificationStatus: 'pending',
  verificationSource: 'owned_runtime',
  errorCode: null,
  revision: 1,
}

function Probe() {
  const media = useMediaStore()
  return (
    <div>
      <button type="button" onClick={() => { void media.playPause() }}>probe-play</button>
      <span data-testid="url">{media.playbackUrl ?? ''}</span>
    </div>
  )
}

function fakeMedia(command: MediaBridge['command'], openAsset: MediaBridge['openAsset']): MediaBridge {
  return {
    list: async () => ({ items: [idle], nextCursor: null }),
    get: async () => idle,
    create: async () => ({ snapshot: idle, operation: playOp }),
    command,
    watch: async () => ({ streamId: '01ARZ3NDEKTSV4RRFFQ69G5FAY', dispose() {} }),
    listAssets: async () => ({ items: [asset], nextCursor: null }),
    openAsset,
    pick: async () => ({ canceled: true, assets: [] }),
    queueCommand: async () => ({ snapshot: idle, operation: playOp }),
    getOperation: async () => playOp,
    listOperations: async () => ({ items: [], nextCursor: null }),
    reportElement: async () => ({ accepted: true }),
  }
}

it('preloads the picked file and a Play click sends play, not pause, without tearing the URL down', async () => {
  const openAsset = vi.fn().mockResolvedValue({ playbackUrl, expiresAt: new Date(Date.now() + 60_000).toISOString() })
  const command = vi.fn().mockResolvedValue({
    snapshot: { ...idle, verificationStatus: 'command_dispatched' },
    operation: playOp,
  })
  render(
    <LanguageProvider value="zh-CN">
      <MediaRuntime media={fakeMedia(command, openAsset)} activity={{ list: async () => ({ items: [], nextCursor: null, snapshotAt: '2026-01-01T00:00:00Z', hasMore: false }) } as ActivityBridge}>
        <Probe />
      </MediaRuntime>
    </LanguageProvider>,
  )
  await waitFor(() => expect(openAsset).toHaveBeenCalled())
  await waitFor(() => expect(screen.getByTestId('url').textContent).toBe(playbackUrl))
  await userEvent.click(screen.getByRole('button', { name: 'probe-play' }))
  await waitFor(() => expect(command).toHaveBeenCalled())
  expect(command.mock.calls[0][0].action).toBe('play')
  await waitFor(() => expect(screen.getByTestId('url').textContent).toBe(playbackUrl))
  await userEvent.click(screen.getByRole('button', { name: 'probe-play' }))
  await waitFor(() => expect(command).toHaveBeenCalledTimes(2))
  expect(command.mock.calls[1][0].action).toBe('play')
})
