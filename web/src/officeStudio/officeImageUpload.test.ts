import { webcrypto } from 'node:crypto';
import { afterEach, expect, it, vi } from 'vitest';
import type { AttachmentBridge } from '../bridge/client';
import { uploadOfficeImage } from './officeImageUpload';

afterEach(() => vi.unstubAllGlobals());
const file = () => { const bytes = new Uint8Array(65545); bytes.set([137,80,78,71,13,10,26,10]); bytes[32768] = 255; return new File([bytes], '原图.png', { type: 'image/png' }); };
const fixture = () => {
  let source: {projectId:string;sessionId:string;sha256:string;size:number};
  return { begin: vi.fn(async (p: typeof source) => {source=p;return { uploadId: 'upload-1', chunkSize: 65536 }}), chunk: vi.fn(async (p: { offset: number; contentBase64: string }) => ({ nextOffset: p.offset + atob(p.contentBase64).length })), commit: vi.fn(async () => ({ attachmentId: 'image-1',...source })), abort: vi.fn(async () => ({ aborted: true })) } as unknown as AttachmentBridge;
};

it('uploads original image bytes with a complete SHA and binds them to the specified session', async () => {
  vi.stubGlobal('crypto', webcrypto); const bridge = fixture();
  const result = await uploadOfficeImage(bridge, 'project-a', 'session-a', file(), vi.fn(), new AbortController().signal);
  expect(result).toMatchObject({ attachmentId: 'image-1', name: '原图.png', mime: 'image/png', size: 65545, sha256: expect.stringMatching(/^[0-9a-f]{64}$/) });
  expect(bridge.begin).toHaveBeenCalledWith(expect.objectContaining({ projectId: 'project-a', sessionId: 'session-a', size: 65545, sha256: result.sha256 }));
  expect(vi.mocked(bridge.chunk).mock.calls.map(([p]) => p.offset)).toEqual([0, 32768, 65536]);
  const received = vi.mocked(bridge.chunk).mock.calls.flatMap(([p]) => Array.from(atob(p.contentBase64), c => c.charCodeAt(0)));
  expect(received).toHaveLength(65545); expect(received[32768]).toBe(255);
  expect(bridge.commit).toHaveBeenCalledWith({ uploadId: 'upload-1', projectId: 'project-a', sessionId: 'session-a' });
});

it('preserves a lost commit identity and never shares its receipt with another session', async () => {
  vi.stubGlobal('crypto', webcrypto); const bridge = fixture();
  vi.mocked(bridge.commit).mockRejectedValueOnce(new Error('receipt lost'));
  await expect(uploadOfficeImage(bridge, 'p', 'a', file(), vi.fn(), new AbortController().signal)).rejects.toThrow('receipt lost');
  await uploadOfficeImage(bridge, 'p', 'a', file(), vi.fn(), new AbortController().signal);
  expect(bridge.begin).toHaveBeenCalledTimes(1); expect(bridge.abort).not.toHaveBeenCalled();
  await uploadOfficeImage(bridge, 'p', 'b', file(), vi.fn(), new AbortController().signal);
  expect(bridge.begin).toHaveBeenCalledTimes(2);
});

it('surfaces a failed abort instead of swallowing it', async () => {
  vi.stubGlobal('crypto', webcrypto);
  const bridge = fixture();
  const progress = vi.fn();
  vi.mocked(bridge.chunk).mockRejectedValueOnce(new Error('分片失败'));
  vi.mocked(bridge.abort).mockRejectedValueOnce(new Error('abort failed'));
  await expect(uploadOfficeImage(bridge, 'p', 's', file(), progress, new AbortController().signal)).rejects.toThrow(
    '分片失败',
  );
  await vi.waitFor(() => expect(progress).toHaveBeenCalledWith('取消未完成的上传失败，请重试。'));
});

it('rejects a fake PNG filename before any upload begins', async () => {
  vi.stubGlobal('crypto', webcrypto); const bridge = fixture();
  await expect(uploadOfficeImage(bridge, 'p', 's', new File(['<svg/>'], 'fake.png', { type: 'image/png' }), vi.fn(), new AbortController().signal)).rejects.toThrow('文件内容不是 PNG 或 JPEG');
  expect(bridge.begin).not.toHaveBeenCalled();
});

it('does not pass a mismatched attachment receipt to the image replacement call', async () => {
  vi.stubGlobal('crypto', webcrypto); const bridge = fixture();
  vi.mocked(bridge.commit).mockResolvedValue({ attachmentId: 'foreign-image', projectId: 'other-project', sessionId: 'other-session', sha256: '0'.repeat(64), size: 65545 } as Awaited<ReturnType<AttachmentBridge['commit']>>);
  await expect(uploadOfficeImage(bridge, 'p', 's', file(), vi.fn(), new AbortController().signal)).rejects.toThrow('上传回执与文件或会话不一致');
});
