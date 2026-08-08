import { randomBytes } from 'node:crypto'
import { ChildProcess, spawn } from 'node:child_process'
import net from 'node:net'
import { join } from 'node:path'
import { app } from 'electron'
import type { EngineState, EngineStatus } from '../shared/engine'

type StatusListener = (status: EngineStatus) => void

const START_TIMEOUT_MS = 15_000
const HEALTH_INTERVAL_MS = 5_000
const MAX_RESTARTS = 3
const RESTART_WINDOW_MS = 60_000

export class EngineManager {
  private child?: ChildProcess
  private token = ''
  private port = 0
  private healthTimer?: NodeJS.Timeout
  private startTimer?: NodeJS.Timeout
  private intentionalStop = false
  private restartTimes: number[] = []
  private listeners = new Set<StatusListener>()
  private status: EngineStatus = this.makeStatus('stopped', '引擎尚未启动')

  subscribe(listener: StatusListener): () => void {
    this.listeners.add(listener)
    listener(this.status)
    return () => this.listeners.delete(listener)
  }

  getStatus(): EngineStatus {
    return { ...this.status }
  }

  async start(): Promise<void> {
    if (this.child) return
    this.intentionalStop = false
    this.port = await findAvailablePort()
    this.token = randomBytes(32).toString('hex')
    this.setStatus('starting', '正在启动本地引擎…')

    const engineRoot = app.isPackaged
      ? join(process.resourcesPath, 'engine')
      : join(app.getAppPath(), 'engine')
    const python = app.isPackaged
      ? join(process.resourcesPath, 'python', 'python.exe')
      : process.env.LUNITIDE_PYTHON || 'python'

    this.child = spawn(
      python,
      ['-m', 'uvicorn', 'app:app', '--host', '127.0.0.1', '--port', String(this.port), '--log-level', 'warning'],
      {
        cwd: engineRoot,
        windowsHide: true,
        env: {
          ...process.env,
          LUNITIDE_ENGINE_TOKEN: this.token,
          LUNITIDE_PARENT_PID: String(process.pid)
        }
      }
    )

    this.child.stdout?.on('data', (chunk) => console.info(`[engine] ${String(chunk).trim()}`))
    this.child.stderr?.on('data', (chunk) => console.error(`[engine] ${String(chunk).trim()}`))
    this.child.once('error', (error) => this.handleFailure(`引擎启动失败：${error.message}`))
    this.child.once('exit', (code) => {
      this.child = undefined
      if (!this.intentionalStop) this.handleFailure(`引擎异常退出（code ${code ?? 'unknown'}）`)
    })

    this.startTimer = setTimeout(() => this.handleFailure('引擎启动超过 15 秒'), START_TIMEOUT_MS)
    this.healthTimer = setInterval(() => void this.checkHealth(), HEALTH_INTERVAL_MS)
    await this.checkHealth()
  }

  async restart(): Promise<void> {
    await this.stop()
    this.intentionalStop = false
    await this.start()
  }

  async stop(): Promise<void> {
    this.intentionalStop = true
    this.clearTimers()
    const child = this.child
    this.child = undefined
    if (child && !child.killed) {
      child.kill()
      await Promise.race([
        new Promise<void>((resolve) => child.once('exit', () => resolve())),
        new Promise<void>((resolve) => setTimeout(() => {
          child.kill('SIGKILL')
          resolve()
        }, 2_000))
      ])
    }
    this.setStatus('stopped', '引擎已停止')
  }

  private async checkHealth(): Promise<void> {
    if (!this.child || !this.port) return
    try {
      const response = await fetch(`http://127.0.0.1:${this.port}/health`, {
        headers: { Authorization: `Bearer ${this.token}` },
        signal: AbortSignal.timeout(1_500)
      })
      if (!response.ok) throw new Error(`HTTP ${response.status}`)
      clearTimeout(this.startTimer)
      this.startTimer = undefined
      this.setStatus('ready', '本地引擎运行中')
    } catch {
      if (this.status.state === 'ready') this.setStatus('degraded', '引擎健康检查失败')
    }
  }

  private handleFailure(detail: string): void {
    this.clearTimers()
    if (this.intentionalStop) return
    const now = Date.now()
    this.restartTimes = this.restartTimes.filter((time) => now - time < RESTART_WINDOW_MS)
    if (this.restartTimes.length >= MAX_RESTARTS) {
      this.setStatus('degraded', `${detail}；已停止自动重启，请手动重试`)
      return
    }
    this.restartTimes.push(now)
    this.setStatus('restarting', `${detail}；正在恢复…`)
    const child = this.child
    this.child = undefined
    if (child && !child.killed) child.kill()
    setTimeout(() => void this.start(), 1_000)
  }

  private clearTimers(): void {
    clearInterval(this.healthTimer)
    clearTimeout(this.startTimer)
    this.healthTimer = undefined
    this.startTimer = undefined
  }

  private setStatus(state: EngineState, detail: string): void {
    this.status = this.makeStatus(state, detail)
    for (const listener of this.listeners) listener(this.status)
  }

  private makeStatus(state: EngineState, detail: string): EngineStatus {
    return {
      state,
      detail,
      pid: this.child?.pid,
      restartCount: this.restartTimes.length,
      updatedAt: new Date().toISOString()
    }
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
      server.close(() => resolve(port))
    })
  })
}
