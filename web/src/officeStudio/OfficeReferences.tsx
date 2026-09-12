import React, { useRef, useState } from 'react';
import { ExternalLink, Paperclip, Plus, X } from 'lucide-react';
import { Dialog } from '../ui/Dialog';
import { OfficePDFViewer } from './OfficePDFViewer';
import type { OfficeArtifact, OfficeStudioApi, OfficeTask } from './officeStudioApi';
import { isOfficeReference } from './officePresentation';
import { officeStudioUserError } from './officeUserError';

export function OfficeReferences({
  api,
  task,
  files,
  selectedId,
  onSelectDeliverable,
  onAdd,
  onOpenExport,
}: {
  api: OfficeStudioApi;
  task: OfficeTask;
  files: OfficeArtifact[];
  selectedId?: string;
  onSelectDeliverable?: (file: OfficeArtifact) => void;
  onAdd?: () => void;
  onOpenExport?: (task: OfficeTask, path: string, reveal: boolean) => Promise<void>;
}): React.JSX.Element {
  const [pdf, setPdf] = useState<OfficeArtifact>();
  const [opening, setOpening] = useState('');
  const [notice, setNotice] = useState('');
  const [error, setError] = useState('');
  const busy = useRef(false);
  const deliverables = files.filter(file => !isOfficeReference(file));
  const references = files.filter(isOfficeReference);
  const open = async (file: OfficeArtifact) => {
    if (busy.current) return;
    busy.current = true;
    setOpening(file.id); setError(''); setNotice('');
    try {
      const copy = await api.exportArtifact({ taskId: task.id, versionId: file.headVersionId, name: file.name, draft: true });
      if (onOpenExport) await onOpenExport(task, copy.path, false);
      else await api.openExport({ taskId: task.id, path: copy.path, reveal: false });
      setNotice(`已打开 ${file.name} 的查看副本`);
    } catch (cause) {
      setError(officeStudioUserError(cause, '附件打开失败，请重试。'));
    } finally { busy.current = false; setOpening(''); }
  };
  return <section className="os-references" aria-label="产物清单">
    <div className="os-reference-heading"><strong>交付文件{deliverables.length ? ` · ${deliverables.length}` : ''}</strong></div>
    <div className="os-reference-files">
      {deliverables.length ? deliverables.map(file => (
        <button
          key={file.id}
          type="button"
          aria-current={selectedId === file.id ? 'true' : undefined}
          title={`查看交付文件 ${file.name}`}
          aria-label={`查看交付文件 ${file.name}`}
          onClick={() => onSelectDeliverable?.(file)}
        >
          <span className={`os-format is-${file.kind}`}>{file.kind.toUpperCase()}</span>
          <span>{file.name}</span>
        </button>
      )) : <p className="os-muted">还没有交付文件</p>}
    </div>
    <section className="os-reference-sources" aria-label="对话参考附件">
      <div className="os-reference-heading"><strong>参考附件{references.length ? ` · ${references.length}` : ''}</strong>
        {onAdd && <button onClick={onAdd} title="添加参考附件" aria-label="添加参考附件"><Plus size={16}/></button>}
      </div>
      <div className="os-reference-files">{references.map(file => <button key={file.id} disabled={!!opening}
        title={`查看附件 ${file.name}`} aria-label={`查看附件 ${file.name}`}
        onClick={() => {
          if (file.kind === 'pdf') { setError(''); setPdf(file); }
          onSelectDeliverable?.(file);
        }}>
        <Paperclip size={15}/><span>{file.name}</span></button>)}</div>
    </section>
    {notice && !pdf && <p role="status">{notice}</p>}{error && !pdf && <p role="alert">{error}</p>}
    <Dialog open={!!pdf} title={pdf?.name || '参考附件'} wide onClose={() => setPdf(undefined)}>
      {pdf && <div className="os-reference-preview"><OfficePDFViewer api={api} taskId={task.id} versionId={pdf.headVersionId} name={pdf.name}/></div>}
      {notice && <p role="status">{notice}</p>}{error && <p role="alert">{error}</p>}
      <div className="dialog-actions">{pdf && <button disabled={!!opening} onClick={() => void open(pdf)}><ExternalLink size={16}/>用本机应用打开</button>}
        <button onClick={() => setPdf(undefined)}><X size={16}/>关闭</button></div>
    </Dialog>
  </section>;
}
