import type { AttachmentBridge } from '../bridge/client';
import { readBoundedFile } from '../files/readBoundedFile';
import { attachmentOperation } from '../session/attachmentOperation';
import type { OfficeKind, OfficeStudioApi, OfficeTask, OfficeTaskDetail } from './officeStudioApi';

export interface OfficeImportRevision {
  artifactId: string;
  baseVersionId: string;
  expectedRevision: number;
  kind: OfficeKind;
}

const LIMIT = 10 * 1024 * 1024;
interface CommittedImport {
  attachmentId: string;
  pending?: Promise<OfficeTaskDetail>;
}
const committedImports = new WeakMap<OfficeStudioApi, Map<string, CommittedImport>>();
const MIME: Record<string, string> = {
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  pdf: 'application/pdf',
};
const base64 = (bytes: Uint8Array): string => {
  let binary = '';
  for (let offset = 0; offset < bytes.length; offset += 8192)
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  return btoa(binary);
};

export function validateOfficeFiles(files: File[], revision?: OfficeImportRevision): void {
  if (revision && (files.length !== 1 || files[0].name.split('.').pop()?.toLowerCase() !== revision.kind))
    throw new Error(`修改版必须是一个 ${revision.kind.toUpperCase()} 文件，与原文件格式一致。`);
  if (files.length > 20 || files.reduce((sum, file) => sum + file.size, 0) > 20 * 1024 * 1024)
    throw new Error('每次最多导入 20 个文件，合计不能超过 20 MiB。');
  for (const file of files) {
    const extension = file.name.split('.').pop()?.toLowerCase() || '';
    if (!MIME[extension]) throw new Error(`不支持 ${file.name}。请选择 PPTX、DOCX、XLSX 或 PDF 文件。`);
    if (file.size > LIMIT || !file.size) throw new Error(`${file.name} 必须是非空文件，且不能超过 10 MiB。`);
  }
}

// Office files retain their exact bytes. They use the shared upload protocol,
// without going through the chat image compression or text-only preparation.
export async function uploadOfficeFiles(
  attachments: AttachmentBridge,
  api: OfficeStudioApi,
  task: OfficeTask,
  projectId: string,
  files: File[],
  progress: (text: string) => void,
  signal: AbortSignal,
  revision?: OfficeImportRevision,
): Promise<void> {
  if (!files.length) return;
  validateOfficeFiles(files, revision);
  for (const file of files) {
    let uploadId = '';
    let fingerprint = '';
    let committed: CommittedImport | undefined;
    const cache = committedImports.get(api) ?? new Map<string, CommittedImport>();
    committedImports.set(api, cache);
    const abort = (id: string) =>
      attachmentOperation(
        attachments.abort({ uploadId: id, projectId, sessionId: task.sessionId }),
        undefined,
        2000,
      ).catch(() => {});
    try {
      if (signal.aborted) throw new Error('导入已取消。');
      progress(`正在读取 ${file.name}…`);
      const bytes = new Uint8Array(await attachmentOperation(readBoundedFile(file, LIMIT), signal));
      const digest = await attachmentOperation(crypto.subtle.digest('SHA-256', bytes), signal);
      const sha256 = Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
      fingerprint = JSON.stringify({
        projectId,
        sessionId: task.sessionId,
        taskId: task.id,
        name: file.name,
        sha256,
        revision,
      });
      committed = cache.get(fingerprint);
      if (!committed) {
        let abandoned = false;
        const beginning = attachments.begin({
          projectId,
          sessionId: task.sessionId,
          originalName: file.name,
          mime: MIME[file.name.split('.').pop()!.toLowerCase()],
          size: bytes.length,
          sha256,
        });
        void beginning.then(
          (result) => {
            if (abandoned || signal.aborted) void abort(result.uploadId);
          },
          () => {},
        );
        let started;
        try {
          started = await attachmentOperation(beginning, signal);
        } catch (cause) {
          abandoned = true;
          throw cause;
        }
        uploadId = started.uploadId;
        if (!Number.isSafeInteger(started.chunkSize) || started.chunkSize <= 0) throw new Error('上传分块大小无效。');
        const chunkSize = Math.min(started.chunkSize, 32 * 1024);
        let offset = 0;
        while (offset < bytes.length) {
          const chunk = bytes.subarray(offset, offset + chunkSize);
          const result = await attachmentOperation(
            attachments.chunk({ uploadId, offset, contentBase64: base64(chunk) }),
            signal,
          );
          if (result.nextOffset !== offset + chunk.length) throw new Error('文件上传进度不一致，请重试。');
          offset = result.nextOffset;
          progress(`正在导入 ${file.name}… ${Math.round((offset / bytes.length) * 100)}%`);
        }
        const result = await attachmentOperation(
          attachments.commit({ uploadId, projectId, sessionId: task.sessionId }),
          signal,
        );
        uploadId = '';
        committed = { attachmentId: result.attachmentId };
        cache.set(fingerprint, committed);
        while (cache.size > 128) cache.delete(cache.keys().next().value!);
      }
      // A lost import receipt retries the committed attachment, not a second
      // upload with a new attachment ID and a new mutation identity.
      const current = committed;
      if (!current.pending) {
        const pending = api.importArtifact({
          taskId: task.id,
          attachmentId: current.attachmentId,
          name: file.name,
          ...(revision
            ? {
                artifactId: revision.artifactId,
                baseVersionId: revision.baseVersionId,
                expectedRevision: revision.expectedRevision,
              }
            : {}),
        });
        current.pending = pending;
        void pending.catch(() => {
          if (current.pending === pending) current.pending = undefined;
        });
      }
      await attachmentOperation(current.pending, signal, 35_000);
      if (cache.get(fingerprint) === current) cache.delete(fingerprint);
    } catch (cause) {
      if (uploadId) void abort(uploadId);
      if (
        cause &&
        typeof cause === 'object' &&
        'retryable' in cause &&
        cause.retryable === false &&
        cache.get(fingerprint) === committed
      )
        cache.delete(fingerprint);
      throw cause;
    }
  }
}
