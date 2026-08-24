import { describe,expect,it } from 'vitest'
import { isLocalTrustedOrigin, normalizeOrigin, originBindingChanged } from './fingerprint'
describe('credential origin parity',()=>{
 it.each([['https://EXAMPLE.com:443/v1','https://example.com'],['https://127.0.0.1:443/x','https://127.0.0.1'],['https://[2001:DB8::1]:443/v1','https://[2001:db8::1]'],['http://example.com','http://example.com']])('normalizes %s', (input,want)=>expect(normalizeOrigin(input)).toBe(want))
 it.each(['https://例子.com','https://example.com.','https://example.com\\evil','https://exa_mple.com'])('rejects %s',input=>expect(()=>normalizeOrigin(input)).toThrow())
 it.each(['http://127.0.0.1:1234','http://localhost:1234/v1','http://192.168.31.100:1234','http://10.0.0.2:11434','http://172.16.1.1:1234'])('trusts local origin %s',input=>expect(isLocalTrustedOrigin(input)).toBe(true))
 it.each(['https://example.com','https://api.openai.com/v1','http://8.8.8.8'])('rejects non-local origin %s',input=>expect(isLocalTrustedOrigin(input)).toBe(false))
 it('detects LAN origin moves without path-only edits',()=>{
  expect(originBindingChanged('openai_compatible','http://127.0.0.1:1234/v1','openai_compatible','http://192.168.31.100:1234')).toBe(true)
  expect(originBindingChanged('openai_compatible','http://192.168.31.100:1234/v1','openai_compatible','http://192.168.31.100:1234')).toBe(false)
 })
})
