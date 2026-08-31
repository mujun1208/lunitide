import { describe, expect, test } from 'vitest'
import { detectImWebhookKind } from './imWebhook'

describe('detectImWebhookKind', () => {
  test('classifies vendor hosts', () => {
    expect(detectImWebhookKind('https://open.feishu.cn/open-apis/bot/v2/hook/aaa')).toBe('feishu')
    expect(detectImWebhookKind('https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x')).toBe('wecom')
    expect(detectImWebhookKind('https://oapi.dingtalk.com/robot/send?access_token=x')).toBe('dingtalk')
  })

  test('rejects unknown or empty urls', () => {
    expect(detectImWebhookKind('')).toBeNull()
    expect(detectImWebhookKind('not-a-url')).toBeNull()
    expect(detectImWebhookKind('https://example.com/hook')).toBeNull()
  })
})
