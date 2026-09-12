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
