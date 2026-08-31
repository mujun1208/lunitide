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

  test('detects a Feishu webhook and enables with a test send', async () => {
    const user = userEvent.setup()
    const set = vi.fn(async (p: ImChannelsSetPayload): Promise<ImChannelsGetResult> => ({
      channels: seeded.channels.map(ch => ch.kind === p.kind ? {
        ...ch,
        enabled: p.enabled ?? true,
        webhookUrl: p.webhookUrl ?? ch.webhookUrl,
        mode: 'webhook',
      } : ch),
    }))
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(seeded), set, deliver: vi.fn() }} />)
    expect(await screen.findByText('飞书')).toBeInTheDocument()
    expect(screen.getByText('微信')).toBeInTheDocument()
    const url = screen.getByLabelText('消息通道 Webhook')
    await user.type(url, 'https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee')
    await user.click(screen.getByRole('button', { name: '识别并启用' }))
    expect(set).toHaveBeenCalledWith({
      kind: 'feishu',
      enabled: true,
      webhookUrl: 'https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
      testSend: true,
    })
  })

  test('connects Feishu inbound and waits for first-message pairing', async () => {
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
    expect(screen.queryByLabelText('飞书 入站白名单')).toBeNull()
    const appId = screen.getByLabelText('飞书 入站 App ID')
    await user.type(appId, 'cli_ok')
    await user.click(screen.getAllByRole('button', { name: '连接并等待配对' })[0])
    expect(set).toHaveBeenCalledWith({
      kind: 'feishu',
      inboundEnabled: true,
      inboundAppId: 'cli_ok',
    })
    expect(set.mock.calls[0][0].inboundAllowlist).toBeUndefined()
  })

  test('shows paired status and enables auto-run only after a sender is stored', async () => {
    const paired: ImChannelsGetResult = {
      channels: seeded.channels.map(ch => ch.kind === 'feishu' ? {
        ...ch,
        inboundEnabled: true,
        inboundAllowlist: 'ou_me',
        inboundAppId: 'cli_ok',
      } : ch),
    }
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(paired), set: vi.fn(), deliver: vi.fn() }} />)
    expect((await screen.findAllByText(/已配对：ou_me/)).length).toBeGreaterThan(0)
    expect(screen.getByLabelText('飞书 入站自动执行')).toBeEnabled()
    expect(screen.getByLabelText('企业微信 入站自动执行')).toBeDisabled()
  })

  test('shows DingTalk inbound pairing next to Feishu', async () => {
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(seeded), set: vi.fn(), deliver: vi.fn() }} />)
    expect(await screen.findByText('钉钉')).toBeInTheDocument()
    expect(screen.getByLabelText('钉钉 入站 App ID')).toBeInTheDocument()
    expect(screen.getAllByText(/本机已登录客户端代发，不能收消息/).length).toBeGreaterThan(0)
    expect(screen.getAllByText(/关窗口 ≠ 退出助手/).length).toBeGreaterThan(0)
  })

  test('shows WeCom Bot ID and Secret for inbound long-connect', async () => {
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(seeded), set: vi.fn(), deliver: vi.fn() }} />)
    expect(await screen.findByText('企业微信')).toBeInTheDocument()
    expect(screen.getByLabelText('企业微信 入站 Bot ID')).toBeInTheDocument()
    expect(screen.getByLabelText('企业微信 入站 Bot Secret')).toBeInTheDocument()
    expect(screen.queryByText(/只走本机桥接投递/)).toBeNull()
  })
})
