import type { ProviderProtocol } from '../generated/bridge'
export function normalizeOrigin(raw: string): string {
  const input = raw.trim()
  if (/[^\x20-\x7e]/.test(input) || input.includes('\\')) throw new Error('地址只能使用 ASCII 字符，且不能包含反斜杠')
  const url = new URL(input)
  if ((url.protocol !== 'https:' && url.protocol !== 'http:') || url.username || url.password || url.search || url.hash) throw new Error('请输入有效的 HTTP(S) 地址（不能包含凭据、查询或片段）')
  const host = url.hostname
  const ipv6 = host.startsWith('[') && host.endsWith(']') && /^[0-9a-fA-F:.]+$/.test(host.slice(1,-1))
  const ipv4 = /^(?:\d{1,3}\.){3}\d{1,3}$/.test(host) && host.split('.').every(x => Number(x) <= 255)
  const dns = /^(?=.{1,253}$)(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(?:\.(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?))*$/.test(host)
  if (input.match(/^https?:\/\/([^/?#]+)/i)?.[1].replace(/:\d+$/,'').endsWith('.') || !(ipv6 || ipv4 || dns)) throw new Error('主机名格式无效')
  return url.origin
}
export async function originFingerprint(protocol: ProviderProtocol, raw: string): Promise<{ origin: string; fingerprint: string }> {
  const origin = normalizeOrigin(raw), digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(`${protocol}\0${origin}`))
  return { origin, fingerprint: [...new Uint8Array(digest)].map(v => v.toString(16).padStart(2, '0')).join('') }
}
