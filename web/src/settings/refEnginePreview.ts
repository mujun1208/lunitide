import { BridgeClientError } from '../bridge/client'

export const REF_LAUNCH_POLL_MAX = 40

export type RefMetaCaption = {
  server_online?: boolean
  pack_exists?: boolean
  pack_dir?: string
  missing_files?: string[]
  host_state?: string
  host_script?: string
  host_last_err?: string
}

/** Local GPT-SoVITS preview is only live once the api_v2 /docs answers. */
export function refPreviewReady(input: {
  engineState: 'probing' | 'available' | 'unavailable'
  hostState?: string
  serverOnline?: boolean
}): boolean {
  if (input.hostState === 'launching') return false
  if (input.serverOnline === true || input.hostState === 'online') return true
  if (input.hostState === 'offline') return true
  return input.engineState === 'available'
}

export function refPreviewButtonLabel(input: { busy: boolean; launching: boolean }): string {
  if (input.busy) return '合成中…'
  if (input.launching) return '启动中…'
  return '试听'
}

/** Settings 试听 must not label a cold load as 该段语音合成失败. */
export function refPreviewStatus(error: unknown): string {
  if (error instanceof BridgeClientError && error.code === 'M95-001' && /启动中/.test(error.message)) {
    return '语音引擎启动中，请稍候再试听。首次加载模型约 30-90 秒。'
  }
  if (error instanceof Error) return `试听失败：${error.message}`
  return '试听失败，请检查引擎配置'
}

export function refLaunchPollStatus(tries: number, maxTries = REF_LAUNCH_POLL_MAX, lastErr?: string): string | undefined {
  if (tries < maxTries) return undefined
  const err = lastErr?.trim()
  return err
    ? `语音引擎超过 2 分钟仍未就绪（${err}）。请查看本机 GPT-SoVITS 窗口或改用云端晓晓。`
    : '语音引擎超过 2 分钟仍未就绪。请查看本机 GPT-SoVITS 窗口或改用云端晓晓。'
}

export function refEngineCaption(meta: RefMetaCaption | undefined, voiceCount = 0): string {
  if (!meta) return '正在检测 GPT-SoVITS 服务…'
  if (meta.server_online) {
    if (!meta.pack_exists) return `引擎在线，但音色包目录缺失（${meta.pack_dir ?? ''}），请检查音色文件`
    const missing = meta.missing_files ?? []
    if (missing.length > 0) {
      const shown = missing.slice(0, 3).join('、')
      const more = missing.length > 3 ? '…' : ''
      return `音色 ${voiceCount} 个，但 ${missing.length} 个参考音频缺失（${shown}${more}），请检查音色包目录`
    }
    return `音色 ${voiceCount} 个（GPT-SoVITS 本地克隆，引擎在线，按风格分组）`
  }
  if (meta.host_state === 'launching') {
    const err = meta.host_last_err?.trim()
    if (err) return `引擎未就绪：${err}。可先用晓晓试听。`
    return '语音引擎启动中…（首次加载模型约 30-90 秒，就绪后 50 种音色自动可用）'
  }
  if (meta.host_script) {
    const err = meta.host_last_err?.trim()
    if (meta.host_state === 'offline' && err) {
      return `语音引擎未就绪：${err}。已再次尝试启动，请稍候或检查 E:\\GPT-SoVITS。`
    }
    return '检测到本机 GPT-SoVITS，服务未运行——已自动在后台启动语音引擎（首次加载模型约 30-90 秒）'
  }
  return '未检测到 GPT-SoVITS 启动脚本（E:\\GPT-SoVITS\\start-api-cpu.bat）——请手动启动 api_v2 服务（默认端口 9880；WebUI 的 9874 端口不提供合成 API）'
}
