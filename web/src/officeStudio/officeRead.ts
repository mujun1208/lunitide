export const OFFICE_READ_TIMEOUT_MS = 20_000;

// Only bound reads: timing out a save must never invite resubmitting that write.
export function officeRead<T>(work: Promise<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => reject(new Error('读取办公记录超时，请重试或返回任务列表。')), OFFICE_READ_TIMEOUT_MS);
    work.then(resolve, reject).finally(() => window.clearTimeout(timer));
  });
}
