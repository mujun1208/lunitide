import type { TemplateListResult, TemplateOpenResult, TemplateReadChunkResult } from '../generated/bridge'
import { bytesToBase64 } from '../assets/assetStage'
import { OFFICE_ENGINE_LIMIT } from './officeUpload'

export type OfficeTemplate = TemplateListResult['items'][number]

const OFFICE_EXTENSION: Record<string, string> = { ppt: 'pptx', word: 'docx', excel: 'xlsx' }

// 单帧桥响应上限 16 MiB，base64 膨胀 4/3：后端对超过 11 MiB 的模版返回
// chunked 标记，前端按 4 MiB 分片取回（对应 internal/app 的
// templateOpenInlineMax / templateReadChunkLimitMax）。
const TEMPLATE_READ_CHUNK = 4 * 1024 * 1024

export interface TemplateContentReader {
  open(payload: { id: string; purpose: 'office' }): Promise<TemplateOpenResult>
  readChunk(payload: { id: string; offset: number; limit?: number }): Promise<TemplateReadChunkResult>
}

export function officeTemplates(items: OfficeTemplate[]): OfficeTemplate[] {
  return items.filter(item => {
    const extension = OFFICE_EXTENSION[item.templateType]
    const name = (item.fileName || '').toLowerCase()
    return item.status === 'enabled' && !!extension && name.endsWith(`.${extension}`)
  })
}

export function fileFromTemplate(fileName: string, contentBase64: string): File {
  const binary = atob(contentBase64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
  return new File([bytes], fileName)
}

export function goalWithAssetDraft(goal: string, assetName: string): string {
  const text = goal.trim()
  if (!assetName) return text
  return `${text}\n底稿使用资产模版「${assetName}」。在这份文件上修改并输出，不要另起一份空文档。`
}

function decodeChunk(contentBase64: string): Uint8Array {
  const binary = atob(contentBase64)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
  return bytes
}

// readTemplateContent 取回一份可引用的办公资产模版。小文件走单帧内联；大文件
// 由后端标记 chunked，前端分片装配，超引擎上限提前拒绝并给出明确原因。
export async function readTemplateContent(
  bridge: TemplateContentReader,
  id: string,
  maxBytes: number = OFFICE_ENGINE_LIMIT,
): Promise<{ fileName: string; contentBase64: string }> {
  const opened = await bridge.open({ id, purpose: 'office' })
  const fileName = opened.fileName || ''
  if (!fileName) throw new Error('资产模版缺少文件名，无法引用。')
  if (opened.contentBase64 !== undefined) return { fileName, contentBase64: opened.contentBase64 }
  if (!opened.chunked) throw new Error('资产模版内容为空或不可用，请重新上传后再试。')
  const size = opened.size
  if (size !== undefined && (size <= 0 || size > maxBytes)) {
    if (size <= 0) throw new Error(`${fileName} 是空文件，无法作为底稿导入。`)
    const mb = Math.round(size / 1024 / 1024)
    throw new Error(`${fileName} 有 ${mb} MB，超过办公引擎导入上限 128 MB，无法作为底稿导入。请先压缩文件（尤其是嵌入的图片和音视频）再试。`)
  }
  const parts: Uint8Array[] = []
  let offset = 0
  let total: number | undefined
  for (;;) {
    const part = await bridge.readChunk({ id, offset, limit: TEMPLATE_READ_CHUNK })
    if (part.fileName !== fileName) throw new Error('资产模版读取期间内容发生变化，请重试。')
    if (!Number.isSafeInteger(part.total) || part.total < 0 || part.total > maxBytes ||
      (total !== undefined && total !== part.total)) {
      throw new Error('这份资产模版超过办公引擎导入上限 128 MB 或大小不一致，无法作为底稿导入。')
    }
    total = part.total
    if (!/^[A-Za-z0-9+/]*={0,2}$/.test(part.contentBase64)) throw new Error('资产模版分片无效。')
    const data = decodeChunk(part.contentBase64)
    if (part.nextOffset !== offset + data.length || part.nextOffset > part.total ||
      (data.length === 0 && !part.eof)) {
      throw new Error('资产模版读取进度不一致，请重试。')
    }
    parts.push(data)
    offset = part.nextOffset
    if (part.eof) break
  }
  const assembled = new Uint8Array(parts.reduce((sum, part) => sum + part.length, 0))
  let cursor = 0
  for (const part of parts) {
    assembled.set(part, cursor)
    cursor += part.length
  }
  if (assembled.length !== total) throw new Error('资产模版读取不完整，请重试。')
  return { fileName, contentBase64: bytesToBase64(assembled) }
}
