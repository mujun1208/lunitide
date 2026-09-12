import React, { useEffect, useState } from 'react';
import type { OfficeArtifact, OfficeNode, OfficePreview, OfficeStudioApi, OfficeVersion } from './officeStudioApi';
import { officeQualityLabel } from './officePresentation';
import { conceptPreviewLabel } from './officeQualityUi';
import { OfficePDFViewer } from './OfficePDFViewer';

export function OfficeArtifactViewer({
  api,
  taskId,
  artifact,
  version,
  preview,
  loading,
  error,
  selectedNodeId,
  onSelectNode,
  onRetry,
}: {
  api: OfficeStudioApi;
  taskId: string;
  artifact?: OfficeArtifact;
  version?: OfficeVersion;
  preview?: OfficePreview;
  loading: boolean;
  error: string;
  selectedNodeId?: string;
  onSelectNode: (node: OfficeNode) => void;
  onRetry: () => void;
}): React.JSX.Element {
  const [layout, setLayout] = useState(false);
  useEffect(() => {
    setLayout(!!preview?.pdfReady);
  }, [preview?.versionId, preview?.pdfReady]);
  if (!artifact || !version)
    return (
      <div className="os-empty os-preview-empty">
        <span className="os-empty-icon" aria-hidden="true">
          ▧
        </span>
        <h2>尚未生成交付文件</h2>
        <p>参考材料已单独保留。生成的 PPT、Word、Excel 或 PDF 将显示在这里。</p>
      </div>
    );
  return (
    <section className="os-viewer" aria-label="文件预览">
      <div className="os-preview-caption">
        <span>
          {conceptPreviewLabel(layout)} · v{version.versionNo}
        </span>
        <span className={`os-quality is-${version.quality}`}>{officeQualityLabel(version.quality)}</span>
        {preview?.pdfReady && (
          <div className="os-row-actions">
            <button aria-pressed={layout} onClick={() => setLayout(true)}>
              排版
            </button>
            <button aria-pressed={!layout} onClick={() => setLayout(false)}>
              结构
            </button>
          </div>
        )}
      </div>
      {loading && (
        <p className="os-inline-status" role="status">
          正在读取此版本…
        </p>
      )}
      {error && (
        <div className="os-alert" role="alert">
          {error}
          <button onClick={onRetry}>重试预览</button>
        </div>
      )}
      {preview?.notice && <p className="os-preview-notice">{preview.notice}</p>}
      {preview?.truncated && (
        <p className="os-preview-notice">
          {preview.totalNodes && preview.nextNodeOffset !== undefined && preview.nextNodeOffset < preview.totalNodes
            ? '内容较长，可用下方“下一批内容”继续查看和选择后续内容。'
            : '当前结构预览包含截取内容。完整内容请通过排版预览或导出副本查看。'}
        </p>
      )}
      {layout && !loading && (
        <OfficePDFViewer api={api} taskId={taskId} versionId={version.id} name={artifact.name} />
      )}
      {preview && !loading && !layout && (
        <div className="os-preview-layout">
          {preview.nodes.length > 0 && (
            <nav className="os-outline" aria-label="内容目录">
              {preview.nodes.map((node, index) => (
                <button
                  key={node.id}
                  aria-current={selectedNodeId === node.id ? 'location' : undefined}
                  onClick={() => onSelectNode(node)}
                >
                  <span>{String(index + 1).padStart(2, '0')}</span>
                  <b>{node.label || node.location || `内容 ${index + 1}`}</b>
                </button>
              ))}
            </nav>
          )}
          <div className="os-paper-scroll">
            {preview.nodes.length > 0 ? (
              <div className={`os-structure-pages is-${artifact.kind}`}>
                {preview.nodes.map((node) => (
                  <article
                    className={`os-paper ${selectedNodeId === node.id ? 'is-selected' : ''}`}
                    key={node.id}
                    id={`office-node-${node.id}`}
                  >
                    <button
                      className="os-node-heading"
                      onClick={() => onSelectNode(node)}
                      aria-label={`选择 ${node.label}`}
                    >
                      <b>{node.label}</b>
                      {node.location && <small>{node.location}</small>}
                    </button>
                    {node.valueType === 'image' && node.image ? <div className="os-image-node"><strong>图片{node.image.pixelWidth && node.image.pixelHeight ? ` · ${node.image.pixelWidth} × ${node.image.pixelHeight}` : ''}</strong><p>{node.text || '未提供替代说明。'}</p><small>{node.image.sourceId ? '来源：上传附件' : '来源：文件内嵌图片'} · {node.image.mediaPart}</small><p className="os-muted">选择此图片可替换原图；精确排版请切换到排版预览或打开导出的文件。</p></div> : node.valueType === 'chart' && node.chart ? <div className="os-image-node"><strong>图表 · {node.chart.seriesCount} 个系列 / {node.chart.categoryCount} 个类别</strong><p>{node.text || '此图表未提供文字说明。'}</p><small>{node.chart.chartPart}{node.chart.sourceRanges?.length ? ` · 源范围 ${node.chart.sourceRanges.join('、')}` : ''}{node.chart.cacheState ? ` · 缓存 ${node.chart.cacheState}` : ''}</small><p className="os-muted">{node.editable ? '选择此图表可编辑数据，修改后另存为新版本。' : '图表对象只读。修改本表源单元格后，受管图表缓存随新版本更新。'}</p></div> : <p>{node.text || '此处没有可提取的文字。'}</p>}
                  </article>
                ))}
              </div>
            ) : (
              <div className="os-paper">
                <pre>{preview.content || '此格式暂不支持结构预览，请导出副本后用本机软件打开。'}</pre>
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
