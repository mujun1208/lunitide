import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'
import type { IdentityBridge, PeopleBridge } from '../bridge/client'
import type { IdentityDTO } from '../generated/bridge'
import { ProfilePanel } from './ProfilePanel'

const profile = (partial: Partial<IdentityDTO> = {}): IdentityDTO => ({
  subjectId: '01ARZ3NDEKTSV4RRFFQ69G5FAV',
  nickname: '月汐用户',
  avatar: '',
  status: 'online',
  department: '',
  title: '',
  orgName: '',
  bio: '',
  publicKey: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  pairingCode: '123456',
  passwordSet: false,
  locked: false,
  discoveryEnabled: false,
  createdAt: '2026-08-27T00:00:00.000Z',
  updatedAt: '2026-08-27T00:00:00.000Z',
  ...partial,
})

describe('ProfilePanel', () => {
  afterEach(() => cleanup())

  test('saves nickname and keeps LAN discovery off by default', async () => {
    const current = profile()
    const identity: IdentityBridge = {
      get: vi.fn().mockResolvedValue(current),
      update: vi.fn().mockImplementation(async payload => ({ ...current, ...payload, nickname: payload.nickname ?? current.nickname })),
      passwordSet: vi.fn(),
      unlock: vi.fn(),
    }
    const people: PeopleBridge = {
      list: vi.fn(), pair: vi.fn(), discoveryGet: vi.fn(),
      discoverySet: vi.fn(), threadList: vi.fn(), threadOpen: vi.fn(),
      threadSend: vi.fn(), groupCreate: vi.fn(), fileDecide: vi.fn(),
      threadTyping: vi.fn(), fileStage: vi.fn(), filePick: vi.fn(), screenCapture: vi.fn(), peerAdd: vi.fn(), contactUpdate: vi.fn(),
    }
    const user = userEvent.setup()
    render(<ProfilePanel identity={identity} people={people} />)
    expect(await screen.findByText('让同网段的月汐看见我')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '已关闭' })).toBeInTheDocument()
    expect(screen.getByDisplayValue('01ARZ3NDEKTSV4RRFFQ69G5FAV')).toBeInTheDocument()
    expect(screen.getByDisplayValue(current.publicKey)).toBeInTheDocument()
    expect(screen.queryByText(/private/i)).not.toBeInTheDocument()
    await user.clear(screen.getByDisplayValue('月汐用户'))
    await user.type(screen.getByRole('textbox', { name: '显示名' }), 'mu')
    await user.click(screen.getByRole('button', { name: '保存名片' }))
    expect(identity.update).toHaveBeenCalledWith(expect.objectContaining({ nickname: 'mu' }))
  })

  test('avatar pick sends a this-PC path instead of a canvas data URL', async () => {
    const current = profile()
    const identity: IdentityBridge = {
      get: vi.fn().mockResolvedValue(current),
      update: vi.fn().mockImplementation(async payload => ({ ...current, ...payload })),
      passwordSet: vi.fn(),
      unlock: vi.fn(),
    }
    const people: PeopleBridge = {
      list: vi.fn(), pair: vi.fn(), discoveryGet: vi.fn(),
      discoverySet: vi.fn(), threadList: vi.fn(), threadOpen: vi.fn(),
      threadSend: vi.fn(), groupCreate: vi.fn(), fileDecide: vi.fn(),
      threadTyping: vi.fn(), fileStage: vi.fn(),
      filePick: vi.fn().mockResolvedValue({ path: 'C:/Users/mu/Pictures/face.jpg', fileName: 'face.jpg' }),
      screenCapture: vi.fn(), peerAdd: vi.fn(), contactUpdate: vi.fn(),
    }
    const user = userEvent.setup()
    render(<ProfilePanel identity={identity} people={people} />)
    const action = await screen.findByText('更换头像')
    expect(action).toHaveClass('profile-avatar-action')
    expect(document.querySelector('.profile-fields')).toBeTruthy()
    await user.click(action)
    await vi.waitFor(() => expect(people.filePick).toHaveBeenCalledWith({ folder: false }))
    expect(identity.update).toHaveBeenCalledWith({ avatar: 'C:/Users/mu/Pictures/face.jpg' })
  })

  test('groups identity, presence, LAN and security with scannable layout', async () => {
    const current = profile()
    const identity: IdentityBridge = {
      get: vi.fn().mockResolvedValue(current),
      update: vi.fn(),
      passwordSet: vi.fn(),
      unlock: vi.fn(),
    }
    const people: PeopleBridge = {
      list: vi.fn(), pair: vi.fn(), discoveryGet: vi.fn(),
      discoverySet: vi.fn(), threadList: vi.fn(), threadOpen: vi.fn(),
      threadSend: vi.fn(), groupCreate: vi.fn(), fileDecide: vi.fn(),
      threadTyping: vi.fn(), fileStage: vi.fn(), filePick: vi.fn(), screenCapture: vi.fn(), peerAdd: vi.fn(), contactUpdate: vi.fn(),
    }
    render(<ProfilePanel identity={identity} people={people} />)
    expect(await screen.findByRole('heading', { name: '身份' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '在场' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '局域网' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '安全' })).toBeInTheDocument()
    expect(document.querySelector('[data-profile-section="identity"]')).toBeTruthy()
    expect(document.querySelector('[data-profile-section="presence"]')).toBeTruthy()
    expect(document.querySelector('[data-profile-section="lan"]')).toBeTruthy()
    expect(document.querySelector('[data-profile-section="security"]')).toBeTruthy()
    expect(screen.getByRole('radio', { name: /隐身/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '已关闭' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByText(/发文件必须对方确认/)).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '显示名' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: '组织' })).toBeInTheDocument()
    expect(document.querySelector('.profile-hero-name')).toHaveTextContent('月汐用户')
  })
})
