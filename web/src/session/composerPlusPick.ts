import {BridgeClientError} from '../bridge/client'
import {attachmentOperation,attachmentCancelled} from './attachmentOperation'
import {ATTACHMENT_FILE_MAX,ATTACHMENT_BATCH_MAX,ATTACHMENT_BATCH_BYTES} from './attachments'

export type DesktopPickItem = {path: string; fileName: string; mime: string; size: number}

export type DesktopFilesBridge = {
  pick(payload?: {folder?: boolean; multiple?: boolean}): Promise<{canceled: boolean; items: DesktopPickItem[]; skipped?: string[]}>
  readChunk(payload: {path: string; offset: number; limit: number}): Promise<{contentBase64: string; nextOffset: number; eof: boolean}>
}

export type ComposerPickResult =
  | {kind: 'fallback'}
  | {kind: 'canceled'}
  | {kind: 'files'; files: File[]; skipped: string[]}
  | {kind: 'error'; error: BridgeClientError}

const CHUNK = 32768

function unavailable(error: unknown): boolean {
  return error instanceof BridgeClientError && (error.code === 'DESKTOP_PICK_UNAVAILABLE' || error.code === 'BRIDGE_UNAVAILABLE' || error.code === 'METHOD_NOT_FOUND')
}

function decodeBase64(raw: string): Uint8Array {
  const bin = atob(raw)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function skippedNames(picked: {skipped?: string[]}): string[] {
  return (picked.skipped ?? []).map(name => name.trim()).filter(Boolean).slice(0, 20)
}

export async function readPickedFile(bridge: DesktopFilesBridge, item: DesktopPickItem, signal?:AbortSignal): Promise<File> {
  if (!Number.isSafeInteger(item.size) || item.size < 0 || item.size > ATTACHMENT_FILE_MAX) throw new Error(`${item.fileName}（超过 10 MiB 或文件大小无效）`)
  const parts: Uint8Array[] = []
  let offset = 0
  for (;;) {
    if(signal?.aborted)throw attachmentCancelled()
    const chunk = await attachmentOperation(bridge.readChunk({path: item.path, offset, limit: CHUNK}),signal,10_000,'文件读取超时，请重试')
    if(chunk.contentBase64.length>Math.ceil(CHUNK/3)*4)throw new Error('文件读取分块超过限制')
    const data=decodeBase64(chunk.contentBase64),next=offset+data.length
    if(!Number.isSafeInteger(chunk.nextOffset)||chunk.nextOffset!==next||data.length>CHUNK||next>item.size||(!chunk.eof&&data.length===0)||(chunk.eof&&next!==item.size))throw new Error('文件读取进度无效，文件可能已改变，请重新选择')
    parts.push(data)
    offset=next
    if(chunk.eof)break
  }
  const total = parts.reduce((n, part) => n + part.length, 0)
  const bytes = new Uint8Array(total)
  let written = 0
  for (const part of parts) {
    bytes.set(part, written)
    written += part.length
  }
  return new File([bytes], item.fileName, {type: item.mime})
}

export async function pickComposerFiles(bridge: DesktopFilesBridge | undefined, folder: boolean, signal?:AbortSignal): Promise<ComposerPickResult> {
  if (!bridge) return {kind: 'fallback'}
  try {
    if(signal?.aborted)return {kind:'canceled'}
    const picked = await attachmentOperation(bridge.pick({folder, multiple: !folder}),signal,120_000,'选择文件超时，请重新打开')
    if (picked.canceled) return {kind: 'canceled'}
    const skipped = skippedNames(picked)
    if (!picked.items?.length) {
      if (folder) {
        const extra = skipped.length ? ` ${skipped.join('、')}` : ''
        return {kind: 'error', error: new BridgeClientError(`这个文件夹里没有可导入的文件。${extra}`.trim(), 'DESKTOP_FOLDER_EMPTY', false, 'renderer')}
      }
      return {kind: 'error', error: new BridgeClientError('系统没打开文件框，请再试一次。', 'DESKTOP_PICK_FAILED', true, 'renderer')}
    }
    const files: File[] = []
    let total=0
    for (const item of picked.items.slice(0,ATTACHMENT_BATCH_MAX)){
      if(signal?.aborted)return {kind:'canceled'}
      if(total+item.size>ATTACHMENT_BATCH_BYTES){skipped.push(`${item.fileName}（本批超过 20 MiB）`);continue}
      try{files.push(await readPickedFile(bridge,item,signal));total+=item.size}catch(error){if(signal?.aborted)return {kind:'canceled'};skipped.push(`${item.fileName}：${error instanceof Error?error.message:'读取失败'}`)}
    }
    if(picked.items.length>ATTACHMENT_BATCH_MAX)skipped.push(`超过 20 个的 ${picked.items.length-ATTACHMENT_BATCH_MAX} 个文件`)
    if(!files.length)return {kind:'error',error:new BridgeClientError(skipped.join('；')||'未读取到文件，请重新选择','DESKTOP_FILE_READ_FAILED',true,'renderer')}
    return {kind: 'files', files, skipped}
  } catch (error) {
    if(signal?.aborted)return {kind:'canceled'}
    if (unavailable(error)) return {kind: 'fallback'}
    if (error instanceof BridgeClientError) return {kind: 'error', error}
    return {kind: 'error', error: new BridgeClientError('系统没打开文件框，请再试一次。', 'DESKTOP_PICK_FAILED', true, 'renderer')}
  }
}
