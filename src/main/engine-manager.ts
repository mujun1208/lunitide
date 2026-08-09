import { randomBytes } from 'node:crypto'
import { ChildProcess, spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import net from 'node:net'
import { join } from 'node:path'
import { app } from 'electron'
import type { EngineState, EngineStatus } from '../shared/engine'
import { AppLogger } from './logger'

type StatusListener = (status: EngineStatus) => void

const START_TIMEOUT_MS = 15_000
const HEALTH_INTERVAL_MS = 5_000
const HEALTH_FAILURE_LIMIT = 3
const MAX_RESTARTS = 3
const RESTART_WINDOW_MS = 60_000
const STOP_TIMEOUT_MS = 2_000

export class EngineManager {
  private child?: ChildProcess
  private token = ''
  private port = 0
  private healthTimer?: NodeJS.Timeout
  private startTimer?: NodeJS.Timeout
  private restartTimer?: NodeJS.Timeout
  private intentionalStop = false
  private operation: Promise<void> = Promise.resolve()
  private restartTimes: number[] = []
  private consecutiveHealthFailures = 0
  private failedChildren = new WeakSet<ChildProcess>()
  private listeners = new Set<StatusListener>()
  private status: EngineStatus = this.makeStatus('stopped', '引擎尚未启动')

  constructor(private readonly logger: AppLogger) {}

  subscribe(listener: StatusListener): () => void {
    this.listeners.add(listener)
    listener(this.getStatus())
    return () => this.listeners.delete(listener)
  }

  getStatus(): EngineStatus {
    return { ...this.status }
  }

  async request<T>(path: string, body: unknown): Promise<T> {
    if (this.status.state !== 'ready' || !this.port || !this.token) throw new Error('本地引擎尚未就绪')
    const response = await fetch(`http://127.0.0.1:${this.port}${path}`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${this.token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(65_000)
    })
    const payload = await response.json().catch(() => ({ detail: `HTTP ${response.status}` })) as T & { detail?: string }
    if (!response.ok) throw new Error(payload.detail || `模型服务请求失败（HTTP ${response.status}）`)
    return payload
  }

  start(): Promise<void> {
    this.intentionalStop = false
    return this.enqueue(() => this.startInternal())
  }

  restart(): Promise<void> {
    this.intentionalStop = true
    clearTimeout(this.restartTimer)
    this.restartTimer = undefined
    return this.enqueue(async () => {
      await this.stopInternal()
      this.intentionalStop = false
      this.restartTimes = []
      await this.startInternal()
    })
  }

  stop(): Promise<void> {
    this.intentionalStop = true
    clearTimeout(this.restartTimer)
    this.restartTimer = undefined
    return this.enqueue(() => this.stopInternal())
  }

  private enqueue(action: () => Promise<void>): Promise<void> {
    const next = this.operation.then(action, action)
    this.operation = next.catch((error: unknown) => {
      this.logger.error('engine', '生命周期操作失败', { error: String(error) })
    })
    return next
  }

  private async startInternal(): Promise<void> {
    if (this.child || this.intentionalStop) return
    this.port = await findAvailablePort()
    this.token = randomBytes(32).toString('hex')
    this.consecutiveHealthFailures = 0
    this.setStatus('starting', '正在启动本地引擎…')

    const engineRoot = app.isPackaged ? join(process.resourcesPath, 'engine') : join(app.getAppPath(), 'engine')
    const venvPython = join(app.getAppPath(), '.venv', 'Scripts', 'python.exe')
    const executable = app.isPackaged
      ? join(engineRoot, 'lunitide-engine.exe')
      : process.env.LUNITIDE_PYTHON || (existsSync(venvPython) ? venvPython : 'python')
    const args = app.isPackaged
      ? ['--port', String(this.port)]
      : [join(engineRoot, 'launcher.py'), '--port', String(this.port)]
    const inheritedEnvironment = [
      'SystemRoot', 'WINDIR', 'TEMP', 'TMP', 'LOCALAPPDATA', 'APPDATA', 'USERPROFILE', 'PATH'
    ].reduce<NodeJS.ProcessEnv>((environment, key) => {
      if (process.env[key]) environment[key] = process.env[key]
      return environment
    }, { PYTHONUTF8: '1' })

    const child = spawn(
      executable,
      args,
      {
        cwd: engineRoot,
        windowsHide: true,
        env: inheritedEnvironment,
        stdio: ['pipe', 'pipe', 'pipe']
      }
    )
    child.stdin?.end(`${JSON.stringify({ token: this.token, parentPid: process.pid })}\n`)
    this.child = child
    this.logger.info('engine', '子进程已创建', { pid: child.pid, port: this.port })

    child.stdout?.on('data', (chunk) => this.logger.info('engine.stdout', String(chunk).trim()))
    child.stderr?.on('data', (chunk) => this.logger.warn('engine.stderr', String(chunk).trim()))
    child.once('error', (error) => this.handleFailure(child, `引擎启动失败：${error.message}`))
    child.once('exit', (code, signal) => {
      this.logger.info('engine', '子进程退出', { pid: child.pid, code, signal })
      if (this.child === child) this.child = undefined
      if (!this.intentionalStop) this.handleFailure(child, `引擎异常退出（code ${code ?? 'unknown'}）`)
    })

    this.startTimer = setTimeout(() => this.handleFailure(child, '引擎启动超过 15 秒'), START_TIMEOUT_MS)
    await this.checkHealth(child)
    if (this.child === child && !this.intentionalStop) {
      this.healthTimer = setInterval(() => void this.checkHealth(child), HEALTH_INTERVAL_MS)
    }
  }

  private async stopInternal(): Promise<void> {
    this.intentionalStop = true
    this.clearTimers()
    const child = this.child
    this.child = undefined
    if (child) await terminateProcessTree(child)
    this.token = ''
    this.port = 0
    this.consecutiveHealthFailures = 0
    this.setStatus('stopped', '引擎已停止')
  }

  private async checkHealth(child: ChildProcess): Promise<void> {
    if (this.child !== child || !this.port || this.intentionalStop) return
    try {
      const response = await fetch(`http://127.0.0.1:${this.port}/health`, {
        headers: { Authorization: `Bearer ${this.token}` },
        signal: AbortSignal.timeout(1_500)
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      const body = await response.json() as { status?: string }
      if (body.status !== 'ready') throw new Error('invalid health response')
      this.consecutiveHealthFailures = 0
      clearTimeout(this.startTimer)
      this.startTimer = undefined
      if (this.status.state !== 'ready') this.setStatus('ready', '本地引擎运行中')
    } catch {
      this.consecutiveHealthFailures += 1
      if (this.status.state === 'ready') this.setStatus('degraded', '引擎健康检查失败，正在重试')
      if (this.consecutiveHealthFailures >= HEALTH_FAILURE_LIMIT && this.status.state !== 'starting') {
        this.handleFailure(child, `引擎连续 ${HEALTH_FAILURE_LIMIT} 次健康检查失败`)
      }
    }
  }

  private handleFailure(child: ChildProcess, detail: string): void {
    if (this.intentionalStop || this.failedChildren.has(child)) return
    this.failedChildren.add(child)
    this.clearTimers()
    const now = Date.now()
    this.restartTimes = this.restartTimes.filter((time) => now - time < RESTART_WINDOW_MS)
    if (this.restartTimes.length >= MAX_RESTARTS) {
      this.setStatus('degraded', `${detail}；已停止自动重启，请手动重试`)
      if (this.child === child) this.child = undefined
      void terminateProcessTree(child)
      return
    }
    this.restartTimes.push(now)
    this.setStatus('restarting', `${detail}；正在恢复…`)
    if (this.child === child) this.child = undefined
    void terminateProcessTree(child).finally(() => {
      if (this.intentionalStop) return
      this.restartTimer = setTimeout(() => {
        this.restartTimer = undefined
        if (this.intentionalStop) return
        void this.enqueue(() => this.startInternal())
      }, 1_000)
    })
  }

  private clearTimers(): void {
    clearInterval(this.healthTimer)
    clearTimeout(this.startTimer)
    clearTimeout(this.restartTimer)
    this.healthTimer = undefined
    this.startTimer = undefined
    this.restartTimer = undefined
  }

  private setStatus(state: EngineState, detail: string): void {
    this.status = this.makeStatus(state, detail)
    this.logger.info('engine.status', detail, { state, pid: this.status.pid, restartCount: this.status.restartCount })
    for (const listener of this.listeners) listener(this.getStatus())
  }

  private makeStatus(state: EngineState, detail: string): EngineStatus {
    return { state, detail, pid: this.child?.pid, restartCount: this.restartTimes.length, updatedAt: new Date().toISOString() }
  }
}

async function findAvailablePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = net.createServer()
    server.unref()
    server.once('error', reject)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      const port = typeof address === 'object' && address ? address.port : 0
      server.close(() => port ? resolve(port) : reject(new Error('无法分配引擎端口')))
    })
  })
}

async function terminateProcessTree(child: ChildProcess): Promise<void> {
  if (!child.pid || child.exitCode !== null) return
  const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()))
  try { child.kill() } catch { return }
  const graceful = await Promise.race([
    exited.then(() => true),
    new Promise<false>((resolve) => setTimeout(() => resolve(false), STOP_TIMEOUT_MS))
  ])
  if (graceful || child.exitCode !== null) return
  if (process.platform === 'win32') {
    await new Promise<void>((resolve) => {
      const killer = spawn('taskkill', ['/PID', String(child.pid), '/T', '/F'], { windowsHide: true })
      killer.once('error', () => resolve())
      killer.once('exit', () => resolve())
    })
  } else {
    try { child.kill('SIGKILL') } catch { /* process already exited */ }
  }
  if (child.exitCode === null) {
    await Promise.race([exited, new Promise<void>((resolve) => setTimeout(resolve, 1_000))])
  }
}
