// Code generated from discovered api/bridge/v1 and api/rpc/v1 schemas. DO NOT EDIT.
export const BRIDGE_METHODS = ["chat.start", "diagnostics.export", "migration.inspect", "migration.run", "migration.status", "project.create", "project.list", "provider.create", "provider.credential.submit", "provider.delete", "provider.get", "provider.list", "provider.model.sync", "provider.test", "provider.update", "session.create", "session.list", "stream.cancel", "system.health"] as const
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
export type MigrationStatusDTO = { "state": "idle" | "running" | "completed" | "failed"; "processed": number; "total": number }
export type ChatStartPayload = { "providerId": ULID; "modelId": string; "messages": Array<{ "role": "system" | "user" | "assistant"; "content": string }> }
export type ChatStartResult = { "streamId": ULID }
export type DiagnosticsExportPayload = { "includeLogs"?: boolean; "redactPaths"?: boolean }
export type DiagnosticsExportResult = { "path": string; "createdAt": string; "redacted": true }
export type MigrationInspectPayload = Record<string, never>
export type MigrationInspectResult = { "required": boolean; "items": number; "sourceVersion": number; "targetVersion": number }
export type MigrationRunPayload = { "dryRun"?: boolean }
export type MigrationRunResult = MigrationStatusDTO
export type MigrationStatusPayload = Record<string, never>
export type MigrationStatusResult = MigrationStatusDTO
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
export type SessionCreatePayload = { "projectId": ULID; "title": string }
export type SessionCreateResult = SessionDTO
export type SessionListPayload = { "projectId": ULID }
export type SessionListResult = { "items": Array<SessionDTO> }
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
