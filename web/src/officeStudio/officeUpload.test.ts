import { afterEach, describe, expect, it, vi } from 'vitest';
import { BridgeClientError, type AttachmentBridge } from '../bridge/client';
import type { OfficeStudioApi, OfficeTask } from './officeStudioApi';
import { uploadOfficeFiles } from './officeUpload';
import { readOfficePDF } from './officePDF';

const task = { id: 'task', sessionId: 'session' } as OfficeTask;
const api = (): OfficeStudioApi =>
  ({ importArtifact: vi.fn(async () => ({})), readChunk: vi.fn() }) as unknown as OfficeStudioApi;
const attachments = (): AttachmentBridge =>
  ({
    begin: vi.fn(async () => ({ uploadId: 'upload', chunkSize: 4 })),
    chunk: vi.fn(async (payload) => ({ nextOffset: payload.offset + atob(payload.contentBase64).length })),
    commit: vi.fn(async () => ({ attachmentId: 'attachment' })),
    abort: vi.fn(async () => ({})),
  }) as unknown as AttachmentBridge;
afterEach(() => vi.restoreAllMocks());

describe('bounded Office byte transport', () => {
  it('retries a lost import receipt with the same committed attachment instead of uploading another copy', async () => {
    const bridge = attachments(),
      office = api();
    vi.spyOn(crypto.subtle, 'digest').mockResolvedValue(new Uint8Array(32).buffer);
    vi.mocked(office.importArtifact).mockRejectedValueOnce(
      new BridgeClientError('导入回执超时', 'REQUEST_DEADLINE_EXCEEDED', true, 'test'),
    );
    const file = new File(['PK12345'], 'retry.docx');
    await expect(
      uploadOfficeFiles(bridge, office, task, 'project', [file], vi.fn(), new AbortController().signal),
    ).rejects.toThrow('导入回执超时');
    await uploadOfficeFiles(bridge, office, task, 'project', [file], vi.fn(), new AbortController().signal);
    expect(bridge.begin).toHaveBeenCalledTimes(1);
    expect(bridge.commit).toHaveBeenCalledTimes(1);
    expect(bridge.abort).not.toHaveBeenCalled();
    expect(office.importArtifact).toHaveBeenCalledTimes(2);
    expect(vi.mocked(office.importArtifact).mock.calls[0][0]).toEqual(
      vi.mocked(office.importArtifact).mock.calls[1][0],
    );
  });
  it('imports an external revision into the exact artifact and base version with conflict protection', async () => {
    const bridge = attachments(),
      office = api();
    vi.spyOn(crypto.subtle, 'digest').mockResolvedValue(new Uint8Array(32).buffer);
    await uploadOfficeFiles(
      bridge,
      office,
      task,
      'project',
      [new File(['PK12345'], '外部修订.docx')],
      vi.fn(),
      new AbortController().signal,
      { artifactId: 'existing-doc', baseVersionId: 'v1', expectedRevision: 7, kind: 'docx' },
    );
    expect(office.importArtifact).toHaveBeenCalledWith({
      taskId: 'task',
      attachmentId: 'attachment',
      name: '外部修订.docx',
      artifactId: 'existing-doc',
      baseVersionId: 'v1',
      expectedRevision: 7,
    });
  });

  it('rejects an external revision with a different file type before upload begins', async () => {
    const bridge = attachments();
    await expect(
      uploadOfficeFiles(
        bridge,
        api(),
        task,
        'project',
        [new File(['PK12345'], 'wrong.xlsx')],
        vi.fn(),
        new AbortController().signal,
        { artifactId: 'doc', baseVersionId: 'v1', expectedRevision: 7, kind: 'docx' },
      ),
    ).rejects.toThrow('与原文件格式一致');
    expect(bridge.begin).not.toHaveBeenCalled();
  });
  it('preserves Office file bytes through shared chunk upload and imports the committed attachment', async () => {
    const bridge = attachments(),
      office = api();
    vi.spyOn(crypto.subtle, 'digest').mockResolvedValue(new Uint8Array(32).buffer);
    const file = new File([new Uint8Array([80, 75, 3, 4, 1, 2, 3, 4, 5])], '汇报.docx');
    await uploadOfficeFiles(bridge, office, task, 'project', [file], vi.fn(), new AbortController().signal);
    expect(bridge.begin).toHaveBeenCalledWith(
      expect.objectContaining({
        sessionId: 'session',
        projectId: 'project',
        originalName: '汇报.docx',
        mime: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
        size: 9,
      }),
    );
    const chunks = vi.mocked(bridge.chunk).mock.calls.map(([payload]) => payload);
    expect(chunks.map((chunk) => chunk.offset)).toEqual([0, 4, 8]);
    expect(chunks.map((chunk) => atob(chunk.contentBase64)).join('')).toBe(
      String.fromCharCode(80, 75, 3, 4, 1, 2, 3, 4, 5),
    );
    expect(office.importArtifact).toHaveBeenCalledWith({
      taskId: 'task',
      attachmentId: 'attachment',
      name: '汇报.docx',
    });
    expect(bridge.abort).not.toHaveBeenCalled();
  });

  it('aborts a malformed upload acknowledgment and never commits or imports it', async () => {
    const bridge = attachments(),
      office = api();
    vi.spyOn(crypto.subtle, 'digest').mockResolvedValue(new Uint8Array(32).buffer);
    vi.mocked(bridge.chunk).mockResolvedValue({ uploadId: 'upload', nextOffset: 999 });
    await expect(
      uploadOfficeFiles(
        bridge,
        office,
        task,
        'project',
        [new File(['PK12345'], 'test.xlsx')],
        vi.fn(),
        new AbortController().signal,
      ),
    ).rejects.toThrow('进度不一致');
    expect(bridge.abort).toHaveBeenCalledWith({ uploadId: 'upload', projectId: 'project', sessionId: 'session' });
    expect(bridge.commit).not.toHaveBeenCalled();
    expect(office.importArtifact).not.toHaveBeenCalled();
  });

  it('validates every file before beginning a batch', async () => {
    const bridge = attachments(),
      office = api();
    await expect(
      uploadOfficeFiles(
        bridge,
        office,
        task,
        'project',
        [new File(['PK12345'], 'safe.docx'), new File(['bad'], 'macro.docm')],
        vi.fn(),
        new AbortController().signal,
      ),
    ).rejects.toThrow('不支持');
    expect(bridge.begin).not.toHaveBeenCalled();
  });

  it('assembles only complete bounded PDF chunks', async () => {
    const office = api(),
      pdf = '%PDF-1.7\ncontent\n%%EOF';
    vi.mocked(office.readChunk).mockImplementation(async (p) => {
      const binary = pdf.slice(p.offset, p.offset + 8);
      return {
        contentBase64: btoa(binary),
        nextOffset: p.offset + binary.length,
        total: pdf.length,
        eof: p.offset + binary.length === pdf.length,
      };
    });
    const blob = await readOfficePDF(office, 'task', 'version', new AbortController().signal);
    expect(blob.type).toBe('application/pdf');
    expect(blob.size).toBe(pdf.length);
    expect(office.readChunk).toHaveBeenCalledTimes(3);
  });

  it.each([
    [{ contentBase64: btoa('not PDF'), nextOffset: 7, total: 7, eof: true }, '不是有效 PDF'],
    [{ contentBase64: btoa('%PDF-'), nextOffset: 5, total: 100, eof: true }, '尚未读取完整'],
    [{ contentBase64: btoa('%PDF-'), nextOffset: 0, total: 5, eof: true }, '进度不一致'],
    [{ contentBase64: btoa('%PDF-'), nextOffset: 5, total: 100 * 1024 * 1024, eof: false }, '超过 64 MiB'],
  ] as const)('rejects corrupt or unsafe PDF transfer %#', async (result, expected) => {
    const office = api();
    vi.mocked(office.readChunk).mockResolvedValue(result);
    await expect(readOfficePDF(office, 'task', 'version', new AbortController().signal)).rejects.toThrow(expected);
  });
});
