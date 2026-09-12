import { describe, expect, test } from 'vitest';
import type { OfficeArtifact } from './officeStudioApi';
import { officeSyncSelection, visibleOfficeArtifact } from './officePresentation';

const file = (id: string, role: OfficeArtifact['role'], headVersionId = 'v1'): OfficeArtifact => ({
  id,
  role,
  revision: 1,
  name: `${id}.pptx`,
  kind: 'pptx',
  headVersionId,
  versions: [
    {
      id: headVersionId,
      versionNo: 1,
      quality: 'unverified',
      mode: 'imported',
      size: 10,
      sha256: 'a'.repeat(64),
      createdAt: '2026-09-12T00:00:00Z',
    },
  ],
});

describe('visibleOfficeArtifact', () => {
  test('keeps a selected reference visible instead of falling back to empty', () => {
    const reference = file('ref', 'reference');
    expect(visibleOfficeArtifact([reference], 'ref')).toBe(reference);
    expect(visibleOfficeArtifact([reference])).toBe(reference);
  });

  test('prefers a deliverable when nothing is selected', () => {
    const reference = file('ref', 'reference');
    const deliverable = file('doc', 'deliverable');
    expect(visibleOfficeArtifact([reference, deliverable])).toBe(deliverable);
    expect(visibleOfficeArtifact([reference, deliverable], 'ref')).toBe(reference);
  });
});

describe('officeSyncSelection', () => {
  test('does not steal a selected reference during background sync', () => {
    const reference = file('ref', 'reference');
    expect(officeSyncSelection([reference], 'ref', 'v1', false)).toEqual({ artifact: reference, replace: false });
  });

  test('does not remount the current head version on selectHead', () => {
    const deliverable = file('doc', 'deliverable', 'v2');
    expect(officeSyncSelection([deliverable], 'doc', 'v2', true)).toEqual({ artifact: deliverable, replace: false });
  });

  test('switches to a newly generated deliverable after chat finishes', () => {
    const reference = file('ref', 'reference');
    const deliverable = file('doc', 'deliverable', 'v3');
    expect(officeSyncSelection([reference, deliverable], 'ref', 'v1', true)).toEqual({
      artifact: deliverable,
      replace: true,
    });
  });

  test('jumps to a new head version after generation', () => {
    const deliverable = file('doc', 'deliverable', 'v4');
    expect(officeSyncSelection([deliverable], 'doc', 'v2', true)).toEqual({ artifact: deliverable, replace: true });
  });
});
