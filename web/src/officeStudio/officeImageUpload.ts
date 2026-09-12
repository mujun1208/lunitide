import type { AttachmentBridge } from '../bridge/client';
import { readBoundedFile } from '../files/readBoundedFile';
import { attachmentOperation } from '../session/attachmentOperation';

export interface OfficeImageUpload {
  attachmentId: string; sha256: string; name: string; mime: string; size: number;
}
const LIMIT = 8 * 1024 * 1024;
type Attempt = { uploadId: string; pending?: Promise<OfficeImageUpload>; result?: OfficeImageUpload };
const receipts = new WeakMap<AttachmentBridge, Map<string, Attempt>>();
function base64(bytes: Uint8Array) {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(binary);
}

/** Upload the original bytes, without the chat thumbnail/downsampling pipeline. */
export async function uploadOfficeImage(bridge: AttachmentBridge, projectId: string, sessionId: string, file: File, progress: (text: string) => void, signal: AbortSignal): Promise<OfficeImageUpload> {
  if (!file.size || file.size > LIMIT) throw new Error('请选择不超过 8 MiB 的 PNG 或 JPEG 图片。');
  progress(`正在读取原图 ${file.name}…`);
  const buffer = await attachmentOperation(readBoundedFile(file, LIMIT), signal);
  const bytes = new Uint8Array(buffer);
  const png = [137,80,78,71,13,10,26,10].every((v, i) => bytes[i] === v);
  const jpeg = bytes[0] === 255 && bytes[1] === 216 && bytes[2] === 255;
  if (!png && !jpeg) throw new Error('文件内容不是 PNG 或 JPEG 图片，请重新选择。');
  if (bytes.length !== file.size) throw new Error('图片读取不完整，请重新选择。');
  const mime = png ? 'image/png' : 'image/jpeg';
  const digest = await attachmentOperation(crypto.subtle.digest('SHA-256', buffer), signal);
  const sha256 = Array.from(new Uint8Array(digest), value => value.toString(16).padStart(2, '0')).join('');
  const key = JSON.stringify([projectId, sessionId, file.name, sha256]);
  const cache = receipts.get(bridge) ?? new Map<string, Attempt>(); receipts.set(bridge, cache);
  let attempt = cache.get(key);
  if (attempt?.result) return attempt.result;
  if (!attempt) {
    let uploadId = '', abandoned = false;
    const abort = (id: string) =>
      bridge.abort({ uploadId: id, projectId, sessionId }).catch(() => {
        progress('取消未完成的上传失败，请重试。');
      });
    try {
      const beginning = bridge.begin({ projectId, sessionId, originalName: file.name, mime, size: bytes.length, sha256 });
      void beginning.then(r => { if (abandoned || signal.aborted) void abort(r.uploadId); }, () => {});
      const started = await attachmentOperation(beginning, signal);
      uploadId = started.uploadId;
      if (!Number.isSafeInteger(started.chunkSize) || started.chunkSize <= 0) throw new Error('图片上传分片规格无效。');
      const chunkSize = Math.min(started.chunkSize, 32768);
      for (let offset = 0; offset < bytes.length; offset += chunkSize) {
        if (signal.aborted) throw new DOMException('图片上传已取消', 'AbortError');
        const chunk = bytes.subarray(offset, offset + chunkSize);
        const result = await attachmentOperation(bridge.chunk({ uploadId, offset, contentBase64: base64(chunk) }), signal);
        if (result.nextOffset !== offset + chunk.length) throw new Error('图片上传进度不一致，请重试。');
        progress(`正在上传原图… ${Math.round(result.nextOffset / bytes.length * 100)}%`);
      }
      if (signal.aborted) throw new DOMException('图片上传已取消', 'AbortError');
      attempt = { uploadId }; cache.set(key, attempt);
    } catch (cause) {
      abandoned = true;
      if (uploadId) void abort(uploadId);
      throw cause;
    }
  }
  progress('正在核对原图上传结果…');
  if (!attempt.pending) {
    const current = attempt;
    current.pending = attachmentOperation(bridge.commit({ uploadId: current.uploadId, projectId, sessionId }), undefined, 60000, '图片上传回执超时，重试会核对同一份原图。')
      .then(result => {
        if (result.projectId !== projectId || result.sessionId !== sessionId || result.sha256 !== sha256 || result.size !== bytes.length) throw new Error('原图上传回执与文件或会话不一致，尚未替换图片。');
        current.result = { attachmentId: result.attachmentId, sha256, name: file.name, mime, size: bytes.length };
        for (const [cacheKey, value] of cache) { if (cache.size <= 128) break; if (cacheKey !== key && value.result) cache.delete(cacheKey); }
        return current.result;
      }).finally(() => { current.pending = undefined; });
  }
  return attempt.pending!;
}
