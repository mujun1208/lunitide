# ADR-003: Local storage threat boundary

- Status: Accepted
- Date: 2026-08-09

Lunitide's current storage design targets a single-user desktop threat model. It does not promise to resist a malicious process that already controls the current user's session, can read Lunitide process memory, and can concurrently modify files with that user's authority.

The production root is pinned by a fixed directory handle opened without share-delete. Database and sidecar files reject reparse points and hard links, and their volume serial, file index, and link count are checked before and after security changes. These controls are defense in depth; they do not completely eliminate every path-reopen window because SQLite still opens filenames itself.

A custom SQLite VFS is deliberately not implemented in the current pure-Go release. Until that decision is revisited, claims must accurately describe the fixed-root handle, no-share-delete policy, and pre/post validation without claiming complete elimination of path-reopen races.
