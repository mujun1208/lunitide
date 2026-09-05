import {BridgeClientError} from '../bridge/client'

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

export async function readPickedFile(bridge: DesktopFilesBridge, item: DesktopPickItem): Promise<File> {
  const parts: Uint8Array[] = []
  let offset = 0
  for (;;) {
    const chunk = await bridge.readChunk({path: item.path, offset, limit: CHUNK})
    if (chunk.contentBase64) parts.push(decodeBase64(chunk.contentBase64))
    offset = chunk.nextOffset
    if (chunk.eof) break
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

export async function pickComposerFiles(bridge: DesktopFilesBridge | undefined, folder: boolean): Promise<ComposerPickResult> {
  if (!bridge) return {kind: 'fallback'}
  try {
    const picked = await bridge.pick({folder, multiple: !folder})
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
    for (const item of picked.items) files.push(await readPickedFile(bridge, item))
    return {kind: 'files', files, skipped}
  } catch (error) {
    if (unavailable(error)) return {kind: 'fallback'}
    if (error instanceof BridgeClientError) return {kind: 'error', error}
    return {kind: 'error', error: new BridgeClientError('系统没打开文件框，请再试一次。', 'DESKTOP_PICK_FAILED', true, 'renderer')}
  }
}
