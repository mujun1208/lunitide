// Code generated from discovered api/bridge/v1 and api/rpc/v1 schemas. DO NOT EDIT.
export const BRIDGE_METHODS = ["chat.start", "diagnostics.export", "memory.delete", "memory.get", "memory.list", "memory.search", "memory.update", "message.append", "message.list", "migration.inspect", "migration.run", "migration.status", "node.complete", "node.fail", "node.list", "node.start", "ontology.edge.list", "ontology.node.get", "ontology.node.list", "ontology.node.search", "plan.activate", "plan.complete", "plan.get", "plan.list", "plan.pause", "plan.resume", "project.create", "project.list", "provider.create", "provider.credential.submit", "provider.delete", "provider.get", "provider.list", "provider.model.sync", "provider.test", "provider.update", "review.approve", "review.list", "review.reject", "session.create", "session.list", "skill.deprecate", "skill.disable", "skill.get", "skill.list", "skill.match", "skill.publish", "stage.create", "stage.list", "stream.cancel", "system.health"] as const
export type BridgeMethod = (typeof BRIDGE_METHODS)[number]
export const PROVIDER_PROTOCOLS = ["openai_compatible", "anthropic"] as const
export type ProviderProtocol = (typeof PROVIDER_PROTOCOLS)[number]
export const CREDENTIAL_STATES = ["configured", "missing", "unavailable", "requires_reentry"] as const
export type CredentialState = (typeof CREDENTIAL_STATES)[number]
export const BRIDGE_VERSION = "1.0" as const
export const RPC_VERSION = { major: 1, minor: 0 } as const

export interface BridgeRequest<TPayload extends object = object> {
  v: typeof BRIDGE_VERSION
  kind: "request"
  id: string
  traceId: string
  method: BridgeMethod
  sentAt: string
  payload: TPayload
  idempotencyKey?: string
  deadlineMs: number
}
export interface BridgeError {
  code: string
  message: string
  retryable: boolean
  details?: object
  correlationId: string
}
export type BridgeResponse<TPayload = unknown> =
  | { v: "1.0"; kind: "response"; id: string; requestId: string; ok: true; payload: TPayload; error?: never }
  | { v: "1.0"; kind: "response"; id: string; requestId: string; ok: false; payload?: never; error: BridgeError }
export type ULID = string
export type ProviderStatus = "enabled" | "disabled"
export type ModelDTO = { "modelId": string; "displayName": string; "isDefault": boolean }
export type ProviderDTO = { "id": ULID; "name": string; "protocol": ProviderProtocol; "baseUrl": string; "models": Array<ModelDTO>; "status": ProviderStatus; "credentialState": CredentialState; "createdAt": string; "updatedAt": string; "version": number }
export type ProjectStatus = "active" | "archived"
export type ProjectDTO = { "id": ULID; "name": string; "status": ProjectStatus; "createdAt": string; "updatedAt": string; "version": number }
export type SessionStatus = "active"
export type SessionDTO = { "id": ULID; "projectId": ULID; "title": string; "status": SessionStatus; "createdAt": string; "updatedAt": string; "version": 1 }
export type MessageDTO = { "id": ULID; "sessionId": ULID; "role": "user"; "status": "completed"; "sequence": number; "text": string; "createdAt": string }
export type StageStatus = "not_started" | "in_progress" | "waiting_review" | "approved" | "completed" | "rejected" | "stale" | "paused" | "blocked" | "cancelled"
export type StageDTO = { "id": ULID; "projectId": ULID; "phase": number; "title": string; "status": StageStatus; "createdAt": string; "updatedAt": string; "version": 1 }
export type MigrationStatusDTO = { "state": "idle" | "running" | "completed" | "failed"; "processed": number; "total": number }
export type PlanStatus = "draft" | "active" | "paused" | "completed" | "cancelled" | "failed"
export type PlanDTO = { "id": ULID; "projectId": ULID; "stageId"?: ULID; "name": string; "description": string; "version": number; "status": PlanStatus; "createdAt": string; "updatedAt": string }
export type NodeStatus = "pending" | "ready" | "running" | "paused" | "completed" | "failed" | "cancelled" | "blocked"
export type RiskLevel = "low" | "medium" | "high" | "critical"
export type PlanNodeDTO = { "id": ULID; "planId": ULID; "parentNodeId"?: ULID; "name": string; "description": string; "status": NodeStatus; "riskLevel": RiskLevel; "budgetTokens"?: number; "estimateTokens"?: number; "workerRole": string; "sequence": number; "createdAt": string; "updatedAt": string }
export type ReviewStatus = "pending" | "approved" | "rejected" | "expired" | "changed_after_approval"
export type ReviewDTO = { "id": ULID; "planId"?: ULID; "nodeId"?: ULID; "actionType": string; "actionDigest": string; "inputDigest": string; "stateDigest": string; "policyVersion": number; "riskLevel": RiskLevel; "status": ReviewStatus; "reviewerNote": string; "expiresAt"?: string; "createdAt": string; "reviewedAt"?: string }
export type MemoryLayer = "working" | "episodic" | "semantic" | "procedural"
export type MemoryScope = "workspace" | "project" | "session"
export type MemoryDTO = { "id": ULID; "projectId": ULID; "layer": MemoryLayer; "scope": MemoryScope; "key": string; "content": string; "embeddingId"?: ULID; "sourceId"?: ULID; "sourceType"?: string; "confidence": number; "accessCount": number; "lastAccessed"?: string; "expiresAt"?: string; "createdAt": string; "updatedAt": string }
export type OntologyNodeType = "class" | "interface" | "function" | "module" | "table" | "file" | "requirement" | "artifact" | "component" | "endpoint" | "test"
export type OntologyNodeDTO = { "id": ULID; "projectId": ULID; "type": OntologyNodeType; "name": string; "fullPath": string; "description": string; "metadataJson": string; "version": number; "createdAt": string; "updatedAt": string }
export type OntologyEdgeType = "implements" | "extends" | "depends_on" | "references" | "contains" | "tests" | "imports" | "satisfies" | "traces" | "generates" | "configures" | "authenticates" | "authorizes"
export type OntologyEdgeDTO = { "id": ULID; "sourceNodeId": ULID; "targetNodeId": ULID; "type": OntologyEdgeType; "label": string; "propertiesJson": string; "weight": number; "version": number; "createdAt": string; "updatedAt": string }
export type SkillStatus = "draft" | "published" | "deprecated" | "disabled"
export type SkillPermission = "read_only" | "read_write" | "network" | "file_system" | "shell" | "admin"
export type SkillDTO = { "id": ULID; "name": string; "displayName": string; "description": string; "version": string; "status": SkillStatus; "permissions": Array<SkillPermission>; "entryPoint": string; "manifestJson": string; "signature"?: string; "publisherId"?: ULID; "minEngineVersion"?: string; "createdAt": string; "updatedAt": string }
export type SkillMatchDTO = { "skill": SkillDTO; "score": number; "reason": string; "matchId": ULID }
export type ChatStartPayload = { "providerId": ULID; "modelId": string; "sessionId"?: ULID; "messages"?: Array<{ "role": "system" | "user" | "assistant"; "content": string }> }
export type ChatStartResult = { "streamId": ULID }
export type DiagnosticsExportPayload = { "includeLogs"?: boolean; "redactPaths"?: boolean }
export type DiagnosticsExportResult = { "path": string; "createdAt": string; "redacted": true }
export type MemoryDeletePayload = { "id": ULID }
export type MemoryDeleteResult = { "deleted": boolean }
export type MemoryGetPayload = { "id": ULID }
export type MemoryGetResult = MemoryDTO
export type MemoryListPayload = { "projectId": ULID; "layer"?: MemoryLayer }
export type MemoryListResult = { "items": Array<MemoryDTO> }
export type MemorySearchPayload = { "projectId": ULID; "query": string }
export type MemorySearchResult = { "items": Array<MemoryDTO> }
export type MemoryUpdatePayload = { "id": ULID; "content": string }
export type MemoryUpdateResult = { "updated": boolean }
export type MessageAppendPayload = { "sessionId": ULID; "text": string }
export type MessageAppendResult = MessageDTO
export type MessageListPayload = { "sessionId": ULID; "cursor"?: string; "direction"?: "forward" | "backward"; "limit"?: number; "byteBudget"?: number }
export type MessageListResult = { "items": Array<MessageDTO>; "hasMore": boolean; "nextCursor": string | null; "snapshotSequence": number }
export type MigrationInspectPayload = Record<string, never>
export type MigrationInspectResult = { "required": boolean; "items": number; "sourceVersion": number; "targetVersion": number }
export type MigrationRunPayload = { "dryRun"?: boolean }
export type MigrationRunResult = MigrationStatusDTO
export type MigrationStatusPayload = Record<string, never>
export type MigrationStatusResult = MigrationStatusDTO
export type NodeCompletePayload = { "nodeId": ULID }
export type NodeCompleteResult = { "completed": boolean }
export type NodeFailPayload = { "nodeId": ULID }
export type NodeFailResult = { "failed": boolean }
export type NodeListPayload = { "planId": ULID }
export type NodeListResult = { "items": Array<PlanNodeDTO> }
export type NodeStartPayload = { "nodeId": ULID }
export type NodeStartResult = { "started": boolean }
export type OntologyEdgeListPayload = { "nodeId": ULID; "direction"?: "outgoing" | "incoming" }
export type OntologyEdgeListResult = { "items": Array<OntologyEdgeDTO> }
export type OntologyNodeGetPayload = { "id": ULID }
export type OntologyNodeGetResult = OntologyNodeDTO
export type OntologyNodeListPayload = { "projectId": ULID; "type"?: OntologyNodeType }
export type OntologyNodeListResult = { "items": Array<OntologyNodeDTO> }
export type OntologyNodeSearchPayload = { "projectId": ULID; "query": string }
export type OntologyNodeSearchResult = { "items": Array<OntologyNodeDTO> }
export type PlanActivatePayload = { "planId": ULID }
export type PlanActivateResult = { "activated": boolean }
export type PlanCompletePayload = { "planId": ULID }
export type PlanCompleteResult = { "completed": boolean }
export type PlanGetPayload = { "id": ULID }
export type PlanGetResult = PlanDTO
export type PlanListPayload = { "projectId": ULID }
export type PlanListResult = { "items": Array<PlanDTO> }
export type PlanPausePayload = { "planId": ULID }
export type PlanPauseResult = { "paused": boolean }
export type PlanResumePayload = { "planId": ULID }
export type PlanResumeResult = { "resumed": boolean }
export type ProjectCreatePayload = { "name": string }
export type ProjectCreateResult = ProjectDTO
export type ProjectListPayload = { "status"?: ProjectStatus }
export type ProjectListResult = { "items": Array<ProjectDTO> }
export type ProviderCreatePayload = { "name": string; "protocol": ProviderProtocol; "baseUrl": string; "models": Array<ModelDTO>; "credentialSubmissionId"?: ULID; "status"?: ProviderStatus }
export type ProviderCreateResult = ProviderDTO
export type ProviderCredentialSubmitPayload = { "scope": { "providerId": ULID } | { "draftFingerprint": string }; "protocol"?: ProviderProtocol; "origin"?: string; "request": object; "credential": string }
export type ProviderCredentialSubmitResult = { "credentialSubmissionId": ULID; "expiresAt": string; "providerId": ULID; "expiresInSeconds": number }
export type ProviderDeletePayload = { "id": ULID; "expectedVersion": number }
export type ProviderDeleteResult = { "deleted": true }
export type ProviderGetPayload = { "id": ULID }
export type ProviderGetResult = ProviderDTO
export type ProviderListPayload = { "protocol"?: ProviderProtocol }
export type ProviderListResult = { "items": Array<ProviderDTO> }
export type ProviderModelSyncPayload = { "providerId": ULID; "expectedVersion": number }
export type ProviderModelSyncResult = { "models": Array<ModelDTO>; "warnings": Array<string>; "version": number }
export type ProviderTestPayload = { "providerId": ULID; "modelId"?: string }
export type ProviderTestResult = { "status": "passed" | "failed"; "stage": "resolve" | "connect" | "authenticate" | "request" | "response"; "httpStatus"?: number; "latencyMs": number; "retryable": boolean; "errorCode"?: string; "sanitizedMessage"?: string; "testedAt": string }
export type ProviderUpdatePayload = { "id": ULID; "name"?: string; "protocol"?: ProviderProtocol; "baseUrl"?: string; "models"?: Array<ModelDTO>; "credentialSubmissionId"?: ULID; "status"?: ProviderStatus; "expectedVersion": number }
export type ProviderUpdateResult = ProviderDTO
export type ReviewApprovePayload = { "reviewId": ULID; "reviewerNote"?: string }
export type ReviewApproveResult = { "approved": boolean }
export type ReviewListPayload = { "planId": ULID }
export type ReviewListResult = { "items": Array<ReviewDTO> }
export type ReviewRejectPayload = { "reviewId": ULID; "reviewerNote"?: string }
export type ReviewRejectResult = { "rejected": boolean }
export type SessionCreatePayload = { "projectId": ULID; "title": string }
export type SessionCreateResult = SessionDTO
export type SessionListPayload = { "projectId": ULID }
export type SessionListResult = { "items": Array<SessionDTO> }
export type SkillDeprecatePayload = { "id": ULID }
export type SkillDeprecateResult = { "deprecated": boolean }
export type SkillDisablePayload = { "id": ULID }
export type SkillDisableResult = { "disabled": boolean }
export type SkillGetPayload = { "id": ULID }
export type SkillGetResult = SkillDTO
export type SkillListPayload = { "status"?: SkillStatus }
export type SkillListResult = { "items": Array<SkillDTO> }
export type SkillMatchPayload = { "query": string }
export type SkillMatchResult = { "items": Array<SkillMatchDTO> }
export type SkillPublishPayload = { "id": ULID }
export type SkillPublishResult = { "published": boolean }
export type StageCreatePayload = { "projectId": ULID; "phase": number; "title": string }
export type StageCreateResult = StageDTO
export type StageListPayload = { "projectId": ULID }
export type StageListResult = { "items": Array<StageDTO> }
export type StreamCancelPayload = { "streamId": ULID }
export type StreamCancelResult = { "cancelled": boolean }
export type SystemHealthPayload = Record<string, never>
export type SystemHealthResult = { "engine": string; "version": string; "protocol": "1.0" }
export interface RpcHandshake {
  rpcMajor: 1
  rpcMinor: number
  clientPid: number
  sessionNonce: string
}
