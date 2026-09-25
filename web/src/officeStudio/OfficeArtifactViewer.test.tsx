import React from 'react';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { OfficeArtifactViewer } from './OfficeArtifactViewer';
import type { OfficeArtifact, OfficePreview, OfficeStudioApi, OfficeVersion } from './officeStudioApi';

afterEach(cleanup);

const artifact: OfficeArtifact = {
  id: 'doc',
  name: '报告.docx',
  kind: 'docx',
  revision: 1,
  headVersionId: 'v2',
  versions: [],
};
const version: OfficeVersion = {
  id: 'v2',
  versionNo: 2,
  quality: 'partial',
  mode: 'imported',
  size: 10,
  sha256: 'b'.repeat(64),
  createdAt: '2026-09-12T00:00:00Z',
};
const api = { readChunk: vi.fn() } as unknown as OfficeStudioApi;

it('offers rebuild when the same-source PDF is stale and never treats it as the current formal reading copy', () => {
  const onRebuild = vi.fn();
  const preview: OfficePreview = {
    versionId: 'v2',
    kind: 'docx',
    content: '',
    previewBasis: '结构预览',
    pdfReady: false,
    truncated: false,
    notice: '源文件已变化，同源 PDF 已失效，需按新版本重建。',
    nodes: [{ id: 'n1', label: '正文', text: '订单 1280单', editable: true }],
  };
  render(
    <OfficeArtifactViewer
      api={api}
      taskId="task"
      artifact={artifact}
      version={version}
      preview={preview}
      loading={false}
      error=""
      onSelectNode={vi.fn()}
      onRetry={vi.fn()}
      onRebuild={onRebuild}
    />,
  );
  expect(screen.getByText(/需按新版本重建/)).toBeTruthy();
  expect(screen.queryByText('文件排版预览')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '按新版本重建' }));
  expect(onRebuild).toHaveBeenCalledTimes(1);
});

it('renders only the active PPT slide and does not keep a viewer-side page rail', () => {
  const ppt: OfficeArtifact = { ...artifact, id: 'ppt', name: '汇报.pptx', kind: 'pptx' };
  const preview: OfficePreview = {
    versionId: 'v2',
    kind: 'pptx',
    content: '',
    previewBasis: '结构预览',
    pdfReady: false,
    truncated: false,
    nodes: [
      { id: 'a', label: 'ppt/slides/slide1.xml · t1', text: '封面标题', location: 'ppt/slides/slide1.xml', editable: true },
      { id: 'b', label: 'ppt/slides/slide1.xml · t2', text: '副标题', location: 'ppt/slides/slide1.xml', editable: true },
      { id: 'c', label: 'ppt/slides/slide2.xml · t1', text: '目录', location: 'ppt/slides/slide2.xml', editable: true },
    ],
  };
  const { rerender } = render(
    <OfficeArtifactViewer
      api={api}
      taskId="task"
      artifact={ppt}
      version={version}
      preview={preview}
      loading={false}
      error=""
      activePageId="slide-1"
      onSelectNode={vi.fn()}
      onRetry={vi.fn()}
    />,
  );
  expect(screen.queryByRole('navigation', { name: '内容目录' })).toBeNull();
  expect(screen.getAllByRole('article')).toHaveLength(1);
  expect(screen.getByText('封面标题')).toBeTruthy();
  expect(screen.getByText('副标题')).toBeTruthy();
  expect(screen.queryByText('目录')).toBeNull();
  expect(screen.queryByText('段落')).toBeNull();
  rerender(
    <OfficeArtifactViewer
      api={api}
      taskId="task"
      artifact={ppt}
      version={version}
      preview={preview}
      loading={false}
      error=""
      activePageId="slide-2"
      onSelectNode={vi.fn()}
      onRetry={vi.fn()}
    />,
  );
  expect(screen.getAllByRole('article')).toHaveLength(1);
  expect(screen.getByText('目录')).toBeTruthy();
  expect(screen.queryByText('封面标题')).toBeNull();
});

it('paints a PPT page from the slide boxes instead of a form', () => {
  const ppt: OfficeArtifact = { ...artifact, id: 'ppt', name: '汇报.pptx', kind: 'pptx' };
  const preview: OfficePreview = {
    versionId: 'v3',
    kind: 'pptx',
    content: '',
    previewBasis: '结构预览',
    pdfReady: false,
    truncated: false,
    nodes: [
      { id: 'a', label: 'ppt/slides/slide1.xml · t1', text: '穆军', location: 'ppt/slides/slide1.xml', editable: true },
    ],
    slides: [{
      part: 'ppt/slides/slide1.xml',
      fill: '#0B1F3A',
      shapes: [{ text: '穆军', x: 8, y: 30, w: 70, h: 18 }],
    }],
  };
  render(
    <OfficeArtifactViewer
      api={api}
      taskId="task"
      artifact={ppt}
      version={version}
      preview={preview}
      loading={false}
      error=""
      onSelectNode={vi.fn()}
      onRetry={vi.fn()}
    />,
  );
  const stage = screen.getByRole('article');
  expect(stage.className).toContain('is-slide-stage');
  expect(stage).toHaveStyle({ background: '#0B1F3A' });
  expect(screen.getByRole('button', { name: '穆军' })).toHaveClass('os-slide-shape');
  expect(screen.getByRole('button', { name: '穆军' })).toHaveStyle({ color: '#F8FAFC' });
});

it('paints a light content slide with its own ink and the header bar', () => {
  const ppt: OfficeArtifact = { ...artifact, id: 'ppt', name: '汇报.pptx', kind: 'pptx' };
  const preview: OfficePreview = {
    versionId: 'v4',
    kind: 'pptx',
    content: '',
    previewBasis: '结构预览',
    pdfReady: false,
    truncated: false,
    nodes: [
      { id: 't', label: 'ppt/slides/slide3.xml · t1', text: '经历', location: 'ppt/slides/slide3.xml', editable: true },
      { id: 'b', label: 'ppt/slides/slide3.xml · t2', text: '航空ERP与MRO', location: 'ppt/slides/slide3.xml', editable: true },
    ],
    slides: [{
      part: 'ppt/slides/slide3.xml',
      fill: '#F4F6F8',
      shapes: [
        { fill: '0B1F3A', x: 0, y: 0, w: 100, h: 16 },
        { text: '经历', color: 'FFFFFF', size: 2.4, bold: true, x: 6, y: 4, w: 80, h: 10 },
        { text: '航空ERP与MRO', color: '1F2937', size: 1.8, x: 6, y: 24, w: 84, h: 40 },
      ],
    }],
  };
  render(
    <OfficeArtifactViewer
      api={api}
      taskId="task"
      artifact={ppt}
      version={version}
      preview={preview}
      loading={false}
      error=""
      onSelectNode={vi.fn()}
      onRetry={vi.fn()}
    />,
  );
  const body = screen.getByRole('button', { name: '航空ERP与MRO' });
  expect(body).toHaveStyle({ color: '#1F2937' });
  expect(screen.getByRole('article').querySelector('.os-slide-fill')).toHaveStyle({ background: '#0B1F3A' });
});
