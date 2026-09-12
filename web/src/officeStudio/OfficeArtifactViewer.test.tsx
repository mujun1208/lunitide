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

it('renders one PPT paper per slide and does not keep a viewer-side page rail', () => {
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
  expect(screen.queryByRole('navigation', { name: '内容目录' })).toBeNull();
  expect(screen.getAllByRole('article')).toHaveLength(2);
  expect(screen.getByText('封面标题')).toBeTruthy();
  expect(screen.getByText('副标题')).toBeTruthy();
});
