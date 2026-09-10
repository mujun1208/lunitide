import React from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { OfficeDiffDialog } from './OfficeDiffDialog';
import type { OfficeDiff, OfficeVersion } from './officeStudioApi';

const baseVersion: OfficeVersion = {
  id: 'old',
  versionNo: 1,
  mode: 'imported',
  quality: 'unverified',
  sha256: 'a'.repeat(64),
  size: 100,
  createdAt: '2026-09-07T00:00:00Z',
};
const version = { ...baseVersion, id: 'current', versionNo: 2, sha256: 'b'.repeat(64) };
const diff = (nodeOffset = 0, partOffset = 0): OfficeDiff => ({
  baseVersionId: 'old',
  versionId: 'current',
  kind: 'docx',
  baseSha256: baseVersion.sha256,
  sha256: version.sha256,
  comparisonBasis: 'structure',
  bytesIdentical: false,
  partHashesCompared: true,
  payloadIdentical: false,
  summary: {
    addedNodes: 0,
    deletedNodes: 0,
    modifiedNodes: 4,
    unchangedNodes: 10,
    addedParts: 0,
    deletedParts: 0,
    modifiedParts: 2,
    unchangedParts: 8,
    unchangedBytes: 1200,
  },
  changes: [
    {
      change: 'modified',
      nodeId: `n-${nodeOffset}`,
      part: 'word/document.xml',
      partNameDigest: 'c'.repeat(64),
      partTruncated: false,
      locator: `段落${nodeOffset + 1}`,
      locatorTruncated: false,
      changedFields: ['text'],
      before: { kind: 'paragraph', text: `原文${nodeOffset + 1}`, digest: 'd'.repeat(64), textTruncated: false },
      after: { kind: 'paragraph', text: `修改后${nodeOffset + 1}`, digest: 'e'.repeat(64), textTruncated: false },
    },
  ],
  parts: [
    {
      change: 'modified',
      name: `part-${partOffset}.xml`,
      nameDigest: String(partOffset),
      nameTruncated: false,
      beforeSha256: 'f'.repeat(64),
      sha256: '0'.repeat(64),
      beforeSize: 100,
      size: 101,
    },
  ],
  nodeOffset,
  partOffset,
  nextNodeOffset: nodeOffset ? 4 : 2,
  nextPartOffset: partOffset + 1,
  totalNodeChanges: 4,
  totalPartChanges: 2,
  truncated: true,
  notice: '比较真实节点，不代表像素排版。',
});
afterEach(cleanup);

it('requests the fixed version pair and shows actual before/after content with independent pagination', async () => {
  const request = vi.fn(async (input: { nodeOffset?: number; partOffset?: number }) =>
    diff(input.nodeOffset, input.partOffset),
  );
  render(
    <OfficeDiffDialog
      taskId="task"
      fileName="报告.docx"
      baseVersion={baseVersion}
      version={version}
      request={request}
      onClose={vi.fn()}
    />,
  );
  await screen.findByText('原文1');
  expect(screen.getByText('修改后1')).toBeTruthy();
  expect(request).toHaveBeenCalledWith({
    taskId: 'task',
    baseVersionId: 'old',
    versionId: 'current',
    nodeOffset: 0,
    partOffset: 0,
  });
  fireEvent.click(screen.getByRole('button', { name: '下一批内容差异' }));
  await screen.findByText('原文3');
  fireEvent.click(screen.getByText('文件部件变化（2）'));
  fireEvent.click(screen.getByRole('button', { name: '下一批部件差异' }));
  await screen.findByText('修改 · part-1.xml');
  expect(request).toHaveBeenLastCalledWith({
    taskId: 'task',
    baseVersionId: 'old',
    versionId: 'current',
    nodeOffset: 2,
    partOffset: 1,
  });
  expect(screen.getByText('原文3')).toBeTruthy();
  expect(screen.getByRole('button', { name: '下一批内容差异' })).toBeDisabled();
  fireEvent.click(screen.getByRole('button', { name: '上一批内容差异' }));
  await screen.findByText('原文1');
  expect(request).toHaveBeenLastCalledWith({
    taskId: 'task',
    baseVersionId: 'old',
    versionId: 'current',
    nodeOffset: 0,
    partOffset: 1,
  });
});

it('retries a failed page without changing version IDs or losing its page position', async () => {
  const request = vi
    .fn()
    .mockResolvedValueOnce(diff())
    .mockRejectedValueOnce(new Error('此页读取超时'))
    .mockResolvedValueOnce(diff(2));
  render(
    <OfficeDiffDialog
      taskId="task"
      fileName="报告.docx"
      baseVersion={baseVersion}
      version={version}
      request={request}
      onClose={vi.fn()}
    />,
  );
  await screen.findByText('原文1');
  fireEvent.click(screen.getByRole('button', { name: '下一批内容差异' }));
  await screen.findByText('此页读取超时');
  fireEvent.click(screen.getByRole('button', { name: '重试对比' }));
  await screen.findByText('原文3');
  expect(request.mock.calls[1][0]).toEqual(request.mock.calls[2][0]);
});

it('does not claim PDF content or layout is unchanged from a hash-only comparison', async () => {
  const request = vi.fn(async () => ({
    ...diff(),
    kind: 'pdf' as const,
    comparisonBasis: 'file-digest',
    bytesIdentical: true,
    payloadIdentical: true,
    changes: [],
    parts: [],
    totalNodeChanges: 0,
    totalPartChanges: 0,
    notice: 'PDF仅比较文件摘要。',
  }));
  render(
    <OfficeDiffDialog
      taskId="task"
      fileName="报告.pdf"
      baseVersion={baseVersion}
      version={version}
      request={request}
      onClose={vi.fn()}
    />,
  );
  await screen.findByText(/PDF 具体页面、文字和排版尚未对比/);
  expect(screen.queryByText('已比较的文件部件内容一致。')).toBeNull();
  expect(screen.queryByText('内容节点')).toBeNull();
});

it('does not show raw English transport failures', async () => {
  render(
    <OfficeDiffDialog
      taskId="task"
      fileName="报告.docx"
      baseVersion={baseVersion}
      version={version}
      request={vi.fn().mockRejectedValue(new Error('Failed to fetch'))}
      onClose={vi.fn()}
    />,
  )
  expect(await screen.findByRole('alert')).toHaveTextContent('版本对比暂时不可用，请重试。')
  expect(screen.queryByText('Failed to fetch')).toBeNull()
})

it('leaves FEATURE_DISABLED inspect text unchanged', async () => {
  render(
    <OfficeDiffDialog
      taskId="task"
      fileName="报告.docx"
      baseVersion={baseVersion}
      version={version}
      request={vi.fn().mockRejectedValue(new Error('FEATURE_DISABLED: office inspect unavailable'))}
      onClose={vi.fn()}
    />,
  )
  expect(await screen.findByRole('alert')).toHaveTextContent('FEATURE_DISABLED: office inspect unavailable')
})

it('rejects a diff response that belongs to another selected version', async () => {
  const request = vi.fn(async () => ({ ...diff(), versionId: 'other-version' }));
  render(
    <OfficeDiffDialog
      taskId="task"
      fileName="报告.docx"
      baseVersion={baseVersion}
      version={version}
      request={request}
      onClose={vi.fn()}
    />,
  );
  await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('对比结果与所选版本不一致'));
  expect(screen.queryByText('原文1')).toBeNull();
});
