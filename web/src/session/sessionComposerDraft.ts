import type { SkillDTO } from '../generated/bridge';
import type { AttachmentProgress } from './attachments';

export interface SessionComposerDraft {
  text: string;
  referencedSkills: SkillDTO[];
  pendingAttachmentIds: string[];
  uploadProgress: AttachmentProgress[];
}

// Keep unsent composer state across views using this same message bridge.
// Nothing is written to disk or shared across projects, sessions or runtimes.
const drafts = new WeakMap<object, Map<string, SessionComposerDraft>>();
const keyOf = (projectId: string, sessionId: string) => `${projectId}\0${sessionId}`;
const snapshot = (draft: SessionComposerDraft): SessionComposerDraft => ({
  text: draft.text,
  referencedSkills: [...draft.referencedSkills],
  pendingAttachmentIds: [...draft.pendingAttachmentIds],
  uploadProgress: draft.uploadProgress
    .filter(
      (item) =>
        item.attachmentId && draft.pendingAttachmentIds.includes(item.attachmentId) && item.status === 'complete',
    )
    .map(({ file: _file, ...item }) => ({ ...item })),
});

export function readSessionComposerDraft(
  owner: object,
  projectId: string,
  sessionId: string,
): SessionComposerDraft | undefined {
  const draft = drafts.get(owner)?.get(keyOf(projectId, sessionId));
  return draft ? snapshot(draft) : undefined;
}

export function writeSessionComposerDraft(
  owner: object,
  projectId: string,
  sessionId: string,
  value: SessionComposerDraft,
): void {
  const key = keyOf(projectId, sessionId);
  const cache = drafts.get(owner) ?? new Map<string, SessionComposerDraft>();
  cache.delete(key);
  if (value.text || value.referencedSkills.length || value.pendingAttachmentIds.length) cache.set(key, snapshot(value));
  while (cache.size > 128) cache.delete(cache.keys().next().value!);
  drafts.set(owner, cache);
}

export function acknowledgeSessionComposerDraft(
  owner: object,
  projectId: string,
  sessionId: string,
  text: string,
  attachmentIds: readonly string[] = [],
): void {
  const draft = readSessionComposerDraft(owner, projectId, sessionId);
  if (!draft) return;
  writeSessionComposerDraft(owner, projectId, sessionId, {
    ...draft,
    text: draft.text === text ? '' : draft.text,
    pendingAttachmentIds: draft.pendingAttachmentIds.filter((id) => !attachmentIds.includes(id)),
  });
}
