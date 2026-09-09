import React, { useEffect, useRef, useState } from 'react';
import { Dialog } from '../ui/Dialog';
import type { OfficeDiff, OfficeVersion } from './officeStudioApi';
import { officeBytes } from './officePresentation';

type DiffInput = { taskId: string; baseVersionId: string; versionId: string; nodeOffset?: number; partOffset?: number };
const changeName = (value: string) => ({ added: '新增', deleted: '删除', modified: '修改' })[value] || value;
const fieldName = (value: string) => ({ text: '内容', kind: '类型', xml: '节点结构' })[value] || value;
export function OfficeDiffDialog({
  taskId,
  fileName,
  baseVersion,
  version,
  request,
  onClose,
}: {
  taskId: string;
  fileName: string;
  baseVersion: OfficeVersion;
  version: OfficeVersion;
  request: (input: DiffInput) => Promise<OfficeDiff>;
  onClose: () => void;
}): React.JSX.Element {
  const [result, setResult] = useState<OfficeDiff>();
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const [nodePages, setNodePages] = useState([0]);
  const [partPages, setPartPages] = useState([0]);
  const intro = useRef<HTMLParagraphElement>(null);
  const nodeOffset = nodePages[nodePages.length - 1],
    partOffset = partPages[partPages.length - 1];
  const requestRef = useRef(request);
  requestRef.current = request;
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError('');
    void requestRef
      .current({ taskId, baseVersionId: baseVersion.id, versionId: version.id, nodeOffset, partOffset })
      .then((value) => {
        if (!active) return;
        if (value.baseVersionId !== baseVersion.id || value.versionId !== version.id)
          throw new Error('对比结果与所选版本不一致，请重试。');
        setResult(value);
      })
      .catch((cause) => {
        if (active) setError(cause instanceof Error ? cause.message : '版本对比暂时不可用，请重试。');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [taskId, baseVersion.id, version.id, nodeOffset, partOffset, revision]);
  const current = result?.nodeOffset === nodeOffset && result?.partOffset === partOffset ? result : undefined;
  return (
    <Dialog
      open
      title="版本对比"
      description={`${fileName} · v${baseVersion.versionNo} → 当前查看 v${version.versionNo}。对比过程不会修改任何文件。`}
      onClose={onClose}
      initialFocus={intro}
      wide
    >
      <div className="os-diff-content">
        <p className="os-muted" tabIndex={-1} ref={intro}>
          {current?.notice || '按文件节点和部件比较；结构差异不代表像素排版效果，也不判断业务含义。'}
        </p>
        {loading && <p role="status">正在读取两份实际文件并比较…</p>}
        {error && (
          <div role="alert" className="os-metric-error">
            {error}
            <button onClick={() => setRevision((value) => value + 1)}>重试对比</button>
          </div>
        )}
        {current && (
          <>
            {current.comparisonBasis === 'file-digest' ? (
              <p>
                {current.bytesIdentical ? '两份文件的完整摘要一致。' : '两份文件的完整摘要不同。'}PDF
                具体页面、文字和排版尚未对比。
              </p>
            ) : (
              <>
                <div className="os-diff-summary">
                  <b>内容节点</b>
                  <span>新增 {current.summary.addedNodes}</span>
                  <span>删除 {current.summary.deletedNodes}</span>
                  <span>修改 {current.summary.modifiedNodes}</span>
                  <span>未变 {current.summary.unchangedNodes}</span>
                </div>
                {current.payloadIdentical && <p className="os-muted">已比较的文件部件内容一致。</p>}
                {current.truncated && (
                  <p className="os-muted">对比记录较长，请翻页查看后续差异；标记“摘要”的单项只显示部分文字。</p>
                )}
                <div className="os-diff-changes">
                  {current.changes.map((change) => (
                    <details open key={`${change.partNameDigest}:${change.nodeId}`} className="os-diff-change">
                      <summary>
                        <b>{changeName(change.change)}</b> · {change.locator || change.nodeId}
                      </summary>
                      <p className="os-muted">
                        {change.part}
                        {change.partTruncated ? '（名称摘要）' : ''} · {change.changedFields.map(fieldName).join('、')}
                      </p>
                      <div className="os-diff-columns">
                        <section>
                          <h3>原版本 v{baseVersion.versionNo}</h3>
                          <pre>{change.before?.text ?? '此处原来没有内容'}</pre>
                          {change.before?.textTruncated && <small className="os-muted">原文摘要</small>}
                        </section>
                        <section>
                          <h3>当前查看 v{version.versionNo}</h3>
                          <pre>{change.after?.text ?? '此处内容已删除'}</pre>
                          {change.after?.textTruncated && <small className="os-muted">新文摘要</small>}
                        </section>
                      </div>
                    </details>
                  ))}
                </div>
                {current.totalNodeChanges === 0 && (
                  <p className="os-muted">
                    受支持的内容节点未发现变化；样式、图片或其他对象还需结合部件记录和排版查看。
                  </p>
                )}
                {current.totalNodeChanges > 0 && (
                  <nav className="os-preview-pages" aria-label="内容差异翻页">
                    <button
                      disabled={loading || nodePages.length < 2}
                      onClick={() => setNodePages((values) => values.slice(0, -1))}
                    >
                      上一批内容差异
                    </button>
                    <span>
                      {current.nodeOffset + 1}–{current.nextNodeOffset} / {current.totalNodeChanges}
                    </span>
                    <button
                      disabled={
                        loading ||
                        current.nextNodeOffset <= nodeOffset ||
                        current.nextNodeOffset >= current.totalNodeChanges
                      }
                      onClick={() => setNodePages((values) => [...values, current.nextNodeOffset])}
                    >
                      下一批内容差异
                    </button>
                  </nav>
                )}
                <details className="os-diff-parts">
                  <summary>文件部件变化（{current.totalPartChanges}）</summary>
                  <p className="os-muted">
                    新增 {current.summary.addedParts} · 删除 {current.summary.deletedParts} · 修改{' '}
                    {current.summary.modifiedParts} · 未变 {current.summary.unchangedParts}
                  </p>
                  {current.parts.map((part) => (
                    <article key={part.nameDigest}>
                      <b>
                        {changeName(part.change)} · {part.name}
                        {part.nameTruncated ? '（名称摘要）' : ''}
                      </b>
                      <p>
                        {officeBytes(part.beforeSize)} → {officeBytes(part.size)}
                      </p>
                      <details>
                        <summary>完整部件摘要</summary>
                        <dl>
                          <dt>原版本</dt>
                          <dd>{part.beforeSha256 || '无'}</dd>
                          <dt>当前查看版</dt>
                          <dd>{part.sha256 || '无'}</dd>
                        </dl>
                      </details>
                    </article>
                  ))}
                  {current.totalPartChanges > 0 && (
                    <nav className="os-preview-pages" aria-label="文件部件差异翻页">
                      <button
                        disabled={loading || partPages.length < 2}
                        onClick={() => setPartPages((values) => values.slice(0, -1))}
                      >
                        上一批部件差异
                      </button>
                      <span>
                        {current.partOffset + 1}–{current.nextPartOffset} / {current.totalPartChanges}
                      </span>
                      <button
                        disabled={
                          loading ||
                          current.nextPartOffset <= partOffset ||
                          current.nextPartOffset >= current.totalPartChanges
                        }
                        onClick={() => setPartPages((values) => [...values, current.nextPartOffset])}
                      >
                        下一批部件差异
                      </button>
                    </nav>
                  )}
                </details>
              </>
            )}
            <details className="os-diff-digests">
              <summary>完整文件摘要</summary>
              <dl>
                <dt>v{baseVersion.versionNo}</dt>
                <dd>{current.baseSha256}</dd>
                <dt>v{version.versionNo}</dt>
                <dd>{current.sha256}</dd>
              </dl>
            </details>
          </>
        )}
        <div className="dialog-actions">
          <button onClick={onClose}>关闭</button>
        </div>
      </div>
    </Dialog>
  );
}
