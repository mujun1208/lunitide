import type { OfficeArtifact, OfficeQuality, OfficeRunStatus } from './officeStudioApi';

export const isOfficeReference = (file: OfficeArtifact): boolean =>
  file.role === 'reference' || (!file.role && file.versions.length > 0 && file.versions.every(v => v.mode === 'imported' && !v.path));
export const defaultOfficeArtifact = (files: OfficeArtifact[]): OfficeArtifact | undefined =>
  files.find(file => !isOfficeReference(file));

const runLabels: Record<OfficeRunStatus, string> = {
  draft: '尚未开始',
  queued: '排队中',
  planning: '整理需求中',
  running: '执行中',
  validating: '检查中',
  succeeded: '文件操作完成',
  waiting_input: '等待补充',
  waiting_approval: '等待确认',
  cancelling: '正在停止',
  cancelled: '已停止',
  failed: '执行失败',
  interrupted: '执行中断',
};
const qualityLabels: Record<OfficeQuality, string> = {
  unverified: '尚未检查',
  checking: '检查中',
  partial: '部分检查完成',
  passed: '所需检查已通过',
  blocked: '存在阻断问题',
  stale: '来源已更新',
};
export const officeRunLabel = (status: OfficeRunStatus): string => runLabels[status] ?? status;
export const officeQualityLabel = (quality: OfficeQuality): string => qualityLabels[quality] ?? quality;
export const officeDate = (value: string): string => {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
};
export const officeBytes = (value: number): string =>
  value < 1024
    ? `${value} B`
    : value < 1024 * 1024
      ? `${(value / 1024).toFixed(1)} KiB`
      : `${(value / 1024 / 1024).toFixed(1)} MiB`;
export const activeOfficeRun = (status: OfficeRunStatus): boolean =>
  ['queued', 'planning', 'running', 'validating', 'cancelling'].includes(status);
