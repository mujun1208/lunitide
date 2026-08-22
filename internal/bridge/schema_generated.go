// Code generated from discovered bridge schemas. DO NOT EDIT.
package bridge

const Version = "1.0"

type Method string

const (
	MethodAgentRunCancel                Method = "agent.run.cancel"
	MethodAgentRunGet                   Method = "agent.run.get"
	MethodAgentRunReconcile             Method = "agent.run.reconcile"
	MethodAgentRunResume                Method = "agent.run.resume"
	MethodAgentRunStart                 Method = "agent.run.start"
	MethodAppUpdateCheck                Method = "appUpdate.check"
	MethodAppUpdateInstall              Method = "appUpdate.install"
	MethodArchivePack                   Method = "archive.pack"
	MethodArchiveUnpack                 Method = "archive.unpack"
	MethodAttachmentDelete              Method = "attachment.delete"
	MethodAttachmentGet                 Method = "attachment.get"
	MethodAttachmentIngest              Method = "attachment.ingest"
	MethodAttachmentList                Method = "attachment.list"
	MethodAttachmentUploadAbort         Method = "attachment.upload.abort"
	MethodAttachmentUploadBegin         Method = "attachment.upload.begin"
	MethodAttachmentUploadChunk         Method = "attachment.upload.chunk"
	MethodAttachmentUploadCommit        Method = "attachment.upload.commit"
	MethodAutomationDispatch            Method = "automation.dispatch"
	MethodAutomationJobDelete           Method = "automation.job.delete"
	MethodAutomationJobList             Method = "automation.job.list"
	MethodAutomationJobSet              Method = "automation.job.set"
	MethodAutomationJobTrigger          Method = "automation.job.trigger"
	MethodAutomationRunList             Method = "automation.run.list"
	MethodAutomationStatus              Method = "automation.status"
	MethodBarrierArrive                 Method = "barrier.arrive"
	MethodBrDataClear                   Method = "br.data.clear"
	MethodBrDataUsage                   Method = "br.data.usage"
	MethodBrModeDetect                  Method = "br.mode.detect"
	MethodBrNavigate                    Method = "br.navigate"
	MethodBrPermissionDecide            Method = "br.permission.decide"
	MethodBrPermissionList              Method = "br.permission.list"
	MethodBrPermissionPolicy            Method = "br.permission.policy"
	MethodBrPermissionRequest           Method = "br.permission.request"
	MethodBrSessionConnect              Method = "br.session.connect"
	MethodBrSessionDisconnect           Method = "br.session.disconnect"
	MethodBrSessionList                 Method = "br.session.list"
	MethodBrSettingsGet                 Method = "br.settings.get"
	MethodBrSettingsUpdate              Method = "br.settings.update"
	MethodBrowserAct                    Method = "browser.act"
	MethodBrowserClose                  Method = "browser.close"
	MethodBrowserOpen                   Method = "browser.open"
	MethodCapabilityList                Method = "capability.list"
	MethodCcEmergencyStop               Method = "cc.emergencyStop"
	MethodCcGetAuditLog                 Method = "cc.getAuditLog"
	MethodCcGetConfig                   Method = "cc.getConfig"
	MethodCcUpdateConfig                Method = "cc.updateConfig"
	MethodChangesetApply                Method = "changeset.apply"
	MethodChangesetPreview              Method = "changeset.preview"
	MethodChangesetRevert               Method = "changeset.revert"
	MethodChatStart                     Method = "chat.start"
	MethodChatToolApprove               Method = "chat.tool.approve"
	MethodCollabGateConfirm             Method = "collabGate.confirm"
	MethodCollabGateEvaluate            Method = "collabGate.evaluate"
	MethodCollabGateStatus              Method = "collabGate.status"
	MethodCommandCancel                 Method = "command.cancel"
	MethodCommandGet                    Method = "command.get"
	MethodCommandReviewRequest          Method = "command.review.request"
	MethodCommandStart                  Method = "command.start"
	MethodComplexityDecide              Method = "complexity.decide"
	MethodConnectorSnapshot             Method = "connector.snapshot"
	MethodContextCompactCancel          Method = "context.compact.cancel"
	MethodContextCompactCommit          Method = "context.compact.commit"
	MethodContextCompactPreview         Method = "context.compact.preview"
	MethodContextHandoffCreate          Method = "context.handoff.create"
	MethodContextHandoffImport          Method = "context.handoff.import"
	MethodContextHandoffInspect         Method = "context.handoff.inspect"
	MethodContextHandoffList            Method = "context.handoff.list"
	MethodContextHandoffListImports     Method = "context.handoff.list-imports"
	MethodContextHandoffRevoke          Method = "context.handoff.revoke"
	MethodContextStatus                 Method = "context.status"
	MethodConversationsRootGet          Method = "conversations.root.get"
	MethodConversationsRootSelect       Method = "conversations.root.select"
	MethodConversationsRootSet          Method = "conversations.root.set"
	MethodDbQuery                       Method = "db.query"
	MethodDelegationCreate              Method = "delegation.create"
	MethodDelegationSettle              Method = "delegation.settle"
	MethodDeliverableConfirmGate        Method = "deliverable.confirmGate"
	MethodDeliverableList               Method = "deliverable.list"
	MethodDeliverableUpsert             Method = "deliverable.upsert"
	MethodDevTaskCreate                 Method = "devTask.create"
	MethodDevTaskTransition             Method = "devTask.transition"
	MethodDiagnosticsExport             Method = "diagnostics.export"
	MethodDocumentParse                 Method = "document.parse"
	MethodEvidenceAttachScan            Method = "evidence.attachScan"
	MethodEvidenceAttachTest            Method = "evidence.attachTest"
	MethodEvidenceList                  Method = "evidence.list"
	MethodExpertArchive                 Method = "expert.archive"
	MethodExpertCatalogList             Method = "expert.catalog.list"
	MethodExpertCreate                  Method = "expert.create"
	MethodExpertDetail                  Method = "expert.detail"
	MethodExpertInstall                 Method = "expert.install"
	MethodExpertList                    Method = "expert.list"
	MethodExpertMount                   Method = "expert.mount"
	MethodExpertMountingGet             Method = "expert.mounting.get"
	MethodExpertScenarioCreate          Method = "expert.scenario.create"
	MethodExpertScenarioDelete          Method = "expert.scenario.delete"
	MethodExpertScenarioList            Method = "expert.scenario.list"
	MethodExpertToggle                  Method = "expert.toggle"
	MethodExpertUpdate                  Method = "expert.update"
	MethodExtensionInstall              Method = "extension.install"
	MethodExtensionLifecycle            Method = "extension.lifecycle"
	MethodExtensionSearch               Method = "extension.search"
	MethodFeedbackCandidates            Method = "feedback.candidates"
	MethodFeedbackRecord                Method = "feedback.record"
	MethodFsGlob                        Method = "fs.glob"
	MethodFsGrep                        Method = "fs.grep"
	MethodFsRead                        Method = "fs.read"
	MethodFsReadMany                    Method = "fs.readMany"
	MethodFsStat                        Method = "fs.stat"
	MethodFsTree                        Method = "fs.tree"
	MethodGitRead                       Method = "git.read"
	MethodHandoffAccept                 Method = "handoff.accept"
	MethodHttpDownload                  Method = "http.download"
	MethodHttpRequest                   Method = "http.request"
	MethodKbUpsertDocument              Method = "kb.upsertDocument"
	MethodMcConfigValidate              Method = "mc.config.validate"
	MethodMcConfirmToken                Method = "mc.confirm.token"
	MethodMcConnectorInstall            Method = "mc.connector.install"
	MethodMcConnectorUninstall          Method = "mc.connector.uninstall"
	MethodMcConnectorUpdate             Method = "mc.connector.update"
	MethodMcConnectorUsage              Method = "mc.connector.usage"
	MethodMcMarketDetail                Method = "mc.market.detail"
	MethodMcMarketList                  Method = "mc.market.list"
	MethodMcTombstoneCheck              Method = "mc.tombstone.check"
	MethodMcpAdd                        Method = "mcp.add"
	MethodMcpHealth                     Method = "mcp.health"
	MethodMcpInvoke                     Method = "mcp.invoke"
	MethodMcpList                       Method = "mcp.list"
	MethodMcpMarketSearch               Method = "mcp.market.search"
	MethodMcpToggle                     Method = "mcp.toggle"
	MethodMcp6Invoke                    Method = "mcp6.invoke"
	MethodMcp6PresetsList               Method = "mcp6.presets.list"
	MethodMcp6Register                  Method = "mcp6.register"
	MethodMcp6Revoke                    Method = "mcp6.revoke"
	MethodMemoryConfirmCandidate        Method = "memory.confirmCandidate"
	MethodMemoryCreate                  Method = "memory.create"
	MethodMemoryDelete                  Method = "memory.delete"
	MethodMemoryExport                  Method = "memory.export"
	MethodMemoryFactsFlag               Method = "memory.facts.flag"
	MethodMemoryFactsList               Method = "memory.facts.list"
	MethodMemoryGet                     Method = "memory.get"
	MethodMemoryGrowthDecide            Method = "memory.growth.decide"
	MethodMemoryGrowthList              Method = "memory.growth.list"
	MethodMemoryList                    Method = "memory.list"
	MethodMemoryNominate                Method = "memory.nominate"
	MethodMemoryNominationList          Method = "memory.nomination.list"
	MethodMemoryNominationWithdraw      Method = "memory.nomination.withdraw"
	MethodMemoryPurge                   Method = "memory.purge"
	MethodMemorySearch                  Method = "memory.search"
	MethodMemorySettingsGet             Method = "memory.settings.get"
	MethodMemorySettingsUpdate          Method = "memory.settings.update"
	MethodMemoryStats                   Method = "memory.stats"
	MethodMemoryTracesList              Method = "memory.traces.list"
	MethodMemoryUpdate                  Method = "memory.update"
	MethodMergeSubmit                   Method = "merge.submit"
	MethodMessageAppend                 Method = "message.append"
	MethodMessageList                   Method = "message.list"
	MethodMessageRewind                 Method = "message.rewind"
	MethodMigrationInspect              Method = "migration.inspect"
	MethodMigrationRun                  Method = "migration.run"
	MethodMigrationStatus               Method = "migration.status"
	MethodNodeComplete                  Method = "node.complete"
	MethodNodeCreate                    Method = "node.create"
	MethodNodeFail                      Method = "node.fail"
	MethodNodeList                      Method = "node.list"
	MethodNodeStart                     Method = "node.start"
	MethodOntologyEdgeCreate            Method = "ontology.edge.create"
	MethodOntologyEdgeDelete            Method = "ontology.edge.delete"
	MethodOntologyEdgeList              Method = "ontology.edge.list"
	MethodOntologyEdgeUpdate            Method = "ontology.edge.update"
	MethodOntologyNodeCreate            Method = "ontology.node.create"
	MethodOntologyNodeDelete            Method = "ontology.node.delete"
	MethodOntologyNodeGet               Method = "ontology.node.get"
	MethodOntologyNodeList              Method = "ontology.node.list"
	MethodOntologyNodeSearch            Method = "ontology.node.search"
	MethodOntologyNodeUpdate            Method = "ontology.node.update"
	MethodOpenapiParse                  Method = "openapi.parse"
	MethodOrgActivate                   Method = "org.activate"
	MethodOrgCreate                     Method = "org.create"
	MethodOrgMemberInvite               Method = "org.member.invite"
	MethodOrgMemberList                 Method = "org.member.list"
	MethodOrgMemberRevoke               Method = "org.member.revoke"
	MethodOrgSpaceCreate                Method = "org.space.create"
	MethodOrgSpaceList                  Method = "org.space.list"
	MethodOrgSummary                    Method = "org.summary"
	MethodOrgSuspend                    Method = "org.suspend"
	MethodOrgSwitch                     Method = "org.switch"
	MethodPlanActivate                  Method = "plan.activate"
	MethodPlanComplete                  Method = "plan.complete"
	MethodPlanCreate                    Method = "plan.create"
	MethodPlanGet                       Method = "plan.get"
	MethodPlanList                      Method = "plan.list"
	MethodPlanPause                     Method = "plan.pause"
	MethodPlanResume                    Method = "plan.resume"
	MethodPlanRunCancel                 Method = "plan.run.cancel"
	MethodPlanRunJoin                   Method = "plan.run.join"
	MethodPlanRunSpawn                  Method = "plan.run.spawn"
	MethodPlanRunStart                  Method = "plan.run.start"
	MethodPlanRunTree                   Method = "plan.run.tree"
	MethodPlanTodoCreate                Method = "plan.todo.create"
	MethodPluginDevCreate               Method = "plugin.dev.create"
	MethodPluginInstall                 Method = "plugin.install"
	MethodPluginList                    Method = "plugin.list"
	MethodPluginMarketDetail            Method = "plugin.market.detail"
	MethodPluginMarketSearch            Method = "plugin.market.search"
	MethodPluginToggle                  Method = "plugin.toggle"
	MethodPluginUninstall               Method = "plugin.uninstall"
	MethodPluginUpgrade                 Method = "plugin.upgrade"
	MethodProjectAdvanceStatus          Method = "project.advanceStatus"
	MethodProjectClose                  Method = "project.close"
	MethodProjectCreate                 Method = "project.create"
	MethodProjectDelete                 Method = "project.delete"
	MethodProjectList                   Method = "project.list"
	MethodProjectPublish                Method = "project.publish"
	MethodProjectReopen                 Method = "project.reopen"
	MethodProjectUpdate                 Method = "project.update"
	MethodProjectAttachmentIngest       Method = "projectAttachment.ingest"
	MethodProjectAttachmentList         Method = "projectAttachment.list"
	MethodProviderCreate                Method = "provider.create"
	MethodProviderCredentialReveal      Method = "provider.credential.reveal"
	MethodProviderCredentialSubmit      Method = "provider.credential.submit"
	MethodProviderDelete                Method = "provider.delete"
	MethodProviderGet                   Method = "provider.get"
	MethodProviderList                  Method = "provider.list"
	MethodProviderModelSync             Method = "provider.model.sync"
	MethodProviderTest                  Method = "provider.test"
	MethodProviderUpdate                Method = "provider.update"
	MethodRecallQuery                   Method = "recall.query"
	MethodReleaseBuildPackage           Method = "release.buildPackage"
	MethodReleaseCreateRevision         Method = "release.createRevision"
	MethodReleaseGetPackage             Method = "release.getPackage"
	MethodReleaseGetPromotion           Method = "release.getPromotion"
	MethodReleaseGetRevision            Method = "release.getRevision"
	MethodReleasePromote                Method = "release.promote"
	MethodReleaseRollback               Method = "release.rollback"
	MethodReviewApprove                 Method = "review.approve"
	MethodReviewDecide                  Method = "review.decide"
	MethodReviewList                    Method = "review.list"
	MethodReviewReject                  Method = "review.reject"
	MethodReviewSubmit                  Method = "review.submit"
	MethodRunCancel                     Method = "run.cancel"
	MethodRunPlanPut                    Method = "run.plan.put"
	MethodRunQueueConsume               Method = "run.queueConsume"
	MethodRunQueueInput                 Method = "run.queueInput"
	MethodRunQueueList                  Method = "run.queueList"
	MethodRunQueueWithdraw              Method = "run.queueWithdraw"
	MethodRunSend                       Method = "run.send"
	MethodSessionCreate                 Method = "session.create"
	MethodSessionDelete                 Method = "session.delete"
	MethodSessionExpertsGet             Method = "session.experts.get"
	MethodSessionExpertsSet             Method = "session.experts.set"
	MethodSessionFolderGet              Method = "session.folder.get"
	MethodSessionFolderOpen             Method = "session.folder.open"
	MethodSessionList                   Method = "session.list"
	MethodSessionUpdate                 Method = "session.update"
	MethodSkillCatalogList              Method = "skill.catalog.list"
	MethodSkillCategorySet              Method = "skill.category.set"
	MethodSkillCreate                   Method = "skill.create"
	MethodSkillDelete                   Method = "skill.delete"
	MethodSkillDeprecate                Method = "skill.deprecate"
	MethodSkillDisable                  Method = "skill.disable"
	MethodSkillExecute                  Method = "skill.execute"
	MethodSkillGet                      Method = "skill.get"
	MethodSkillImportApprove            Method = "skill.import.approve"
	MethodSkillImportDiscover           Method = "skill.import.discover"
	MethodSkillImportInspect            Method = "skill.import.inspect"
	MethodSkillImportReject             Method = "skill.import.reject"
	MethodSkillImportRevoke             Method = "skill.import.revoke"
	MethodSkillImportSubmit             Method = "skill.import.submit"
	MethodSkillInstall                  Method = "skill.install"
	MethodSkillInvoke                   Method = "skill.invoke"
	MethodSkillList                     Method = "skill.list"
	MethodSkillMatch                    Method = "skill.match"
	MethodSkillPublish                  Method = "skill.publish"
	MethodSkillUpdate                   Method = "skill.update"
	MethodStageCreate                   Method = "stage.create"
	MethodStageList                     Method = "stage.list"
	MethodStageUpdate                   Method = "stage.update"
	MethodStreamCancel                  Method = "stream.cancel"
	MethodSubagentJoin                  Method = "subagent.join"
	MethodSubagentSpawn                 Method = "subagent.spawn"
	MethodSubagentTree                  Method = "subagent.tree"
	MethodSyncPush                      Method = "sync.push"
	MethodSystemHealth                  Method = "system.health"
	MethodSystemSettingsOpen            Method = "system.settings.open"
	MethodTemplateCreate                Method = "template.create"
	MethodTemplateDelete                Method = "template.delete"
	MethodTemplateEnable                Method = "template.enable"
	MethodTemplateList                  Method = "template.list"
	MethodTemplateVoid                  Method = "template.void"
	MethodTerminalClose                 Method = "terminal.close"
	MethodTerminalInput                 Method = "terminal.input"
	MethodTerminalResize                Method = "terminal.resize"
	MethodTerminalStart                 Method = "terminal.start"
	MethodTombstoneDelete               Method = "tombstone.delete"
	MethodToolsCommandPolicyGet         Method = "tools.commandPolicy.get"
	MethodToolsCommandPolicySet         Method = "tools.commandPolicy.set"
	MethodToolsHooksEventsList          Method = "tools.hooksEvents.list"
	MethodToolsHooksPolicyGet           Method = "tools.hooksPolicy.get"
	MethodToolsHooksPolicySet           Method = "tools.hooksPolicy.set"
	MethodTraceAddEdge                  Method = "trace.addEdge"
	MethodTraceMarkStale                Method = "trace.markStale"
	MethodTraceQuery                    Method = "trace.query"
	MethodTraceResolveStale             Method = "trace.resolveStale"
	MethodTtsCancel                     Method = "tts.cancel"
	MethodTtsEnsureRefEngine            Method = "tts.ensureRefEngine"
	MethodTtsRefAudios                  Method = "tts.refAudios"
	MethodTtsSynthesize                 Method = "tts.synthesize"
	MethodTtsVoices                     Method = "tts.voices"
	MethodUiThemeSet                    Method = "ui.theme.set"
	MethodWebFetch                      Method = "web.fetch"
	MethodWebSearch                     Method = "web.search"
	MethodWorkerDispatch                Method = "worker.dispatch"
	MethodWorkflowCaptureInput          Method = "workflow.captureInput"
	MethodWorkflowCreateCheckpoint      Method = "workflow.createCheckpoint"
	MethodWorkflowCreateVersion         Method = "workflow.createVersion"
	MethodWorkflowEvaluateGate          Method = "workflow.evaluateGate"
	MethodWorkflowPublish               Method = "workflow.publish"
	MethodWorkflowStartStage            Method = "workflow.startStage"
	MethodWorkflowTransitionStage       Method = "workflow.transitionStage"
	MethodWorkspaceArtifactExport       Method = "workspace.artifact.export"
	MethodWorkspaceArtifactPreview      Method = "workspace.artifact.preview"
	MethodWorkspaceArtifactReviewAppend Method = "workspace.artifactReview.append"
	MethodWorkspaceArtifactReviewList   Method = "workspace.artifactReview.list"
	MethodWorkspaceConvert              Method = "workspace.convert"
	MethodWorkspaceGrant                Method = "workspace.grant"
	MethodWorkspaceLease                Method = "workspace.lease"
	MethodWorkspaceList                 Method = "workspace.list"
	MethodWorkspaceRead                 Method = "workspace.read"
	MethodWorkspaceRegister             Method = "workspace.register"
	MethodWorkspaceRootClear            Method = "workspace.root.clear"
	MethodWorkspaceRootGet              Method = "workspace.root.get"
	MethodWorkspaceRootSelect           Method = "workspace.root.select"
)

type MethodMetadata struct {
	Owner   string
	Enabled bool
}

var MethodMetadataByMethod = map[Method]MethodMetadata{
	MethodAgentRunCancel:                {Owner: "engine", Enabled: true},
	MethodAgentRunGet:                   {Owner: "engine", Enabled: true},
	MethodAgentRunReconcile:             {Owner: "engine", Enabled: true},
	MethodAgentRunResume:                {Owner: "engine", Enabled: true},
	MethodAgentRunStart:                 {Owner: "engine", Enabled: true},
	MethodAppUpdateCheck:                {Owner: "engine", Enabled: true},
	MethodAppUpdateInstall:              {Owner: "engine", Enabled: true},
	MethodArchivePack:                   {Owner: "engine", Enabled: true},
	MethodArchiveUnpack:                 {Owner: "engine", Enabled: true},
	MethodAttachmentDelete:              {Owner: "engine", Enabled: true},
	MethodAttachmentGet:                 {Owner: "engine", Enabled: true},
	MethodAttachmentIngest:              {Owner: "engine", Enabled: true},
	MethodAttachmentList:                {Owner: "engine", Enabled: true},
	MethodAttachmentUploadAbort:         {Owner: "engine", Enabled: true},
	MethodAttachmentUploadBegin:         {Owner: "engine", Enabled: true},
	MethodAttachmentUploadChunk:         {Owner: "engine", Enabled: true},
	MethodAttachmentUploadCommit:        {Owner: "engine", Enabled: true},
	MethodAutomationDispatch:            {Owner: "engine", Enabled: true},
	MethodAutomationJobDelete:           {Owner: "engine", Enabled: true},
	MethodAutomationJobList:             {Owner: "engine", Enabled: true},
	MethodAutomationJobSet:              {Owner: "engine", Enabled: true},
	MethodAutomationJobTrigger:          {Owner: "engine", Enabled: true},
	MethodAutomationRunList:             {Owner: "engine", Enabled: true},
	MethodAutomationStatus:              {Owner: "engine", Enabled: true},
	MethodBarrierArrive:                 {Owner: "engine", Enabled: true},
	MethodBrDataClear:                   {Owner: "engine", Enabled: true},
	MethodBrDataUsage:                   {Owner: "engine", Enabled: true},
	MethodBrModeDetect:                  {Owner: "engine", Enabled: true},
	MethodBrNavigate:                    {Owner: "engine", Enabled: true},
	MethodBrPermissionDecide:            {Owner: "engine", Enabled: true},
	MethodBrPermissionList:              {Owner: "engine", Enabled: true},
	MethodBrPermissionPolicy:            {Owner: "engine", Enabled: true},
	MethodBrPermissionRequest:           {Owner: "engine", Enabled: true},
	MethodBrSessionConnect:              {Owner: "engine", Enabled: true},
	MethodBrSessionDisconnect:           {Owner: "engine", Enabled: true},
	MethodBrSessionList:                 {Owner: "engine", Enabled: true},
	MethodBrSettingsGet:                 {Owner: "engine", Enabled: true},
	MethodBrSettingsUpdate:              {Owner: "engine", Enabled: true},
	MethodBrowserAct:                    {Owner: "engine", Enabled: true},
	MethodBrowserClose:                  {Owner: "host", Enabled: true},
	MethodBrowserOpen:                   {Owner: "host", Enabled: true},
	MethodCapabilityList:                {Owner: "engine", Enabled: true},
	MethodCcEmergencyStop:               {Owner: "engine", Enabled: true},
	MethodCcGetAuditLog:                 {Owner: "engine", Enabled: true},
	MethodCcGetConfig:                   {Owner: "engine", Enabled: true},
	MethodCcUpdateConfig:                {Owner: "engine", Enabled: true},
	MethodChangesetApply:                {Owner: "engine", Enabled: true},
	MethodChangesetPreview:              {Owner: "engine", Enabled: true},
	MethodChangesetRevert:               {Owner: "engine", Enabled: true},
	MethodChatStart:                     {Owner: "engine", Enabled: true},
	MethodChatToolApprove:               {Owner: "engine", Enabled: true},
	MethodCollabGateConfirm:             {Owner: "engine", Enabled: true},
	MethodCollabGateEvaluate:            {Owner: "engine", Enabled: true},
	MethodCollabGateStatus:              {Owner: "engine", Enabled: true},
	MethodCommandCancel:                 {Owner: "engine", Enabled: true},
	MethodCommandGet:                    {Owner: "engine", Enabled: true},
	MethodCommandReviewRequest:          {Owner: "engine", Enabled: true},
	MethodCommandStart:                  {Owner: "engine", Enabled: true},
	MethodComplexityDecide:              {Owner: "engine", Enabled: true},
	MethodConnectorSnapshot:             {Owner: "engine", Enabled: true},
	MethodContextCompactCancel:          {Owner: "engine", Enabled: true},
	MethodContextCompactCommit:          {Owner: "engine", Enabled: true},
	MethodContextCompactPreview:         {Owner: "engine", Enabled: true},
	MethodContextHandoffCreate:          {Owner: "engine", Enabled: true},
	MethodContextHandoffImport:          {Owner: "engine", Enabled: true},
	MethodContextHandoffInspect:         {Owner: "engine", Enabled: true},
	MethodContextHandoffList:            {Owner: "engine", Enabled: true},
	MethodContextHandoffListImports:     {Owner: "engine", Enabled: true},
	MethodContextHandoffRevoke:          {Owner: "engine", Enabled: true},
	MethodContextStatus:                 {Owner: "engine", Enabled: true},
	MethodConversationsRootGet:          {Owner: "engine", Enabled: true},
	MethodConversationsRootSelect:       {Owner: "host", Enabled: true},
	MethodConversationsRootSet:          {Owner: "engine", Enabled: true},
	MethodDbQuery:                       {Owner: "engine", Enabled: true},
	MethodDelegationCreate:              {Owner: "engine", Enabled: true},
	MethodDelegationSettle:              {Owner: "engine", Enabled: true},
	MethodDeliverableConfirmGate:        {Owner: "engine", Enabled: true},
	MethodDeliverableList:               {Owner: "engine", Enabled: true},
	MethodDeliverableUpsert:             {Owner: "engine", Enabled: true},
	MethodDevTaskCreate:                 {Owner: "engine", Enabled: true},
	MethodDevTaskTransition:             {Owner: "engine", Enabled: true},
	MethodDiagnosticsExport:             {Owner: "host", Enabled: true},
	MethodDocumentParse:                 {Owner: "engine", Enabled: true},
	MethodEvidenceAttachScan:            {Owner: "engine", Enabled: true},
	MethodEvidenceAttachTest:            {Owner: "engine", Enabled: true},
	MethodEvidenceList:                  {Owner: "engine", Enabled: true},
	MethodExpertArchive:                 {Owner: "engine", Enabled: true},
	MethodExpertCatalogList:             {Owner: "engine", Enabled: true},
	MethodExpertCreate:                  {Owner: "engine", Enabled: true},
	MethodExpertDetail:                  {Owner: "engine", Enabled: true},
	MethodExpertInstall:                 {Owner: "engine", Enabled: true},
	MethodExpertList:                    {Owner: "engine", Enabled: true},
	MethodExpertMount:                   {Owner: "engine", Enabled: true},
	MethodExpertMountingGet:             {Owner: "engine", Enabled: true},
	MethodExpertScenarioCreate:          {Owner: "engine", Enabled: true},
	MethodExpertScenarioDelete:          {Owner: "engine", Enabled: true},
	MethodExpertScenarioList:            {Owner: "engine", Enabled: true},
	MethodExpertToggle:                  {Owner: "engine", Enabled: true},
	MethodExpertUpdate:                  {Owner: "engine", Enabled: true},
	MethodExtensionInstall:              {Owner: "engine", Enabled: true},
	MethodExtensionLifecycle:            {Owner: "engine", Enabled: true},
	MethodExtensionSearch:               {Owner: "engine", Enabled: true},
	MethodFeedbackCandidates:            {Owner: "engine", Enabled: true},
	MethodFeedbackRecord:                {Owner: "engine", Enabled: true},
	MethodFsGlob:                        {Owner: "engine", Enabled: true},
	MethodFsGrep:                        {Owner: "engine", Enabled: true},
	MethodFsRead:                        {Owner: "engine", Enabled: true},
	MethodFsReadMany:                    {Owner: "engine", Enabled: true},
	MethodFsStat:                        {Owner: "engine", Enabled: true},
	MethodFsTree:                        {Owner: "engine", Enabled: true},
	MethodGitRead:                       {Owner: "engine", Enabled: true},
	MethodHandoffAccept:                 {Owner: "engine", Enabled: true},
	MethodHttpDownload:                  {Owner: "engine", Enabled: true},
	MethodHttpRequest:                   {Owner: "engine", Enabled: true},
	MethodKbUpsertDocument:              {Owner: "engine", Enabled: true},
	MethodMcConfigValidate:              {Owner: "engine", Enabled: true},
	MethodMcConfirmToken:                {Owner: "engine", Enabled: true},
	MethodMcConnectorInstall:            {Owner: "engine", Enabled: true},
	MethodMcConnectorUninstall:          {Owner: "engine", Enabled: true},
	MethodMcConnectorUpdate:             {Owner: "engine", Enabled: true},
	MethodMcConnectorUsage:              {Owner: "engine", Enabled: true},
	MethodMcMarketDetail:                {Owner: "engine", Enabled: true},
	MethodMcMarketList:                  {Owner: "engine", Enabled: true},
	MethodMcTombstoneCheck:              {Owner: "engine", Enabled: true},
	MethodMcpAdd:                        {Owner: "engine", Enabled: true},
	MethodMcpHealth:                     {Owner: "engine", Enabled: true},
	MethodMcpInvoke:                     {Owner: "engine", Enabled: true},
	MethodMcpList:                       {Owner: "engine", Enabled: true},
	MethodMcpMarketSearch:               {Owner: "engine", Enabled: true},
	MethodMcpToggle:                     {Owner: "engine", Enabled: true},
	MethodMcp6Invoke:                    {Owner: "engine", Enabled: true},
	MethodMcp6PresetsList:               {Owner: "engine", Enabled: true},
	MethodMcp6Register:                  {Owner: "engine", Enabled: true},
	MethodMcp6Revoke:                    {Owner: "engine", Enabled: true},
	MethodMemoryConfirmCandidate:        {Owner: "engine", Enabled: true},
	MethodMemoryCreate:                  {Owner: "engine", Enabled: true},
	MethodMemoryDelete:                  {Owner: "engine", Enabled: true},
	MethodMemoryExport:                  {Owner: "engine", Enabled: true},
	MethodMemoryFactsFlag:               {Owner: "engine", Enabled: true},
	MethodMemoryFactsList:               {Owner: "engine", Enabled: true},
	MethodMemoryGet:                     {Owner: "engine", Enabled: true},
	MethodMemoryGrowthDecide:            {Owner: "engine", Enabled: true},
	MethodMemoryGrowthList:              {Owner: "engine", Enabled: true},
	MethodMemoryList:                    {Owner: "engine", Enabled: true},
	MethodMemoryNominate:                {Owner: "engine", Enabled: true},
	MethodMemoryNominationList:          {Owner: "engine", Enabled: true},
	MethodMemoryNominationWithdraw:      {Owner: "engine", Enabled: true},
	MethodMemoryPurge:                   {Owner: "engine", Enabled: true},
	MethodMemorySearch:                  {Owner: "engine", Enabled: true},
	MethodMemorySettingsGet:             {Owner: "engine", Enabled: true},
	MethodMemorySettingsUpdate:          {Owner: "engine", Enabled: true},
	MethodMemoryStats:                   {Owner: "engine", Enabled: true},
	MethodMemoryTracesList:              {Owner: "engine", Enabled: true},
	MethodMemoryUpdate:                  {Owner: "engine", Enabled: true},
	MethodMergeSubmit:                   {Owner: "engine", Enabled: true},
	MethodMessageAppend:                 {Owner: "engine", Enabled: true},
	MethodMessageList:                   {Owner: "engine", Enabled: true},
	MethodMessageRewind:                 {Owner: "engine", Enabled: true},
	MethodMigrationInspect:              {Owner: "engine", Enabled: true},
	MethodMigrationRun:                  {Owner: "engine", Enabled: true},
	MethodMigrationStatus:               {Owner: "engine", Enabled: true},
	MethodNodeComplete:                  {Owner: "engine", Enabled: true},
	MethodNodeCreate:                    {Owner: "engine", Enabled: true},
	MethodNodeFail:                      {Owner: "engine", Enabled: true},
	MethodNodeList:                      {Owner: "engine", Enabled: true},
	MethodNodeStart:                     {Owner: "engine", Enabled: true},
	MethodOntologyEdgeCreate:            {Owner: "engine", Enabled: true},
	MethodOntologyEdgeDelete:            {Owner: "engine", Enabled: true},
	MethodOntologyEdgeList:              {Owner: "engine", Enabled: true},
	MethodOntologyEdgeUpdate:            {Owner: "engine", Enabled: true},
	MethodOntologyNodeCreate:            {Owner: "engine", Enabled: true},
	MethodOntologyNodeDelete:            {Owner: "engine", Enabled: true},
	MethodOntologyNodeGet:               {Owner: "engine", Enabled: true},
	MethodOntologyNodeList:              {Owner: "engine", Enabled: true},
	MethodOntologyNodeSearch:            {Owner: "engine", Enabled: true},
	MethodOntologyNodeUpdate:            {Owner: "engine", Enabled: true},
	MethodOpenapiParse:                  {Owner: "engine", Enabled: true},
	MethodOrgActivate:                   {Owner: "engine", Enabled: true},
	MethodOrgCreate:                     {Owner: "engine", Enabled: true},
	MethodOrgMemberInvite:               {Owner: "engine", Enabled: true},
	MethodOrgMemberList:                 {Owner: "engine", Enabled: true},
	MethodOrgMemberRevoke:               {Owner: "engine", Enabled: true},
	MethodOrgSpaceCreate:                {Owner: "engine", Enabled: true},
	MethodOrgSpaceList:                  {Owner: "engine", Enabled: true},
	MethodOrgSummary:                    {Owner: "engine", Enabled: true},
	MethodOrgSuspend:                    {Owner: "engine", Enabled: true},
	MethodOrgSwitch:                     {Owner: "engine", Enabled: true},
	MethodPlanActivate:                  {Owner: "engine", Enabled: true},
	MethodPlanComplete:                  {Owner: "engine", Enabled: true},
	MethodPlanCreate:                    {Owner: "engine", Enabled: true},
	MethodPlanGet:                       {Owner: "engine", Enabled: true},
	MethodPlanList:                      {Owner: "engine", Enabled: true},
	MethodPlanPause:                     {Owner: "engine", Enabled: true},
	MethodPlanResume:                    {Owner: "engine", Enabled: true},
	MethodPlanRunCancel:                 {Owner: "engine", Enabled: true},
	MethodPlanRunJoin:                   {Owner: "engine", Enabled: true},
	MethodPlanRunSpawn:                  {Owner: "engine", Enabled: true},
	MethodPlanRunStart:                  {Owner: "engine", Enabled: true},
	MethodPlanRunTree:                   {Owner: "engine", Enabled: true},
	MethodPlanTodoCreate:                {Owner: "engine", Enabled: true},
	MethodPluginDevCreate:               {Owner: "engine", Enabled: true},
	MethodPluginInstall:                 {Owner: "engine", Enabled: true},
	MethodPluginList:                    {Owner: "engine", Enabled: true},
	MethodPluginMarketDetail:            {Owner: "engine", Enabled: true},
	MethodPluginMarketSearch:            {Owner: "engine", Enabled: true},
	MethodPluginToggle:                  {Owner: "engine", Enabled: true},
	MethodPluginUninstall:               {Owner: "engine", Enabled: true},
	MethodPluginUpgrade:                 {Owner: "engine", Enabled: true},
	MethodProjectAdvanceStatus:          {Owner: "engine", Enabled: true},
	MethodProjectClose:                  {Owner: "engine", Enabled: true},
	MethodProjectCreate:                 {Owner: "engine", Enabled: true},
	MethodProjectDelete:                 {Owner: "engine", Enabled: true},
	MethodProjectList:                   {Owner: "engine", Enabled: true},
	MethodProjectPublish:                {Owner: "engine", Enabled: true},
	MethodProjectReopen:                 {Owner: "engine", Enabled: true},
	MethodProjectUpdate:                 {Owner: "engine", Enabled: true},
	MethodProjectAttachmentIngest:       {Owner: "engine", Enabled: true},
	MethodProjectAttachmentList:         {Owner: "engine", Enabled: true},
	MethodProviderCreate:                {Owner: "engine", Enabled: true},
	MethodProviderCredentialReveal:      {Owner: "host", Enabled: true},
	MethodProviderCredentialSubmit:      {Owner: "host", Enabled: true},
	MethodProviderDelete:                {Owner: "engine", Enabled: true},
	MethodProviderGet:                   {Owner: "engine", Enabled: true},
	MethodProviderList:                  {Owner: "engine", Enabled: true},
	MethodProviderModelSync:             {Owner: "engine", Enabled: true},
	MethodProviderTest:                  {Owner: "engine", Enabled: true},
	MethodProviderUpdate:                {Owner: "engine", Enabled: true},
	MethodRecallQuery:                   {Owner: "engine", Enabled: true},
	MethodReleaseBuildPackage:           {Owner: "engine", Enabled: true},
	MethodReleaseCreateRevision:         {Owner: "engine", Enabled: true},
	MethodReleaseGetPackage:             {Owner: "engine", Enabled: true},
	MethodReleaseGetPromotion:           {Owner: "engine", Enabled: true},
	MethodReleaseGetRevision:            {Owner: "engine", Enabled: true},
	MethodReleasePromote:                {Owner: "engine", Enabled: true},
	MethodReleaseRollback:               {Owner: "engine", Enabled: true},
	MethodReviewApprove:                 {Owner: "engine", Enabled: true},
	MethodReviewDecide:                  {Owner: "engine", Enabled: true},
	MethodReviewList:                    {Owner: "engine", Enabled: true},
	MethodReviewReject:                  {Owner: "engine", Enabled: true},
	MethodReviewSubmit:                  {Owner: "engine", Enabled: true},
	MethodRunCancel:                     {Owner: "engine", Enabled: true},
	MethodRunPlanPut:                    {Owner: "engine", Enabled: true},
	MethodRunQueueConsume:               {Owner: "engine", Enabled: true},
	MethodRunQueueInput:                 {Owner: "engine", Enabled: true},
	MethodRunQueueList:                  {Owner: "engine", Enabled: true},
	MethodRunQueueWithdraw:              {Owner: "engine", Enabled: true},
	MethodRunSend:                       {Owner: "engine", Enabled: true},
	MethodSessionCreate:                 {Owner: "engine", Enabled: true},
	MethodSessionDelete:                 {Owner: "engine", Enabled: true},
	MethodSessionExpertsGet:             {Owner: "engine", Enabled: true},
	MethodSessionExpertsSet:             {Owner: "engine", Enabled: true},
	MethodSessionFolderGet:              {Owner: "engine", Enabled: true},
	MethodSessionFolderOpen:             {Owner: "engine", Enabled: true},
	MethodSessionList:                   {Owner: "engine", Enabled: true},
	MethodSessionUpdate:                 {Owner: "engine", Enabled: true},
	MethodSkillCatalogList:              {Owner: "engine", Enabled: true},
	MethodSkillCategorySet:              {Owner: "engine", Enabled: true},
	MethodSkillCreate:                   {Owner: "engine", Enabled: true},
	MethodSkillDelete:                   {Owner: "engine", Enabled: true},
	MethodSkillDeprecate:                {Owner: "engine", Enabled: true},
	MethodSkillDisable:                  {Owner: "engine", Enabled: true},
	MethodSkillExecute:                  {Owner: "engine", Enabled: true},
	MethodSkillGet:                      {Owner: "engine", Enabled: true},
	MethodSkillImportApprove:            {Owner: "engine", Enabled: true},
	MethodSkillImportDiscover:           {Owner: "engine", Enabled: true},
	MethodSkillImportInspect:            {Owner: "engine", Enabled: true},
	MethodSkillImportReject:             {Owner: "engine", Enabled: true},
	MethodSkillImportRevoke:             {Owner: "engine", Enabled: true},
	MethodSkillImportSubmit:             {Owner: "engine", Enabled: true},
	MethodSkillInstall:                  {Owner: "engine", Enabled: true},
	MethodSkillInvoke:                   {Owner: "engine", Enabled: true},
	MethodSkillList:                     {Owner: "engine", Enabled: true},
	MethodSkillMatch:                    {Owner: "engine", Enabled: true},
	MethodSkillPublish:                  {Owner: "engine", Enabled: true},
	MethodSkillUpdate:                   {Owner: "engine", Enabled: true},
	MethodStageCreate:                   {Owner: "engine", Enabled: true},
	MethodStageList:                     {Owner: "engine", Enabled: true},
	MethodStageUpdate:                   {Owner: "engine", Enabled: true},
	MethodStreamCancel:                  {Owner: "engine", Enabled: true},
	MethodSubagentJoin:                  {Owner: "engine", Enabled: true},
	MethodSubagentSpawn:                 {Owner: "engine", Enabled: true},
	MethodSubagentTree:                  {Owner: "engine", Enabled: true},
	MethodSyncPush:                      {Owner: "engine", Enabled: true},
	MethodSystemHealth:                  {Owner: "engine", Enabled: true},
	MethodSystemSettingsOpen:            {Owner: "host", Enabled: true},
	MethodTemplateCreate:                {Owner: "engine", Enabled: true},
	MethodTemplateDelete:                {Owner: "engine", Enabled: true},
	MethodTemplateEnable:                {Owner: "engine", Enabled: true},
	MethodTemplateList:                  {Owner: "engine", Enabled: true},
	MethodTemplateVoid:                  {Owner: "engine", Enabled: true},
	MethodTerminalClose:                 {Owner: "engine", Enabled: true},
	MethodTerminalInput:                 {Owner: "engine", Enabled: true},
	MethodTerminalResize:                {Owner: "engine", Enabled: true},
	MethodTerminalStart:                 {Owner: "engine", Enabled: true},
	MethodTombstoneDelete:               {Owner: "engine", Enabled: true},
	MethodToolsCommandPolicyGet:         {Owner: "engine", Enabled: true},
	MethodToolsCommandPolicySet:         {Owner: "engine", Enabled: true},
	MethodToolsHooksEventsList:          {Owner: "engine", Enabled: true},
	MethodToolsHooksPolicyGet:           {Owner: "engine", Enabled: true},
	MethodToolsHooksPolicySet:           {Owner: "engine", Enabled: true},
	MethodTraceAddEdge:                  {Owner: "engine", Enabled: true},
	MethodTraceMarkStale:                {Owner: "engine", Enabled: true},
	MethodTraceQuery:                    {Owner: "engine", Enabled: true},
	MethodTraceResolveStale:             {Owner: "engine", Enabled: true},
	MethodTtsCancel:                     {Owner: "engine", Enabled: true},
	MethodTtsEnsureRefEngine:            {Owner: "engine", Enabled: true},
	MethodTtsRefAudios:                  {Owner: "engine", Enabled: true},
	MethodTtsSynthesize:                 {Owner: "engine", Enabled: true},
	MethodTtsVoices:                     {Owner: "engine", Enabled: true},
	MethodUiThemeSet:                    {Owner: "host", Enabled: true},
	MethodWebFetch:                      {Owner: "engine", Enabled: true},
	MethodWebSearch:                     {Owner: "engine", Enabled: true},
	MethodWorkerDispatch:                {Owner: "engine", Enabled: true},
	MethodWorkflowCaptureInput:          {Owner: "engine", Enabled: true},
	MethodWorkflowCreateCheckpoint:      {Owner: "engine", Enabled: true},
	MethodWorkflowCreateVersion:         {Owner: "engine", Enabled: true},
	MethodWorkflowEvaluateGate:          {Owner: "engine", Enabled: true},
	MethodWorkflowPublish:               {Owner: "engine", Enabled: true},
	MethodWorkflowStartStage:            {Owner: "engine", Enabled: true},
	MethodWorkflowTransitionStage:       {Owner: "engine", Enabled: true},
	MethodWorkspaceArtifactExport:       {Owner: "engine", Enabled: true},
	MethodWorkspaceArtifactPreview:      {Owner: "engine", Enabled: true},
	MethodWorkspaceArtifactReviewAppend: {Owner: "engine", Enabled: true},
	MethodWorkspaceArtifactReviewList:   {Owner: "engine", Enabled: true},
	MethodWorkspaceConvert:              {Owner: "engine", Enabled: true},
	MethodWorkspaceGrant:                {Owner: "engine", Enabled: true},
	MethodWorkspaceLease:                {Owner: "engine", Enabled: true},
	MethodWorkspaceList:                 {Owner: "host", Enabled: true},
	MethodWorkspaceRead:                 {Owner: "host", Enabled: true},
	MethodWorkspaceRegister:             {Owner: "engine", Enabled: true},
	MethodWorkspaceRootClear:            {Owner: "host", Enabled: true},
	MethodWorkspaceRootGet:              {Owner: "host", Enabled: true},
	MethodWorkspaceRootSelect:           {Owner: "host", Enabled: true},
}
var Methods = [...]Method{MethodAgentRunCancel, MethodAgentRunGet, MethodAgentRunReconcile, MethodAgentRunResume, MethodAgentRunStart, MethodAppUpdateCheck, MethodAppUpdateInstall, MethodArchivePack, MethodArchiveUnpack, MethodAttachmentDelete, MethodAttachmentGet, MethodAttachmentIngest, MethodAttachmentList, MethodAttachmentUploadAbort, MethodAttachmentUploadBegin, MethodAttachmentUploadChunk, MethodAttachmentUploadCommit, MethodAutomationDispatch, MethodAutomationJobDelete, MethodAutomationJobList, MethodAutomationJobSet, MethodAutomationJobTrigger, MethodAutomationRunList, MethodAutomationStatus, MethodBarrierArrive, MethodBrDataClear, MethodBrDataUsage, MethodBrModeDetect, MethodBrNavigate, MethodBrPermissionDecide, MethodBrPermissionList, MethodBrPermissionPolicy, MethodBrPermissionRequest, MethodBrSessionConnect, MethodBrSessionDisconnect, MethodBrSessionList, MethodBrSettingsGet, MethodBrSettingsUpdate, MethodBrowserAct, MethodBrowserClose, MethodBrowserOpen, MethodCapabilityList, MethodCcEmergencyStop, MethodCcGetAuditLog, MethodCcGetConfig, MethodCcUpdateConfig, MethodChangesetApply, MethodChangesetPreview, MethodChangesetRevert, MethodChatStart, MethodChatToolApprove, MethodCollabGateConfirm, MethodCollabGateEvaluate, MethodCollabGateStatus, MethodCommandCancel, MethodCommandGet, MethodCommandReviewRequest, MethodCommandStart, MethodComplexityDecide, MethodConnectorSnapshot, MethodContextCompactCancel, MethodContextCompactCommit, MethodContextCompactPreview, MethodContextHandoffCreate, MethodContextHandoffImport, MethodContextHandoffInspect, MethodContextHandoffList, MethodContextHandoffListImports, MethodContextHandoffRevoke, MethodContextStatus, MethodConversationsRootGet, MethodConversationsRootSelect, MethodConversationsRootSet, MethodDbQuery, MethodDelegationCreate, MethodDelegationSettle, MethodDeliverableConfirmGate, MethodDeliverableList, MethodDeliverableUpsert, MethodDevTaskCreate, MethodDevTaskTransition, MethodDiagnosticsExport, MethodDocumentParse, MethodEvidenceAttachScan, MethodEvidenceAttachTest, MethodEvidenceList, MethodExpertArchive, MethodExpertCatalogList, MethodExpertCreate, MethodExpertDetail, MethodExpertInstall, MethodExpertList, MethodExpertMount, MethodExpertMountingGet, MethodExpertScenarioCreate, MethodExpertScenarioDelete, MethodExpertScenarioList, MethodExpertToggle, MethodExpertUpdate, MethodExtensionInstall, MethodExtensionLifecycle, MethodExtensionSearch, MethodFeedbackCandidates, MethodFeedbackRecord, MethodFsGlob, MethodFsGrep, MethodFsRead, MethodFsReadMany, MethodFsStat, MethodFsTree, MethodGitRead, MethodHandoffAccept, MethodHttpDownload, MethodHttpRequest, MethodKbUpsertDocument, MethodMcConfigValidate, MethodMcConfirmToken, MethodMcConnectorInstall, MethodMcConnectorUninstall, MethodMcConnectorUpdate, MethodMcConnectorUsage, MethodMcMarketDetail, MethodMcMarketList, MethodMcTombstoneCheck, MethodMcpAdd, MethodMcpHealth, MethodMcpInvoke, MethodMcpList, MethodMcpMarketSearch, MethodMcpToggle, MethodMcp6Invoke, MethodMcp6PresetsList, MethodMcp6Register, MethodMcp6Revoke, MethodMemoryConfirmCandidate, MethodMemoryCreate, MethodMemoryDelete, MethodMemoryExport, MethodMemoryFactsFlag, MethodMemoryFactsList, MethodMemoryGet, MethodMemoryGrowthDecide, MethodMemoryGrowthList, MethodMemoryList, MethodMemoryNominate, MethodMemoryNominationList, MethodMemoryNominationWithdraw, MethodMemoryPurge, MethodMemorySearch, MethodMemorySettingsGet, MethodMemorySettingsUpdate, MethodMemoryStats, MethodMemoryTracesList, MethodMemoryUpdate, MethodMergeSubmit, MethodMessageAppend, MethodMessageList, MethodMessageRewind, MethodMigrationInspect, MethodMigrationRun, MethodMigrationStatus, MethodNodeComplete, MethodNodeCreate, MethodNodeFail, MethodNodeList, MethodNodeStart, MethodOntologyEdgeCreate, MethodOntologyEdgeDelete, MethodOntologyEdgeList, MethodOntologyEdgeUpdate, MethodOntologyNodeCreate, MethodOntologyNodeDelete, MethodOntologyNodeGet, MethodOntologyNodeList, MethodOntologyNodeSearch, MethodOntologyNodeUpdate, MethodOpenapiParse, MethodOrgActivate, MethodOrgCreate, MethodOrgMemberInvite, MethodOrgMemberList, MethodOrgMemberRevoke, MethodOrgSpaceCreate, MethodOrgSpaceList, MethodOrgSummary, MethodOrgSuspend, MethodOrgSwitch, MethodPlanActivate, MethodPlanComplete, MethodPlanCreate, MethodPlanGet, MethodPlanList, MethodPlanPause, MethodPlanResume, MethodPlanRunCancel, MethodPlanRunJoin, MethodPlanRunSpawn, MethodPlanRunStart, MethodPlanRunTree, MethodPlanTodoCreate, MethodPluginDevCreate, MethodPluginInstall, MethodPluginList, MethodPluginMarketDetail, MethodPluginMarketSearch, MethodPluginToggle, MethodPluginUninstall, MethodPluginUpgrade, MethodProjectAdvanceStatus, MethodProjectClose, MethodProjectCreate, MethodProjectDelete, MethodProjectList, MethodProjectPublish, MethodProjectReopen, MethodProjectUpdate, MethodProjectAttachmentIngest, MethodProjectAttachmentList, MethodProviderCreate, MethodProviderCredentialReveal, MethodProviderCredentialSubmit, MethodProviderDelete, MethodProviderGet, MethodProviderList, MethodProviderModelSync, MethodProviderTest, MethodProviderUpdate, MethodRecallQuery, MethodReleaseBuildPackage, MethodReleaseCreateRevision, MethodReleaseGetPackage, MethodReleaseGetPromotion, MethodReleaseGetRevision, MethodReleasePromote, MethodReleaseRollback, MethodReviewApprove, MethodReviewDecide, MethodReviewList, MethodReviewReject, MethodReviewSubmit, MethodRunCancel, MethodRunPlanPut, MethodRunQueueConsume, MethodRunQueueInput, MethodRunQueueList, MethodRunQueueWithdraw, MethodRunSend, MethodSessionCreate, MethodSessionDelete, MethodSessionExpertsGet, MethodSessionExpertsSet, MethodSessionFolderGet, MethodSessionFolderOpen, MethodSessionList, MethodSessionUpdate, MethodSkillCatalogList, MethodSkillCategorySet, MethodSkillCreate, MethodSkillDelete, MethodSkillDeprecate, MethodSkillDisable, MethodSkillExecute, MethodSkillGet, MethodSkillImportApprove, MethodSkillImportDiscover, MethodSkillImportInspect, MethodSkillImportReject, MethodSkillImportRevoke, MethodSkillImportSubmit, MethodSkillInstall, MethodSkillInvoke, MethodSkillList, MethodSkillMatch, MethodSkillPublish, MethodSkillUpdate, MethodStageCreate, MethodStageList, MethodStageUpdate, MethodStreamCancel, MethodSubagentJoin, MethodSubagentSpawn, MethodSubagentTree, MethodSyncPush, MethodSystemHealth, MethodSystemSettingsOpen, MethodTemplateCreate, MethodTemplateDelete, MethodTemplateEnable, MethodTemplateList, MethodTemplateVoid, MethodTerminalClose, MethodTerminalInput, MethodTerminalResize, MethodTerminalStart, MethodTombstoneDelete, MethodToolsCommandPolicyGet, MethodToolsCommandPolicySet, MethodToolsHooksEventsList, MethodToolsHooksPolicyGet, MethodToolsHooksPolicySet, MethodTraceAddEdge, MethodTraceMarkStale, MethodTraceQuery, MethodTraceResolveStale, MethodTtsCancel, MethodTtsEnsureRefEngine, MethodTtsRefAudios, MethodTtsSynthesize, MethodTtsVoices, MethodUiThemeSet, MethodWebFetch, MethodWebSearch, MethodWorkerDispatch, MethodWorkflowCaptureInput, MethodWorkflowCreateCheckpoint, MethodWorkflowCreateVersion, MethodWorkflowEvaluateGate, MethodWorkflowPublish, MethodWorkflowStartStage, MethodWorkflowTransitionStage, MethodWorkspaceArtifactExport, MethodWorkspaceArtifactPreview, MethodWorkspaceArtifactReviewAppend, MethodWorkspaceArtifactReviewList, MethodWorkspaceConvert, MethodWorkspaceGrant, MethodWorkspaceLease, MethodWorkspaceList, MethodWorkspaceRead, MethodWorkspaceRegister, MethodWorkspaceRootClear, MethodWorkspaceRootGet, MethodWorkspaceRootSelect}

func ValidMethod(method string) bool { _, ok := MethodMetadataByMethod[Method(method)]; return ok }
