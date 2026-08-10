# ADR-005: Long-context compaction and cross-window handoff

- Status: Accepted
- Date: 2026-08-10
- Scope: P2 Chat and P4 Memory

## Context

A Lunitide Session must retain a long-lived logical conversation of up to at least one million tokens while the active provider may expose a smaller or larger model context window. The complete durable history cannot be sent in one Host↔Engine RPC frame or rendered in one page. Naive truncation loses decisions and constraints; replacing old messages with an opaque summary destroys provenance and makes recovery impossible.

The system therefore needs three independent limits:

1. durable history and storage quotas;
2. paginated Bridge/UI transport budgets;
3. the selected model's prompt-token budget.

A page-size limit is never a Session message-count limit. Compaction never deletes or edits source Messages.

## External references

The implementation is Go-native. The following projects are design references, not runtime dependencies:

| Reference | License | Adopted ideas | Not adopted |
|---|---|---|---|
| [openai/codex](https://github.com/openai/codex) | Apache-2.0 | automatic and manual compaction; persisted compaction checkpoints; separate pre-turn and mid-turn handling; reinjection of authoritative initial context; window/trigger/reason metadata; preserving a recent-user-message budget; pre/post validation hooks | Rust runtime, provider-specific encrypted compaction payloads, protocol-specific session implementation |
| [langchain-ai/langmem](https://github.com/langchain-ai/langmem) | MIT | token-budget-based message selection; rolling summaries; separation of short-term context from long-term memory | Python/LangGraph runtime and framework state model |
| [letta-ai/letta](https://github.com/letta-ai/letta) | Apache-2.0 | working-memory versus archival-history hierarchy; explicit memory pressure; model-agnostic memory concepts | Python service/runtime and agent self-modification as an authority boundary |
| [openai/openai-agents-python](https://github.com/openai/openai-agents-python) | MIT | Session-backed history; trim/compress distinction; compaction as a typed stream/session item | Python runtime and OpenAI-only session APIs |
| [mem0ai/mem0](https://github.com/mem0ai/mem0) | Apache-2.0 | candidate extraction/evaluation patterns for P4 semantic memory | use as P2 conversation truth or mandatory hosted/vector dependency |
| [getzep/graphiti](https://github.com/getzep/graphiti) | Apache-2.0 | temporal facts, validity intervals and provenance for later P4 retrieval | graph database requirement and replacement of immutable Message history |

Repository metadata and licenses were verified through the public GitHub API on 2026-08-10. Before copying any source rather than independently implementing an idea, engineering must pin an upstream commit, preserve required notices and complete a dependency/license review.

## Decision

### 1. Immutable archival history

`messages` and `message_parts` are the source of truth. Messages are append-only for this lifecycle. A compaction checkpoint references an inclusive source range and never substitutes for, mutates or deletes that range.

History APIs use cursor pagination with both an item limit and an encoded-byte budget below the 4 MiB RPC frame. They support forward and backward navigation without materializing the whole Session.

### 2. Provider-aware token ledger

Every Message, content part, tool result, summary and injected instruction has token-accounting metadata:

- tokenizer/provider/model identity and tokenizer revision;
- exact token count when a compatible tokenizer is available;
- conservative estimate and estimation method otherwise;
- UTF-8 byte count;
- computed timestamp.

Counts are caches, not message truth. A model/tokenizer change invalidates or lazily recomputes them. Prompt assembly reserves output tokens and safety headroom before selecting input.

A provider declaring a one-million-token context is supported without assuming every provider does. The effective input budget is:

```text
min(provider context window, configured safety ceiling)
- reserved output
- system/developer policy
- tool schemas and fixed workspace context
- safety margin
```

Automatic compaction starts before exhaustion using high/low watermarks, not only after a provider rejects a request. The default policy targets compaction at 80% and reduces reusable conversational context below 60%; model-specific configuration may override this.

### 3. Hierarchical prompt assembly

Prompt input is assembled in this precedence order:

1. authoritative system/security/product instructions, freshly injected and never summarized as authority;
2. current workspace/world state and active task state;
3. latest accepted compaction checkpoint covering the oldest selected range;
4. relevant pinned facts/decisions with provenance;
5. recent original Messages, keeping complete turn/tool-call boundaries;
6. the latest user turn, protected by a dedicated reserve;
7. retrieved older evidence only when relevant and within budget.

Selection never splits an assistant tool call from its tool result and never emits an invalid provider message sequence.

### 4. Versioned compaction checkpoints

Each checkpoint records at least:

- canonical ID, Session ID and monotonically increasing version;
- source start/end Message IDs and sequences;
- source-range digest and previous-checkpoint ID/digest;
- summary schema version and prompt-template version;
- provider/model and token-accounting metadata;
- trigger (`automatic`, `manual`, `handoff`) and reason;
- status (`pending`, `running`, `succeeded`, `failed`, `superseded`);
- structured summary plus a human-readable rendering;
- created/completed timestamps and sanitized failure code.

The structured summary contains explicit sections:

- objective and current phase;
- authoritative user requirements and non-negotiable constraints;
- decisions and rationale;
- completed work and verification evidence;
- current repository/runtime state;
- relevant files, symbols, commits and artifacts;
- unresolved defects, risks and external blockers;
- ordered next actions;
- exact identifiers, values or quotations that must not be approximated;
- uncertainty and conflicts requiring source lookup.

A checkpoint is accepted only after deterministic validation confirms valid source bounds, digest binding, schema/size limits, no impossible identifiers, and preservation of protected facts. Failed validation leaves the previous accepted checkpoint active.

Repeated compaction summarizes the previous accepted checkpoint plus the next immutable source range. It does not recursively summarize an already lossy free-form string without provenance.

### 5. Cross-window handoff capsule

A user may manually create a handoff before opening a new window. Automatic handoff is offered near the UI/model-context threshold. A handoff capsule is a specialized accepted checkpoint containing:

- source Session/window and destination Session/window binding;
- the structured summary fields above;
- active task/TODO state;
- latest relevant original turns retained verbatim within a bounded budget;
- source checkpoint and Message-range digests;
- expiration/revocation state when exported outside the same local database.

Opening a new window creates a new window identity while preserving the logical Session unless the user explicitly creates a new Session. If a separate Session is created, the imported capsule is an immutable provenance-linked input, not fabricated original Messages.

The new window displays that context was compacted and allows the user to inspect the summary and jump to source Messages. The Engine, not the Renderer, validates and activates capsules.

### 6. Accuracy and safety gates

Compaction is model output and is therefore untrusted data until validated. It cannot grant tools, change security policy, introduce secrets, or override system/user authority. Summary text is rendered inertly and never interpreted as HTML.

Required gates include:

- golden long-conversation corpora with decisions, reversals, TODOs, exact IDs and code references;
- adversarial prompt-injection content inside historical Messages;
- invariant/property tests for range coverage, no overlap/gaps in checkpoint chains and deterministic selection;
- recall checks for protected facts and contradiction detection against source spans;
- restart, cancellation, retry, concurrent-compaction and idempotency tests;
- provider/tokenizer-change tests;
- maximum 1M-token logical-history tests without a single oversized RPC frame;
- handoff round-trip tests proving source provenance and continuation accuracy;
- explicit failure behavior when no safe compacted prompt fits.

No evaluation score alone authorizes deletion of source history.

## Consequences

### Positive

- Long conversations remain usable across model and UI window limits.
- Compression saves tokens while preserving source truth and auditability.
- Users can continue work in a new window with Codex-like handoff behavior.
- P2 conversation continuity and P4 semantic memory remain separate, reducing accidental authority escalation.
- The Go/WebView2 production architecture gains no Python, Node or Rust runtime dependency.

### Negative

- Token accounting is provider/model specific and must be versioned.
- Summary generation adds latency and model cost.
- Accurate validation requires curated corpora and repeatable evaluation infrastructure.
- Cursor pagination and checkpoint chains add schema and migration complexity.

### Neutral

- A model's advertised 1M context does not imply that every request should consume 1M tokens. The system may compact earlier for latency, cost and attention quality while preserving the full history.
- Semantic retrieval and temporal knowledge graphs remain P4 capabilities and cannot replace P2 archival Messages or compaction provenance.

## Implementation order

1. Correct Message persistence to remove any Session-wide page-size cap and add byte-budgeted cursor pagination.
2. Persist provider-aware token ledger entries.
3. Implement deterministic prompt assembly and protected recent-turn budgeting.
4. Add checkpoint schema, state machine, source digests and manual compaction.
5. Add automatic high/low-watermark compaction and restart recovery.
6. Add cross-window handoff capsule creation, inspection and activation.
7. Add P4 semantic/episodic memory extraction and retrieval behind separate provenance-aware interfaces.
