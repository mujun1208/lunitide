import {
  BRIDGE_VERSION, type BridgeMethod, type BridgeRequest, type BridgeResponse,
  type ProviderCreatePayload, type ProviderCreateResult, type ProviderCredentialSubmitPayload,
  type ProviderCredentialSubmitResult, type ProviderCredentialRevealPayload, type ProviderCredentialRevealResult, type ProviderDeletePayload, type ProviderDeleteResult,
  type ProviderGetPayload, type ProviderGetResult, type ProviderListPayload, type ProviderListResult,
  type ProviderModelSyncPayload, type ProviderModelSyncResult, type ProviderTestPayload,
  type ProviderTestResult, type ProviderUpdatePayload, type ProviderUpdateResult,
  type ChatStartPayload, type ChatStartResult, type ChatToolApprovePayload, type ChatToolApproveResult, type StreamCancelResult,
  type ProjectCreatePayload, type ProjectCreateResult, type ProjectListPayload, type ProjectListResult,
  type ProjectUpdatePayload, type ProjectUpdateResult, type ProjectPublishPayload, type ProjectPublishResult,
  type ProjectClosePayload, type ProjectCloseResult, type ProjectReopenPayload, type ProjectReopenResult,
  type SessionCreatePayload, type SessionCreateResult, type SessionListPayload, type SessionListResult, type SessionUpdatePayload, type SessionUpdateResult,
  type MessageAppendPayload, type MessageAppendResult, type MessageListPayload, type MessageListResult, type MessageRewindPayload, type MessageRewindResult,
  type StageCreatePayload, type StageCreateResult, type StageListPayload, type StageListResult, type StageUpdatePayload, type StageUpdateResult,
  type DeliverableListPayload, type DeliverableListResult, type DeliverableUpsertPayload, type DeliverableUpsertResult, type DeliverableConfirmGatePayload, type DeliverableConfirmGateResult,
  type DbQueryPayload, type DbQueryResult, type OpenapiParsePayload, type OpenapiParseResult,
  type ProjectAttachmentGetPayload, type ProjectAttachmentGetResult, type ProjectAttachmentIngestPayload, type ProjectAttachmentIngestResult, type ProjectAttachmentListPayload, type ProjectAttachmentListResult,
  type ReleaseBuildPackagePayload, type ReleaseBuildPackageResult, type ReleaseCreateRevisionPayload, type ReleaseCreateRevisionResult, type ReleaseGetRevisionPayload, type ReleaseGetRevisionResult, type ReleaseGetPackagePayload, type ReleaseGetPackageResult, type ReleasePromotePayload, type ReleasePromoteResult, type ReleaseGetPromotionPayload, type ReleaseGetPromotionResult, type ReleaseRollbackPayload, type ReleaseRollbackResult,
  type SkillImportDiscoverPayload, type SkillImportDiscoverResult, type SkillImportInspectPayload, type SkillImportInspectResult, type SkillImportSubmitPayload, type SkillImportSubmitResult, type SkillImportApprovePayload, type SkillImportApproveResult, type SkillImportRejectPayload, type SkillImportRejectResult, type SkillImportRevokePayload, type SkillImportRevokeResult,
  type PlanGetPayload, type PlanGetResult, type PlanListPayload, type PlanListResult,
  type PlanCreatePayload, type PlanCreateResult,
  type PlanActivatePayload, type PlanActivateResult, type PlanCompletePayload, type PlanCompleteResult,
  type PlanPausePayload, type PlanPauseResult, type PlanResumePayload, type PlanResumeResult,
  type NodeListPayload, type NodeListResult, type NodeStartPayload, type NodeStartResult,
  type NodeCreatePayload, type NodeCreateResult,
  type NodeCompletePayload, type NodeCompleteResult, type NodeFailPayload, type NodeFailResult,
  type ReviewListPayload, type ReviewListResult, type ReviewApprovePayload, type ReviewApproveResult,
  type ReviewRejectPayload, type ReviewRejectResult,
  type MemoryGetPayload, type MemoryGetResult, type MemoryListPayload, type MemoryListResult,
  type MemorySearchPayload, type MemorySearchResult, type MemoryUpdatePayload, type MemoryUpdateResult,
  type MemoryDeletePayload, type MemoryDeleteResult, type MemoryCreatePayload, type MemoryCreateResult,
  type MemoryConfirmCandidatePayload, type MemoryConfirmCandidateResult,
  type MemoryNominatePayload, type MemoryNominateResult,
  type MemoryNominationListPayload, type MemoryNominationListResult,
  type MemoryNominationWithdrawPayload, type MemoryNominationWithdrawResult,
  type MemoryStatsPayload, type MemoryStatsResult,
  type MemoryFactsListPayload, type MemoryFactsListResult,
  type MemoryFactsFlagPayload, type MemoryFactsFlagResult,
  type MemoryTracesListPayload, type MemoryTracesListResult,
  type MemoryGrowthListPayload, type MemoryGrowthListResult,
  type MemoryGrowthDecidePayload, type MemoryGrowthDecideResult,
  type MemorySettingsGetPayload, type MemorySettingsGetResult,
  type MemorySettingsUpdatePayload, type MemorySettingsUpdateResult,
  type MemoryExportPayload, type MemoryExportResult,
  type MemoryPurgePayload, type MemoryPurgeResult,
  type RunQueueInputPayload, type RunQueueInputResult,
  type RunQueueListPayload, type RunQueueListResult,
  type RunQueueWithdrawPayload, type RunQueueWithdrawResult,
  type RunQueueConsumePayload, type RunQueueConsumeResult,
  type McMarketListPayload, type McMarketListResult,
  type McMarketDetailPayload, type McMarketDetailResult,
  type McConfigValidatePayload, type McConfigValidateResult,
  type McConfirmTokenPayload, type McConfirmTokenResult,
  type McConnectorInstallPayload, type McConnectorInstallResult,
  type McConnectorUninstallPayload, type McConnectorUninstallResult,
  type McConnectorUpdatePayload, type McConnectorUpdateResult,
  type McConnectorUsagePayload, type McConnectorUsageResult,
  type McTombstoneCheckResult,
  type BrSettingsGetPayload, type BrSettingsGetResult,
  type BrSettingsUpdatePayload, type BrSettingsUpdateResult,
  type BrModeDetectPayload, type BrModeDetectResult,
  type BrSessionConnectPayload, type BrSessionConnectResult,
  type BrSessionListPayload, type BrSessionListResult,
  type BrSessionDisconnectPayload, type BrSessionDisconnectResult,
  type BrNavigatePayload, type BrNavigateResult,
  type BrDataUsagePayload, type BrDataUsageResult,
  type BrDataClearPayload, type BrDataClearResult,
  type BrPermissionListPayload, type BrPermissionListResult,
  type BrPermissionRequestPayload, type BrPermissionRequestResult,
  type BrPermissionDecidePayload, type BrPermissionDecideResult,
  type BrPermissionPolicyPayload, type BrPermissionPolicyResult,
  type CcGetConfigPayload, type CcGetConfigResult,
  type CcUpdateConfigPayload, type CcUpdateConfigResult,
  type CcGetAuditLogPayload, type CcGetAuditLogResult,
  type CcEmergencyStopPayload, type CcEmergencyStopResult,
  type ImChannelsGetPayload, type ImChannelsGetResult,
  type ImChannelsSetPayload, type ImChannelsSetResult,
  type FeedbackRecordPayload, type FeedbackRecordResult, type FeedbackCandidatesPayload, type FeedbackCandidatesResult,
  type OntologyNodeGetPayload, type OntologyNodeGetResult, type OntologyNodeListPayload, type OntologyNodeListResult,
  type OntologyNodeSearchPayload, type OntologyNodeSearchResult, type OntologyEdgeListPayload, type OntologyEdgeListResult,
  type OntologyNodeCreatePayload, type OntologyNodeCreateResult, type OntologyNodeUpdatePayload, type OntologyNodeUpdateResult,
  type OntologyNodeDeletePayload, type OntologyNodeDeleteResult,
  type OntologyEdgeCreatePayload, type OntologyEdgeCreateResult, type OntologyEdgeUpdatePayload, type OntologyEdgeUpdateResult,
  type OntologyEdgeDeletePayload, type OntologyEdgeDeleteResult,
  type SkillGetPayload, type SkillGetResult, type SkillListPayload, type SkillListResult,
  type SkillMatchPayload, type SkillMatchResult, type SkillPublishPayload, type SkillPublishResult,
  type SkillDeprecatePayload, type SkillDeprecateResult, type SkillDisablePayload, type SkillDisableResult,
  type SkillCreatePayload, type SkillCreateResult, type SkillUpdatePayload, type SkillUpdateResult,
  type SkillDeletePayload, type SkillDeleteResult,
  type SkillInvokePayload,type SkillInvokeResult,type SkillExecutePayload,type SkillExecuteResult,
  type SkillCatalogListPayload,type SkillCatalogListResult,type SkillInstallPayload,type SkillInstallResult,
  type SkillCategorySetPayload,type SkillCategorySetResult,
  type UiThemeSetPayload, type UiThemeSetResult, type SystemSettingsOpenPayload, type SystemSettingsOpenResult,
  type BrowserOpenPayload, type BrowserOpenResult, type BrowserCloseResult,
  type WorkspaceRootGetResult,type WorkspaceRootSelectResult,type WorkspaceRootClearResult,type WorkspaceListResult,type WorkspaceReadResult,type WorkspaceOpenPayload,type WorkspaceOpenResult,
  type ContextStatusPayload, type ContextStatusResult,
  type ContextCompactPreviewPayload, type ContextCompactPreviewResult,
  type ContextCompactCommitPayload, type ContextCompactCommitResult,
  type ContextCompactCancelPayload, type ContextCompactCancelResult,
  type ContextHandoffCreatePayload, type ContextHandoffCreateResult,
  type ContextHandoffImportPayload, type ContextHandoffImportResult,
  type ContextHandoffInspectPayload, type ContextHandoffInspectResult,
  type ContextHandoffListPayload, type ContextHandoffListResult,
  type ContextHandoffListImportsPayload, type ContextHandoffListImportsResult,
  type ContextHandoffRevokePayload, type ContextHandoffRevokeResult,
  type AttachmentIngestPayload, type AttachmentIngestResult,
  type AttachmentGetPayload, type AttachmentGetResult,
  type AttachmentListPayload, type AttachmentListResult,
  type TemplateListPayload, type TemplateListResult,
  type TemplateCreatePayload, type TemplateCreateResult,
  type TemplateFileStagePayload, type TemplateFileStageResult,
  type TemplateEnablePayload, type TemplateEnableResult,
  type TemplateVoidPayload, type TemplateVoidResult,
  type TemplateRestorePayload, type TemplateRestoreResult,
  type TemplateDeletePayload, type TemplateDeleteResult,
  type AttachmentDeletePayload, type AttachmentDeleteResult,
  type AttachmentUploadBeginPayload,type AttachmentUploadBeginResult,type AttachmentUploadChunkPayload,type AttachmentUploadChunkResult,type AttachmentUploadCommitPayload,type AttachmentUploadCommitResult,type AttachmentUploadAbortPayload,type AttachmentUploadAbortResult,
  type TerminalStartPayload,type TerminalStartResult,type TerminalInputResult,type TerminalResizeResult,type TerminalCloseResult,
  type ProjectDeletePayload, type ProjectDeleteResult,
  type SessionDeletePayload, type SessionDeleteResult,
  type PlanDTO, type PlanNodeDTO, type ReviewDTO, type MemoryDTO, type OntologyNodeDTO, type OntologyEdgeDTO, type SkillDTO, type SkillMatchDTO,
  type PlanStatus, type NodeStatus, type RiskLevel, type ReviewStatus, type MemoryLayer, type MemoryScope,
	 type PlanTodoCreatePayload,type PlanTodoCreateResult,type PlanRunStartPayload,type PlanRunStartResult,type PlanRunTreePayload,type PlanRunTreeResult,type PlanRunSpawnPayload,type PlanRunSpawnResult,type PlanRunJoinPayload,type PlanRunJoinResult,type PlanRunCancelPayload,type PlanRunCancelResult,
  type AgentRunStartPayload,type AgentRunStartResult,type AgentRunGetPayload,type AgentRunGetResult,type AgentRunCancelPayload,type AgentRunCancelResult,type AgentRunResumePayload,type AgentRunResumeResult,type AgentRunReconcilePayload,type AgentRunReconcileResult,
  type CapabilityListResult,type WorkspaceRegisterPayload,type WorkspaceRegisterResult,type WorkspaceGrantPayload,type WorkspaceGrantResult,type WorkspaceLeasePayload,type WorkspaceLeaseResult,
  type ReviewDecidePayload,type ReviewDecideResult,type ChangesetPreviewPayload,type ChangesetPreviewResult,type ChangesetApplyPayload,type ChangesetApplyResult,type ChangesetRevertPayload,type ChangesetRevertResult,
  type CommandReviewRequestPayload,type CommandReviewRequestResult,type CommandStartPayload,type CommandStartResult,type CommandGetPayload,type CommandGetResult,type CommandCancelPayload,type CommandCancelResult,type WebFetchPayload,type WebFetchResult,type WebSearchPayload,type WebSearchResult,type RunPlanPutPayload,type RunPlanPutResult,type EvidenceListPayload,type EvidenceListResult,
  type OntologyNodeType, type OntologyEdgeType, type SkillStatus, type SkillPermission,
  type McpListPayload,type McpListResult,type McpAddPayload,type McpAddResult,type McpTogglePayload,type McpToggleResult,type McpHealthPayload,type McpHealthResult,type McpMarketSearchPayload,type McpMarketSearchResult,type Mcp6PresetsListPayload,type Mcp6PresetsListResult,
  type PluginListPayload,type PluginListResult,type PluginInstallPayload,type PluginInstallResult,type PluginTogglePayload,type PluginToggleResult,type PluginUninstallPayload,type PluginUninstallResult,type PluginUpgradePayload,type PluginUpgradeResult,type PluginMarketSearchPayload,type PluginMarketSearchResult,type PluginMarketDetailPayload,type PluginMarketDetailResult,type PluginDevCreatePayload,type PluginDevCreateResult,
  type ExpertListPayload,type ExpertListResult,type ExpertDetailPayload,type ExpertDetailResult,type ExpertCreatePayload,type ExpertCreateResult,type ExpertUpdatePayload,type ExpertUpdateResult,type ExpertTogglePayload,type ExpertToggleResult,type ExpertArchivePayload,type ExpertArchiveResult,type ExpertMountPayload,type ExpertMountResult,type ExpertMountingGetPayload,type ExpertMountingGetResult,type ExpertScenarioCreatePayload,type ExpertScenarioCreateResult,type ExpertScenarioListPayload,type ExpertScenarioListResult,type ExpertScenarioDeletePayload,type ExpertScenarioDeleteResult,type ExpertCatalogListPayload,type ExpertCatalogListResult,type ExpertInstallPayload,type ExpertInstallResult,
  type OrgSummaryPayload,type OrgSummaryResult,type OrgCreatePayload,type OrgCreateResult,type OrgSwitchPayload,type OrgSwitchResult,type OrgActivatePayload,type OrgActivateResult,type OrgSuspendPayload,type OrgSuspendResult,type OrgSpaceListPayload,type OrgSpaceListResult,type OrgSpaceCreatePayload,type OrgSpaceCreateResult,type OrgMemberListPayload,type OrgMemberListResult,type OrgMemberInvitePayload,type OrgMemberInviteResult,type OrgMemberRevokePayload,type OrgMemberRevokeResult,
  type IdentityDTO, type IdentityUpdatePayload, type IdentityPasswordSetPayload, type IdentityUnlockPayload,
  type PeopleListResult, type PeoplePairPayload, type PeopleDiscoverySetPayload, type PeopleDiscoveryGetResult,
  type PeopleThreadListResult, type PeopleThreadOpenPayload, type PeopleThreadOpenResult,
  type PeopleThreadSendPayload, type PeopleThreadSendResult, type PeopleGroupCreatePayload,
  type PeopleFileDecidePayload, type PeopleContactDTO, type PeopleFileOfferDTO, type PeopleThreadDTO,
  type PeoplePeerAddPayload, type PeopleContactUpdatePayload, type PeopleThreadTypingPayload,
  type PeopleFileStagePayload, type PeopleFileStageResult, type PeopleFilePickPayload, type PeopleFilePickResult,
  type PeopleScreenCapturePayload, type PeopleScreenCaptureResult,
  type MeetingsListResult, type MeetingsStartPayload, type MeetingsStartResult,
  type MeetingsAppendPayload, type MeetingsAppendResult, type MeetingsStopPayload, type MeetingsStopResult,
  type MeetingsGetPayload, type MeetingsGetResult, type MeetingsSummarizePayload, type MeetingsSummarizeResult,
  type MeetingsHeartbeatPayload, type MeetingsHeartbeatResult,
  type MeetingsAudioAppendPayload, type MeetingsAudioAppendResult, type MeetingsCatchupPayload, type MeetingsCatchupResult,
  type MeetingsLoopbackPollPayload, type MeetingsLoopbackPollResult,
  type MeetingsExportPayload, type MeetingsExportResult, type MeetingsUpdatePayload, type MeetingsDeletePayload, type MeetingsDeleteResult, type MeetingDTO, type MeetingSegmentDTO,
  type AppUpdateCheckPayload,type AppUpdateCheckResult,type AppUpdateInstallPayload,type AppUpdateInstallResult,
  type TtsVoicesResult,type TtsVoicesPayload,type TtsCancelResult,type TtsSynthesizePayload,type TtsSynthesizeResult,type TtsRefAudiosPayload,type TtsRefAudiosResult,type TtsEnsureRefEnginePayload,type TtsEnsureRefEngineResult,
  type VoiceStatusResult,type VoiceInstallPayload,type VoiceInstallResult,type VoiceSelectPayload,type VoiceSelectResult,type VoiceStartPayload,type VoiceStartResult,type VoiceAppendPayload,type VoiceAppendResult,type VoiceFinishPayload,type VoiceFinishResult,type VoiceStopPayload,type VoiceStopResult,
  type OmniStatusResult,type OmniInstallResult,type OmniEnsureResult,type OmniStartPayload,type OmniStartResult,type OmniAppendPayload,type OmniAppendResult,type OmniStopPayload,type OmniStopResult,
  type SubagentSpawnPayload,type SubagentSpawnResult,type SubagentJoinPayload,type SubagentJoinResult,type SubagentTreePayload,type SubagentTreeResult,
  type ConversationsRootGetResult,type ConversationsRootSelectResult,type ConversationsRootSetPayload,type ConversationsRootSetResult,
  type SessionFolderGetPayload,type SessionFolderGetResult,type SessionFolderListPayload,type SessionFolderListResult,type SessionFolderOpenPayload,type SessionFolderOpenResult,
  type CollabGateStatusPayload,type CollabGateStatusResult,type CollabGateEvaluatePayload,type CollabGateEvaluateResult,type CollabGateConfirmPayload,type CollabGateConfirmResult,
  type DiagnosticsExportPayload,type DiagnosticsExportResult,type SystemHealthResult,
  type ToolsCommandPolicyGetResult,type ToolsCommandPolicySetPayload,type ToolsCommandPolicySetResult,
  type ToolsHooksPolicyGetResult,type ToolsHooksPolicySetPayload,type ToolsHooksPolicySetResult,type ToolsHooksEventsListPayload,type ToolsHooksEventsListResult,
  type WorkspaceArtifactReviewListPayload,type WorkspaceArtifactReviewListResult,type WorkspaceArtifactReviewAppendPayload,type WorkspaceArtifactReviewAppendResult,type WorkspaceArtifactPreviewPayload,type WorkspaceArtifactPreviewResult,type WorkspaceArtifactExportPayload,type WorkspaceArtifactExportResult,
  type AutomationJobListResult,type AutomationJobSetPayload,type AutomationJobSetResult,type AutomationJobDeletePayload,type AutomationJobDeleteResult,type AutomationJobTriggerPayload,type AutomationJobTriggerResult,type AutomationRunListPayload,type AutomationRunListResult,type AutomationStatusResult,
} from '../generated/bridge'

export class BridgeClientError extends Error {
  constructor(message: string, public readonly code: string, public readonly retryable: boolean, public readonly correlationId: string) {
    super(message); this.name = 'BridgeClientError'
  }
}
export interface LocalWorkspaceBridge{root():Promise<WorkspaceRootGetResult>;select():Promise<WorkspaceRootSelectResult>;clear():Promise<WorkspaceRootClearResult>;list(path?:string):Promise<WorkspaceListResult>;read(path:string):Promise<WorkspaceReadResult>;open(payload?:WorkspaceOpenPayload):Promise<WorkspaceOpenResult>}
export interface WebViewTransport {
  postMessage(value: unknown): void
  addEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void
  removeEventListener(type: 'message', listener: (event: MessageEvent<BridgeResponse>) => void): void
}
declare global { interface Window { chrome?: { webview?: WebViewTransport } } }

export type TerminalEvent={type:'output';data:string}|{type:'exit';exitCode:number}
export interface TerminalSession{terminalId:string;input(data:string):Promise<boolean>;resize(cols:number,rows:number):Promise<boolean>;close():Promise<boolean>;dispose():void}
export interface TerminalBridge{start(payload:TerminalStartPayload,onEvent:(event:TerminalEvent)=>void):Promise<TerminalSession>;dispose():void}

export type MutationMethod = 'project.create'|'project.delete'|'project.update'|'project.publish'|'project.close'|'project.reopen'|'project.advanceStatus'|'session.create'|'session.update'|'session.delete'|'session.experts.set'|'message.append'|'message.rewind'|'provider.create'|'provider.update'|'provider.delete'|'provider.model.sync'|'stage.create'|'stage.update'|'deliverable.upsert'|'deliverable.confirmGate'|'template.create'|'template.delete'|'template.enable'|'template.restore'|'template.void'|'release.buildPackage'|'release.createRevision'|'release.promote'|'release.rollback'|'skill.import.discover'|'skill.import.inspect'|'skill.import.submit'|'skill.import.approve'|'skill.import.reject'|'skill.import.revoke'|'plan.create'|'node.create'|'memory.create'|'memory.confirmCandidate'|'ontology.node.create'|'ontology.node.update'|'ontology.node.delete'|'ontology.edge.create'|'ontology.edge.update'|'ontology.edge.delete'|'skill.create'|'skill.update'|'skill.delete'|'skill.category.set'|'attachment.ingest'|'attachment.delete'|'agent.run.start'|'agent.run.cancel'|'agent.run.resume'|'agent.run.reconcile'|'workspace.register'|'workspace.grant'|'workspace.lease'|'review.decide'|'changeset.preview'|'changeset.apply'|'changeset.revert'|'command.review.request'|'command.start'|'command.cancel'|'web.fetch'|'web.search'|'run.plan.put'|'mcp.add'|'mcp.toggle'|'plugin.install'|'plugin.toggle'|'plugin.uninstall'|'plugin.upgrade'|'plugin.dev.create'|'expert.create'|'expert.update'|'expert.toggle'|'expert.archive'|'expert.mount'|'expert.scenario.create'|'expert.scenario.delete'|'appUpdate.install'|'subagent.spawn'|'org.create'|'org.switch'|'org.activate'|'org.suspend'|'org.space.create'|'org.member.invite'|'org.member.revoke'|'mc.confirm.token'|'mc.connector.install'|'mc.connector.uninstall'|'mc.connector.update'
export type MutationOptions<T extends object> = { attempt?: MutationAttempt<T> }
export interface MutationAttempt<T extends object> { readonly method: MutationMethod; readonly payload: Readonly<T>; readonly idempotencyKey: string; readonly fingerprint: string }
const stable = (value: unknown): string => value === null || typeof value !== 'object' ? JSON.stringify(value) : Array.isArray(value) ? `[${value.map(stable).join(',')}]` : `{${Object.keys(value as object).sort().map(k=>`${JSON.stringify(k)}:${stable((value as Record<string,unknown>)[k])}`).join(',')}}`
const clone = <T>(value:T):T => structuredClone(value)
const freeze = <T>(value:T):T => { if(value && typeof value==='object'){Object.freeze(value);Object.values(value as object).forEach(freeze)}return value }
export function createMutationAttempt<T extends object>(method: MutationMethod, payload: T): MutationAttempt<T> { const copy=freeze(clone(payload)); return Object.freeze({method,payload:copy,idempotencyKey:ulid(),fingerprint:stable(copy)}) }
const deeplyFrozen=(value:unknown):boolean=>!value||typeof value!=='object'||Object.isFrozen(value)&&Object.values(value).every(deeplyFrozen)
function checkedAttempt<T extends object>(method:MutationMethod,payload:T,attempt?:MutationAttempt<T>):{payload:T;key:string} {
 if(!attempt)return{payload:clone(payload),key:ulid()}
 const ownFingerprint=stable(attempt.payload)
 if(!Object.isFrozen(attempt)||!deeplyFrozen(attempt.payload)||attempt.method!==method||attempt.fingerprint!==ownFingerprint||ownFingerprint!==stable(payload)||!isULID(attempt.idempotencyKey))throw new BridgeClientError('MutationAttempt 与请求负载不匹配','MUTATION_ATTEMPT_MISMATCH',false,'renderer')
 return{payload:clone(attempt.payload) as T,key:attempt.idempotencyKey}
}

export interface ProviderBridge {
  get(payload: ProviderGetPayload): Promise<ProviderGetResult>; list(payload?: ProviderListPayload): Promise<ProviderListResult>
  create(payload: ProviderCreatePayload, options?:MutationOptions<ProviderCreatePayload>): Promise<ProviderCreateResult>
  update(payload: ProviderUpdatePayload, options?:MutationOptions<ProviderUpdatePayload>): Promise<ProviderUpdateResult>
  delete(payload: ProviderDeletePayload, options?:MutationOptions<ProviderDeletePayload>): Promise<ProviderDeleteResult>
  revealCredential(payload: ProviderCredentialRevealPayload): Promise<ProviderCredentialRevealResult>
  submitCredential(payload: ProviderCredentialSubmitPayload): Promise<ProviderCredentialSubmitResult>
  syncModels(payload: ProviderModelSyncPayload, options?:MutationOptions<ProviderModelSyncPayload>): Promise<ProviderModelSyncResult>
  test(payload: ProviderTestPayload): Promise<ProviderTestResult>
}
export type ProjectAdvanceStatusPayload = { id: string; version: number; phase: number }
export type ProjectAdvanceStatusResult = ProjectCreateResult
export interface ProjectBridge {
  list(payload?:ProjectListPayload):Promise<ProjectListResult>
  create(payload:ProjectCreatePayload,options?:MutationOptions<ProjectCreatePayload>):Promise<ProjectCreateResult>
  update(payload:ProjectUpdatePayload,options?:MutationOptions<ProjectUpdatePayload>):Promise<ProjectUpdateResult>
  publish(payload:ProjectPublishPayload,options?:MutationOptions<ProjectPublishPayload>):Promise<ProjectPublishResult>
  close(payload:ProjectClosePayload,options?:MutationOptions<ProjectClosePayload>):Promise<ProjectCloseResult>
  reopen(payload:ProjectReopenPayload&{reason:string},options?:MutationOptions<ProjectReopenPayload&{reason:string}>):Promise<ProjectReopenResult>
  advanceStatus(payload:ProjectAdvanceStatusPayload,options?:MutationOptions<ProjectAdvanceStatusPayload>):Promise<ProjectAdvanceStatusResult>
  delete(payload:ProjectDeletePayload,options?:MutationOptions<ProjectDeletePayload>):Promise<ProjectDeleteResult>
}
export interface SessionBridge { list(payload:SessionListPayload):Promise<SessionListResult>; create(payload:SessionCreatePayload,options?:MutationOptions<SessionCreatePayload>):Promise<SessionCreateResult>; update(payload:SessionUpdatePayload,options?:MutationOptions<SessionUpdatePayload>):Promise<SessionUpdateResult>; delete(payload:SessionDeletePayload,options?:MutationOptions<SessionDeletePayload>):Promise<SessionDeleteResult> }
export interface MessageBridge { list(payload:MessageListPayload):Promise<MessageListResult>; append(payload:MessageAppendPayload,options?:MutationOptions<MessageAppendPayload>):Promise<MessageAppendResult>; rewind?(payload:MessageRewindPayload,options?:MutationOptions<MessageRewindPayload>):Promise<MessageRewindResult> }
export interface UIThemeBridge { set(payload:UiThemeSetPayload):Promise<UiThemeSetResult> }
export interface SystemSettingsBridge { open(payload:SystemSettingsOpenPayload):Promise<SystemSettingsOpenResult> }
export interface BrowserBridge { open(payload:BrowserOpenPayload):Promise<BrowserOpenResult>; close():Promise<BrowserCloseResult> }
const mutationMethods = new Set<BridgeMethod>(['project.create','project.delete','project.update','project.publish','project.close','project.reopen','project.advanceStatus' as BridgeMethod,'session.create','session.update','session.delete','session.experts.set','message.append','message.rewind','provider.create','provider.update','provider.delete','provider.model.sync','stage.create','stage.update' as BridgeMethod,'deliverable.upsert','deliverable.confirmGate','template.create','template.delete','template.enable','template.restore','template.void','release.buildPackage','release.createRevision','release.promote','release.rollback','skill.import.discover','skill.import.inspect','skill.import.submit','skill.import.approve','skill.import.reject','skill.import.revoke','plan.create','node.create','memory.create','memory.confirmCandidate','ontology.node.create','ontology.node.update','ontology.node.delete','ontology.edge.create','ontology.edge.update','ontology.edge.delete','skill.create','skill.update','skill.delete','skill.category.set','attachment.ingest','attachment.delete','agent.run.start','agent.run.cancel','agent.run.resume','agent.run.reconcile','workspace.register','workspace.grant','workspace.lease','review.decide','changeset.preview','changeset.apply','changeset.revert','command.review.request','command.start','command.cancel','web.fetch','web.search','run.plan.put','mcp.add','mcp.toggle','plugin.install','plugin.toggle','plugin.uninstall','plugin.upgrade','plugin.dev.create','expert.create','expert.update','expert.toggle','expert.archive','expert.mount','expert.scenario.create','expert.scenario.delete','appUpdate.install','subagent.spawn','org.create','org.switch','org.activate','org.suspend','org.space.create','org.member.invite','org.member.revoke','mc.confirm.token','mc.connector.install','mc.connector.uninstall','mc.connector.update'])
function ulid(): string { const a='0123456789ABCDEFGHJKMNPQRSTVWXYZ',b=crypto.getRandomValues(new Uint8Array(10));let v=(BigInt(Date.now())<<80n)|b.reduce((n,x)=>(n<<8n)|BigInt(x),0n),r='';for(let i=0;i<26;i++){r=a[Number(v&31n)]+r;v>>=5n}return r }
const isObj=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)
const exact=(v:Record<string,unknown>,required:string[],optional:string[]=[])=>required.every(k=>k in v)&&Object.keys(v).every(k=>required.includes(k)||optional.includes(k))
const isULID=(v:unknown)=>typeof v==='string'&&/^[0-7][0-9A-HJKMNP-TV-Z]{25}$/.test(v)
const isTime=(v:unknown)=>{if(typeof v!=='string')return false;const m=/^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(\.\d+)?(Z|[+-]\d\d:\d\d)$/.exec(v);if(!m)return false;const y=+m[1],mo=+m[2],d=+m[3],h=+m[4],mi=+m[5],s=+m[6];if(mo<1||mo>12||d<1||d>31||h>23||mi>59||s>59)return false;const days=[31,28+(y%4===0&&(y%100!==0||y%400===0)?1:0),31,30,31,30,31,31,30,31,30,31];if(d>days[mo-1])return false;return !Number.isNaN(Date.parse(v))}
const normalizedProjectName=(v:string)=>v.split(/\p{White_Space}+/u).filter(Boolean).join(' ')
const isProjectCode=(v:unknown)=>typeof v==='string'&&/^ITM[0-9]{3,12}$/.test(v)
const isDate=(v:unknown)=>typeof v==='string'&&(v===''||/^\d{4}-\d{2}-\d{2}$/.test(v))
const PROJECT_ADVANCE_METHOD='project.advanceStatus' as const
const isProject=(v:unknown)=>isObj(v)&&exact(v,['id','name','projectCode','type','status','createdAt','updatedAt','version'],['description','summary','objective','client','contractNo','amount','budget','planStart','planEnd','remark','closeReason','statusBeforeClose','reopenReason','orgId','spaceId'])&&isULID(v.id)&&typeof v.name==='string'&&v.name===normalizedProjectName(v.name)&&Array.from(v.name).length>=1&&Array.from(v.name).length<=200&&isProjectCode(v.projectCode)&&['implementation','operations','enhancement'].includes(String(v.type))&&['created','chartered','req_architecture','req_assessment','in_progress','integration_test','go_live_prep','live','closed','archived','active'].includes(String(v.status))&&(!('description'in v)||typeof v.description==='string')&&(!('client'in v)||typeof v.client==='string')&&(!('amount'in v)||typeof v.amount==='number'&&v.amount>=0)&&(!('budget'in v)||typeof v.budget==='number'&&v.budget>=0)&&(!('planStart'in v)||isDate(v.planStart))&&(!('planEnd'in v)||isDate(v.planEnd))&&(!('statusBeforeClose'in v)||v.statusBeforeClose===''||['created','chartered','req_architecture','req_assessment','in_progress','integration_test','go_live_prep','live','closed','archived','active'].includes(String(v.statusBeforeClose)))&&(!('reopenReason'in v)||typeof v.reopenReason==='string')&&(!('orgId'in v)||v.orgId===''||isULID(v.orgId))&&(!('spaceId'in v)||v.spaceId===''||isULID(v.spaceId))&&isTime(v.createdAt)&&isTime(v.updatedAt)&&Date.parse(String(v.updatedAt))>=Date.parse(String(v.createdAt))&&Number.isInteger(v.version)&&Number(v.version)>=1
const isSession=(v:unknown,projectId?:string)=>isObj(v)&&exact(v,['id','projectId','title','pinned','status','createdAt','updatedAt','version'])&&isULID(v.id)&&(!projectId||v.projectId===projectId)&&typeof v.title==='string'&&v.title===normalizedProjectName(v.title)&&Array.from(v.title).length>=1&&Array.from(v.title).length<=200&&typeof v.pinned==='boolean'&&v.status==='active'&&isTime(v.createdAt)&&isTime(v.updatedAt)&&Date.parse(String(v.updatedAt))>=Date.parse(String(v.createdAt))&&Number.isInteger(v.version)&&Number(v.version)>=1
const isModel=(v:unknown)=>isObj(v)&&exact(v,['modelId','displayName','isDefault'],['contextWindow','kind','supportsVision','kindDefault'])&&typeof v.modelId==='string'&&/^[\x21-\x7E]{1,200}$/.test(v.modelId)&&typeof v.displayName==='string'&&v.displayName===v.displayName.trim()&&v.displayName.length>0&&new TextEncoder().encode(v.displayName).length<=200&&typeof v.isDefault==='boolean'&&(!('contextWindow'in v)||(Number.isSafeInteger(v.contextWindow)&&Number(v.contextWindow)>=0&&Number(v.contextWindow)<=100_000_000))&&(!('kind'in v)||['llm','vision','image','video','voice'].includes(String(v.kind)))&&(!('supportsVision'in v)||typeof v.supportsVision==='boolean')&&(!('kindDefault'in v)||typeof v.kindDefault==='boolean')
const isModels=(v:unknown)=>Array.isArray(v)&&v.length>=1&&v.length<=50&&v.every(isModel)&&new Set(v.map(x=>(x as {modelId:string}).modelId)).size===v.length&&v.filter(x=>(x as {isDefault:boolean}).isDefault).length===1
const isProvider=(v:unknown)=>isObj(v)&&exact(v,['id','name','protocol','baseUrl','models','status','credentialState','createdAt','updatedAt','version'])&&isULID(v.id)&&typeof v.name==='string'&&v.name===v.name.trim()&&v.name.length>0&&['openai_compatible','anthropic','volc_speech'].includes(String(v.protocol))&&typeof v.baseUrl==='string'&&isModels(v.models)&&['enabled','disabled'].includes(String(v.status))&&['configured','missing','unavailable','requires_reentry'].includes(String(v.credentialState))&&isTime(v.createdAt)&&isTime(v.updatedAt)&&Number.isInteger(v.version)&&Number(v.version)>=1
const guards:Partial<Record<BridgeMethod|(typeof PROJECT_ADVANCE_METHOD),(v:unknown)=>boolean>>={
 'project.create':isProject,'project.update':isProject,'project.publish':isProject,'project.close':isProject,'project.reopen':isProject,[PROJECT_ADVANCE_METHOD]:isProject,
 'project.list':v=>isObj(v)&&exact(v,['items'])&&Array.isArray(v.items)&&v.items.length<=100&&v.items.every(isProject),
 'project.delete':v=>isObj(v)&&exact(v,['deleted','id'])&&v.deleted===true&&isULID(v.id),
 'provider.get':isProvider,'provider.create':isProvider,'provider.update':isProvider,
 'provider.list':v=>isObj(v)&&exact(v,['items'])&&Array.isArray(v.items)&&v.items.every(isProvider),
 'provider.delete':v=>isObj(v)&&exact(v,['deleted'])&&v.deleted===true,
 'provider.credential.reveal':v=>isObj(v)&&exact(v,['credential'])&&typeof v.credential==='string'&&v.credential.length>=1&&v.credential.length<=61440,
 'provider.credential.submit':v=>isObj(v)&&exact(v,['credentialSubmissionId','expiresAt','providerId','expiresInSeconds'])&&isULID(v.credentialSubmissionId)&&isULID(v.providerId)&&isTime(v.expiresAt)&&Number.isInteger(v.expiresInSeconds)&&Number(v.expiresInSeconds)>=1&&Number(v.expiresInSeconds)<=300,
 'provider.model.sync':v=>isObj(v)&&exact(v,['models','warnings','version'])&&isModels(v.models)&&Array.isArray(v.warnings)&&v.warnings.every(x=>typeof x==='string')&&Number.isInteger(v.version)&&Number(v.version)>=1,
 'provider.test':v=>isObj(v)&&exact(v,['status','stage','latencyMs','retryable','testedAt'],['httpStatus','errorCode','sanitizedMessage'])&&['passed','failed'].includes(String(v.status))&&['resolve','connect','authenticate','request','response'].includes(String(v.stage))&&Number.isInteger(v.latencyMs)&&Number(v.latencyMs)>=0&&typeof v.retryable==='boolean'&&isTime(v.testedAt)&&(!('httpStatus'in v)||(Number.isInteger(v.httpStatus)&&Number(v.httpStatus)>=100&&Number(v.httpStatus)<=599))&&(!('errorCode'in v)||typeof v.errorCode==='string')&&(!('sanitizedMessage'in v)||typeof v.sanitizedMessage==='string')
}
function validEnvelope(v:unknown):v is BridgeResponse { if(!isObj(v)||v.v!==BRIDGE_VERSION||v.kind!=='response'||!exact(v,['v','kind','id','requestId','ok'],v.ok===true?['payload']:['error']))return false;if(!isULID(v.id)||!isULID(v.requestId)||typeof v.ok!=='boolean')return false;return v.ok===true||isObj(v.error)&&exact(v.error,['code','message','retryable','correlationId'],['details'])&&typeof v.error.code==='string'&&typeof v.error.message==='string'&&typeof v.error.retryable==='boolean'&&typeof v.error.correlationId==='string' }
export function createProviderBridge(transport: WebViewTransport, defaultDeadlineMs=8_000): ProviderBridge {
 const pending=new Map<string,{method:BridgeMethod;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const requestId=raw.requestId;const waiting=pending.get(requestId)!;clearTimeout(waiting.timer);pending.delete(requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,requestId));return}if(raw.ok){if(!guards[waiting.method]?.(raw.payload)){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)}else waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId))})
 const request=<T>(method:BridgeMethod,payload:object,deadlineMs=defaultDeadlineMs,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid();const mutation=mutationMethods.has(method)?checkedAttempt(method as MutationMethod,payload,attempt):undefined;const secretSubmission=method==='provider.credential.submit';const outgoing=mutation?.payload??(secretSubmission?payload:clone(payload));const message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId:ulid(),method,sentAt:new Date().toISOString(),payload:outgoing,deadlineMs:Math.min(30_000,Math.max(1,deadlineMs)),...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,message.traceId))},message.deadlineMs+250);pending.set(id,{method,resolve,reject,timer});try{transport.postMessage(message);if(secretSubmission&&isObj(outgoing)&&typeof outgoing.credential==='string')outgoing.credential=''}catch{clearTimeout(timer);pending.delete(id);if(secretSubmission&&isObj(outgoing)&&typeof outgoing.credential==='string')outgoing.credential='';reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,message.traceId))}})}
 return {get:p=>request('provider.get',p),list:(p={})=>request('provider.list',p),create:(p,o)=>request('provider.create',p,defaultDeadlineMs,o?.attempt),update:(p,o)=>request('provider.update',p,defaultDeadlineMs,o?.attempt),delete:(p,o)=>request('provider.delete',p,defaultDeadlineMs,o?.attempt),revealCredential:p=>request('provider.credential.reveal',p),submitCredential:p=>request('provider.credential.submit',p),syncModels:(p,o)=>request('provider.model.sync',p,30_000,o?.attempt),test:p=>request('provider.test',p,30_000)}
}
// A bridge call must never throw synchronously: most facades below are
// reached from React render paths and effects, where a throw unmounts the
// whole tree and leaves a blank window. Resolving the host object lazily
// keeps every facade constructible before WebView2 exists — the failure
// then travels the normal postMessage path, which already rejects with
// BRIDGE_UNAVAILABLE. Listeners registered too early are replayed once the
// host object appears, so a late WebView2 does not strand the singletons.
const earlyListeners:Array<(event:MessageEvent<BridgeResponse>)=>void>=[]
let boundHost:WebViewTransport|undefined
function host():WebViewTransport|undefined{
 const v=window.chrome?.webview
 if(!v)return undefined
 if(boundHost!==v){boundHost=v;for(const listener of earlyListeners)v.addEventListener('message',listener)}
 return v
}
const lazyTransport:WebViewTransport={
 postMessage(value){const v=host();if(!v)throw new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,'renderer');v.postMessage(value)},
 addEventListener(type,listener){const v=host();if(v)v.addEventListener(type,listener);else earlyListeners.push(listener)},
 removeEventListener(type,listener){const v=host();if(v)v.removeEventListener(type,listener);const at=earlyListeners.indexOf(listener);if(at>=0)earlyListeners.splice(at,1)},
}
function webview():WebViewTransport{return lazyTransport}
export function createUIThemeBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):UIThemeBridge{const core=createSimpleBridge(transport,{},defaultDeadlineMs);return{set:p=>core.request('ui.theme.set',p)}}
let uiThemeSingleton:UIThemeBridge|undefined
export function getUIThemeBridge():UIThemeBridge{return uiThemeSingleton??=createUIThemeBridge(webview())}
export const uiThemeBridge:UIThemeBridge={set:p=>{try{return getUIThemeBridge().set(p)}catch(error){return Promise.reject(error)}}}
export function createSystemSettingsBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):SystemSettingsBridge{const core=createSimpleBridge(transport,{},defaultDeadlineMs);return{open:p=>core.request('system.settings.open',p)}}
let systemSettingsSingleton:SystemSettingsBridge|undefined
export function getSystemSettingsBridge():SystemSettingsBridge{return systemSettingsSingleton??=createSystemSettingsBridge(webview())}
export const systemSettingsBridge:SystemSettingsBridge={open:p=>{try{return getSystemSettingsBridge().open(p)}catch(error){return Promise.reject(error)}}}
export function createBrowserBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):BrowserBridge{const core=createSimpleBridge(transport,{},defaultDeadlineMs);return{open:p=>core.request('browser.open',p),close:()=>core.request('browser.close',{})}}
let browserSingleton:BrowserBridge|undefined
export function getBrowserBridge():BrowserBridge{return browserSingleton??=createBrowserBridge(webview())}
export const browserBridge:BrowserBridge={open:p=>{try{return getBrowserBridge().open(p)}catch(error){return Promise.reject(error)}},close:()=>{try{return getBrowserBridge().close()}catch(error){return Promise.reject(error)}}}
let singleton:ProviderBridge|undefined
export function getProviderBridge():ProviderBridge{return singleton??=createProviderBridge(webview())}
export const providerBridge:ProviderBridge={get:p=>getProviderBridge().get(p),list:p=>getProviderBridge().list(p),create:(p,o)=>getProviderBridge().create(p,o),update:(p,o)=>getProviderBridge().update(p,o),delete:(p,o)=>getProviderBridge().delete(p,o),revealCredential:p=>getProviderBridge().revealCredential(p),submitCredential:p=>getProviderBridge().submitCredential(p),syncModels:(p,o)=>getProviderBridge().syncModels(p,o),test:p=>getProviderBridge().test(p)}

export function createProjectBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):ProjectBridge{
 const pending=new Map<string,{method:BridgeMethod;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const requestId=raw.requestId,waiting=pending.get(requestId)!;clearTimeout(waiting.timer);pending.delete(requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}const guard=(guards as Partial<Record<string,(v:unknown)=>boolean>>)[waiting.method];if(!guard?.(raw.payload)){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'project.create'|'project.list'|'project.delete'|'project.update'|'project.publish'|'project.close'|'project.reopen'|'project.advanceStatus',payload:object,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=method!=='project.list'?checkedAttempt(method,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30_000,Math.max(1,defaultDeadlineMs)),message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method:method as BridgeMethod,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);pending.set(id,{method:method as BridgeMethod,resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:(p={})=>request('project.list',p),create:(p,o)=>request('project.create',p,o?.attempt),update:(p,o)=>request('project.update',p,o?.attempt),publish:(p,o)=>request('project.publish',p,o?.attempt),close:(p,o)=>request('project.close',p,o?.attempt),reopen:(p,o)=>request('project.reopen',p,o?.attempt),advanceStatus:(p,o)=>request('project.advanceStatus',p,o?.attempt),delete:(p,o)=>request('project.delete',p,o?.attempt)}
}
let projectSingleton:ProjectBridge|undefined
export function getProjectBridge():ProjectBridge{return projectSingleton??=createProjectBridge(webview())}
export const projectBridge:ProjectBridge={list:p=>getProjectBridge().list(p),create:(p,o)=>getProjectBridge().create(p,o),update:(p,o)=>getProjectBridge().update(p,o),publish:(p,o)=>getProjectBridge().publish(p,o),close:(p,o)=>getProjectBridge().close(p,o),reopen:(p,o)=>getProjectBridge().reopen(p,o),advanceStatus:(p,o)=>getProjectBridge().advanceStatus(p,o),delete:(p,o)=>getProjectBridge().delete(p,o)}

export function createSessionBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):SessionBridge{
 const pending=new Map<string,{method:'session.create'|'session.update'|'session.list'|'session.delete';projectId:string;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const waiting=pending.get(raw.requestId)!;clearTimeout(waiting.timer);pending.delete(raw.requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,raw.requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}let valid:boolean;if(waiting.method==='session.create'||waiting.method==='session.update')valid=isSession(raw.payload,waiting.projectId||undefined);else if(waiting.method==='session.delete')valid=isObj(raw.payload)&&exact(raw.payload,['deleted','id'])&&raw.payload.deleted===true&&isULID(raw.payload.id);else valid=isObj(raw.payload)&&exact(raw.payload,['items'])&&Array.isArray(raw.payload.items)&&raw.payload.items.length<=100&&raw.payload.items.every(v=>isSession(v,waiting.projectId));if(!valid){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'session.create'|'session.update'|'session.list'|'session.delete',payload:SessionCreatePayload|SessionUpdatePayload|SessionListPayload|SessionDeletePayload,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=(method==='session.create'||method==='session.update'||method==='session.delete')?checkedAttempt(method,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30000,Math.max(1,defaultDeadlineMs)),message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);const p=payload as{projectId?:string;id?:string};pending.set(id,{method,projectId:p.projectId??(method==='session.update'?'':p.id??''),resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:p=>request('session.list',p),create:(p,o)=>request('session.create',p,o?.attempt),update:(p,o)=>request('session.update',p,o?.attempt),delete:(p,o)=>request('session.delete',p,o?.attempt)}
}
let sessionSingleton:SessionBridge|undefined
export function getSessionBridge():SessionBridge{return sessionSingleton??=createSessionBridge(webview())}
export const sessionBridge:SessionBridge={list:p=>getSessionBridge().list(p),create:(p,o)=>getSessionBridge().create(p,o),update:(p,o)=>getSessionBridge().update(p,o),delete:(p,o)=>getSessionBridge().delete(p,o)}

const textValid=(v:unknown)=>typeof v==='string'&&v.length>0&&!v.includes('\0')&&Array.from(v).length<=2048&&new TextEncoder().encode(v).length<=8192
const dtoTextValid=(v:unknown)=>typeof v==='string'&&v.length>=1&&v.length<=65536
const messageArtifactPathValid=(path:unknown)=>typeof path==='string'&&path.length>0&&path.length<=512&&!path.startsWith('/')&&!path.includes('\\')&&!path.split('/').includes('..')
const isMessageArtifact=(v:unknown)=>isObj(v)&&exact(v,['kind','path','callId','toolName'])&&typeof v.callId==='string'&&v.callId.length>0&&v.callId.length<=128&&typeof v.toolName==='string'&&v.toolName.length>0&&messageArtifactPathValid(v.path)&&(v.kind==='html'||v.kind==='xlsx'||v.kind==='docx'||v.kind==='pptx'||v.kind==='pdf')
const isMessage=(v:unknown,sessionId:string)=>isObj(v)&&exact(v,['id','sessionId','role','status','sequence','text','createdAt'],['artifacts'])&&isULID(v.id)&&v.sessionId===sessionId&&(v.role==='user'||v.role==='assistant'||v.role==='tool')&&v.status==='completed'&&Number.isSafeInteger(v.sequence)&&Number(v.sequence)>0&&dtoTextValid(v.text)&&isTime(v.createdAt)&&(!('artifacts'in v)||(Array.isArray(v.artifacts)&&v.artifacts.every(isMessageArtifact)))
export function createMessageBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):MessageBridge{
 type Waiting={method:'message.append'|'message.list'|'message.rewind';sessionId:string;direction:'forward'|'backward';cursor?:string;resolve(v:unknown):void;reject(e:Error):void;timer:number}
 const pending=new Map<string,Waiting>(),cursors=new Map<string,{sessionId:string;direction:'forward'|'backward';snapshot:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const waiting=pending.get(raw.requestId)!;clearTimeout(waiting.timer);pending.delete(raw.requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,raw.requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}let valid=isMessage(raw.payload,waiting.sessionId);if(waiting.method==='message.rewind'){const p=raw.payload;valid=isObj(p)&&exact(p,['sessionId','messageId','deletedCount','lastSequence','historyRevision'])&&p.sessionId===waiting.sessionId&&isULID(p.messageId)&&Number.isSafeInteger(p.deletedCount)&&Number(p.deletedCount)>=1&&Number.isSafeInteger(p.lastSequence)&&Number(p.lastSequence)>=0&&Number.isSafeInteger(p.historyRevision)&&Number(p.historyRevision)>=1;if(valid)for(const[cursor,binding]of cursors)if(binding.sessionId===waiting.sessionId)cursors.delete(cursor)}else if(waiting.method==='message.list'){const p=raw.payload as Record<string,unknown>,known=waiting.cursor?cursors.get(waiting.cursor):undefined;valid=isObj(p)&&exact(p,['items','hasMore','nextCursor','snapshotSequence'])&&Array.isArray(p.items)&&p.items.length<=256&&p.items.every(x=>isMessage(x,waiting.sessionId))&&typeof p.hasMore==='boolean'&&Number.isSafeInteger(p.snapshotSequence)&&Number(p.snapshotSequence)>=0&&(p.nextCursor===null||typeof p.nextCursor==='string'&&p.nextCursor.length>=1&&p.nextCursor.length<=1024)&&p.hasMore===(p.nextCursor!==null)&&(!p.hasMore||p.items.length>0)&&(!known||known.sessionId===waiting.sessionId&&known.direction===waiting.direction&&known.snapshot===p.snapshotSequence);if(valid){const messageItems=p.items as unknown[],seq=messageItems.map((x:unknown)=>Number((x as Record<string,unknown>).sequence));for(let i=1;i<seq.length;i++)if(seq[i]!==seq[i-1]+(waiting.direction==='forward'?1:-1))valid=false;if(seq.some((x:number)=>x>Number(p.snapshotSequence)))valid=false;if(valid&&typeof p.nextCursor==='string')cursors.set(p.nextCursor,{sessionId:waiting.sessionId,direction:waiting.direction,snapshot:Number(p.snapshotSequence)})}}
 if(!valid){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'message.append'|'message.list'|'message.rewind',payload:MessageAppendPayload|MessageListPayload|MessageRewindPayload,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=method!=='message.list'?checkedAttempt(method,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30000,Math.max(1,defaultDeadlineMs)),listPayload=payload as MessageListPayload,appendPayload=payload as MessageAppendPayload,direction=method==='message.list'?(listPayload.direction??'backward'):'backward';if(method==='message.list'&&(listPayload.cursor!==undefined&&(listPayload.cursor.length<1||listPayload.cursor.length>1024)||listPayload.limit!==undefined&&(!Number.isInteger(listPayload.limit)||listPayload.limit<1||listPayload.limit>256)||listPayload.byteBudget!==undefined&&(!Number.isInteger(listPayload.byteBudget)||listPayload.byteBudget<16384||listPayload.byteBudget>245760)))return Promise.reject(new BridgeClientError('消息分页参数无效','INVALID_BRIDGE_REQUEST',false,'renderer'));if(method==='message.append'&&!textValid(appendPayload.text))return Promise.reject(new BridgeClientError('消息文本无效','INVALID_BRIDGE_REQUEST',false,'renderer'));const message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);pending.set(id,{method,sessionId:payload.sessionId,direction,cursor:method==='message.list'?listPayload.cursor:undefined,resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:p=>request('message.list',p),append:(p,o)=>request('message.append',p,o?.attempt),rewind:(p,o)=>request('message.rewind',p,o?.attempt)}
}
let messageSingleton:MessageBridge|undefined
export function getMessageBridge():MessageBridge{return messageSingleton??=createMessageBridge(webview())}
export const messageBridge:MessageBridge={list:p=>getMessageBridge().list(p),append:(p,o)=>getMessageBridge().append(p,o),rewind:(p,o)=>getMessageBridge().rewind!(p,o)}

const stageStatuses=['not_started','in_progress','waiting_review','approved','completed','rejected','stale','paused','blocked','cancelled']
const isStage=(v:unknown,projectId:string)=>isObj(v)&&exact(v,['id','projectId','phase','title','status','createdAt','updatedAt','version'])&&isULID(v.id)&&v.projectId===projectId&&Number.isInteger(v.phase)&&Number(v.phase)>=1&&Number(v.phase)<=9&&typeof v.title==='string'&&v.title===normalizedProjectName(v.title)&&Array.from(v.title).length>=1&&Array.from(v.title).length<=200&&stageStatuses.includes(String(v.status))&&isTime(v.createdAt)&&isTime(v.updatedAt)&&Number.isInteger(v.version)&&Number(v.version)>=1
export interface StageBridge { list(payload:StageListPayload):Promise<StageListResult>; create(payload:StageCreatePayload,options?:MutationOptions<StageCreatePayload>):Promise<StageCreateResult>; update(payload:StageUpdatePayload,options?:MutationOptions<StageUpdatePayload>):Promise<StageUpdateResult> }
export function createStageBridge(transport:WebViewTransport,defaultDeadlineMs=8_000):StageBridge{
 const pending=new Map<string,{method:'stage.create'|'stage.list'|'stage.update';projectId:string;resolve(v:unknown):void;reject(e:Error):void;timer:number}>()
 transport.addEventListener('message',event=>{const raw:unknown=event.data;if(!isObj(raw)||typeof raw.requestId!=='string'||!pending.has(raw.requestId))return;const waiting=pending.get(raw.requestId)!;clearTimeout(waiting.timer);pending.delete(raw.requestId);if(!validEnvelope(raw)){waiting.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,raw.requestId));return}if(!raw.ok){waiting.reject(new BridgeClientError(raw.error.message,raw.error.code,raw.error.retryable,raw.error.correlationId));return}const payloadProjectId=waiting.method==='stage.update'&&isObj(raw.payload)?String(raw.payload.projectId):waiting.projectId;const valid=waiting.method==='stage.list'?isObj(raw.payload)&&exact(raw.payload,['items'])&&Array.isArray(raw.payload.items)&&raw.payload.items.length<=9&&raw.payload.items.every(v=>isStage(v,waiting.projectId)):isStage(raw.payload,payloadProjectId);if(!valid){waiting.reject(new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,raw.id));return}waiting.resolve(raw.payload)})
 const request=<T>(method:'stage.create'|'stage.list'|'stage.update',payload:StageCreatePayload|StageListPayload|StageUpdatePayload,attempt?:MutationAttempt<object>):Promise<T>=>{const id=ulid(),mutation=method!=='stage.list'?checkedAttempt(method as MutationMethod,payload,attempt):undefined,traceId=ulid(),deadlineMs=Math.min(30000,Math.max(1,defaultDeadlineMs)),projectId=method==='stage.update'?'':(payload as StageCreatePayload|StageListPayload).projectId,message:BridgeRequest<object>={v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload:mutation?.payload??clone(payload),deadlineMs,...(mutation?{idempotencyKey:mutation.key}:{})};return new Promise((resolve,reject)=>{const timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},deadlineMs+250);pending.set(id,{method,projectId,resolve,reject,timer});try{transport.postMessage(message)}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})}
 return{list:p=>request('stage.list',p),create:(p,o)=>request('stage.create',p,o?.attempt),update:(p,o)=>request('stage.update',p,o?.attempt)}
}
let stageSingleton:StageBridge|undefined
export function getStageBridge():StageBridge{return stageSingleton??=createStageBridge(webview())}
export const stageBridge:StageBridge={list:p=>{try{return getStageBridge().list(p)}catch(error){return Promise.reject(error)}},create:(p,o)=>{try{return getStageBridge().create(p,o)}catch(error){return Promise.reject(error)}},update:(p,o)=>{try{return getStageBridge().update(p,o)}catch(error){return Promise.reject(error)}}}

// M7 subagent research bridge — read-only parallel collection panel
// (subagent-ui / T-7.6.5). spawn carries requestId + idempotency key;
// join blocks up to waitMs (≤30s) so its deadline is 35s.
export interface SubagentBridge{
  spawn(payload:SubagentSpawnPayload,options?:MutationOptions<SubagentSpawnPayload>):Promise<SubagentSpawnResult>
  join(payload:SubagentJoinPayload):Promise<SubagentJoinResult>
  tree(payload:SubagentTreePayload):Promise<SubagentTreeResult>
}
export function createSubagentBridge(transport:WebViewTransport=webview(),deadlineMs=10_000):SubagentBridge{
  const core=createSimpleBridge(transport,{},deadlineMs)
  return{spawn:(p,o)=>core.request('subagent.spawn',p,30_000,o?.attempt),join:p=>core.request('subagent.join',p,15_000),tree:p=>core.request('subagent.tree',p)}
}
let subagentSingleton:SubagentBridge|undefined
export function getSubagentBridge():SubagentBridge{return subagentSingleton??=createSubagentBridge()}
export const subagentBridge:SubagentBridge={spawn:(p,o)=>{try{return getSubagentBridge().spawn(p,o)}catch(error){return Promise.reject(error)}},join:p=>{try{return getSubagentBridge().join(p)}catch(error){return Promise.reject(error)}},tree:p=>{try{return getSubagentBridge().tree(p)}catch(error){return Promise.reject(error)}}}

export interface ConversationsBridge{get():Promise<ConversationsRootGetResult>;select():Promise<ConversationsRootSelectResult>;set(payload:ConversationsRootSetPayload):Promise<ConversationsRootSetResult>}
export interface SessionFolderBridge{get(payload:SessionFolderGetPayload):Promise<SessionFolderGetResult>;list(payload:SessionFolderListPayload):Promise<SessionFolderListResult>;open(payload:SessionFolderOpenPayload):Promise<SessionFolderOpenResult>}
export function createConversationsBridge(transport:WebViewTransport=webview()):ConversationsBridge{const core=createSimpleBridge(transport,{},120_000);return{get:()=>core.request('conversations.root.get',{}),select:()=>core.request('conversations.root.select',{}),set:p=>core.request('conversations.root.set',p,120_000)}}
export function createSessionFolderBridge(transport:WebViewTransport=webview()):SessionFolderBridge{const core=createSimpleBridge(transport,{},8_000);return{get:p=>core.request('session.folder.get',p),list:p=>core.request('session.folder.list',p),open:p=>core.request('session.folder.open',p)}}
let conversationsSingleton:ConversationsBridge|undefined
let sessionFolderSingleton:SessionFolderBridge|undefined
export function getConversationsBridge():ConversationsBridge{return conversationsSingleton??=createConversationsBridge()}
export function getSessionFolderBridge():SessionFolderBridge{return sessionFolderSingleton??=createSessionFolderBridge()}
export const conversationsBridge:ConversationsBridge={get:()=>getConversationsBridge().get(),select:()=>getConversationsBridge().select(),set:p=>getConversationsBridge().set(p)}
export const sessionFolderBridge:SessionFolderBridge={get:p=>getSessionFolderBridge().get(p),list:p=>getSessionFolderBridge().list(p),open:p=>getSessionFolderBridge().open(p)}

// M8 collab gate bridge — capability status / evidence evaluation /
// single-use decision-token confirm (FR-17, GT-01~GT-06).
export interface CollabGateBridge{
  status(payload:CollabGateStatusPayload):Promise<CollabGateStatusResult>
  evaluate(payload:CollabGateEvaluatePayload):Promise<CollabGateEvaluateResult>
  confirm(payload:CollabGateConfirmPayload):Promise<CollabGateConfirmResult>
}
export function createCollabGateBridge(transport:WebViewTransport=webview(),deadlineMs=10_000):CollabGateBridge{
  const core=createSimpleBridge(transport,{},deadlineMs)
  return{status:p=>core.request('collabGate.status',p),evaluate:p=>core.request('collabGate.evaluate',p,30_000),confirm:p=>core.request('collabGate.confirm',p)}
}
let collabGateSingleton:CollabGateBridge|undefined
export function getCollabGateBridge():CollabGateBridge{return collabGateSingleton??=createCollabGateBridge()}
export const collabGateBridge:CollabGateBridge={status:p=>{try{return getCollabGateBridge().status(p)}catch(error){return Promise.reject(error)}},evaluate:p=>{try{return getCollabGateBridge().evaluate(p)}catch(error){return Promise.reject(error)}},confirm:p=>{try{return getCollabGateBridge().confirm(p)}catch(error){return Promise.reject(error)}}}

// Diagnostics export bridge — host-owned redacted diagnostic bundle
// (diagnostics.export, settings → diagnostics panel).
export interface DiagnosticsBridge{exportDiagnostics(payload?:DiagnosticsExportPayload):Promise<DiagnosticsExportResult>}
export function createDiagnosticsBridge(transport:WebViewTransport=webview()):DiagnosticsBridge{
  const core=createSimpleBridge(transport,{},30_000)
  return{exportDiagnostics:p=>core.request('diagnostics.export',p??{})}
}
let diagnosticsSingleton:DiagnosticsBridge|undefined
export function getDiagnosticsBridge():DiagnosticsBridge{return diagnosticsSingleton??=createDiagnosticsBridge()}
export const diagnosticsBridge:DiagnosticsBridge={exportDiagnostics:p=>{try{return getDiagnosticsBridge().exportDiagnostics(p)}catch(error){return Promise.reject(error)}}}

export interface SystemHealthBridge{health():Promise<SystemHealthResult>}
export function createSystemHealthBridge(transport:WebViewTransport=webview()):SystemHealthBridge{
  const core=createSimpleBridge(transport,{},8_000)
  return{health:()=>core.request('system.health',{})}
}
let systemHealthSingleton:SystemHealthBridge|undefined
export function getSystemHealthBridge():SystemHealthBridge{return systemHealthSingleton??=createSystemHealthBridge()}
export const systemHealthBridge:SystemHealthBridge={health:()=>{try{return getSystemHealthBridge().health()}catch(error){return Promise.reject(error)}}}

// Tools command policy bridge — user-editable read-only command whitelist
// (tools.commandPolicy.get/set, settings → security panel).
export interface ToolsPolicyBridge{
  getCommandPolicy():Promise<ToolsCommandPolicyGetResult>
  setCommandPolicy(payload:ToolsCommandPolicySetPayload):Promise<ToolsCommandPolicySetResult>
}
export function createToolsPolicyBridge(transport:WebViewTransport=webview()):ToolsPolicyBridge{
  const core=createSimpleBridge(transport,{},10_000)
  return{getCommandPolicy:()=>core.request('tools.commandPolicy.get',{}),setCommandPolicy:p=>core.request('tools.commandPolicy.set',p)}
}
let toolsPolicySingleton:ToolsPolicyBridge|undefined
export function getToolsPolicyBridge():ToolsPolicyBridge{return toolsPolicySingleton??=createToolsPolicyBridge()}
export const toolsPolicyBridge:ToolsPolicyBridge={getCommandPolicy:()=>{try{return getToolsPolicyBridge().getCommandPolicy()}catch(error){return Promise.reject(error)}},setCommandPolicy:p=>{try{return getToolsPolicyBridge().setCommandPolicy(p)}catch(error){return Promise.reject(error)}}}

// Tools hooks policy bridge — P3-B tool-call interceptors
// (tools.hooksPolicy.get/set + tools.hooksEvents.list, settings → security panel).
export interface HooksPolicyBridge{
  getHooksPolicy():Promise<ToolsHooksPolicyGetResult>
  setHooksPolicy(payload:ToolsHooksPolicySetPayload):Promise<ToolsHooksPolicySetResult>
  listHookEvents(payload:ToolsHooksEventsListPayload):Promise<ToolsHooksEventsListResult>
}
export function createHooksPolicyBridge(transport:WebViewTransport=webview()):HooksPolicyBridge{
  const core=createSimpleBridge(transport,{},10_000)
  return{getHooksPolicy:()=>core.request('tools.hooksPolicy.get',{}),setHooksPolicy:p=>core.request('tools.hooksPolicy.set',p),listHookEvents:p=>core.request('tools.hooksEvents.list',p)}
}
let hooksPolicySingleton:HooksPolicyBridge|undefined
export function getHooksPolicyBridge():HooksPolicyBridge{return hooksPolicySingleton??=createHooksPolicyBridge()}
export const hooksPolicyBridge:HooksPolicyBridge={getHooksPolicy:()=>{try{return getHooksPolicyBridge().getHooksPolicy()}catch(error){return Promise.reject(error)}},setHooksPolicy:p=>{try{return getHooksPolicyBridge().setHooksPolicy(p)}catch(error){return Promise.reject(error)}},listHookEvents:p=>{try{return getHooksPolicyBridge().listHookEvents(p)}catch(error){return Promise.reject(error)}}}

// Artifact review bridge — P2-2 acceptance loop (comment → revise → accept)
// plus kind-aware preview (workspace.artifactReview.*, workspace.artifact.preview).
export interface ArtifactReviewBridge{
  list(payload:WorkspaceArtifactReviewListPayload):Promise<WorkspaceArtifactReviewListResult>
  append(payload:WorkspaceArtifactReviewAppendPayload):Promise<WorkspaceArtifactReviewAppendResult>
  preview(payload:WorkspaceArtifactPreviewPayload):Promise<WorkspaceArtifactPreviewResult>
  exportArtifact(payload:WorkspaceArtifactExportPayload):Promise<WorkspaceArtifactExportResult>
}
export function createArtifactReviewBridge(transport:WebViewTransport=webview()):ArtifactReviewBridge{
  const core=createSimpleBridge(transport,{},15_000)
  return{list:p=>core.request('workspace.artifactReview.list',p),append:p=>core.request('workspace.artifactReview.append',p),preview:p=>core.request('workspace.artifact.preview',p),exportArtifact:p=>core.request('workspace.artifact.export',p)}
}
let artifactReviewSingleton:ArtifactReviewBridge|undefined
export function getArtifactReviewBridge():ArtifactReviewBridge{return artifactReviewSingleton??=createArtifactReviewBridge()}
export const artifactReviewBridge:ArtifactReviewBridge={list:p=>{try{return getArtifactReviewBridge().list(p)}catch(error){return Promise.reject(error)}},append:p=>{try{return getArtifactReviewBridge().append(p)}catch(error){return Promise.reject(error)}},preview:p=>{try{return getArtifactReviewBridge().preview(p)}catch(error){return Promise.reject(error)}},exportArtifact:p=>{try{return getArtifactReviewBridge().exportArtifact(p)}catch(error){return Promise.reject(error)}}}

// Automation bridge — P2-3 resident cron automation (automation.*).
export interface AutomationBridge{
  listJobs():Promise<AutomationJobListResult>
  setJob(payload:AutomationJobSetPayload):Promise<AutomationJobSetResult>
  deleteJob(payload:AutomationJobDeletePayload):Promise<AutomationJobDeleteResult>
  triggerJob(payload:AutomationJobTriggerPayload):Promise<AutomationJobTriggerResult>
  listRuns(payload:AutomationRunListPayload):Promise<AutomationRunListResult>
  status():Promise<AutomationStatusResult>
}
export function createAutomationBridge(transport:WebViewTransport=webview()):AutomationBridge{
  const core=createSimpleBridge(transport,{},15_000)
  return{listJobs:()=>core.request('automation.job.list',{}),setJob:p=>core.request('automation.job.set',p),deleteJob:p=>core.request('automation.job.delete',p),triggerJob:p=>core.request('automation.job.trigger',p),listRuns:p=>core.request('automation.run.list',p),status:()=>core.request('automation.status',{})}
}
let automationSingleton:AutomationBridge|undefined
export function getAutomationBridge():AutomationBridge{return automationSingleton??=createAutomationBridge()}
export const automationBridge:AutomationBridge={listJobs:()=>{try{return getAutomationBridge().listJobs()}catch(error){return Promise.reject(error)}},setJob:p=>{try{return getAutomationBridge().setJob(p)}catch(error){return Promise.reject(error)}},deleteJob:p=>{try{return getAutomationBridge().deleteJob(p)}catch(error){return Promise.reject(error)}},triggerJob:p=>{try{return getAutomationBridge().triggerJob(p)}catch(error){return Promise.reject(error)}},listRuns:p=>{try{return getAutomationBridge().listRuns(p)}catch(error){return Promise.reject(error)}},status:()=>{try{return getAutomationBridge().status()}catch(error){return Promise.reject(error)}}}

// This-PC meeting notes — microphone transcript, then 摘要/待办/逐字稿. Never mixes into session.* or people P2P.
export const BRIDGE_DEADLINE_CAP_MS = 30_000
export const MEETING_APPEND_DEADLINE_MS = 120_000
export const MEETING_STOP_DEADLINE_MS = 120_000
export const MEETING_HEARTBEAT_DEADLINE_MS = 120_000
export const MEETING_SUMMARIZE_DEADLINE_MS = 600_000
export const MEETING_HEARTBEAT_INTERVAL_MS = 20_000
export const PEOPLE_FILE_DEADLINE_MS = 120_000
export const PEOPLE_CAPTURE_DEADLINE_MS = 180_000
export const TEMPLATE_FILE_DEADLINE_MS = 120_000
export function capBridgeDeadlineMs(method: string, deadlineMs: number): number {
  let cap = BRIDGE_DEADLINE_CAP_MS
  if (method === 'meetings.summarize' || method === 'meetings.catchup') cap = MEETING_SUMMARIZE_DEADLINE_MS
  else if (method === 'meetings.append' || method === 'meetings.audio.append' || method === 'meetings.stop' || method === 'meetings.heartbeat' || method === 'meetings.get' || method === 'meetings.export') cap = MEETING_APPEND_DEADLINE_MS
  else if (method === 'people.file.stage' || method === 'people.file.pick' || method === 'people.thread.send' || method === 'people.screen.capture') cap = method === 'people.screen.capture' ? PEOPLE_CAPTURE_DEADLINE_MS : PEOPLE_FILE_DEADLINE_MS
  else if (method === 'template.file.stage' || method === 'template.create') cap = TEMPLATE_FILE_DEADLINE_MS
  else if (method === 'appUpdate.install') cap = 120_000
  return Math.min(cap, Math.max(1, deadlineMs))
}
const isRetryableBridgeError = (error: unknown) => error instanceof BridgeClientError && error.retryable
async function retryBridgeRequest<T>(op: () => Promise<T>, attempts = 4): Promise<T> {
  let last: unknown
  for (let i = 0; i < attempts; i++) {
    try { return await op() }
    catch (error) {
      last = error
      if (i === attempts - 1 || !isRetryableBridgeError(error)) throw error
      await new Promise<void>(resolve => { window.setTimeout(resolve, 350 * (i + 1)) })
    }
  }
  throw last
}
export interface MeetingsBridge{
  list():Promise<MeetingsListResult>
  start(payload?:MeetingsStartPayload):Promise<MeetingDTO>
  append(payload:MeetingsAppendPayload):Promise<MeetingSegmentDTO>
  audioAppend(payload:MeetingsAudioAppendPayload):Promise<MeetingsAudioAppendResult>
  loopbackPoll(payload:MeetingsLoopbackPollPayload):Promise<MeetingsLoopbackPollResult>
  stop(payload:MeetingsStopPayload):Promise<MeetingDTO>
  get(payload:MeetingsGetPayload):Promise<MeetingDTO>
  heartbeat(payload:MeetingsHeartbeatPayload):Promise<MeetingDTO>
  catchup(payload:MeetingsCatchupPayload):Promise<MeetingsCatchupResult>
  summarize(payload:MeetingsSummarizePayload):Promise<MeetingDTO>
  exportMeeting(payload:MeetingsExportPayload):Promise<MeetingsExportResult>
  update(payload:MeetingsUpdatePayload):Promise<MeetingDTO>
  delete(payload:MeetingsDeletePayload):Promise<MeetingsDeleteResult>
}
export function createMeetingsBridge(transport:WebViewTransport=webview()):MeetingsBridge{
  const core=createSimpleBridge(transport,{},15_000)
  return{
    list:()=>core.request('meetings.list',{}),
    start:p=>core.request('meetings.start',p??{}),
    append:p=>retryBridgeRequest(()=>core.request('meetings.append',p,MEETING_APPEND_DEADLINE_MS)),
    audioAppend:p=>retryBridgeRequest(()=>core.request('meetings.audio.append',p,MEETING_APPEND_DEADLINE_MS)),
    loopbackPoll:p=>core.request('meetings.loopback.poll',p),
    stop:p=>retryBridgeRequest(()=>core.request('meetings.stop',p,MEETING_STOP_DEADLINE_MS)),
    get:p=>core.request('meetings.get',p,MEETING_HEARTBEAT_DEADLINE_MS),
    heartbeat:p=>retryBridgeRequest(()=>core.request('meetings.heartbeat',p,MEETING_HEARTBEAT_DEADLINE_MS)),
    catchup:p=>core.request('meetings.catchup',p,MEETING_SUMMARIZE_DEADLINE_MS),
    summarize:p=>core.request('meetings.summarize',p,MEETING_SUMMARIZE_DEADLINE_MS),
    exportMeeting:p=>core.request('meetings.export',p,MEETING_APPEND_DEADLINE_MS),
    update:p=>core.request('meetings.update',p),
    delete:p=>core.request('meetings.delete',p),
  }
}
let meetingsSingleton:MeetingsBridge|undefined
export function getMeetingsBridge():MeetingsBridge{return meetingsSingleton??=createMeetingsBridge()}
export const meetingsBridge:MeetingsBridge={list:()=>{try{return getMeetingsBridge().list()}catch(error){return Promise.reject(error)}},start:p=>{try{return getMeetingsBridge().start(p)}catch(error){return Promise.reject(error)}},append:p=>{try{return getMeetingsBridge().append(p)}catch(error){return Promise.reject(error)}},audioAppend:p=>{try{return getMeetingsBridge().audioAppend(p)}catch(error){return Promise.reject(error)}},loopbackPoll:p=>{try{return getMeetingsBridge().loopbackPoll(p)}catch(error){return Promise.reject(error)}},stop:p=>{try{return getMeetingsBridge().stop(p)}catch(error){return Promise.reject(error)}},get:p=>{try{return getMeetingsBridge().get(p)}catch(error){return Promise.reject(error)}},heartbeat:p=>{try{return getMeetingsBridge().heartbeat(p)}catch(error){return Promise.reject(error)}},catchup:p=>{try{return getMeetingsBridge().catchup(p)}catch(error){return Promise.reject(error)}},summarize:p=>{try{return getMeetingsBridge().summarize(p)}catch(error){return Promise.reject(error)}},exportMeeting:p=>{try{return getMeetingsBridge().exportMeeting(p)}catch(error){return Promise.reject(error)}},update:p=>{try{return getMeetingsBridge().update(p)}catch(error){return Promise.reject(error)}},delete:p=>{try{return getMeetingsBridge().delete(p)}catch(error){return Promise.reject(error)}}}

// P3/P4 Bridge — 简化模式：envelope 校验 + 基本 request/response
function createSimpleBridge<TMethods extends Record<string, BridgeMethod>>(
  transport: WebViewTransport,
  methods: TMethods,
  defaultDeadlineMs = 8_000
) {
  const pending = new Map<string, { resolve(v: unknown): void; reject(e: Error): void; timer: number }>()
  transport.addEventListener('message', event => {
    const raw: unknown = event.data
    if (!isObj(raw) || typeof raw.requestId !== 'string' || !pending.has(raw.requestId)) return
    const requestId = raw.requestId
    const waiting = pending.get(requestId)!
    clearTimeout(waiting.timer)
    pending.delete(requestId)
    if (!validEnvelope(raw)) { waiting.reject(new BridgeClientError('Bridge 响应格式无效', 'INVALID_BRIDGE_RESPONSE', false, requestId)); return }
    if (raw.ok) waiting.resolve(raw.payload)
    else waiting.reject(new BridgeClientError(raw.error.message, raw.error.code, raw.error.retryable, raw.error.correlationId))
  })
  const request = <T>(method: BridgeMethod, payload: object, deadlineMs = defaultDeadlineMs, attempt?: MutationAttempt<object>): Promise<T> => {
    const id = ulid(), traceId = ulid()
    const mutation = mutationMethods.has(method) ? checkedAttempt(method as MutationMethod, payload, attempt) : undefined
    const message: BridgeRequest<object> = { v: BRIDGE_VERSION, kind: 'request', id, traceId, method, sentAt: new Date().toISOString(), payload: mutation?.payload ?? clone(payload), deadlineMs: capBridgeDeadlineMs(method, deadlineMs), ...(mutation ? { idempotencyKey: mutation.key } : {}) }
    return new Promise((resolve, reject) => {
      const timer = window.setTimeout(() => { pending.delete(id); reject(new BridgeClientError('Bridge 请求超时', 'REQUEST_DEADLINE_EXCEEDED', true, traceId)) }, message.deadlineMs + 250)
      pending.set(id, { resolve, reject, timer })
      try { transport.postMessage(message) } catch { clearTimeout(timer); pending.delete(id); reject(new BridgeClientError('WebView2 Bridge 当前不可用', 'BRIDGE_UNAVAILABLE', true, traceId)) }
    })
  }
  return { request }
}

export interface PlanBridge {
  get(payload: PlanGetPayload): Promise<PlanGetResult>
  list(payload: PlanListPayload): Promise<PlanListResult>
  create(payload: PlanCreatePayload, options?: MutationOptions<PlanCreatePayload>): Promise<PlanCreateResult>
  activate(payload: PlanActivatePayload): Promise<PlanActivateResult>
  complete(payload: PlanCompletePayload): Promise<PlanCompleteResult>
  pause(payload: PlanPausePayload): Promise<PlanPauseResult>
  resume(payload: PlanResumePayload): Promise<PlanResumeResult>
  listNodes(payload: NodeListPayload): Promise<NodeListResult>
  createNode(payload: NodeCreatePayload, options?: MutationOptions<NodeCreatePayload>): Promise<NodeCreateResult>
  startNode(payload: NodeStartPayload): Promise<NodeStartResult>
  completeNode(payload: NodeCompletePayload): Promise<NodeCompleteResult>
  failNode(payload: NodeFailPayload): Promise<NodeFailResult>
  createTodo(payload: PlanTodoCreatePayload): Promise<PlanTodoCreateResult>
  startRun(payload: PlanRunStartPayload): Promise<PlanRunStartResult>
  runTree(payload: PlanRunTreePayload): Promise<PlanRunTreeResult>
  spawnRun(payload: PlanRunSpawnPayload): Promise<PlanRunSpawnResult>
  joinRun(payload: PlanRunJoinPayload): Promise<PlanRunJoinResult>
  cancelRun(payload: PlanRunCancelPayload): Promise<PlanRunCancelResult>
}
export function createPlanBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): PlanBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    get: p => core.request('plan.get', p),
    list: p => core.request('plan.list', p),
    create: (p, o) => core.request('plan.create', p, defaultDeadlineMs, o?.attempt),
    activate: p => core.request('plan.activate', p),
    complete: p => core.request('plan.complete', p),
    pause: p => core.request('plan.pause', p),
    resume: p => core.request('plan.resume', p),
    listNodes: p => core.request('node.list', p),
    createNode: (p, o) => core.request('node.create', p, defaultDeadlineMs, o?.attempt),
    startNode: p => core.request('node.start', p),
    completeNode: p => core.request('node.complete', p),
    failNode: p => core.request('node.fail', p),
    createTodo: p => core.request('plan.todo.create', p),
    startRun: p => core.request('plan.run.start', p),
    runTree: p => core.request('plan.run.tree', p),
    spawnRun: p => core.request('plan.run.spawn', p),
    joinRun: p => core.request('plan.run.join', p),
    cancelRun: p => core.request('plan.run.cancel', p),
  }
}
let planSingleton: PlanBridge | undefined
export function getPlanBridge(): PlanBridge { return planSingleton ??= createPlanBridge(webview()) }
export const planBridge: PlanBridge = { get: p => getPlanBridge().get(p), list: p => getPlanBridge().list(p), create: (p, o) => getPlanBridge().create(p, o), activate: p => getPlanBridge().activate(p), complete: p => getPlanBridge().complete(p), pause: p => getPlanBridge().pause(p), resume: p => getPlanBridge().resume(p), listNodes: p => getPlanBridge().listNodes(p), createNode: (p, o) => getPlanBridge().createNode(p, o), startNode: p => getPlanBridge().startNode(p), completeNode: p => getPlanBridge().completeNode(p), failNode: p => getPlanBridge().failNode(p), createTodo: p => getPlanBridge().createTodo(p), startRun: p => getPlanBridge().startRun(p), runTree: p => getPlanBridge().runTree(p), spawnRun: p => getPlanBridge().spawnRun(p), joinRun: p => getPlanBridge().joinRun(p), cancelRun: p => getPlanBridge().cancelRun(p) }

export interface AgentRuntimeBridge{
 capabilities():Promise<CapabilityListResult>;start(p:AgentRunStartPayload):Promise<AgentRunStartResult>;get(p:AgentRunGetPayload):Promise<AgentRunGetResult>;cancel(p:AgentRunCancelPayload):Promise<AgentRunCancelResult>;resume(p:AgentRunResumePayload):Promise<AgentRunResumeResult>;reconcile(p:AgentRunReconcilePayload):Promise<AgentRunReconcileResult>;registerWorkspace(p:WorkspaceRegisterPayload):Promise<WorkspaceRegisterResult>;grantWorkspace(p:WorkspaceGrantPayload):Promise<WorkspaceGrantResult>;leaseWorkspace(p:WorkspaceLeasePayload):Promise<WorkspaceLeaseResult>;decide(p:ReviewDecidePayload):Promise<ReviewDecideResult>;previewChanges(p:ChangesetPreviewPayload):Promise<ChangesetPreviewResult>;applyChanges(p:ChangesetApplyPayload):Promise<ChangesetApplyResult>;revertChanges(p:ChangesetRevertPayload):Promise<ChangesetRevertResult>;requestCommandReview(p:CommandReviewRequestPayload):Promise<CommandReviewRequestResult>;startCommand(p:CommandStartPayload):Promise<CommandStartResult>;getCommand(p:CommandGetPayload):Promise<CommandGetResult>;cancelCommand(p:CommandCancelPayload):Promise<CommandCancelResult>;fetchWeb(p:WebFetchPayload):Promise<WebFetchResult>;searchWeb(p:WebSearchPayload):Promise<WebSearchResult>;putPlan(p:RunPlanPutPayload):Promise<RunPlanPutResult>;evidence(p:EvidenceListPayload):Promise<EvidenceListResult>
}
export function createAgentRuntimeBridge(transport:WebViewTransport,deadline=30_000):AgentRuntimeBridge{const core=createSimpleBridge(transport,{},deadline),mut=<P extends object,R>(method:MutationMethod,p:P)=>core.request<R>(method as BridgeMethod,p,deadline,createMutationAttempt(method,p) as MutationAttempt<object>);return{capabilities:()=>core.request('capability.list',{}),start:p=>mut('agent.run.start',p),get:p=>core.request('agent.run.get',p),cancel:p=>mut('agent.run.cancel',p),resume:p=>mut('agent.run.resume',p),reconcile:p=>mut('agent.run.reconcile',p),registerWorkspace:p=>mut('workspace.register',p),grantWorkspace:p=>mut('workspace.grant',p),leaseWorkspace:p=>mut('workspace.lease',p),decide:p=>mut('review.decide',p),previewChanges:p=>mut('changeset.preview',p),applyChanges:p=>mut('changeset.apply',p),revertChanges:p=>mut('changeset.revert',p),requestCommandReview:p=>mut('command.review.request',p),startCommand:p=>mut('command.start',p),getCommand:p=>core.request('command.get',p),cancelCommand:p=>mut('command.cancel',p),fetchWeb:p=>mut('web.fetch',p),searchWeb:p=>mut('web.search',p),putPlan:p=>mut('run.plan.put',p),evidence:p=>core.request('evidence.list',p)}}
let agentRuntimeSingleton:AgentRuntimeBridge|undefined
export function getAgentRuntimeBridge(){return agentRuntimeSingleton??=createAgentRuntimeBridge(webview())}
export const agentRuntimeBridge:AgentRuntimeBridge=new Proxy({} as AgentRuntimeBridge,{get:(_target,key)=>(...args:unknown[])=>(getAgentRuntimeBridge() as unknown as Record<PropertyKey,(...x:unknown[])=>unknown>)[key](...args)})

export interface ReviewBridge {
  list(payload: ReviewListPayload): Promise<ReviewListResult>
  approve(payload: ReviewApprovePayload): Promise<ReviewApproveResult>
  reject(payload: ReviewRejectPayload): Promise<ReviewRejectResult>
}
export function createReviewBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): ReviewBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { list: p => core.request('review.list', p), approve: p => core.request('review.approve', p), reject: p => core.request('review.reject', p) }
}
let reviewSingleton: ReviewBridge | undefined
export function getReviewBridge(): ReviewBridge { return reviewSingleton ??= createReviewBridge(webview()) }
export const reviewBridge: ReviewBridge = { list: p => getReviewBridge().list(p), approve: p => getReviewBridge().approve(p), reject: p => getReviewBridge().reject(p) }

export interface MemoryBridge {
  get(payload: MemoryGetPayload): Promise<MemoryGetResult>
  list(payload: MemoryListPayload): Promise<MemoryListResult>
  create(payload: MemoryCreatePayload, options?: MutationOptions<MemoryCreatePayload>): Promise<MemoryCreateResult>
  search(payload: MemorySearchPayload): Promise<MemorySearchResult>
  update(payload: MemoryUpdatePayload): Promise<MemoryUpdateResult>
  delete(payload: MemoryDeletePayload): Promise<MemoryDeleteResult>
  confirmCandidate?(payload: MemoryConfirmCandidatePayload): Promise<MemoryConfirmCandidateResult>
}
export function createMemoryBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): MemoryBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { get: p => core.request('memory.get', p), list: p => core.request('memory.list', p), create: (p, o) => core.request('memory.create', p, defaultDeadlineMs, o?.attempt), search: p => core.request('memory.search', p), update: p => core.request('memory.update', p), delete: p => core.request('memory.delete', p), confirmCandidate: p => core.request('memory.confirmCandidate', p) }
}
let memorySingleton: MemoryBridge | undefined
export function getMemoryBridge(): MemoryBridge { return memorySingleton ??= createMemoryBridge(webview()) }
export const memoryBridge: MemoryBridge = { get: p => getMemoryBridge().get(p), list: p => getMemoryBridge().list(p), create: (p, o) => getMemoryBridge().create(p, o), search: p => getMemoryBridge().search(p), update: p => getMemoryBridge().update(p), delete: p => getMemoryBridge().delete(p), confirmCandidate: p => getMemoryBridge().confirmCandidate!(p) }

export interface FeedbackBridge {
  record(payload: FeedbackRecordPayload): Promise<FeedbackRecordResult>
  candidates(payload: FeedbackCandidatesPayload): Promise<FeedbackCandidatesResult>
}
export function createFeedbackBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): FeedbackBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { record: p => core.request('feedback.record', p), candidates: p => core.request('feedback.candidates', p) }
}
let feedbackSingleton: FeedbackBridge | undefined
export function getFeedbackBridge(): FeedbackBridge { return feedbackSingleton ??= createFeedbackBridge(webview()) }
export const feedbackBridge: FeedbackBridge = { record: p => getFeedbackBridge().record(p), candidates: p => getFeedbackBridge().candidates(p) }

export interface NominationBridge {
  nominate(payload: MemoryNominatePayload): Promise<MemoryNominateResult>
  list(payload: MemoryNominationListPayload): Promise<MemoryNominationListResult>
  withdraw(payload: MemoryNominationWithdrawPayload): Promise<MemoryNominationWithdrawResult>
}
export function createNominationBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): NominationBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { nominate: p => core.request('memory.nominate', p), list: p => core.request('memory.nomination.list', p), withdraw: p => core.request('memory.nomination.withdraw', p) }
}
let nominationSingleton: NominationBridge | undefined
export function getNominationBridge(): NominationBridge { return nominationSingleton ??= createNominationBridge(webview()) }
export const nominationBridge: NominationBridge = { nominate: p => getNominationBridge().nominate(p), list: p => getNominationBridge().list(p), withdraw: p => getNominationBridge().withdraw(p) }

export interface MemoryOpsBridge {
  stats(payload: MemoryStatsPayload): Promise<MemoryStatsResult>
  listFacts(payload: MemoryFactsListPayload): Promise<MemoryFactsListResult>
  flagFact(payload: MemoryFactsFlagPayload, options?: MutationOptions<MemoryFactsFlagPayload>): Promise<MemoryFactsFlagResult>
  listTraces(payload: MemoryTracesListPayload): Promise<MemoryTracesListResult>
  listGrowth(payload: MemoryGrowthListPayload): Promise<MemoryGrowthListResult>
  decideGrowth(payload: MemoryGrowthDecidePayload, options?: MutationOptions<MemoryGrowthDecidePayload>): Promise<MemoryGrowthDecideResult>
  getSettings(payload: MemorySettingsGetPayload): Promise<MemorySettingsGetResult>
  updateSettings(payload: MemorySettingsUpdatePayload, options?: MutationOptions<MemorySettingsUpdatePayload>): Promise<MemorySettingsUpdateResult>
  export(payload: MemoryExportPayload): Promise<MemoryExportResult>
  purge(payload: MemoryPurgePayload, options?: MutationOptions<MemoryPurgePayload>): Promise<MemoryPurgeResult>
}
export function createMemoryOpsBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): MemoryOpsBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    stats: p => core.request('memory.stats', p),
    listFacts: p => core.request('memory.facts.list', p),
    flagFact: (p, o) => core.request('memory.facts.flag', p, defaultDeadlineMs, o?.attempt),
    listTraces: p => core.request('memory.traces.list', p),
    listGrowth: p => core.request('memory.growth.list', p),
    decideGrowth: (p, o) => core.request('memory.growth.decide', p, defaultDeadlineMs, o?.attempt),
    getSettings: p => core.request('memory.settings.get', p),
    updateSettings: (p, o) => core.request('memory.settings.update', p, defaultDeadlineMs, o?.attempt),
    export: p => core.request('memory.export', p),
    purge: (p, o) => core.request('memory.purge', p, defaultDeadlineMs, o?.attempt),
  }
}
let memoryOpsSingleton: MemoryOpsBridge | undefined
export function getMemoryOpsBridge(): MemoryOpsBridge { return memoryOpsSingleton ??= createMemoryOpsBridge(webview()) }
export const memoryOpsBridge: MemoryOpsBridge = {
  stats: p => getMemoryOpsBridge().stats(p),
  listFacts: p => getMemoryOpsBridge().listFacts(p),
  flagFact: (p, o) => getMemoryOpsBridge().flagFact(p, o),
  listTraces: p => getMemoryOpsBridge().listTraces(p),
  listGrowth: p => getMemoryOpsBridge().listGrowth(p),
  decideGrowth: (p, o) => getMemoryOpsBridge().decideGrowth(p, o),
  getSettings: p => getMemoryOpsBridge().getSettings(p),
  updateSettings: (p, o) => getMemoryOpsBridge().updateSettings(p, o),
  export: p => getMemoryOpsBridge().export(p),
  purge: (p, o) => getMemoryOpsBridge().purge(p, o),
}

export interface RunQueueBridge {
  input(payload: RunQueueInputPayload, options?: MutationOptions<RunQueueInputPayload>): Promise<RunQueueInputResult>
  list(payload: RunQueueListPayload): Promise<RunQueueListResult>
  withdraw(payload: RunQueueWithdrawPayload, options?: MutationOptions<RunQueueWithdrawPayload>): Promise<RunQueueWithdrawResult>
  consume(payload: RunQueueConsumePayload): Promise<RunQueueConsumeResult>
}
export function createRunQueueBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): RunQueueBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { input: (p, o) => core.request('run.queueInput', p, defaultDeadlineMs, o?.attempt), list: p => core.request('run.queueList', p), withdraw: (p, o) => core.request('run.queueWithdraw', p, defaultDeadlineMs, o?.attempt), consume: p => core.request('run.queueConsume', p) }
}
let runQueueSingleton: RunQueueBridge | undefined
export function getRunQueueBridge(): RunQueueBridge { return runQueueSingleton ??= createRunQueueBridge(webview()) }
export const runQueueBridge: RunQueueBridge = { input: (p, o) => getRunQueueBridge().input(p, o), list: p => getRunQueueBridge().list(p), withdraw: (p, o) => getRunQueueBridge().withdraw(p, o), consume: p => getRunQueueBridge().consume(p) }

// M10 wave-3 MCP-market bridge — catalog browse, 8-rule validation chain,
// single-use confirmation tokens, endpoint lifecycle and usage statistics.
export interface McBridge {
  marketList(payload?: McMarketListPayload): Promise<McMarketListResult>
  marketDetail(payload: McMarketDetailPayload): Promise<McMarketDetailResult>
  configValidate(payload: McConfigValidatePayload): Promise<McConfigValidateResult>
  confirmToken(payload: McConfirmTokenPayload, options?: MutationOptions<McConfirmTokenPayload>): Promise<McConfirmTokenResult>
  install(payload: McConnectorInstallPayload, options?: MutationOptions<McConnectorInstallPayload>): Promise<McConnectorInstallResult>
  uninstall(payload: McConnectorUninstallPayload, options?: MutationOptions<McConnectorUninstallPayload>): Promise<McConnectorUninstallResult>
  update(payload: McConnectorUpdatePayload, options?: MutationOptions<McConnectorUpdatePayload>): Promise<McConnectorUpdateResult>
  usage(payload?: McConnectorUsagePayload): Promise<McConnectorUsageResult>
  tombstoneCheck(): Promise<McTombstoneCheckResult>
}
export function createMcBridge(transport: WebViewTransport, defaultDeadlineMs = 12_000): McBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    marketList: p => core.request('mc.market.list', p ?? {}),
    marketDetail: p => core.request('mc.market.detail', p),
    configValidate: p => core.request('mc.config.validate', p),
    confirmToken: (p, o) => core.request('mc.confirm.token', p, defaultDeadlineMs, o?.attempt),
    install: (p, o) => core.request('mc.connector.install', p, 30_000, o?.attempt),
    uninstall: (p, o) => core.request('mc.connector.uninstall', p, defaultDeadlineMs, o?.attempt),
    update: (p, o) => core.request('mc.connector.update', p, 30_000, o?.attempt),
    usage: p => core.request('mc.connector.usage', p ?? {}),
    tombstoneCheck: () => core.request('mc.tombstone.check', {}, 15_000),
  }
}
let mcSingleton: McBridge | undefined
export function getMcBridge(): McBridge { return mcSingleton ??= createMcBridge(webview()) }
export const mcBridge: McBridge = {
  marketList: p => getMcBridge().marketList(p),
  marketDetail: p => getMcBridge().marketDetail(p),
  configValidate: p => getMcBridge().configValidate(p),
  confirmToken: (p, o) => getMcBridge().confirmToken(p, o),
  install: (p, o) => getMcBridge().install(p, o),
  uninstall: (p, o) => getMcBridge().uninstall(p, o),
  update: (p, o) => getMcBridge().update(p, o),
  usage: p => getMcBridge().usage(p),
  tombstoneCheck: () => getMcBridge().tombstoneCheck(),
}

// M10 wave-3 browser multi-mode bridge — 5-mode connection settings, CDP
// session lifecycle, navigation policy, data usage and permission queue.
export interface BrBridge {
  getSettings(payload?: BrSettingsGetPayload): Promise<BrSettingsGetResult>
  updateSettings(payload: BrSettingsUpdatePayload): Promise<BrSettingsUpdateResult>
  detectModes(payload?: BrModeDetectPayload): Promise<BrModeDetectResult>
  connect(payload: BrSessionConnectPayload): Promise<BrSessionConnectResult>
  listSessions(payload?: BrSessionListPayload): Promise<BrSessionListResult>
  disconnect(payload: BrSessionDisconnectPayload): Promise<BrSessionDisconnectResult>
  navigate(payload: BrNavigatePayload): Promise<BrNavigateResult>
  dataUsage(payload?: BrDataUsagePayload): Promise<BrDataUsageResult>
  clearData(payload: BrDataClearPayload): Promise<BrDataClearResult>
  listPermissions(payload?: BrPermissionListPayload): Promise<BrPermissionListResult>
  requestPermission(payload: BrPermissionRequestPayload): Promise<BrPermissionRequestResult>
  decidePermission(payload: BrPermissionDecidePayload): Promise<BrPermissionDecideResult>
  setPermissionPolicy(payload: BrPermissionPolicyPayload): Promise<BrPermissionPolicyResult>
}
export function createBrBridge(transport: WebViewTransport, defaultDeadlineMs = 10_000): BrBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    getSettings: () => core.request('br.settings.get', {}),
    updateSettings: p => core.request('br.settings.update', p),
    detectModes: () => core.request('br.mode.detect', {}),
    connect: p => core.request('br.session.connect', p, 12_000),
    listSessions: () => core.request('br.session.list', {}),
    disconnect: p => core.request('br.session.disconnect', p),
    navigate: p => core.request('br.navigate', p),
    dataUsage: p => core.request('br.data.usage', p ?? {}),
    clearData: p => core.request('br.data.clear', p ?? {}, 15_000),
    listPermissions: p => core.request('br.permission.list', p ?? {}),
    requestPermission: p => core.request('br.permission.request', p),
    decidePermission: p => core.request('br.permission.decide', p),
    setPermissionPolicy: p => core.request('br.permission.policy', p),
  }
}
let brSingleton: BrBridge | undefined
export function getBrBridge(): BrBridge { return brSingleton ??= createBrBridge(webview()) }
export const brBridge: BrBridge = {
  getSettings: p => getBrBridge().getSettings(p),
  updateSettings: p => getBrBridge().updateSettings(p),
  detectModes: p => getBrBridge().detectModes(p),
  connect: p => getBrBridge().connect(p),
  listSessions: p => getBrBridge().listSessions(p),
  disconnect: p => getBrBridge().disconnect(p),
  navigate: p => getBrBridge().navigate(p),
  dataUsage: p => getBrBridge().dataUsage(p),
  clearData: p => getBrBridge().clearData(p),
  listPermissions: p => getBrBridge().listPermissions(p),
  requestPermission: p => getBrBridge().requestPermission(p),
  decidePermission: p => getBrBridge().decidePermission(p),
  setPermissionPolicy: p => getBrBridge().setPermissionPolicy(p),
}

// M10 wave-4 computer-control bridge — the security configuration, the
// append-only audit ledger and the emergency-stop latch.
export interface CcBridge {
  getConfig(payload?: CcGetConfigPayload): Promise<CcGetConfigResult>
  updateConfig(payload: CcUpdateConfigPayload): Promise<CcUpdateConfigResult>
  getAuditLog(payload?: CcGetAuditLogPayload): Promise<CcGetAuditLogResult>
  emergencyStop(payload?: CcEmergencyStopPayload): Promise<CcEmergencyStopResult>
}
export function createCcBridge(transport: WebViewTransport, defaultDeadlineMs = 10_000): CcBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    getConfig: () => core.request('cc.getConfig', {}),
    updateConfig: p => core.request('cc.updateConfig', p),
    getAuditLog: p => core.request('cc.getAuditLog', p ?? {}),
    emergencyStop: p => core.request('cc.emergencyStop', p ?? {}),
  }
}
let ccSingleton: CcBridge | undefined
export function getCcBridge(): CcBridge { return ccSingleton ??= createCcBridge(webview()) }
export const ccBridge: CcBridge = {
  getConfig: p => getCcBridge().getConfig(p),
  updateConfig: p => getCcBridge().updateConfig(p),
  getAuditLog: p => getCcBridge().getAuditLog(p),
  emergencyStop: p => getCcBridge().emergencyStop(p),
}

export interface ImBridge {
  get(payload?: ImChannelsGetPayload): Promise<ImChannelsGetResult>
  set(payload: ImChannelsSetPayload): Promise<ImChannelsSetResult>
}
export function createImBridge(transport: WebViewTransport, defaultDeadlineMs = 10_000): ImBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    get: () => core.request('im.channels.get', {}),
    set: p => core.request('im.channels.set', p),
  }
}
let imSingleton: ImBridge | undefined
export function getImBridge(): ImBridge { return imSingleton ??= createImBridge(webview()) }
export const imBridge: ImBridge = {
  get: p => getImBridge().get(p),
  set: p => getImBridge().set(p),
}

export interface OntologyBridge {
  getNode(payload: OntologyNodeGetPayload): Promise<OntologyNodeGetResult>
  listNodes(payload: OntologyNodeListPayload): Promise<OntologyNodeListResult>
  searchNodes(payload: OntologyNodeSearchPayload): Promise<OntologyNodeSearchResult>
  createNode(payload: OntologyNodeCreatePayload): Promise<OntologyNodeCreateResult>
  updateNode(payload: OntologyNodeUpdatePayload): Promise<OntologyNodeUpdateResult>
  deleteNode(payload: OntologyNodeDeletePayload): Promise<OntologyNodeDeleteResult>
  listEdges(payload: OntologyEdgeListPayload): Promise<OntologyEdgeListResult>
  createEdge(payload: OntologyEdgeCreatePayload): Promise<OntologyEdgeCreateResult>
  updateEdge(payload: OntologyEdgeUpdatePayload): Promise<OntologyEdgeUpdateResult>
  deleteEdge(payload: OntologyEdgeDeletePayload): Promise<OntologyEdgeDeleteResult>
}
export function createOntologyBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): OntologyBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { getNode: p => core.request('ontology.node.get', p), listNodes: p => core.request('ontology.node.list', p), searchNodes: p => core.request('ontology.node.search', p), createNode: p => core.request('ontology.node.create', p), updateNode: p => core.request('ontology.node.update', p), deleteNode: p => core.request('ontology.node.delete', p), listEdges: p => core.request('ontology.edge.list', p), createEdge: p => core.request('ontology.edge.create', p), updateEdge: p => core.request('ontology.edge.update', p), deleteEdge: p => core.request('ontology.edge.delete', p) }
}
let ontologySingleton: OntologyBridge | undefined
export function getOntologyBridge(): OntologyBridge { return ontologySingleton ??= createOntologyBridge(webview()) }
export const ontologyBridge: OntologyBridge = { getNode: p => getOntologyBridge().getNode(p), listNodes: p => getOntologyBridge().listNodes(p), searchNodes: p => getOntologyBridge().searchNodes(p), createNode: p => getOntologyBridge().createNode(p), updateNode: p => getOntologyBridge().updateNode(p), deleteNode: p => getOntologyBridge().deleteNode(p), listEdges: p => getOntologyBridge().listEdges(p), createEdge: p => getOntologyBridge().createEdge(p), updateEdge: p => getOntologyBridge().updateEdge(p), deleteEdge: p => getOntologyBridge().deleteEdge(p) }

export interface SkillBridge {
  get(payload: SkillGetPayload): Promise<SkillGetResult>
  list(payload: SkillListPayload): Promise<SkillListResult>
  create(payload: SkillCreatePayload, options?: MutationOptions<SkillCreatePayload>): Promise<SkillCreateResult>
  update(payload: SkillUpdatePayload, options?: MutationOptions<SkillUpdatePayload>): Promise<SkillUpdateResult>
  delete(payload: SkillDeletePayload, options?: MutationOptions<SkillDeletePayload>): Promise<SkillDeleteResult>
  match(payload: SkillMatchPayload): Promise<SkillMatchResult>
  publish(payload: SkillPublishPayload): Promise<SkillPublishResult>
  deprecate(payload: SkillDeprecatePayload): Promise<SkillDeprecateResult>
  disable(payload: SkillDisablePayload): Promise<SkillDisableResult>
  invoke?(payload:SkillInvokePayload):Promise<SkillInvokeResult>
  execute?(payload:SkillExecutePayload):Promise<SkillExecuteResult>
  catalogList?(payload:SkillCatalogListPayload):Promise<SkillCatalogListResult>
  install?(payload:SkillInstallPayload):Promise<SkillInstallResult>
  categorySet?(payload:SkillCategorySetPayload):Promise<SkillCategorySetResult>
}
export function createSkillBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): SkillBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return { get: p => core.request('skill.get', p), list: p => core.request('skill.list', p), create: (p, o) => core.request('skill.create', p, defaultDeadlineMs, o?.attempt), update: (p, o) => core.request('skill.update', p, defaultDeadlineMs, o?.attempt), delete: (p, o) => core.request('skill.delete', p, defaultDeadlineMs, o?.attempt), match: p => core.request('skill.match', p), publish: p => core.request('skill.publish', p), deprecate: p => core.request('skill.deprecate', p), disable: p => core.request('skill.disable', p),invoke:p=>core.request('skill.invoke',p),execute:p=>core.request('skill.execute',p),catalogList:p=>core.request('skill.catalog.list',p),install:p=>core.request('skill.install',p),categorySet:p=>core.request('skill.category.set',p,defaultDeadlineMs) }
}
let skillSingleton: SkillBridge | undefined
export function getSkillBridge(): SkillBridge { return skillSingleton ??= createSkillBridge(webview()) }
export const skillBridge: SkillBridge = { get: p => getSkillBridge().get(p), list: p => getSkillBridge().list(p), create: (p, o) => getSkillBridge().create(p, o), update: (p, o) => getSkillBridge().update(p, o), delete: (p, o) => getSkillBridge().delete(p, o), match: p => getSkillBridge().match(p), publish: p => getSkillBridge().publish(p), deprecate: p => getSkillBridge().deprecate(p), disable: p => getSkillBridge().disable(p),invoke:p=>getSkillBridge().invoke!(p),execute:p=>getSkillBridge().execute!(p),catalogList:p=>getSkillBridge().catalogList!(p),install:p=>getSkillBridge().install!(p),categorySet:p=>getSkillBridge().categorySet!(p) }

export type StreamArtifact={kind:'html'|'xlsx'|'docx'|'pptx'|'pdf'|'image';path:string;content:string}
export type StreamEvent =
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'delta';delta:{text:string}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'thinking';thinking:{text:string}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'usage';usage:{inputTokens:number;outputTokens:number;totalTokens:number}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'tool_started';tool:{callId:string;name:string;argsDigest:string;summary?:string}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'tool_completed';tool:{callId:string;name:string;argsDigest:string;summary?:string;artifact?:StreamArtifact}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'approval_required';tool:{callId:string;name:string;argsDigest:string;summary?:string}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'tool_output';tool:{callId:string;name:string;argsDigest:string;summary?:string}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'completed';completed?:{messageId:string}}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'cancelled'}
 | {v:typeof BRIDGE_VERSION;kind:'event';id:string;streamId:string;sequence:number;type:'failed';error:{code:string;message:string;retryable:boolean}}
export interface ChatStream { readonly streamId:string; cancel():Promise<boolean>; dispose():void }
export interface ChatBridge { start(payload:ChatStartPayload,onEvent:(event:StreamEvent)=>void):Promise<ChatStream>; approve?(payload:ChatToolApprovePayload):Promise<ChatToolApproveResult>; dispose():void }
const nonnegativeInt=(v:unknown)=>Number.isInteger(v)&&Number(v)>=0
const validArtifactPath=(path:unknown)=>typeof path==='string'&&path.length>0&&path.length<=512&&!path.startsWith('/')&&!path.includes('\\')&&!path.split('/').includes('..')
const isStreamArtifact=(artifact:unknown):artifact is StreamArtifact=>{
 if(!isObj(artifact)||!exact(artifact,['kind','path','content']))return false
 const path=artifact.path
 if(typeof path!=='string'||!validArtifactPath(path)||typeof artifact.content!=='string')return false
 switch(artifact.kind){
  case'html':return/\.html?$/i.test(path)&&new TextEncoder().encode(artifact.content).length<=184320
  case'xlsx':case'docx':case'pptx':case'pdf':return artifact.content===''
  case'image':return/\.png$/i.test(path)&&artifact.content===''
  default:return false
 }
}
const isStreamEvent=(v:unknown):v is StreamEvent=>{
 if(!isObj(v)||v.v!==BRIDGE_VERSION||v.kind!=='event'||!isULID(v.id)||!isULID(v.streamId)||!Number.isInteger(v.sequence)||Number(v.sequence)<1||typeof v.type!=='string')return false
 const base=['v','kind','id','streamId','sequence','type']
 switch(v.type){
  case'delta':return exact(v,[...base,'delta'])&&isObj(v.delta)&&exact(v.delta,['text'])&&typeof v.delta.text==='string'&&v.delta.text.length>0
  case'thinking':return exact(v,[...base,'thinking'])&&isObj(v.thinking)&&exact(v.thinking,['text'])&&typeof v.thinking.text==='string'&&v.thinking.text.length>0&&new TextEncoder().encode(v.thinking.text).length<=16384
  case'usage':return exact(v,[...base,'usage'])&&isObj(v.usage)&&exact(v.usage,['inputTokens','outputTokens','totalTokens'])&&nonnegativeInt(v.usage.inputTokens)&&nonnegativeInt(v.usage.outputTokens)&&nonnegativeInt(v.usage.totalTokens)
  case'tool_started':case'approval_required':case'tool_output':return exact(v,[...base,'tool'])&&isObj(v.tool)&&exact(v.tool,['callId','name','argsDigest'],['summary'])&&typeof v.tool.callId==='string'&&v.tool.callId.length>0&&typeof v.tool.name==='string'&&v.tool.name.length>0&&typeof v.tool.argsDigest==='string'&&/^[0-9a-f]{64}$/.test(v.tool.argsDigest)&&(!('summary'in v.tool)||typeof v.tool.summary==='string')
  case'tool_completed':return exact(v,[...base,'tool'])&&isObj(v.tool)&&exact(v.tool,['callId','name','argsDigest'],['summary','artifact'])&&typeof v.tool.callId==='string'&&v.tool.callId.length>0&&typeof v.tool.name==='string'&&v.tool.name.length>0&&typeof v.tool.argsDigest==='string'&&/^[0-9a-f]{64}$/.test(v.tool.argsDigest)&&(!('summary'in v.tool)||typeof v.tool.summary==='string')&&(!('artifact'in v.tool)||isStreamArtifact(v.tool.artifact))
  case'completed':return exact(v,'completed'in v?[...base,'completed']:base)&&(!('completed'in v)||isObj(v.completed)&&exact(v.completed,['messageId'])&&isULID(v.completed.messageId))
  case'cancelled':return exact(v,base)
  case'failed':return exact(v,[...base,'error'])&&isObj(v.error)&&exact(v.error,['code','message','retryable'])&&typeof v.error.code==='string'&&v.error.code.length>0&&typeof v.error.message==='string'&&v.error.message.length>0&&typeof v.error.retryable==='boolean'
  default:return false
 }
}
export function createChatBridge(transport:WebViewTransport,deadlineMs=30_000):ChatBridge {
 type Pending={resolve(v:unknown):void;reject(e:Error):void;timer:number}
 type Active={listener:(e:StreamEvent)=>void;next:number;terminal:boolean}
 const pending=new Map<string,Pending>(),active=new Map<string,Active>(),early=new Map<string,StreamEvent[]>(),tombstones=new Map<string,number>();let disposed=false
 const tombstone=(id:string)=>{tombstones.delete(id);tombstones.set(id,Date.now());while(tombstones.size>128)tombstones.delete(tombstones.keys().next().value!)}
 const failStream=(id:string)=>{active.delete(id);early.delete(id);tombstone(id)}
 const failActive=(id:string,state:Active,code:string,message:string)=>{if(state.terminal)return;state.terminal=true;const sequence=state.next;failStream(id);state.listener({v:BRIDGE_VERSION,kind:'event',id:ulid(),streamId:id,sequence,type:'failed',error:{code,message,retryable:false}})}
 const deliver=(state:Active,event:StreamEvent)=>{if(state.terminal)return;if(event.sequence!==state.next){failActive(event.streamId,state,'BRIDGE_EVENT_SEQUENCE_INVALID','流事件顺序无效，已安全终止');return}state.next++;if(['completed','cancelled','failed'].includes(event.type)){state.terminal=true;failStream(event.streamId)}try{state.listener(event)}catch(err){console.error('[lunitide] chat stream listener',err);if(!state.terminal){try{failActive(event.streamId,state,'RENDERER_STREAM_LISTENER','界面处理流事件失败')}catch{failStream(event.streamId)}}}}
 const route=(event:MessageEvent<BridgeResponse>)=>{const value:unknown=event.data;if(disposed)return;if(isObj(value)&&typeof value.requestId==='string'&&pending.has(value.requestId)){const p=pending.get(value.requestId)!;pending.delete(value.requestId);clearTimeout(p.timer);if(!validEnvelope(value))p.reject(new BridgeClientError('Bridge 响应格式无效','INVALID_BRIDGE_RESPONSE',false,value.requestId));else if(value.ok)p.resolve(value.payload);else p.reject(new BridgeClientError(value.error.message,value.error.code,value.error.retryable,value.error.correlationId));return}const candidateId=isObj(value)&&typeof value.streamId==='string'&&isULID(value.streamId)?value.streamId:undefined;if(!isStreamEvent(value)){if(candidateId){const state=active.get(candidateId);if(state)failActive(candidateId,state,'INVALID_BRIDGE_EVENT','流事件格式无效，已安全终止');else{early.delete(candidateId);tombstone(candidateId)}}return}if(tombstones.has(value.streamId))return;const state=active.get(value.streamId);if(state){deliver(state,value);return}const buffered=early.get(value.streamId)??[];if(buffered.length>=32||early.size>=32&&!early.has(value.streamId)){early.delete(value.streamId);tombstone(value.streamId);return}if(value.sequence!==buffered.length+1||buffered.some(e=>['completed','cancelled','failed'].includes(e.type))){early.delete(value.streamId);tombstone(value.streamId);return}buffered.push(value);early.set(value.streamId,buffered)}
 transport.addEventListener('message',route)
 const request=<T>(method:BridgeMethod,payload:object)=>new Promise<T>((resolve,reject)=>{if(disposed){reject(new BridgeClientError('Chat Bridge 已释放','BRIDGE_UNAVAILABLE',false,'renderer'));return}const id=ulid(),traceId=ulid(),ms=Math.min(30_000,Math.max(1,deadlineMs)),timer=window.setTimeout(()=>{pending.delete(id);reject(new BridgeClientError('Bridge 请求超时','REQUEST_DEADLINE_EXCEEDED',true,traceId))},ms+250);pending.set(id,{resolve,reject,timer});try{transport.postMessage({v:BRIDGE_VERSION,kind:'request',id,traceId,method,sentAt:new Date().toISOString(),payload,deadlineMs:ms})}catch{clearTimeout(timer);pending.delete(id);reject(new BridgeClientError('WebView2 Bridge 当前不可用','BRIDGE_UNAVAILABLE',true,traceId))}})
 const cancelLocal=(id:string)=>{if(!active.has(id)&&!early.has(id))return;failStream(id);try{void request<StreamCancelResult>('stream.cancel',{streamId:id}).catch(()=>{})}catch{/* best effort */}}
 return {async start(payload,onEvent){const result=await request<ChatStartResult>('chat.start',payload);if(!isObj(result)||!exact(result,['streamId'])||!isULID(result.streamId))throw new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,'renderer');if(disposed){try{void request('stream.cancel',{streamId:result.streamId})}catch{}throw new BridgeClientError('Chat Bridge 已释放','BRIDGE_UNAVAILABLE',false,'renderer')}const state:Active={listener:onEvent,next:1,terminal:false};active.set(result.streamId,state);if(tombstones.has(result.streamId)){failActive(result.streamId,state,'BRIDGE_EARLY_EVENT_INVALID','流在建立前收到无效事件，已安全终止')}else{const buffered=early.get(result.streamId)??[];early.delete(result.streamId);for(const event of buffered){if(!active.has(result.streamId))break;deliver(state,event)}}return{streamId:result.streamId,cancel:async()=>{if(!active.has(result.streamId))return false;const r=await request<StreamCancelResult>('stream.cancel',{streamId:result.streamId});return isObj(r)&&exact(r,['cancelled'])&&r.cancelled===true},dispose:()=>cancelLocal(result.streamId)}},approve:p=>request<ChatToolApproveResult>('chat.tool.approve',p),dispose(){if(disposed)return;for(const id of [...active.keys()])cancelLocal(id);disposed=true;early.clear();for(const [id,p]of pending){clearTimeout(p.timer);p.reject(new BridgeClientError('Chat Bridge 已释放','BRIDGE_UNAVAILABLE',false,id))}pending.clear();transport.removeEventListener('message',route)}}
}

export interface ContextBridge {
  status(payload: ContextStatusPayload): Promise<ContextStatusResult>
  compactPreview(payload: ContextCompactPreviewPayload): Promise<ContextCompactPreviewResult>
  compactCommit(payload: ContextCompactCommitPayload): Promise<ContextCompactCommitResult>
  compactCancel(payload: ContextCompactCancelPayload): Promise<ContextCompactCancelResult>
  handoffCreate(payload: ContextHandoffCreatePayload): Promise<ContextHandoffCreateResult>
  handoffImport(payload: ContextHandoffImportPayload): Promise<ContextHandoffImportResult>
  handoffInspect(payload: ContextHandoffInspectPayload): Promise<ContextHandoffInspectResult>
  handoffList(payload: ContextHandoffListPayload): Promise<ContextHandoffListResult>
  handoffListImports(payload: ContextHandoffListImportsPayload): Promise<ContextHandoffListImportsResult>
  handoffRevoke(payload: ContextHandoffRevokePayload): Promise<ContextHandoffRevokeResult>
}
export function createContextBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): ContextBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  const longDeadline = 60_000 // compaction/handoff may invoke provider summarization
  return {
    status: p => core.request('context.status', p),
    compactPreview: p => core.request('context.compact.preview', p, longDeadline),
    compactCommit: p => core.request('context.compact.commit', p),
    compactCancel: p => core.request('context.compact.cancel', p),
    handoffCreate: p => core.request('context.handoff.create', p),
    handoffImport: p => core.request('context.handoff.import', p),
    handoffInspect: p => core.request('context.handoff.inspect', p),
    handoffList: p => core.request('context.handoff.list', p),
    handoffListImports: p => core.request('context.handoff.list-imports', p),
    handoffRevoke: p => core.request('context.handoff.revoke', p),
  }
}
let contextSingleton: ContextBridge | undefined
export function getContextBridge(): ContextBridge { return contextSingleton ??= createContextBridge(webview()) }
export const contextBridge: ContextBridge = {
  status: p => getContextBridge().status(p),
  compactPreview: p => getContextBridge().compactPreview(p),
  compactCommit: p => getContextBridge().compactCommit(p),
  compactCancel: p => getContextBridge().compactCancel(p),
  handoffCreate: p => getContextBridge().handoffCreate(p),
  handoffImport: p => getContextBridge().handoffImport(p),
  handoffInspect: p => getContextBridge().handoffInspect(p),
  handoffList: p => getContextBridge().handoffList(p),
  handoffListImports: p => getContextBridge().handoffListImports(p),
  handoffRevoke: p => getContextBridge().handoffRevoke(p),
}

export interface AttachmentBridge {
  ingest(payload: AttachmentIngestPayload, options?: MutationOptions<AttachmentIngestPayload>): Promise<AttachmentIngestResult>
  get(payload: AttachmentGetPayload): Promise<AttachmentGetResult>
  list(payload: AttachmentListPayload): Promise<AttachmentListResult>
  delete(payload: AttachmentDeletePayload, options?: MutationOptions<AttachmentDeletePayload>): Promise<AttachmentDeleteResult>
  begin(payload:AttachmentUploadBeginPayload):Promise<AttachmentUploadBeginResult>; chunk(payload:AttachmentUploadChunkPayload):Promise<AttachmentUploadChunkResult>; commit(payload:AttachmentUploadCommitPayload):Promise<AttachmentUploadCommitResult>; abort(payload:AttachmentUploadAbortPayload):Promise<AttachmentUploadAbortResult>
}
export function createAttachmentBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): AttachmentBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    ingest: (p, o) => core.request('attachment.ingest', p, 30_000, o?.attempt),
    get: p => core.request('attachment.get', p),
    list: p => core.request('attachment.list', p),
    delete: (p, o) => core.request('attachment.delete', p, defaultDeadlineMs, o?.attempt),
    begin:p=>core.request('attachment.upload.begin',p,30_000),chunk:p=>core.request('attachment.upload.chunk',p,30_000),commit:p=>core.request('attachment.upload.commit',p,30_000),abort:p=>core.request('attachment.upload.abort',p),
  }
}
let attachmentSingleton: AttachmentBridge | undefined
export function getAttachmentBridge(): AttachmentBridge { return attachmentSingleton ??= createAttachmentBridge(webview()) }
export const attachmentBridge: AttachmentBridge = {
  ingest: (p, o) => getAttachmentBridge().ingest(p, o),
  get: p => getAttachmentBridge().get(p),
  list: p => getAttachmentBridge().list(p),
  delete: (p, o) => getAttachmentBridge().delete(p, o),
  begin:p=>getAttachmentBridge().begin(p),chunk:p=>getAttachmentBridge().chunk(p),commit:p=>getAttachmentBridge().commit(p),abort:p=>getAttachmentBridge().abort(p),
}

export interface TemplateBridge {
  list(payload?: TemplateListPayload): Promise<TemplateListResult>
  fileStage(payload: TemplateFileStagePayload): Promise<TemplateFileStageResult>
  create(payload: TemplateCreatePayload, options?: MutationOptions<TemplateCreatePayload>): Promise<TemplateCreateResult>
  enable(payload: TemplateEnablePayload, options?: MutationOptions<TemplateEnablePayload>): Promise<TemplateEnableResult>
  void(payload: TemplateVoidPayload, options?: MutationOptions<TemplateVoidPayload>): Promise<TemplateVoidResult>
  restore(payload: TemplateRestorePayload, options?: MutationOptions<TemplateRestorePayload>): Promise<TemplateRestoreResult>
  delete(payload: TemplateDeletePayload, options?: MutationOptions<TemplateDeletePayload>): Promise<TemplateDeleteResult>
}
export function createTemplateBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): TemplateBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    list: p => core.request('template.list', p ?? {}),
    fileStage: p => retryBridgeRequest(() => core.request('template.file.stage', p, TEMPLATE_FILE_DEADLINE_MS)),
    create: (p, o) => retryBridgeRequest(() => core.request('template.create', p, TEMPLATE_FILE_DEADLINE_MS, o?.attempt)),
    enable: (p, o) => core.request('template.enable', p, defaultDeadlineMs, o?.attempt),
    void: (p, o) => core.request('template.void', p, defaultDeadlineMs, o?.attempt),
    restore: (p, o) => core.request('template.restore', p, defaultDeadlineMs, o?.attempt),
    delete: (p, o) => core.request('template.delete', p, defaultDeadlineMs, o?.attempt),
  }
}
let templateSingleton: TemplateBridge | undefined
export function getTemplateBridge(): TemplateBridge { return templateSingleton ??= createTemplateBridge(webview()) }
export const templateBridge: TemplateBridge = {
  list: p => getTemplateBridge().list(p),
  fileStage: p => getTemplateBridge().fileStage(p),
  create: (p, o) => getTemplateBridge().create(p, o),
  enable: (p, o) => getTemplateBridge().enable(p, o),
  void: (p, o) => getTemplateBridge().void(p, o),
  restore: (p, o) => getTemplateBridge().restore(p, o),
  delete: (p, o) => getTemplateBridge().delete(p, o),
}

export interface DeliverableBridge {
  list(payload: DeliverableListPayload): Promise<DeliverableListResult>
  upsert(payload: DeliverableUpsertPayload, options?: MutationOptions<DeliverableUpsertPayload>): Promise<DeliverableUpsertResult>
  confirmGate(payload: DeliverableConfirmGatePayload, options?: MutationOptions<DeliverableConfirmGatePayload>): Promise<DeliverableConfirmGateResult>
}
export function createDeliverableBridge(transport: WebViewTransport, defaultDeadlineMs = 8_000): DeliverableBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    list: p => core.request('deliverable.list', p),
    upsert: (p, o) => core.request('deliverable.upsert', p, defaultDeadlineMs, o?.attempt),
    confirmGate: (p, o) => core.request('deliverable.confirmGate', p, defaultDeadlineMs, o?.attempt),
  }
}
let deliverableSingleton: DeliverableBridge | undefined
export function getDeliverableBridge(): DeliverableBridge { return deliverableSingleton ??= createDeliverableBridge(webview()) }
export const deliverableBridge: DeliverableBridge = {
  list: p => getDeliverableBridge().list(p),
  upsert: (p, o) => getDeliverableBridge().upsert(p, o),
  confirmGate: (p, o) => getDeliverableBridge().confirmGate(p, o),
}

export interface ReleaseBridge {
  buildPackage(payload: ReleaseBuildPackagePayload, options?: MutationOptions<ReleaseBuildPackagePayload>): Promise<ReleaseBuildPackageResult>
  createRevision(payload: ReleaseCreateRevisionPayload, options?: MutationOptions<ReleaseCreateRevisionPayload>): Promise<ReleaseCreateRevisionResult>
  getRevision(payload: ReleaseGetRevisionPayload): Promise<ReleaseGetRevisionResult>
  getPackage(payload: ReleaseGetPackagePayload): Promise<ReleaseGetPackageResult>
  promote(payload: ReleasePromotePayload, options?: MutationOptions<ReleasePromotePayload>): Promise<ReleasePromoteResult>
  getPromotion(payload: ReleaseGetPromotionPayload): Promise<ReleaseGetPromotionResult>
  rollback(payload: ReleaseRollbackPayload, options?: MutationOptions<ReleaseRollbackPayload>): Promise<ReleaseRollbackResult>
}
export function createReleaseBridge(transport: WebViewTransport, defaultDeadlineMs = 15_000): ReleaseBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    buildPackage: (p, o) => core.request('release.buildPackage', p, 30_000, o?.attempt),
    createRevision: (p, o) => core.request('release.createRevision', p, defaultDeadlineMs, o?.attempt),
    getRevision: p => core.request('release.getRevision', p),
    getPackage: p => core.request('release.getPackage', p),
    promote: (p, o) => core.request('release.promote', p, 30_000, o?.attempt),
    getPromotion: p => core.request('release.getPromotion', p),
    rollback: (p, o) => core.request('release.rollback', p, 30_000, o?.attempt),
  }
}
let releaseSingleton: ReleaseBridge | undefined
export function getReleaseBridge(): ReleaseBridge { return releaseSingleton ??= createReleaseBridge(webview()) }
export const releaseBridge: ReleaseBridge = {
  buildPackage: (p, o) => getReleaseBridge().buildPackage(p, o),
  createRevision: (p, o) => getReleaseBridge().createRevision(p, o),
  getRevision: p => getReleaseBridge().getRevision(p),
  getPackage: p => getReleaseBridge().getPackage(p),
  promote: (p, o) => getReleaseBridge().promote(p, o),
  getPromotion: p => getReleaseBridge().getPromotion(p),
  rollback: (p, o) => getReleaseBridge().rollback(p, o),
}

export interface SkillImportBridge {
  discover(payload: SkillImportDiscoverPayload, options?: MutationOptions<SkillImportDiscoverPayload>): Promise<SkillImportDiscoverResult>
  inspect(payload: SkillImportInspectPayload, options?: MutationOptions<SkillImportInspectPayload>): Promise<SkillImportInspectResult>
  submit(payload: SkillImportSubmitPayload, options?: MutationOptions<SkillImportSubmitPayload>): Promise<SkillImportSubmitResult>
  approve(payload: SkillImportApprovePayload, options?: MutationOptions<SkillImportApprovePayload>): Promise<SkillImportApproveResult>
  reject(payload: SkillImportRejectPayload, options?: MutationOptions<SkillImportRejectPayload>): Promise<SkillImportRejectResult>
  revoke(payload: SkillImportRevokePayload, options?: MutationOptions<SkillImportRevokePayload>): Promise<SkillImportRevokeResult>
}
export function createSkillImportBridge(transport: WebViewTransport, defaultDeadlineMs = 15_000): SkillImportBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    discover: (p, o) => core.request('skill.import.discover', p, defaultDeadlineMs, o?.attempt),
    inspect: (p, o) => core.request('skill.import.inspect', p, defaultDeadlineMs, o?.attempt),
    submit: (p, o) => core.request('skill.import.submit', p, defaultDeadlineMs, o?.attempt),
    approve: (p, o) => core.request('skill.import.approve', p, defaultDeadlineMs, o?.attempt),
    reject: (p, o) => core.request('skill.import.reject', p, defaultDeadlineMs, o?.attempt),
    revoke: (p, o) => core.request('skill.import.revoke', p, defaultDeadlineMs, o?.attempt),
  }
}
let skillImportSingleton: SkillImportBridge | undefined
export function getSkillImportBridge(): SkillImportBridge { return skillImportSingleton ??= createSkillImportBridge(webview()) }
export const skillImportBridge: SkillImportBridge = {
  discover: (p, o) => getSkillImportBridge().discover(p, o),
  inspect: (p, o) => getSkillImportBridge().inspect(p, o),
  submit: (p, o) => getSkillImportBridge().submit(p, o),
  approve: (p, o) => getSkillImportBridge().approve(p, o),
  reject: (p, o) => getSkillImportBridge().reject(p, o),
  revoke: (p, o) => getSkillImportBridge().revoke(p, o),
}

export interface RegistryBridge {
  parseOpenAPI(payload: OpenapiParsePayload): Promise<OpenapiParseResult>
  queryDb(payload: DbQueryPayload): Promise<DbQueryResult>
}
export function createRegistryBridge(transport: WebViewTransport, defaultDeadlineMs = 12_000): RegistryBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    parseOpenAPI: p => core.request('openapi.parse', p),
    queryDb: p => core.request('db.query', p, defaultDeadlineMs),
  }
}
let registrySingleton: RegistryBridge | undefined
export function getRegistryBridge(): RegistryBridge { return registrySingleton ??= createRegistryBridge(webview()) }
export const registryBridge: RegistryBridge = {
  parseOpenAPI: p => getRegistryBridge().parseOpenAPI(p),
  queryDb: p => getRegistryBridge().queryDb(p),
}

export interface ProjectAttachmentBridge {
  list(payload: ProjectAttachmentListPayload): Promise<ProjectAttachmentListResult>
  get(payload: ProjectAttachmentGetPayload): Promise<ProjectAttachmentGetResult>
  ingest(payload: ProjectAttachmentIngestPayload): Promise<ProjectAttachmentIngestResult>
}
export function createProjectAttachmentBridge(transport: WebViewTransport, defaultDeadlineMs = 15_000): ProjectAttachmentBridge {
  const core = createSimpleBridge(transport, {}, defaultDeadlineMs)
  return {
    list: p => core.request('projectAttachment.list', p),
    get: p => core.request('projectAttachment.get', p, 30_000),
    ingest: p => core.request('projectAttachment.ingest', p, 30_000),
  }
}
let projectAttachmentSingleton: ProjectAttachmentBridge | undefined
export function getProjectAttachmentBridge(): ProjectAttachmentBridge { return projectAttachmentSingleton ??= createProjectAttachmentBridge(webview()) }
export const projectAttachmentBridge: ProjectAttachmentBridge = {
  list: p => getProjectAttachmentBridge().list(p),
  get: p => getProjectAttachmentBridge().get(p),
  ingest: p => getProjectAttachmentBridge().ingest(p),
}

export function createTerminalBridge(transport:WebViewTransport,deadlineMs=8000):TerminalBridge{
 const core=createSimpleBridge(transport,{},deadlineMs),listeners=new Map<string,(event:TerminalEvent)=>void>();let disposed=false
 const route=(message:MessageEvent)=>{const e=message.data as unknown;if(!isObj(e)||e.v!==BRIDGE_VERSION||e.kind!=='event'||typeof e.streamId!=='string'||!listeners.has(e.streamId)||!isObj(e.terminal))return;const listener=listeners.get(e.streamId)!;if(e.type==='terminal_output'&&typeof e.terminal.data==='string'&&e.terminal.data.length>0&&e.terminal.data.length<=16384)listener({type:'output',data:e.terminal.data});else if(e.type==='terminal_exit'&&Number.isSafeInteger(e.terminal.exitCode)){listeners.delete(e.streamId);listener({type:'exit',exitCode:Number(e.terminal.exitCode)})}}
 transport.addEventListener('message',route)
 return{async start(payload,onEvent){if(disposed)throw new BridgeClientError('终端桥已释放','BRIDGE_UNAVAILABLE',false,'renderer');const result=await core.request<TerminalStartResult>('terminal.start',payload);if(!isObj(result)||!exact(result,['terminalId'])||!isULID(result.terminalId))throw new BridgeClientError('Bridge 方法结果格式无效','INVALID_BRIDGE_RESULT',false,'renderer');const id=result.terminalId;listeners.set(id,onEvent);let closed=false;const close=async()=>{if(closed)return false;closed=true;listeners.delete(id);const r=await core.request<TerminalCloseResult>('terminal.close',{terminalId:id});return r.closed===true};return{terminalId:id,input:async data=>{if(closed||!data||new TextEncoder().encode(data).length>65536)return false;const r=await core.request<TerminalInputResult>('terminal.input',{terminalId:id,data});return r.accepted===true},resize:async(cols,rows)=>{if(closed||!Number.isInteger(cols)||!Number.isInteger(rows)||cols<1||cols>500||rows<1||rows>500)return false;const r=await core.request<TerminalResizeResult>('terminal.resize',{terminalId:id,cols,rows});return r.resized===true},close,dispose:()=>{void close().catch(()=>{})}}},dispose(){if(disposed)return;disposed=true;for(const id of listeners.keys())void core.request('terminal.close',{terminalId:id}).catch(()=>{});listeners.clear();transport.removeEventListener('message',route)}}
}
let terminalSingleton:TerminalBridge|undefined
export function getTerminalBridge(){return terminalSingleton??=createTerminalBridge(webview())}

export function createLocalWorkspaceBridge(transport:WebViewTransport=webview()):LocalWorkspaceBridge{const core=createSimpleBridge(transport,{},8_000);return{root:()=>core.request('workspace.root.get',{}),select:()=>core.request('workspace.root.select',{}),clear:()=>core.request('workspace.root.clear',{}),list:(path='')=>core.request('workspace.list',path?{path}:{}),read:path=>core.request('workspace.read',{path}),open:payload=>core.request('workspace.open',payload??{})}}

// M9.5 Moon Companion TTS bridge — engine-routed synthesis (tts.voices /
// tts.synthesize / tts.cancel / tts.refAudios). Voices and synthesis take
// an engine selector (sapi | edge | ref); the reference engine browses
// local audio collections via tts.refAudios. Synthesis deadline stays
// generous for GPT-SoVITS: a cold first segment can take 20s+ on the
// local service, so tts.synthesize gets its own 40s core.
export type TtsVoice=TtsVoicesResult['voices'][number]
export type TtsRefMeta=NonNullable<TtsVoicesResult['ref_meta']>
export type{TtsSynthesizePayload,TtsSynthesizeResult,TtsVoicesPayload,TtsVoicesResult,TtsRefAudiosPayload,TtsRefAudiosResult,TtsEnsureRefEnginePayload,TtsEnsureRefEngineResult}
export interface TtsBridge{voices(payload?:TtsVoicesPayload):Promise<TtsVoicesResult>;synthesize(payload:TtsSynthesizePayload):Promise<TtsSynthesizeResult>;cancel():Promise<TtsCancelResult>;refAudios(dir:string):Promise<TtsRefAudiosResult>;ensureRefEngine(payload?:TtsEnsureRefEnginePayload):Promise<TtsEnsureRefEngineResult>}
export function createTtsBridge(transport:WebViewTransport=webview()):TtsBridge{const core=createSimpleBridge(transport,{},15_000);const synthCore=createSimpleBridge(transport,{},40_000);return{voices:payload=>core.request('tts.voices',payload??{}),synthesize:payload=>synthCore.request('tts.synthesize',payload),cancel:()=>core.request('tts.cancel',{}),refAudios:dir=>core.request('tts.refAudios',{dir}),ensureRefEngine:payload=>core.request('tts.ensureRefEngine',payload??{})}}
let ttsSingleton:TtsBridge|undefined
export function getTtsBridge():TtsBridge{return ttsSingleton??=createTtsBridge()}

// Local speech recognition — voice.status / install / start / append /
// finish / stop. Audio goes up a frame at a time and each reply carries the
// partial transcript back, so no second channel is needed for captions.
// append gets a tight deadline because a frame that has not been answered by
// the time the next one is captured is already too late to draw; install
// returns immediately with progress rather than holding a request open for a
// transfer measured in minutes.
export type{VoiceStatusResult,VoiceInstallPayload,VoiceInstallResult,VoiceSelectPayload,VoiceSelectResult,VoiceStartPayload,VoiceStartResult,VoiceAppendPayload,VoiceAppendResult,VoiceFinishPayload,VoiceFinishResult,VoiceStopPayload,VoiceStopResult}
export interface VoiceBridge{status():Promise<VoiceStatusResult>;install(payload?:VoiceInstallPayload):Promise<VoiceInstallResult>;select(payload:VoiceSelectPayload):Promise<VoiceSelectResult>;start(payload?:VoiceStartPayload):Promise<VoiceStartResult>;append(payload:VoiceAppendPayload):Promise<VoiceAppendResult>;finish(payload:VoiceFinishPayload):Promise<VoiceFinishResult>;stop(payload?:VoiceStopPayload):Promise<VoiceStopResult>}
export function createVoiceBridge(transport:WebViewTransport=webview()):VoiceBridge{const core=createSimpleBridge(transport,{},8_000);const frameCore=createSimpleBridge(transport,{},20_000);const startCore=createSimpleBridge(transport,{},30_000);return{status:()=>core.request('voice.status',{}),install:payload=>core.request('voice.install',payload??{}),select:payload=>core.request('voice.select',payload),start:payload=>startCore.request('voice.start',payload??{}),append:payload=>frameCore.request('voice.append',payload),finish:payload=>core.request('voice.finish',payload,20_000),stop:payload=>core.request('voice.stop',payload??{})}}
let voiceSingleton:VoiceBridge|undefined
export function getVoiceBridge():VoiceBridge{return voiceSingleton??=createVoiceBridge()}

export type{OmniStatusResult,OmniInstallResult,OmniEnsureResult,OmniStartPayload,OmniStartResult,OmniAppendPayload,OmniAppendResult,OmniStopPayload,OmniStopResult}
export interface OmniBridge{
  status():Promise<OmniStatusResult>
  install():Promise<OmniInstallResult>
  ensure():Promise<OmniEnsureResult>
  start(payload?:OmniStartPayload):Promise<OmniStartResult>
  append(payload:OmniAppendPayload):Promise<OmniAppendResult>
  stop(payload?:OmniStopPayload):Promise<OmniStopResult>
}
export function createOmniBridge(transport:WebViewTransport=webview()):OmniBridge{
  const core=createSimpleBridge(transport,{},8_000)
  const startCore=createSimpleBridge(transport,{},60_000)
  const frameCore=createSimpleBridge(transport,{},30_000)
  return{
    status:()=>core.request('omni.status',{}),
    install:()=>core.request('omni.install',{}),
    ensure:()=>core.request('omni.ensure',{}),
    start:payload=>startCore.request('omni.start',payload??{}),
    append:payload=>frameCore.request('omni.append',payload),
    stop:payload=>core.request('omni.stop',payload??{}),
  }
}
let omniSingleton:OmniBridge|undefined
export function getOmniBridge():OmniBridge{return omniSingleton??=createOmniBridge()}

// M7 MCP server settings bridge — mcp.list / add / toggle / health /
// market.search (T-7.8.5 settings page data source) + mcp6.presets.list
// (c3-mcp 免费官方预置目录).
export interface McpBridge{
  list(payload?:McpListPayload):Promise<McpListResult>
  add(payload:McpAddPayload,options?:MutationOptions<McpAddPayload>):Promise<McpAddResult>
  toggle(payload:McpTogglePayload,options?:MutationOptions<McpTogglePayload>):Promise<McpToggleResult>
  health(payload:McpHealthPayload):Promise<McpHealthResult>
  marketSearch(payload:McpMarketSearchPayload):Promise<McpMarketSearchResult>
  presets(payload?:Mcp6PresetsListPayload):Promise<Mcp6PresetsListResult>
}
export function createMcpBridge(transport:WebViewTransport=webview(),deadlineMs=10_000):McpBridge{
  const core=createSimpleBridge(transport,{},deadlineMs)
  return{list:p=>core.request('mcp.list',p??{}),add:(p,o)=>core.request('mcp.add',p,20_000,o?.attempt),toggle:(p,o)=>core.request('mcp.toggle',p,deadlineMs,o?.attempt),health:p=>core.request('mcp.health',p,15_000),marketSearch:p=>core.request('mcp.market.search',p,15_000),presets:p=>core.request('mcp6.presets.list',p??{})}
}
let mcpSingleton:McpBridge|undefined
export function getMcpBridge():McpBridge{return mcpSingleton??=createMcpBridge()}
export const mcpBridge:McpBridge={list:p=>getMcpBridge().list(p),add:(p,o)=>getMcpBridge().add(p,o),toggle:(p,o)=>getMcpBridge().toggle(p,o),health:p=>getMcpBridge().health(p),marketSearch:p=>getMcpBridge().marketSearch(p),presets:p=>getMcpBridge().presets(p)}

// M8 plugin system bridge — install / list / toggle / uninstall / upgrade +
// market browse + dev bundles (T-8.9.7 settings page data source).
export interface PluginBridge{
  list(payload?:PluginListPayload):Promise<PluginListResult>
  install(payload:PluginInstallPayload,options?:MutationOptions<PluginInstallPayload>):Promise<PluginInstallResult>
  toggle(payload:PluginTogglePayload,options?:MutationOptions<PluginTogglePayload>):Promise<PluginToggleResult>
  uninstall(payload:PluginUninstallPayload,options?:MutationOptions<PluginUninstallPayload>):Promise<PluginUninstallResult>
  upgrade(payload:PluginUpgradePayload,options?:MutationOptions<PluginUpgradePayload>):Promise<PluginUpgradeResult>
  marketSearch(payload:PluginMarketSearchPayload):Promise<PluginMarketSearchResult>
  marketDetail(payload:PluginMarketDetailPayload):Promise<PluginMarketDetailResult>
  devCreate(payload:PluginDevCreatePayload,options?:MutationOptions<PluginDevCreatePayload>):Promise<PluginDevCreateResult>
}
export function createPluginBridge(transport:WebViewTransport=webview(),deadlineMs=12_000):PluginBridge{
  const core=createSimpleBridge(transport,{},deadlineMs)
  return{list:p=>core.request('plugin.list',p??{}),install:(p,o)=>core.request('plugin.install',p,30_000,o?.attempt),toggle:(p,o)=>core.request('plugin.toggle',p,deadlineMs,o?.attempt),uninstall:(p,o)=>core.request('plugin.uninstall',p,deadlineMs,o?.attempt),upgrade:(p,o)=>core.request('plugin.upgrade',p,30_000,o?.attempt),marketSearch:p=>core.request('plugin.market.search',p,15_000),marketDetail:p=>core.request('plugin.market.detail',p,15_000),devCreate:(p,o)=>core.request('plugin.dev.create',p,30_000,o?.attempt)}
}
let pluginSingleton:PluginBridge|undefined
export function getPluginBridge():PluginBridge{return pluginSingleton??=createPluginBridge()}
export const pluginBridge:PluginBridge={list:p=>getPluginBridge().list(p),install:(p,o)=>getPluginBridge().install(p,o),toggle:(p,o)=>getPluginBridge().toggle(p,o),uninstall:(p,o)=>getPluginBridge().uninstall(p,o),upgrade:(p,o)=>getPluginBridge().upgrade(p,o),marketSearch:p=>getPluginBridge().marketSearch(p),marketDetail:p=>getPluginBridge().marketDetail(p),devCreate:(p,o)=>getPluginBridge().devCreate(p,o)}

// M8 expert center bridge — six-section catalog CRUD + nine-phase mounting
// (T-8.11.5 expert center data source).
export interface ExpertBridge{
  list(payload?:ExpertListPayload):Promise<ExpertListResult>
  detail(payload:ExpertDetailPayload):Promise<ExpertDetailResult>
  create(payload:ExpertCreatePayload,options?:MutationOptions<ExpertCreatePayload>):Promise<ExpertCreateResult>
  update(payload:ExpertUpdatePayload,options?:MutationOptions<ExpertUpdatePayload>):Promise<ExpertUpdateResult>
  toggle(payload:ExpertTogglePayload,options?:MutationOptions<ExpertTogglePayload>):Promise<ExpertToggleResult>
  archive(payload:ExpertArchivePayload,options?:MutationOptions<ExpertArchivePayload>):Promise<ExpertArchiveResult>
  mount(payload:ExpertMountPayload,options?:MutationOptions<ExpertMountPayload>):Promise<ExpertMountResult>
  mountingGet(payload:ExpertMountingGetPayload):Promise<ExpertMountingGetResult>
  scenarioCreate(payload:ExpertScenarioCreatePayload,options?:MutationOptions<ExpertScenarioCreatePayload>):Promise<ExpertScenarioCreateResult>
  scenarioList(payload:ExpertScenarioListPayload):Promise<ExpertScenarioListResult>
  scenarioDelete(payload:ExpertScenarioDeletePayload,options?:MutationOptions<ExpertScenarioDeletePayload>):Promise<ExpertScenarioDeleteResult>
  catalogList?(payload?:ExpertCatalogListPayload):Promise<ExpertCatalogListResult>
  install?(payload:ExpertInstallPayload):Promise<ExpertInstallResult>
  sessionMountGet?(payload:{sessionId:string}):Promise<{expertIds:string[]}>
  sessionMountSet?(payload:{sessionId:string;expertIds:string[]},options?:MutationOptions<{sessionId:string;expertIds:string[]}>):Promise<{expertIds:string[]}>
}
export function createExpertBridge(transport:WebViewTransport=webview(),deadlineMs=12_000):ExpertBridge{
  const core=createSimpleBridge(transport,{},deadlineMs)
  return{list:p=>core.request('expert.list',p??{}),detail:p=>core.request('expert.detail',p),create:(p,o)=>core.request('expert.create',p,20_000,o?.attempt),update:(p,o)=>core.request('expert.update',p,deadlineMs,o?.attempt),toggle:(p,o)=>core.request('expert.toggle',p,deadlineMs,o?.attempt),archive:(p,o)=>core.request('expert.archive',p,deadlineMs,o?.attempt),mount:(p,o)=>core.request('expert.mount',p,deadlineMs,o?.attempt),mountingGet:p=>core.request('expert.mounting.get',p),scenarioCreate:(p,o)=>core.request('expert.scenario.create',p,deadlineMs,o?.attempt),scenarioList:p=>core.request('expert.scenario.list',p),scenarioDelete:(p,o)=>core.request('expert.scenario.delete',p,deadlineMs,o?.attempt),catalogList:p=>core.request('expert.catalog.list',p??{}),install:p=>core.request('expert.install',p,20_000),sessionMountGet:p=>core.request('session.experts.get',p),sessionMountSet:(p,o)=>core.request('session.experts.set',p,deadlineMs,o?.attempt)}
}
let expertSingleton:ExpertBridge|undefined
export function getExpertBridge():ExpertBridge{return expertSingleton??=createExpertBridge()}
export const expertBridge:ExpertBridge={list:p=>{try{return getExpertBridge().list(p)}catch(error){return Promise.reject(error)}},detail:p=>{try{return getExpertBridge().detail(p)}catch(error){return Promise.reject(error)}},create:(p,o)=>{try{return getExpertBridge().create(p,o)}catch(error){return Promise.reject(error)}},update:(p,o)=>{try{return getExpertBridge().update(p,o)}catch(error){return Promise.reject(error)}},toggle:(p,o)=>{try{return getExpertBridge().toggle(p,o)}catch(error){return Promise.reject(error)}},archive:(p,o)=>{try{return getExpertBridge().archive(p,o)}catch(error){return Promise.reject(error)}},mount:(p,o)=>{try{return getExpertBridge().mount(p,o)}catch(error){return Promise.reject(error)}},mountingGet:p=>{try{return getExpertBridge().mountingGet(p)}catch(error){return Promise.reject(error)}},scenarioCreate:(p,o)=>{try{return getExpertBridge().scenarioCreate(p,o)}catch(error){return Promise.reject(error)}},scenarioList:p=>{try{return getExpertBridge().scenarioList(p)}catch(error){return Promise.reject(error)}},scenarioDelete:(p,o)=>{try{return getExpertBridge().scenarioDelete(p,o)}catch(error){return Promise.reject(error)}},catalogList:p=>{try{return getExpertBridge().catalogList!(p)}catch(error){return Promise.reject(error)}},install:p=>{try{return getExpertBridge().install!(p)}catch(error){return Promise.reject(error)}},sessionMountGet:p=>{try{return getExpertBridge().sessionMountGet!(p)}catch(error){return Promise.reject(error)}},sessionMountSet:(p,o)=>{try{return getExpertBridge().sessionMountSet!(p,o)}catch(error){return Promise.reject(error)}}}

// M9 slice-1 org-admin bridge — org foundation, isolation gate, spaces and
// members (T-9.1.3 org.* data source).
export interface OrgBridge{
  summary(payload?:OrgSummaryPayload):Promise<OrgSummaryResult>
  create(payload:OrgCreatePayload,options?:MutationOptions<OrgCreatePayload>):Promise<OrgCreateResult>
  switch(payload:OrgSwitchPayload,options?:MutationOptions<OrgSwitchPayload>):Promise<OrgSwitchResult>
  activate(payload?:OrgActivatePayload,options?:MutationOptions<OrgActivatePayload>):Promise<OrgActivateResult>
  suspend(payload?:OrgSuspendPayload,options?:MutationOptions<OrgSuspendPayload>):Promise<OrgSuspendResult>
  spaceList(payload?:OrgSpaceListPayload):Promise<OrgSpaceListResult>
  spaceCreate(payload:OrgSpaceCreatePayload,options?:MutationOptions<OrgSpaceCreatePayload>):Promise<OrgSpaceCreateResult>
  memberList(payload?:OrgMemberListPayload):Promise<OrgMemberListResult>
  memberInvite(payload:OrgMemberInvitePayload,options?:MutationOptions<OrgMemberInvitePayload>):Promise<OrgMemberInviteResult>
  memberRevoke(payload:OrgMemberRevokePayload,options?:MutationOptions<OrgMemberRevokePayload>):Promise<OrgMemberRevokeResult>
}
export function createOrgBridge(transport:WebViewTransport=webview(),deadlineMs=12_000):OrgBridge{
  const core=createSimpleBridge(transport,{},deadlineMs)
  return{summary:p=>core.request('org.summary',p??{}),create:(p,o)=>core.request('org.create',p,deadlineMs,o?.attempt),switch:(p,o)=>core.request('org.switch',p,deadlineMs,o?.attempt),activate:(p,o)=>core.request('org.activate',p??{},deadlineMs,o?.attempt),suspend:(p,o)=>core.request('org.suspend',p??{},deadlineMs,o?.attempt),spaceList:p=>core.request('org.space.list',p??{}),spaceCreate:(p,o)=>core.request('org.space.create',p,deadlineMs,o?.attempt),memberList:p=>core.request('org.member.list',p??{}),memberInvite:(p,o)=>core.request('org.member.invite',p,deadlineMs,o?.attempt),memberRevoke:(p,o)=>core.request('org.member.revoke',p,deadlineMs,o?.attempt)}
}
let orgSingleton:OrgBridge|undefined
export function getOrgBridge():OrgBridge{return orgSingleton??=createOrgBridge()}
export const orgBridge:OrgBridge={summary:p=>{try{return getOrgBridge().summary(p)}catch(error){return Promise.reject(error)}},create:(p,o)=>{try{return getOrgBridge().create(p,o)}catch(error){return Promise.reject(error)}},switch:(p,o)=>{try{return getOrgBridge().switch(p,o)}catch(error){return Promise.reject(error)}},activate:(p,o)=>{try{return getOrgBridge().activate(p,o)}catch(error){return Promise.reject(error)}},suspend:(p,o)=>{try{return getOrgBridge().suspend(p,o)}catch(error){return Promise.reject(error)}},spaceList:p=>{try{return getOrgBridge().spaceList(p)}catch(error){return Promise.reject(error)}},spaceCreate:(p,o)=>{try{return getOrgBridge().spaceCreate(p,o)}catch(error){return Promise.reject(error)}},memberList:p=>{try{return getOrgBridge().memberList(p)}catch(error){return Promise.reject(error)}},memberInvite:(p,o)=>{try{return getOrgBridge().memberInvite(p,o)}catch(error){return Promise.reject(error)}},memberRevoke:(p,o)=>{try{return getOrgBridge().memberRevoke(p,o)}catch(error){return Promise.reject(error)}}}

export type{IdentityDTO,PeopleContactDTO,PeopleFileOfferDTO,PeopleThreadDTO,PeopleThreadOpenResult,PeopleThreadSendResult,MeetingDTO,MeetingSegmentDTO}
export interface IdentityBridge{
  get():Promise<IdentityDTO>
  update(payload:IdentityUpdatePayload):Promise<IdentityDTO>
  passwordSet(payload:IdentityPasswordSetPayload):Promise<IdentityDTO>
  unlock(payload:IdentityUnlockPayload):Promise<IdentityDTO>
}
export interface PeopleBridge{
  list():Promise<PeopleListResult>
  pair(payload:PeoplePairPayload):Promise<PeopleContactDTO>
  discoveryGet():Promise<PeopleDiscoveryGetResult>
  discoverySet(payload:PeopleDiscoverySetPayload):Promise<PeopleDiscoveryGetResult>
  threadList():Promise<PeopleThreadListResult>
  threadOpen(payload:PeopleThreadOpenPayload):Promise<PeopleThreadOpenResult>
  threadSend(payload:PeopleThreadSendPayload):Promise<PeopleThreadSendResult>
  threadTyping(payload:PeopleThreadTypingPayload):Promise<{ok:boolean}>
  groupCreate(payload:PeopleGroupCreatePayload):Promise<PeopleThreadDTO>
  fileDecide(payload:PeopleFileDecidePayload):Promise<PeopleFileOfferDTO>
  fileStage(payload:PeopleFileStagePayload):Promise<PeopleFileStageResult>
  filePick(payload?:PeopleFilePickPayload):Promise<PeopleFilePickResult>
  screenCapture(payload?:PeopleScreenCapturePayload):Promise<PeopleScreenCaptureResult>
  peerAdd(payload:PeoplePeerAddPayload):Promise<PeopleContactDTO>
  contactUpdate(payload:PeopleContactUpdatePayload):Promise<PeopleContactDTO>
}
export function createIdentityBridge(transport:WebViewTransport=webview()):IdentityBridge{
  const core=createSimpleBridge(transport,{},8_000)
  return{get:()=>core.request('identity.get',{}),update:p=>core.request('identity.update',p),passwordSet:p=>core.request('identity.password.set',p),unlock:p=>core.request('identity.unlock',p)}
}
export function createPeopleBridge(transport:WebViewTransport=webview()):PeopleBridge{
  const core=createSimpleBridge(transport,{},12_000)
  return{list:()=>core.request('people.list',{}),pair:p=>core.request('people.pair',p),discoveryGet:()=>core.request('people.discovery.get',{}),discoverySet:p=>core.request('people.discovery.set',p),threadList:()=>core.request('people.thread.list',{}),threadOpen:p=>core.request('people.thread.open',p),threadSend:p=>retryBridgeRequest(()=>core.request('people.thread.send',p,PEOPLE_FILE_DEADLINE_MS)),threadTyping:p=>core.request('people.thread.typing',p),groupCreate:p=>core.request('people.group.create',p),fileDecide:p=>core.request('people.file.decide',p),fileStage:p=>retryBridgeRequest(()=>core.request('people.file.stage',p,PEOPLE_FILE_DEADLINE_MS)),filePick:p=>retryBridgeRequest(()=>core.request('people.file.pick',p??{},PEOPLE_FILE_DEADLINE_MS)),screenCapture:p=>core.request('people.screen.capture',p??{},PEOPLE_CAPTURE_DEADLINE_MS),peerAdd:p=>core.request('people.peer.add',p,8_000),contactUpdate:p=>core.request('people.contact.update',p)}
}
let identitySingleton:IdentityBridge|undefined
let peopleSingleton:PeopleBridge|undefined
export function getIdentityBridge():IdentityBridge{return identitySingleton??=createIdentityBridge()}
export function getPeopleBridge():PeopleBridge{return peopleSingleton??=createPeopleBridge()}

// M7 app update bridge — appUpdate.check / install (settings → diagnostics).
export interface AppUpdateBridge{
  check(payload:AppUpdateCheckPayload):Promise<AppUpdateCheckResult>
  install(payload:AppUpdateInstallPayload,options?:MutationOptions<AppUpdateInstallPayload>):Promise<AppUpdateInstallResult>
}
export function createAppUpdateBridge(transport:WebViewTransport=webview(),deadlineMs=30_000):AppUpdateBridge{
  const core=createSimpleBridge(transport,{},deadlineMs)
  return{check:p=>core.request('appUpdate.check',p,30_000),install:(p,o)=>core.request('appUpdate.install',p,120_000,o?.attempt)}
}
let appUpdateSingleton:AppUpdateBridge|undefined
export function getAppUpdateBridge():AppUpdateBridge{return appUpdateSingleton??=createAppUpdateBridge()}
