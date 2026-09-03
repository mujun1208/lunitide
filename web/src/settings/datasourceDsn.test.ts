import { describe, expect, it } from 'vitest'
import { composeDatasourceDsn, FIXED_DATABASE } from './datasourceDsn'

describe('composeDatasourceDsn', () => {
  it('defaults postgres to local host, prefer TLS, and the fixed database', () => {
    const local = composeDatasourceDsn('postgres', { host: '', port: '', user: 'postgres', password: 'pw', ssl: false })
    expect(local).toBe(`postgres://postgres:pw@127.0.0.1:5432/${FIXED_DATABASE}?sslmode=prefer`)
  })

  it('keeps strict require and an explicit database when SSL is checked for postgres', () => {
    const strict = composeDatasourceDsn('postgres', { host: 'db.example.com', port: '5433', database: 'ops', user: 'ro', password: 's3cret', ssl: true })
    expect(strict).toBe('postgres://ro:s3cret@db.example.com:5433/ops?sslmode=require')
  })

  it('builds a local MySQL DSN with opportunistic TLS, key retrieval, and the fixed database', () => {
    const dsn = composeDatasourceDsn('mysql', { host: '', port: '', user: 'root', password: '128128', ssl: false })
    expect(dsn).toContain(`@tcp(127.0.0.1:3306)/${FIXED_DATABASE}`)
    expect(dsn).toContain('tls=preferred')
    expect(dsn).toContain('allowPublicKeyRetrieval=true')
  })

  it('uses strict TLS and no key retrieval when SSL is checked for MySQL', () => {
    const dsn = composeDatasourceDsn('mysql', { host: '10.0.0.5', port: '3307', user: 'ro', password: 'p', ssl: true })
    expect(dsn).toContain('@tcp(10.0.0.5:3307)/')
    expect(dsn).toContain('tls=true')
    expect(dsn).not.toContain('allowPublicKeyRetrieval')
  })

  it('percent-encodes credentials with reserved characters', () => {
    const dsn = composeDatasourceDsn('mysql', { host: '', port: '', user: 'a@b', password: 'p:w/@d', ssl: false })
    expect(dsn).toContain('a%40b')
    expect(dsn).toContain('p%3Aw%2F%40d')
  })
})
