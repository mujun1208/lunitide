import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { OfficeImageDialog, type OfficeImageTarget } from './OfficeImageDialog';
import type { OfficeStudioApi, OfficeTaskDetail } from './officeStudioApi';
import type { OfficeImageUpload } from './officeImageUpload';

beforeEach(() => vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:fixture'), revokeObjectURL: vi.fn() }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
const target: OfficeImageTarget = {
  task: { id: 'task-a', sessionId: 'session-a', title: 'PPT', goal: '', status: 'succeeded', revision: 5, createdAt: '', updatedAt: '' }, artifactId: 'ppt-a', baseVersionId: 'v3', expectedRevision: 8, versionNo: 3,
  node: { id: 'slide2-image1', label: '第二页 · 图片 1', text: '原说明', digest: 'node-digest-v3', valueType: 'image', image: { mediaPart: 'ppt/media/image1.png', mediaSha256: 'a'.repeat(64), relationshipId: 'rId2', pixelWidth: 800, pixelHeight: 600, x: 1, y: 2, width: 3, height: 4 } },
};
const receipt: OfficeImageUpload = { attachmentId: 'attachment-a', sha256: 'b'.repeat(64), name: '原图.png', mime: 'image/png', size: 10 };
const choose = async () => {
  fireEvent.change(screen.getByLabelText('选择替换图片'), { target: { files: [new File([new Uint8Array([137,80,78,71,13,10,26,10,1,2])], '原图.png')] } });
  await screen.findByAltText('待替换原图预览');
  await waitFor(() => expect(screen.getByRole('button', { name: '替换并生成新版本' })).toBeEnabled());
};

it('replaces exactly the selected image version with the original attachment hash and preserves alt by default', async () => {
  const next = { task: target.task, artifacts: [], sources: [], steps: [] } as OfficeTaskDetail;
  const replaceImage = vi.fn(async () => next), upload = vi.fn(async () => receipt), changed = vi.fn(), close = vi.fn();
  render(<OfficeImageDialog api={{ replaceImage } as unknown as OfficeStudioApi} target={target} onUpload={upload} onChanged={changed} onClose={close}/>);
  expect(screen.getByText('ppt/media/image1.png')).toBeInTheDocument();
  await choose(); fireEvent.change(screen.getByLabelText('图片填充方式'), { target: { value: 'cover' } });
  fireEvent.click(screen.getByRole('button', { name: '替换并生成新版本' }));
  await waitFor(() => expect(replaceImage).toHaveBeenCalledOnce());
  expect(replaceImage).toHaveBeenCalledWith({ taskId: 'task-a', artifactId: 'ppt-a', baseVersionId: 'v3', expectedRevision: 8, nodeId: 'slide2-image1', nodeDigest: 'node-digest-v3', attachmentId: 'attachment-a', sha256: 'b'.repeat(64), fit: 'cover' });
  expect(upload).toHaveBeenCalledWith(target.task, expect.any(File), expect.any(Function), expect.any(AbortSignal));
  expect(changed).toHaveBeenCalledWith(next); expect(close).toHaveBeenCalledOnce();
});

it('ignores a late upload after changing tasks instead of applying it to the new picture', async () => {
  let done!: (value: OfficeImageUpload) => void;
  const replaceImage = vi.fn(), upload = vi.fn(() => new Promise<OfficeImageUpload>(resolve => { done = resolve; }));
  const props = { api: { replaceImage } as unknown as OfficeStudioApi, onUpload: upload, onChanged: vi.fn(), onClose: vi.fn() };
  const view = render(<OfficeImageDialog {...props} target={target}/>);
  await choose(); fireEvent.click(screen.getByRole('button', { name: '替换并生成新版本' }));
  await waitFor(() => expect(upload).toHaveBeenCalledOnce());
  view.rerender(<OfficeImageDialog {...props} target={{ ...target, task: { ...target.task, id: 'task-b' }, node: { ...target.node, id: 'other-image' } }}/>);
  await act(async () => done(receipt));
  expect(replaceImage).not.toHaveBeenCalled(); expect(props.onChanged).not.toHaveBeenCalled();
});

it('keeps the original version on CAS conflict and shows the backend error', async () => {
  const replaceImage = vi.fn().mockRejectedValue(new Error('版本已更新，请重新选择图片')), changed = vi.fn();
  render(<OfficeImageDialog api={{ replaceImage } as unknown as OfficeStudioApi} target={target} onUpload={vi.fn(async () => receipt)} onChanged={changed} onClose={vi.fn()}/>);
  await choose(); fireEvent.click(screen.getByRole('button', { name: '替换并生成新版本' }));
  await screen.findByText('版本已更新，请重新选择图片'); expect(changed).not.toHaveBeenCalled();
  expect(screen.getByRole('dialog')).toBeInTheDocument();
});
