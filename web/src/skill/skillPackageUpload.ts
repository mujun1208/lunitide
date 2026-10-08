import type { SkillBridge } from '../bridge/client'
import type { SkillDTO } from '../generated/bridge'
import { readBoundedFile } from '../files/readBoundedFile'
import { attachmentOperation } from '../session/attachmentOperation'
import { BRIDGE_SAFE_CHUNK_BYTES } from '../session/attachments'

export const SKILL_PACKAGE_LIMIT = 8 * 1024 * 1024
type Progress = { stage: 'reading' | 'uploading' | 'committing'; percent: number }
type Attempt = { uploadId: string; pending?: Promise<SkillDTO> }
// A lost commit receipt must retry the same upload, including after returning to the page.
const commits = new WeakMap<SkillBridge, Map<string, Attempt>>()
const encode = (bytes: Uint8Array) => {
  let binary = ''
  for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192))
  return btoa(binary)
}

export async function uploadSkillPackage(bridge: SkillBridge, file: File, onProgress?: (progress: Progress) => void, signal?: AbortSignal): Promise<SkillDTO> {
  if (!bridge.uploadBegin || !bridge.uploadChunk || !bridge.uploadCommit || !bridge.uploadAbort) throw new Error('当前核心尚不支持技能包上传，请更新核心后重试。')
  if (file.size < 2 || file.size > SKILL_PACKAGE_LIMIT) throw new Error('技能包需为 2 字节至 8 MiB。')
  if (!globalThis.crypto?.subtle) throw new Error('当前运行环境不支持文件校验，尚未上传技能包。请在桌面应用中重试。')
  onProgress?.({ stage: 'reading', percent: 0 })
  const buffer = await attachmentOperation(readBoundedFile(file, SKILL_PACKAGE_LIMIT), signal, 12_000, '读取技能包超时，请重新选择文件。')
  if (buffer.byteLength !== file.size) throw new Error('技能包读取不完整，请重新选择文件。')
  const digest = await attachmentOperation(crypto.subtle.digest('SHA-256', buffer), signal, 10_000, '技能包校验超时，请重试。')
  const sha256 = Array.from(new Uint8Array(digest), value => value.toString(16).padStart(2, '0')).join('')
  const key = JSON.stringify([file.name, file.size, sha256])
  const cache = commits.get(bridge) ?? new Map<string, Attempt>()
  commits.set(bridge, cache)
  let attempt = cache.get(key)
  if (!attempt) {
    let uploadId = '', abandoned = false
    try {
      const beginning = bridge.uploadBegin({ name: file.name, size: file.size, sha256 })
      void beginning.then(result => { if (abandoned || signal?.aborted) void bridge.uploadAbort!({ uploadId: result.uploadId }).catch(() => {}) }, () => {})
      const started = await attachmentOperation(beginning, signal, 30_000, '技能包上传准备超时，请重试。')
      uploadId = started.uploadId
      if (!Number.isSafeInteger(started.chunkSize) || started.chunkSize <= 0) throw new Error('技能包分片规格不兼容，请更新核心后重试。')
      // WebView2 页面→宿主 postMessage 对超过 65536 字节的消息静默丢弃：无论核心
      // 广播多大，每块都按 40 KiB 二进制切片，保证 base64 + 信封稳定低于 64 KiB。
      const step = Math.min(started.chunkSize, BRIDGE_SAFE_CHUNK_BYTES)
      const bytes = new Uint8Array(buffer)
      for (let offset = 0; offset < bytes.length; offset += step) {
        if (signal?.aborted) throw new DOMException('技能包上传已取消', 'AbortError')
        const chunk = bytes.subarray(offset, Math.min(offset + step, bytes.length))
        const receipt = await attachmentOperation(bridge.uploadChunk({ uploadId, offset, dataBase64: encode(chunk) }), signal, 30_000, '技能包上传超时，请重试。')
        if (receipt.received !== offset + chunk.length) throw new Error('技能包上传进度不一致，尚未导入，请重试。')
        onProgress?.({ stage: 'uploading', percent: Math.round(receipt.received / bytes.length * 100) })
      }
      if (signal?.aborted) throw new DOMException('技能包上传已取消', 'AbortError')
      attempt = { uploadId }
      cache.set(key, attempt)
    } catch (cause) {
      abandoned = true
      if (uploadId) void bridge.uploadAbort({ uploadId }).catch(() => {})
      throw cause
    }
  }
  onProgress?.({ stage: 'committing', percent: 100 })
  if (!attempt.pending) {
    const current = attempt
    // Once sent, cancellation cannot prove that import did not commit. Retain its identity.
    current.pending = attachmentOperation(bridge.uploadCommit({ uploadId: current.uploadId }), undefined, 60_000, '导入回执超时，结果待确认。点击重试可核对同一技能包，不会重复导入。')
      .then(result => { cache.delete(key); return result.skill })
      .finally(() => { current.pending = undefined })
  }
  return attempt.pending!
}
