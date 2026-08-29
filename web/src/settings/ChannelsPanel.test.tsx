import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'
import { ChannelsPanel } from './SettingsPage'
import type { ImChannelsGetResult, ImChannelsSetPayload } from '../generated/bridge'

const seeded: ImChannelsGetResult = {
  channels: [
    { kind: 'feishu', label: '飞书', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '飞书', updatedAt: '2026-08-30T00:00:00Z' },
    { kind: 'wecom', label: '企业微信', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '企业微信', updatedAt: '2026-08-30T00:00:00Z' },
    { kind: 'dingtalk', label: '钉钉', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '钉钉', updatedAt: '2026-08-30T00:00:00Z' },
    { kind: 'wechat', label: '微信', enabled: false, webhookUrl: '', mode: 'off', desktopApp: '微信', updatedAt: '2026-08-30T00:00:00Z' },
    { kind: 'qq', label: 'QQ', enabled: false, webhookUrl: '', mode: 'off', desktopApp: 'QQ', updatedAt: '2026-08-30T00:00:00Z' },
  ],
}

describe('ChannelsPanel', () => {
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
    render(<ChannelsPanel bridge={{ get: vi.fn().mockResolvedValue(seeded), set }} />)
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
})
