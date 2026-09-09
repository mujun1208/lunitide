import { useEffect, useRef, useState } from 'react';
import { Dialog } from '../ui/Dialog';
import type { OfficeStudioApi } from './officeStudioApi';
import { officeBytes } from './officePresentation';

export function OfficeStorageDialog({ api, open, onClose }: { api: OfficeStudioApi; open: boolean; onClose: () => void }) {
  const [usage, setUsage] = useState<Awaited<ReturnType<OfficeStudioApi['storageUsage']>>>();
  const [report, setReport] = useState<Awaited<ReturnType<OfficeStudioApi['sweepStorage']>>>();
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [notice, setNotice] = useState('');
  const generation = useRef(0);
  const refresh = async () => {
    const revision = ++generation.current;
    setBusy(true); setError('');
    try { const next = await api.storageUsage(); if (revision === generation.current) setUsage(next); }
    catch (cause) { if (revision === generation.current) setError(cause instanceof Error ? cause.message : '存储用量读取失败。'); }
    finally { if (revision === generation.current) setBusy(false); }
  };
  useEffect(() => { if (open) { setUsage(undefined); setReport(undefined); setNotice(''); void refresh(); } return () => { generation.current++; }; }, [open, api]);
  const sweep = async (dryRun: boolean) => {
    if (busy) return;
    const revision = ++generation.current;
    setBusy(true); setError(''); setNotice('');
    if (dryRun) setReport(undefined);
    try {
      const next = await api.sweepStorage({ dryRun, limit: 100 });
      if (revision !== generation.current) return;
      setReport(next);
      if (!dryRun) {
        setNotice(`本次清理 ${next.removed} 个文件，释放 ${officeBytes(next.freedBytes)}。`);
        try { const current = await api.storageUsage(); if (revision === generation.current) setUsage(current); }
        catch { if (revision === generation.current) setNotice(`清理已完成：释放 ${officeBytes(next.freedBytes)}。最新用量暂时无法读取，请刷新用量；无需再次提交清理。`); }
      }
    } catch (cause) {
      if (revision === generation.current) {
        setError(cause instanceof Error ? cause.message : '清理检查未完成。');
        if (!dryRun) { setReport(undefined); setNotice('清理结果暂未确认。请先刷新用量并重新检查可清理文件，再决定是否继续。'); }
      }
    }
    finally { if (revision === generation.current) setBusy(false); }
  };
  return <Dialog open={open} title="办公文件存储" onClose={() => { if (!busy) onClose(); }}>
    <div className="os-storage-dialog">
      <p className="os-muted">查看本机办公工作台的文件占用。清理只处理超过保留期限且未被任何版本或工作引用的受管文件，执行前会重新核对引用。</p>
      {usage && <>
        <dl className="os-storage-metrics">
          <div><dt>已占用</dt><dd>{officeBytes(usage.usedBytes)}</dd></div>
          <div><dt>写入中预留</dt><dd>{officeBytes(usage.reservedBytes)}</dd></div>
          <div><dt>容量上限</dt><dd>{officeBytes(usage.limitBytes)}</dd></div>
          <div><dt>仍被引用</dt><dd>{officeBytes(usage.referencedBytes)}</dd></div>
          <div><dt>其他受管目录内容</dt><dd>{officeBytes(usage.unmanagedBytes)}</dd></div>
          <div><dt>文件 / 活动写入</dt><dd>{usage.blobCount} / {usage.activeLeaseCount}</dd></div>
        </dl>
        {usage.overLimit && <p role="status" className="os-preview-notice">当前占用已达到容量上限，新文件写入可能暂时受限。</p>}
      </>}
      {report && <div className="os-storage-report">
        <p>{report.dryRun ? `本批发现 ${report.candidates} 个可清理文件，尚未删除。` : `实际删除 ${report.removed} 个文件，释放 ${officeBytes(report.freedBytes)}。`}</p>
        {report.releasedReservationBytes > 0 && <p>{report.dryRun ? '可释放' : '已释放'}过期写入预留：{officeBytes(report.releasedReservationBytes)}</p>}
        {report.hasMore && <p>还有后续批次，可继续检查。每次最多处理 100 项。</p>}
        {!!report.errors?.length && <ul>{(report.errors ?? []).map((item, index) => <li key={index}>{item}</li>)}</ul>}
      </div>}
      {notice && <p role="status">{notice}</p>}{error && <p role="alert">{error}</p>}
      {busy && <p role="status">正在读取或核对文件占用…</p>}
      <div className="dialog-actions">
        <button disabled={busy} onClick={onClose}>关闭</button>
        <button disabled={busy} onClick={() => void refresh()}>刷新用量</button>
        <button disabled={busy} onClick={() => void sweep(true)}>检查可清理文件</button>
        {report?.dryRun && (report.candidates > 0 || report.releasedReservationBytes > 0) && <button disabled={busy} onClick={() => void sweep(false)}>清理无引用文件</button>}
      </div>
    </div>
  </Dialog>;
}
