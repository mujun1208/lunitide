// classifyAddresses 纯函数契约：局域网 IPv4 / 公网 IPv6 / 尾网（100.64.0.0/10）
// 三分类——尾网地址不得冒充局域网地址展示（Tailscale 方案回归的教训）。
import { describe, expect, it } from 'vitest'
import { classifyAddresses } from './RemoteCompanionPanel'

describe('classifyAddresses', () => {
  it('splits lan IPv4, global IPv6 and CGNAT tailnet addresses', () => {
    expect(classifyAddresses(['192.168.1.5', '2409:8900:1::a', '100.95.14.34']))
      .toEqual({ lan: ['192.168.1.5'], cellular: ['2409:8900:1::a'], tailnet: ['100.95.14.34'] })
  })

  it('bounds the CGNAT range at 100.64.0.0/10', () => {
    expect(classifyAddresses(['100.63.1.1']).tailnet).toEqual([])
    expect(classifyAddresses(['100.64.0.1']).tailnet).toEqual(['100.64.0.1'])
    expect(classifyAddresses(['100.127.255.255']).tailnet).toEqual(['100.127.255.255'])
    expect(classifyAddresses(['100.128.0.1']).tailnet).toEqual([])
    expect(classifyAddresses(['100.200.1.1']).lan).toEqual(['100.200.1.1'])
  })

  it('handles empty input and IPv6-only machines', () => {
    expect(classifyAddresses([])).toEqual({ lan: [], cellular: [], tailnet: [] })
    expect(classifyAddresses(['2409:8900:1::a'])).toEqual({ lan: [], cellular: ['2409:8900:1::a'], tailnet: [] })
  })
})
