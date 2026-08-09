import { appendFileSync, copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { app } from 'electron'

type LogLevel = 'info' | 'warn' | 'error'

interface LogRecord {
  timestamp: string
  level: LogLevel
  scope: string
  message: string
  data?: Record<string, unknown>
}

export class AppLogger {
  readonly path: string

  constructor() {
    const logsDirectory = app.getPath('logs') || join(app.getPath('userData'), 'logs')
    mkdirSync(logsDirectory, { recursive: true })
    this.path = join(logsDirectory, 'main.jsonl')
  }

  info(scope: string, message: string, data?: Record<string, unknown>): void {
    this.write('info', scope, message, data)
  }

  warn(scope: string, message: string, data?: Record<string, unknown>): void {
    this.write('warn', scope, message, data)
  }

  error(scope: string, message: string, data?: Record<string, unknown>): void {
    this.write('error', scope, message, data)
  }

  exportDiagnostics(destination: string, status: unknown): void {
    mkdirSync(dirname(destination), { recursive: true })
    const logCopy = `${destination}.logs.jsonl`
    if (existsSync(this.path)) copyFileSync(this.path, logCopy)
    writeFileSync(destination, JSON.stringify({
      exportedAt: new Date().toISOString(),
      app: { name: app.getName(), version: app.getVersion(), packaged: app.isPackaged },
      platform: { platform: process.platform, arch: process.arch, versions: process.versions },
      engine: status,
      logFile: existsSync(this.path) ? logCopy : null
    }, null, 2), 'utf8')
  }

  tail(maxBytes = 64 * 1024): string {
    if (!existsSync(this.path)) return ''
    const content = readFileSync(this.path)
    return content.subarray(Math.max(0, content.length - maxBytes)).toString('utf8')
  }

  private write(level: LogLevel, scope: string, message: string, data?: Record<string, unknown>): void {
    const record: LogRecord = { timestamp: new Date().toISOString(), level, scope, message, data }
    appendFileSync(this.path, `${JSON.stringify(record)}\n`, 'utf8')
    const output = level === 'error' ? console.error : level === 'warn' ? console.warn : console.info
    output(`[${scope}] ${message}`, data ?? '')
  }
}
