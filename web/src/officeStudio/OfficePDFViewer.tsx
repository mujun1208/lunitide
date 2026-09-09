import React, { useEffect, useRef, useState } from 'react';
import { ArrowLeft, ArrowRight, Minus, Plus, RotateCw } from 'lucide-react';
import type { PDFDocumentProxy, PDFDocumentLoadingTask, RenderTask } from 'pdfjs-dist';
import type { OfficeStudioApi } from './officeStudioApi';
import { readOfficePDF } from './officePDF';

export function OfficePDFViewer({ api, taskId, versionId, name }: {
  api: OfficeStudioApi; taskId: string; versionId: string; name: string;
}): React.JSX.Element {
  const [document, setDocument] = useState<PDFDocumentProxy>();
  const [page, setPage] = useState(1);
  const [zoom, setZoom] = useState(1);
  const [rotation, setRotation] = useState(0);
  const [width, setWidth] = useState(600);
  const [error, setError] = useState('');
  const [rendering, setRendering] = useState(true);
  const [retry, setRetry] = useState(0);
  const host = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const controller = new AbortController();
    let task: PDFDocumentLoadingTask | undefined;
    setDocument(undefined);
    setError('');
    setPage(1);
    setZoom(1);
    setRotation(0);
    setRendering(true);
    void (async () => {
      const blob = await readOfficePDF(api, taskId, versionId, controller.signal);
      const bytes = new Uint8Array(await blob.arrayBuffer());
      const { loadOfficePDF } = await import('./officePDFRenderer');
      if (controller.signal.aborted) return;
      task = loadOfficePDF(bytes);
      task.onPassword = () => {
        if (!controller.signal.aborted) {
          setError('此 PDF 已加密，请提供未加密的副本。');
          setRendering(false);
          void task?.destroy();
        }
      };
      const loaded = await task.promise;
      if (!controller.signal.aborted) setDocument(loaded);
    })().catch(cause => {
      if (!controller.signal.aborted) {
        setError(cause instanceof Error ? cause.message : 'PDF 预览读取失败。');
        setRendering(false);
      }
    });
    return () => { controller.abort(); void task?.destroy(); };
  }, [api, taskId, versionId, retry]);

  useEffect(() => {
    if (!host.current) return;
    const observer = new ResizeObserver(entries => {
      const value = entries[0]?.contentRect.width;
      if (value > 0) setWidth(Math.max(100, Math.floor(value - 24)));
    });
    observer.observe(host.current);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const target = host.current;
    if (!document || !target) return;
    let disposed = false;
    let render: RenderTask | undefined;
    // Every render owns its canvas, so a cancelled page never paints the next one.
    const canvas = window.document.createElement('canvas');
    canvas.setAttribute('aria-label', `${name} · 第 ${page} 页`);
    canvas.setAttribute('role', 'img');
    setRendering(true);
    setError('');
    void document.getPage(page).then(pdfPage => {
      if (disposed) return;
      const pageRotation = (pdfPage.rotate + rotation) % 360;
      const base = pdfPage.getViewport({ scale: 1, rotation: pageRotation });
      const scale = Math.min(width / base.width, 2) * zoom;
      const viewport = pdfPage.getViewport({ scale, rotation: pageRotation });
      const ratio = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = Math.ceil(viewport.width * ratio);
      canvas.height = Math.ceil(viewport.height * ratio);
      canvas.style.width = `${Math.ceil(viewport.width)}px`;
      canvas.style.height = `${Math.ceil(viewport.height)}px`;
      target.replaceChildren(canvas);
      render = pdfPage.render({ canvas, viewport, transform: ratio === 1 ? undefined : [ratio, 0, 0, ratio, 0, 0] });
      return render.promise;
    }).then(() => {
      if (!disposed) setRendering(false);
    }).catch(cause => {
      if (!disposed) {
        setError(cause instanceof Error ? cause.message : '此页无法显示。');
        setRendering(false);
      }
    });
    return () => { disposed = true; render?.cancel(); canvas.remove(); };
  }, [document, page, width, zoom, rotation, name]);

  return <div className="os-pdf-view">
    <div className="os-pdf-toolbar" aria-label="PDF 翻页与缩放">
      <button title="上一页" aria-label="上一页" disabled={!document || page <= 1} onClick={() => setPage(p => p - 1)}><ArrowLeft size={16} /></button>
      <span>{page} / {document?.numPages ?? '…'}</span>
      <button title="下一页" aria-label="下一页" disabled={!document || page >= document.numPages} onClick={() => setPage(p => p + 1)}><ArrowRight size={16} /></button>
      <button title="缩小" aria-label="缩小" disabled={zoom <= 0.5} onClick={() => setZoom(z => Math.max(0.5, z - 0.25))}><Minus size={16} /></button>
      <button title="适合宽度" onClick={() => setZoom(1)}>{Math.round(zoom * 100)}%</button>
      <button title="放大" aria-label="放大" disabled={zoom >= 2} onClick={() => setZoom(z => Math.min(2, z + 0.25))}><Plus size={16} /></button>
      <button title="旋转页面" aria-label="旋转页面" disabled={!document} onClick={() => setRotation(r => (r + 90) % 360)}><RotateCw size={16} /></button>
    </div>
    {error && <div className="os-alert" role="alert">{error}<button onClick={() => setRetry(r => r + 1)}>重试预览</button></div>}
    {rendering && <p role="status">正在绘制第 {page} 页…</p>}
    <div className="os-pdf-canvas" ref={host} aria-busy={rendering} />
  </div>;
}
