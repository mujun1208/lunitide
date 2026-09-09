import type { OfficeArtifact, OfficeTaskDetail, OfficeVersion } from './officeStudioApi'

export interface SnapshotPage {
  snapshotOffset?: number
  nextSnapshotOffset?: number
  totalSnapshotItems?: number
  snapshotDigest?: string
}
export type SnapshotCursor = { snapshotOffset: number; snapshotDigest: string }

function nextCursor(page: SnapshotPage): SnapshotCursor | undefined {
  if (page.nextSnapshotOffset === undefined || page.nextSnapshotOffset < 0) return
  if (!page.snapshotDigest || page.nextSnapshotOffset <= (page.snapshotOffset ?? 0)) {
    throw new Error('办公记录分页未能继续，请刷新后重试。')
  }
  return { snapshotOffset: page.nextSnapshotOffset, snapshotDigest: page.snapshotDigest }
}
function sameSnapshot(first: SnapshotPage, next: SnapshotPage) {
  if (first.snapshotDigest && next.snapshotDigest !== first.snapshotDigest) {
    throw Object.assign(new Error('办公记录已更新，正在重新读取。'), { code: 'OFFICE_SNAPSHOT_CHANGED' })
  }
}
function changed(error: unknown) {
  return !!error && typeof error === 'object' && 'code' in error && error.code === 'OFFICE_SNAPSHOT_CHANGED'
}
function byId<T extends { id: string }>(old: T[], incoming: T[]): T[] {
  const values = new Map(old.map(item => [item.id, item]))
  for (const item of incoming) values.set(item.id, item)
  return [...values.values()]
}
function mergeDetail(old: OfficeTaskDetail, page: OfficeTaskDetail): OfficeTaskDetail {
  if (old.task.id !== page.task.id) throw new Error('办公记录不属于当前任务，请刷新后重试。')
  const files = new Map<string, OfficeArtifact>(old.artifacts.map(item => [item.id, item]))
  for (const file of page.artifacts) {
    const previous = files.get(file.id)
    const versions = new Map<string, OfficeVersion>((previous?.versions ?? []).map(item => [item.id, item]))
    for (const version of file.versions) {
      versions.set(version.id, { ...version, validations: byId(versions.get(version.id)?.validations ?? [], version.validations ?? []) })
    }
    files.set(file.id, { ...file, versions: [...versions.values()] })
  }
  return { ...old, ...page, artifacts: [...files.values()], steps: byId(old.steps, page.steps), sources: byId(old.sources, page.sources) }
}

// Fetch only additional read pages after a mutation has committed. A lost read
// must not be reported as a failed write or cause the original action to replay.
export async function collectOfficeDetail(
  initial: Promise<OfficeTaskDetail>,
  read: (taskId: string, cursor?: SnapshotCursor) => Promise<OfficeTaskDetail>,
): Promise<OfficeTaskDetail> {
  let result = await initial
  if (result.snapshotIncomplete) {
    const committed = result
    try {
      const fresh = await read(committed.task.id)
      if (fresh.task.id !== committed.task.id || fresh.snapshotIncomplete) throw new Error('任务详情尚未恢复。')
      result = fresh
    } catch {
      return { ...committed, loadNotice: committed.loadNotice || '操作已保存，最新记录暂时读不到。请重新读取完整记录，无需再次提交。' }
    }
  }
  let refreshed = false
  for (let count = 0; count < 2000; count++) {
    try {
      const cursor = nextCursor(result)
      if (!cursor) return result
      const page = await read(result.task.id, cursor)
      sameSnapshot(result, page)
      result = mergeDetail(result, page)
    } catch (error) {
      if (changed(error) && !refreshed) {
        refreshed = true
        try { result = await read(result.task.id); continue } catch { /* Keep the committed snapshot below. */ }
      }
      return { ...result, loadNotice: '操作结果已保留，部分历史记录暂未读完。请刷新工作台继续查看。' }
    }
  }
  return { ...result, loadNotice: '办公历史记录较多，本次尚未读取完整。请缩小任务范围后重试。' }
}

export async function collectOfficeItems<T extends { id: string }>(
  read: (cursor?: SnapshotCursor) => Promise<SnapshotPage & { items: T[] }>,
): Promise<{ items: T[] }> {
  let page = await read()
  let items = page.items
  let refreshed = false
  for (let count = 0; count < 2000; count++) {
    try {
      const cursor = nextCursor(page)
      if (!cursor) return { items }
      const next = await read(cursor)
      sameSnapshot(page, next)
      page = next
      items = byId(items, next.items)
    } catch (error) {
      if (!changed(error) || refreshed) throw error
      refreshed = true
      page = await read()
      items = page.items
    }
  }
  throw new Error('办公列表较长，本次未读取完整，请缩小范围后重试。')
}
