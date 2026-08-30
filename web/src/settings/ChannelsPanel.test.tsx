import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'
import { ChannelsPanel } from './SettingsPage'
import type { ImChannelsGetResult, ImChannelsSetPayload } from '../generated/bridge'

const inbound = { inboundEnabled: false, inboundAllowlist: '', inboundAutoRun: false, inboundAppId: '', inboundHasSecret: false }
const seeded: ImChannelsGetResult = {
  channels: [
    { kind: 'feishu', label: '飞书', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '飞书', updatedAt: '2026-08-30T00:00:00Z', ...inbound },
    { kind: 'wecom', label: '企业微信', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '企业微信', updatedAt: '2026-08-30T00:00:00Z', ...inbound },
    { kind: 'dingtalk', label: '钉钉', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '钉钉', updatedAt: '2026-08-30T00:00:00Z', ...inbound },
    { kind: 'wechat', label: '微信', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '微信', updatedAt: '2026-08-30T00:00:00Z', ...inbound },
    { kind: 'qq', label: 'QQ', enabled: false, webhookUrl: '', mode: 'off', desktopApp: 'QQ', updatedAt: '2026-08-30T00:00:00Z', ...inbound },
  ],
}

describe('ChannelsPanel', () => {
  afterEach(() => cleanup())

  test('lists five channels and saves a Feishu webhook', async () => {
    const user = userEvent.setup()
    const set = vi.fn(async (p: ImChannelsSetPayload): Promise<ImChannelsGetResult> => ({
      channels: seeded.channels.map(ch => ch.kind === p.kind ? {
        ...ch,
        enabled: p.enabled ?? true,
        webhookUrl: p.webhookUrl ?? ch.webhookUrl,
        mode: p.kind === 'feishu' ? 'webhook' : 'desktop',
      } : ch),
    }))
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(seeded), set, deliver: vi.fn() }} />)
    expect(await screen.findByText('飞书')).toBeInTheDocument()
    expect(screen.getByText('微信')).toBeInTheDocument()
    const url = screen.getByLabelText('飞书 Webhook 地址')
    await user.clear(url)
    await user.type(url, 'https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee')
    await user.click(screen.getByRole('button', { name: '保存飞书 Webhook' }))
    expect(set).toHaveBeenCalledWith({
      kind: 'feishu',
      enabled: true,
      webhookUrl: 'https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
    })
  })

  test('enables Feishu inbound only after an allowlist is saved', async () => {
    const user = userEvent.setup()
    const set = vi.fn(async (p: ImChannelsSetPayload): Promise<ImChannelsGetResult> => ({
      channels: seeded.channels.map(ch => ch.kind === p.kind ? {
        ...ch,
        inboundEnabled: p.inboundEnabled ?? ch.inboundEnabled,
        inboundAllowlist: p.inboundAllowlist ?? ch.inboundAllowlist,
        inboundAppId: p.inboundAppId ?? ch.inboundAppId,
      } : ch),
    }))
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(seeded), set, deliver: vi.fn() }} />)
    expect(await screen.findByText('飞书')).toBeInTheDocument()
    const allow = screen.getByLabelText('飞书 入站白名单')
    await user.type(allow, 'ou_ok')
    await user.click(screen.getAllByRole('button', { name: '开启入站' })[0])
    expect(set).toHaveBeenCalledWith({
      kind: 'feishu',
      inboundEnabled: true,
      inboundAllowlist: 'ou_ok',
      inboundAppId: '',
    })
  })

  test('shows WeCom Bot ID and Secret for inbound long-connect', async () => {
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(seeded), set: vi.fn(), deliver: vi.fn() }} />)
    expect(await screen.findByText('企业微信')).toBeInTheDocument()
    expect(screen.getByLabelText('企业微信 入站 Bot ID')).toBeInTheDocument()
    expect(screen.getByLabelText('企业微信 入站 Bot Secret')).toBeInTheDocument()
    expect(screen.queryByText(/只走本机桥接投递/)).toBeNull()
  })
})
