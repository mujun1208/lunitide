import { describe,expect,it } from 'vitest'
import { normalizeOrigin } from './fingerprint'
describe('credential origin parity',()=>{
 it.each([['https://EXAMPLE.com:443/v1','https://example.com'],['https://127.0.0.1:443/x','https://127.0.0.1'],['https://[2001:DB8::1]:443/v1','https://[2001:db8::1]'],['http://example.com','http://example.com']])('normalizes %s', (input,want)=>expect(normalizeOrigin(input)).toBe(want))
 it.each(['https://例子.com','https://example.com.','https://example.com\\evil','https://exa_mple.com'])('rejects %s',input=>expect(()=>normalizeOrigin(input)).toThrow())
})
