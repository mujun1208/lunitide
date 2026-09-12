import React from 'react';
import type { OfficeArtifact, OfficeSource, OfficeVersion } from './officeStudioApi';
import { officeBytes, officeDate, officeQualityLabel } from './officePresentation';
import { canFormalDeliver, capabilityUsabilityLabels, formalDeliverBlockedReason, officeCheckStatusLabel, qualityPromiseLabels } from './officeQualityUi';

export type OfficeInspectorTab = 'conversation' | 'checks' | 'versions' | 'sources';
export const officeInspectorLabels: Record<OfficeInspectorTab, string> = {
  conversation: '对话',
  checks: '检查',
  versions: '版本',
  sources: '来源',
};

export function OfficeChecks({
  version,
  busy,
  checking,
  stopping,
  checkStopped,
  facts,
  nodes,
  onValidate,
  onStop,
  onLocate,
}: {
  version?: OfficeVersion;
  busy: boolean;
  checking: boolean;
  stopping: boolean;
  checkStopped?: boolean;
  facts?: Array<{ factId: string; value: string; unit?: string }>;
  nodes?: Array<{ id: string; text: string; valueType?: string }>;
  onValidate: () => void;
  onStop: () => void;
  onLocate: (nodeId: string) => void;
}): React.JSX.Element {
  if (!version) return <p className="os-muted">选择一份文件后查看检查结果。</p>;
  const checks = version.validations ?? [];
  const promises = qualityPromiseLabels(version, { facts, nodes });
  const usable = capabilityUsabilityLabels(checks);
  return (
    <>
      <div className={`os-check-summary is-${version.quality}`}>
        <h3>{checking ? '检查中' : officeQualityLabel(version.quality)}</h3>
        <p>v{version.versionNo} · 检查结果只适用于此版本。</p>
        {checkStopped ? <p>已停止，检查未完成。</p> : null}
        {promises.length ? (
          <ul aria-label="质量承诺">
            {promises.map((label) => (
              <li key={label}>{label}</li>
            ))}
          </ul>
        ) : null}
        {usable.length ? (
          <ul aria-label="可用状态">
            {usable.map((label) => (
              <li key={label}>{label}</li>
            ))}
          </ul>
        ) : null}
      </div>
      {checking || version.quality === 'checking' ? (
        <button disabled={stopping} onClick={onStop}>
          {stopping ? '正在停止…' : '停止检查'}
        </button>
      ) : (
        <button disabled={busy} onClick={onValidate}>
          检查此版本
        </button>
      )}
      {checks.length ? (
        <ul className="os-check-list">
          {checks.map((check) => (
            <li key={check.id} className={`is-${check.status}`}>
              <div>
                <strong>{check.label}</strong>
                <span>
                  {officeCheckStatusLabel(check.status, check.id)}
                </span>
              </div>
              <p>{check.message}</p>
              {check.messageTruncated && <small className="os-muted">检查说明较长，此处显示摘要。</small>}
              {check.nodeId && <button onClick={() => onLocate(check.nodeId!)}>定位内容</button>}
            </li>
          ))}
        </ul>
      ) : (
        <p className="os-muted">还没有检查回执。文件生成完成不代表排版或数据检查已经通过。</p>
      )}
    </>
  );
}

export function OfficeVersions({
  artifact,
  selectedVersionId,
  busy,
  onSelect,
  onAccept,
  onRestore,
  onCompare,
}: {
  artifact?: OfficeArtifact;
  selectedVersionId?: string;
  busy: boolean;
  onSelect: (versionId: string) => void;
  onAccept: (version: OfficeVersion, formal?: boolean) => void;
  onRestore: (version: OfficeVersion) => void;
  onCompare: (version: OfficeVersion) => void;
}): React.JSX.Element {
  if (!artifact) return <p className="os-muted">选择一份文件后查看版本。</p>;
  return (
    <ol className="os-version-list">
      {[...artifact.versions]
        .sort((a, b) => b.versionNo - a.versionNo)
        .map((version) => {
          const formalReason = formalDeliverBlockedReason(version);
          return (
          <li key={version.id} className={selectedVersionId === version.id ? 'is-selected' : ''}>
            <button className="os-version-select" onClick={() => onSelect(version.id)}>
              <strong>v{version.versionNo}</strong>
              <span>
                {version.id === artifact.headVersionId && <i>最新</i>}
                {version.id === artifact.acceptedVersionId && <i>已接受</i>}
                {version.id === selectedVersionId && <i>当前查看</i>}
              </span>
            </button>
            <p>{version.summary || (version.mode === 'imported' ? '原文件导入' : '文件生成')}</p>
            <small>
              {officeDate(version.createdAt)} · {officeBytes(version.size)}
            </small>
            <p className={`os-quality is-${version.quality}`}>{officeQualityLabel(version.quality)}</p>
            <div className="os-row-actions">
              <button disabled={busy || selectedVersionId === version.id} onClick={() => onCompare(version)}>
                与当前查看版对比
              </button>
              <button disabled={busy || artifact.acceptedVersionId === version.id} onClick={() => onAccept(version)}>
                {version.quality === 'passed' ? '使用此版' : '接受为草稿'}
              </button>
              <button
                disabled={busy || artifact.acceptedVersionId === version.id || !canFormalDeliver(version)}
                onClick={() => onAccept(version, true)}
              >
                作为正式交付
              </button>
              {formalReason ? <p className="os-muted">{formalReason}</p> : null}
              {version.id !== artifact.headVersionId && (
                <button disabled={busy} onClick={() => onRestore(version)}>
                  恢复为新版本
                </button>
              )}
            </div>
            <details>
              <summary>版本记录</summary>
              <dl>
                <dt>摘要校验</dt>
                <dd>{version.sha256}</dd>
                <dt>父版本</dt>
                <dd>{version.parentVersionId || '首次版本'}</dd>
                <dt>文件位置</dt>
                <dd>{version.path || '由工作台托管，导出后可打开副本'}</dd>
              </dl>
            </details>
          </li>
          );
        })}
    </ol>
  );
}

export function OfficeSources({ sources }: { sources: OfficeSource[] }): React.JSX.Element {
  if (!sources.length)
    return <p className="os-muted">当前任务尚无可追溯来源。请在对话中提供材料，生成后可在这里核对引用和数据来源。</p>;
  return (
    <ul className="os-source-list">
      {sources.map((source) => (
        <li key={source.id}>
          <h3>{source.name}</h3>
          {source.stale && <p className="os-quality is-stale">来源已更新，当前交付文件仍使用旧版。</p>}
          <dl>
            {source.location && (
              <>
                <dt>位置</dt>
                <dd>{source.location}</dd>
              </>
            )}
            {source.rawValue !== undefined && (
              <>
                <dt>原值</dt>
                <dd>{source.rawValue}</dd>
              </>
            )}
            {source.transform && (
              <>
                <dt>换算</dt>
                <dd>
                  {source.transform}
                  {source.transformTruncated && <small className="os-muted">（换算说明摘要）</small>}
                </dd>
              </>
            )}
            {source.displayValue !== undefined && (
              <>
                <dt>显示值</dt>
                <dd>{source.displayValue}</dd>
              </>
            )}
            {source.versionId && (
              <>
                <dt>来源版本</dt>
                <dd>{source.versionId}</dd>
              </>
            )}
          </dl>
        </li>
      ))}
    </ul>
  );
}
