export type DatasourceKind = 'postgres' | 'mysql'

// FIXED_DATABASE is the one database name Lunitide uses on a connected server.
// It is not shown in the form: the user only supplies an account + password, and
// the backend auto-creates this database on a local server if it is missing (see
// datasourceapp.SQLProvisioner). Keeping it fixed removes the "what do I name
// it?" decision entirely.
export const FIXED_DATABASE = 'lunitide'

export function composeDatasourceDsn(kind: DatasourceKind, input: {
  host: string
  port: string
  database?: string
  user: string
  password: string
  ssl: boolean
}): string {
  const host = input.host.trim() || '127.0.0.1'
  const port = input.port.trim() || (kind === 'postgres' ? '5432' : '3306')
  const database = (input.database ?? '').trim() || FIXED_DATABASE
  const user = encodeURIComponent(input.user)
  const password = encodeURIComponent(input.password)
  if (kind === 'postgres') {
    // A bare local Postgres has no TLS listener, so `require` would refuse the
    // connection out of the box — the most common "it doesn't work on my
    // machine" trap. Unchecked uses `prefer`: it still negotiates TLS when the
    // server offers it (managed/prod DBs) and falls back to plaintext for a
    // local server. Checked enforces strict `require` for anyone who needs it.
    const sslmode = input.ssl ? 'require' : 'prefer'
    return `postgres://${user}:${password}@${host}:${port}/${database}?sslmode=${sslmode}`
  }
  // MySQL: honour the SSL box (it used to be ignored). `preferred` uses TLS when
  // available yet still connects to a plain local server. On a non-TLS link
  // allowPublicKeyRetrieval lets MySQL 8 caching_sha2_password authenticate
  // (needed the first time a fresh user connects to a local server).
  const params = new URLSearchParams()
  params.set('tls', input.ssl ? 'true' : 'preferred')
  if (!input.ssl) params.set('allowPublicKeyRetrieval', 'true')
  return `${user}:${password}@tcp(${host}:${port})/${database}?${params.toString()}`
}

export function datasourceStatus(row: { state: string; readonlyVerified: boolean }): 'disabled' | 'unverified' | 'verified' {
  if (row.state === 'disabled') return 'disabled'
  if (!row.readonlyVerified) return 'unverified'
  return 'verified'
}
