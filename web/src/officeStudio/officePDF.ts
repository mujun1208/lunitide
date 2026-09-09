import type { OfficeStudioApi } from './officeStudioApi';

export async function readOfficePDF(
  api: OfficeStudioApi,
  taskId: string,
  versionId: string,
  signal: AbortSignal,
): Promise<Blob> {
  const maxBytes = 64 * 1024 * 1024;
  const parts: Uint8Array<ArrayBuffer>[] = [];
  let offset = 0;
  let total: number | undefined;
  for (;;) {
    if (signal.aborted) throw new Error('PDF 预览已取消。');
    const part = await api.readChunk({ taskId, versionId, offset, limit: 32 * 1024 });
    if (
      !Number.isSafeInteger(part.total) ||
      part.total <= 0 ||
      part.total > maxBytes ||
      (total !== undefined && total !== part.total)
    )
      throw new Error('PDF 预览大小无效或超过 64 MiB，请导出后用本机软件打开。');
    total = part.total;
    if (!/^[A-Za-z0-9+/]*={0,2}$/.test(part.contentBase64) || part.contentBase64.length > 44 * 1024)
      throw new Error('PDF 预览分片无效。');
    const binary = atob(part.contentBase64);
    if (!binary.length || part.nextOffset !== offset + binary.length || part.nextOffset > total)
      throw new Error('PDF 预览读取进度不一致。');
    if (offset === 0 && !binary.startsWith('%PDF-')) throw new Error('当前排版结果不是有效 PDF 文件。');
    parts.push(Uint8Array.from(binary, (char) => char.charCodeAt(0)));
    offset = part.nextOffset;
    if (part.eof) {
      if (offset !== total) throw new Error('PDF 预览尚未读取完整。');
      break;
    }
  }
  if (signal.aborted) throw new Error('PDF 预览已取消。');
  return new Blob(parts, { type: 'application/pdf' });
}
