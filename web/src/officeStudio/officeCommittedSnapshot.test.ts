import { expect, it, vi } from 'vitest';
import { collectOfficeDetail } from './officeSnapshot';
import type { OfficeTaskDetail } from './officeStudioApi';

const saved: OfficeTaskDetail = {
  task: {
    id: 'task',
    sessionId: 'session',
    title: '已保存的报告',
    goal: '',
    revision: 2,
    status: 'succeeded',
    createdAt: '',
    updatedAt: '',
  },
  artifacts: [],
  steps: [],
  sources: [],
  committed: true,
  snapshotIncomplete: true,
  taskSnapshotStale: true,
  loadNotice: '操作已保存，最新记录暂时读不到。请重新读取完整记录。',
};
it('reads a committed first-page fallback exactly once without repeating its write', async () => {
  const write = vi.fn().mockResolvedValue(saved);
  const read = vi.fn().mockRejectedValue(new Error('read failed'));
  const result = await collectOfficeDetail(write(), read);
  expect(write).toHaveBeenCalledOnce();
  expect(read).toHaveBeenCalledExactlyOnceWith('task');
  expect(result.committed).toBe(true);
  expect(result.snapshotIncomplete).toBe(true);
  expect(result.loadNotice).toContain('操作已保存');
});
it('continues normal pagination after recovering the first committed snapshot using reads only', async () => {
  const fresh = {
    ...saved,
    committed: undefined,
    snapshotIncomplete: undefined,
    taskSnapshotStale: undefined,
    loadNotice: undefined,
    snapshotOffset: 0,
    nextSnapshotOffset: 1,
    snapshotDigest: 'current',
  };
  const read = vi
    .fn()
    .mockResolvedValueOnce(fresh)
    .mockResolvedValueOnce({
      ...fresh,
      snapshotOffset: 1,
      nextSnapshotOffset: -1,
      sources: [{ id: 'source', name: '真实来源' }],
    });
  const result = await collectOfficeDetail(Promise.resolve(saved), read);
  expect(read).toHaveBeenCalledTimes(2);
  expect(read).toHaveBeenLastCalledWith('task', { snapshotOffset: 1, snapshotDigest: 'current' });
  expect(result.snapshotIncomplete).toBeUndefined();
  expect(result.sources[0].name).toBe('真实来源');
});
it('keeps the original committed receipt if a recovery read returns another task', async () => {
  const read = vi
    .fn()
    .mockResolvedValue({ ...saved, snapshotIncomplete: false, task: { ...saved.task, id: 'other-task' } });
  const result = await collectOfficeDetail(Promise.resolve(saved), read);
  expect(result.task.id).toBe('task');
  expect(result.snapshotIncomplete).toBe(true);
  expect(read).toHaveBeenCalledOnce();
});
