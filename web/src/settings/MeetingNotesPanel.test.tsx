import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import type { ProviderDTO } from '../generated/bridge'
import { MeetingNotesPanel } from './MeetingNotesPanel'
import { defaultMeetingSettings, saveMeetingSettings } from '../meetings/meetingSettings'

const now = '2026-08-27T03:00:00.000Z'
const voiceProvider: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAW', name: 'Volc', protocol: 'volc_speech', baseUrl: 'https://openspeech.bytedance.com',
  status: 'enabled', credentialState: 'configured', createdAt: now, updatedAt: now, version: 1,
  models: [{ modelId: 'seed-asr', displayName: '听写', isDefault: true, kind: 'asr', kindDefault: true }],
}
const llmOnly: ProviderDTO = {
  id: '01ARZ3NDEKTSV4RRFFQ69G5FAX', name: 'Chat', protocol: 'openai_compatible', baseUrl: 'https://example.com',
  status: 'enabled', credentialState: 'configured', createdAt: now, updatedAt: now, version: 1,
  models: [{ modelId: 'qwen', displayName: 'Qwen', isDefault: true, kind: 'llm', kindDefault: true }],
}

let providers: ProviderDTO[] = []
vi.mock('../bridge/client', async importOriginal => {
  const actual = await importOriginal<typeof import('../bridge/client')>()
  return { ...actual, getProviderBridge: () => ({ list: () => Promise.resolve({ items: providers }) }) }
})

afterEach(() => { cleanup(); localStorage.clear() })

it('pre-disables the 火山 tile until a seed-asr voice model is configured', async () => {
  providers = [llmOnly]
  render(<MeetingNotesPanel />)
  const volc = await screen.findByRole('radio', { name: /火山/ })
  await waitFor(() => expect(volc).toBeDisabled())
  expect(volc).toHaveAttribute('title', expect.stringMatching(/seed-asr/))
  expect(volc).toHaveAttribute('aria-checked', 'false')
})

it('enables the 火山 tile once a seed-asr provider exists', async () => {
  providers = [voiceProvider, llmOnly]
  render(<MeetingNotesPanel />)
  const volc = await screen.findByRole('radio', { name: /火山/ })
  await waitFor(() => expect(volc).not.toBeDisabled())
  await userEvent.setup().click(volc)
  expect(volc).toHaveAttribute('aria-checked', 'true')
})

it('locks listen engine and notes model while a meeting is recording', async () => {
  providers = [voiceProvider, llmOnly]
  render(<MeetingNotesPanel recordingLock />)
  const volc = await screen.findByRole('radio', { name: /火山/ })
  await waitFor(() => expect(volc).toBeDisabled())
  expect(volc).toHaveAttribute('title', '本场结束后生效')
  expect(screen.getByLabelText('纪要模型')).toBeDisabled()
  expect(screen.getByRole('status')).toHaveTextContent('本场结束后生效')
})

it('warns when 火山 is already the saved choice but no seed-asr is available', async () => {
  saveMeetingSettings({ ...defaultMeetingSettings(), listen: 'volc' })
  providers = [llmOnly]
  render(<MeetingNotesPanel />)
  expect(await screen.findByRole('alert')).toHaveTextContent(/没有可用的 seed-asr/)
})
