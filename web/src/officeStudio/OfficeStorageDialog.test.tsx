import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { OfficeStorageDialog } from './OfficeStorageDialog';
import type { OfficeStudioApi } from './officeStudioApi';

afterEach(cleanup);
const usage = { limitBytes: 2 ** 31, usedBytes: 2048, reservedBytes: 100, totalBytes: 2148, referencedBytes: 1024, unmanagedBytes: 0, blobCount: 3, activeLeaseCount: 1, overLimit: false };
const report = { dryRun: true, candidates: 1, removed: 0, freedBytes: 0, releasedReservationBytes: 0, hasMore: false, errors: [] };
const fixture = () => ({ storageUsage: vi.fn(async () => usage), sweepStorage: vi.fn(async () => report) } as unknown as OfficeStudioApi);

it('reads real usage and only performs cleanup after a dry-run report and explicit click', async () => {
  const api = fixture(); render(<OfficeStorageDialog api={api} open onClose={vi.fn()}/>);
  await screen.findByText('已占用'); expect(api.sweepStorage).not.toHaveBeenCalled();
  expect(screen.queryByRole('button', { name: '清理无引用文件' })).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: '检查可清理文件' }));
  await screen.findByText('本批发现 1 个可清理文件，尚未删除。');
  expect(api.sweepStorage).toHaveBeenCalledWith({ dryRun: true, limit: 100 });
  vi.mocked(api.sweepStorage).mockResolvedValue({ ...report, dryRun: false, removed: 1, freedBytes: 1024 });
  fireEvent.click(screen.getByRole('button', { name: '清理无引用文件' }));
  await waitFor(() => expect(api.sweepStorage).toHaveBeenLastCalledWith({ dryRun: false, limit: 100 }));
  await screen.findByText(/本次清理 1 个文件/);
});

it('retains a successful cleanup receipt when refreshing the usage fails', async () => {
  const api = fixture(); render(<OfficeStorageDialog api={api} open onClose={vi.fn()}/>);
  await screen.findByText('已占用'); fireEvent.click(screen.getByRole('button', { name: '检查可清理文件' }));
  await screen.findByRole('button', { name: '清理无引用文件' });
  vi.mocked(api.sweepStorage).mockResolvedValue({ ...report, dryRun: false, removed: 1, freedBytes: 512 });
  vi.mocked(api.storageUsage).mockRejectedValue(new Error('读取失败'));
  fireEvent.click(screen.getByRole('button', { name: '清理无引用文件' }));
  await screen.findByText(/清理已完成：释放.*无需再次提交清理/);
  expect(api.sweepStorage).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole('button', { name: '清理无引用文件' })).not.toBeInTheDocument();
});

it('ignores an old usage response after the dialog closes and reopens', async () => {
  const api = fixture(); let complete!: (value: typeof usage) => void;
  vi.mocked(api.storageUsage).mockImplementationOnce(() => new Promise(resolve => { complete = resolve; }));
  const view = render(<OfficeStorageDialog api={api} open onClose={vi.fn()}/>);
  view.rerender(<OfficeStorageDialog api={api} open={false} onClose={vi.fn()}/>);
  view.rerender(<OfficeStorageDialog api={api} open onClose={vi.fn()}/>);
  await screen.findByText('已占用'); await act(async () => complete({ ...usage, overLimit: true }));
  expect(screen.queryByText(/当前占用已达到容量上限/)).not.toBeInTheDocument();
});

it('requires a new read and check after a missing cleanup receipt instead of replaying an old batch', async () => {
  const api = fixture(); render(<OfficeStorageDialog api={api} open onClose={vi.fn()}/>);
  await screen.findByText('已占用'); fireEvent.click(screen.getByRole('button', { name: '检查可清理文件' }));
  await screen.findByRole('button', { name: '清理无引用文件' }); vi.mocked(api.sweepStorage).mockRejectedValue(new Error('回执超时'));
  fireEvent.click(screen.getByRole('button', { name: '清理无引用文件' }));
  await screen.findByText(/清理结果暂未确认/);
  expect(screen.queryByRole('button', { name: '清理无引用文件' })).not.toBeInTheDocument();
  expect(api.sweepStorage).toHaveBeenCalledTimes(2);
});
