export type ImWebhookKind = 'feishu' | 'wecom' | 'dingtalk'

export function detectImWebhookKind(raw: string): ImWebhookKind | null {
  const value = raw.trim()
  if (!value) return null
  let host = ''
  try {
    host = new URL(value).hostname.toLowerCase()
  } catch {
    return null
  }
  if (host.endsWith('feishu.cn') || host.endsWith('larksuite.com') || host.endsWith('lark.com')) return 'feishu'
  if (host.endsWith('qyapi.weixin.qq.com')) return 'wecom'
  if (host.endsWith('dingtalk.com')) return 'dingtalk'
  return null
}
