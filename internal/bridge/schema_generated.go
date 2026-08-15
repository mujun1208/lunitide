// Code generated from discovered bridge schemas. DO NOT EDIT.
package bridge

const Version = "1.0"

type Method string

const (
	MethodAgentRunCancel            Method = "agent.run.cancel"
	MethodAgentRunGet               Method = "agent.run.get"
	MethodAgentRunReconcile         Method = "agent.run.reconcile"
	MethodAgentRunResume            Method = "agent.run.resume"
	MethodAgentRunStart             Method = "agent.run.start"
	MethodAttachmentDelete          Method = "attachment.delete"
	MethodAttachmentGet             Method = "attachment.get"
	MethodAttachmentIngest          Method = "attachment.ingest"
	MethodAttachmentList            Method = "attachment.list"
	MethodAttachmentUploadAbort     Method = "attachment.upload.abort"
	MethodAttachmentUploadBegin     Method = "attachment.upload.begin"
	MethodAttachmentUploadChunk     Method = "attachment.upload.chunk"
	MethodAttachmentUploadCommit    Method = "attachment.upload.commit"
	MethodBarrierArrive             Method = "barrier.arrive"
	MethodBrowserAct                Method = "browser.act"
	MethodBrowserClose              Method = "browser.close"
	MethodBrowserOpen               Method = "browser.open"
	MethodCapabilityList            Method = "capability.list"
	MethodChangesetApply            Method = "changeset.apply"
	MethodChangesetPreview          Method = "changeset.preview"
	MethodChangesetRevert           Method = "changeset.revert"
	MethodChatStart                 Method = "chat.start"
	MethodChatToolApprove           Method = "chat.tool.approve"
	MethodCommandCancel             Method = "command.cancel"
	MethodCommandGet                Method = "command.get"
	MethodCommandReviewRequest      Method = "command.review.request"
	MethodCommandStart              Method = "command.start"
	MethodComplexityDecide          Method = "complexity.decide"
	MethodConnectorSnapshot         Method = "connector.snapshot"
	MethodContextCompactCancel      Method = "context.compact.cancel"
	MethodContextCompactCommit      Method = "context.compact.commit"
	MethodContextCompactPreview     Method = "context.compact.preview"
	MethodContextHandoffCreate      Method = "context.handoff.create"
	MethodContextHandoffImport      Method = "context.handoff.import"
	MethodContextHandoffInspect     Method = "context.handoff.inspect"
	MethodContextHandoffList        Method = "context.handoff.list"
	MethodContextHandoffListImports Method = "context.handoff.list-imports"
	MethodContextHandoffRevoke      Method = "context.handoff.revoke"
	MethodContextStatus             Method = "context.status"
	MethodDelegationCreate          Method = "delegation.create"
	MethodDelegationSettle          Method = "delegation.settle"
	MethodDevTaskCreate             Method = "devTask.create"
	MethodDevTaskTransition         Method = "devTask.transition"
	MethodDiagnosticsExport         Method = "diagnostics.export"
	MethodEvidenceAttachScan        Method = "evidence.attachScan"
	MethodEvidenceAttachTest        Method = "evidence.attachTest"
	MethodEvidenceList              Method = "evidence.list"
	MethodExtensionInstall          Method = "extension.install"
	MethodExtensionLifecycle        Method = "extension.lifecycle"
	MethodExtensionSearch           Method = "extension.search"
	MethodFsGlob                    Method = "fs.glob"
	MethodFsGrep                    Method = "fs.grep"
	MethodFsRead                    Method = "fs.read"
	MethodFsReadMany                Method = "fs.readMany"
	MethodFsStat                    Method = "fs.stat"
	MethodFsTree                    Method = "fs.tree"
	MethodMcpInvoke                 Method = "mcp.invoke"
	MethodMcp6Invoke                Method = "mcp6.invoke"
	MethodMcp6Register              Method = "mcp6.register"
	MethodMcp6Revoke                Method = "mcp6.revoke"
	MethodMemoryCreate              Method = "memory.create"
	MethodMemoryDelete              Method = "memory.delete"
	MethodMemoryGet                 Method = "memory.get"
	MethodMemoryList                Method = "memory.list"
	MethodMemorySearch              Method = "memory.search"
	MethodMemoryUpdate              Method = "memory.update"
	MethodMergeSubmit               Method = "merge.submit"
	MethodMessageAppend             Method = "message.append"
	MethodMessageList               Method = "message.list"
	MethodMessageRewind             Method = "message.rewind"
	MethodMigrationInspect          Method = "migration.inspect"
	MethodMigrationRun              Method = "migration.run"
	MethodMigrationStatus           Method = "migration.status"
	MethodNodeComplete              Method = "node.complete"
	MethodNodeCreate                Method = "node.create"
	MethodNodeFail                  Method = "node.fail"
	MethodNodeList                  Method = "node.list"
	MethodNodeStart                 Method = "node.start"
	MethodOntologyEdgeCreate        Method = "ontology.edge.create"
	MethodOntologyEdgeDelete        Method = "ontology.edge.delete"
	MethodOntologyEdgeList          Method = "ontology.edge.list"
	MethodOntologyEdgeUpdate        Method = "ontology.edge.update"
	MethodOntologyNodeCreate        Method = "ontology.node.create"
	MethodOntologyNodeDelete        Method = "ontology.node.delete"
	MethodOntologyNodeGet           Method = "ontology.node.get"
	MethodOntologyNodeList          Method = "ontology.node.list"
	MethodOntologyNodeSearch        Method = "ontology.node.search"
	MethodOntologyNodeUpdate        Method = "ontology.node.update"
	MethodOpenapiParse              Method = "openapi.parse"
	MethodPlanActivate              Method = "plan.activate"
	MethodPlanComplete              Method = "plan.complete"
	MethodPlanCreate                Method = "plan.create"
	MethodPlanGet                   Method = "plan.get"
	MethodPlanList                  Method = "plan.list"
	MethodPlanPause                 Method = "plan.pause"
	MethodPlanResume                Method = "plan.resume"
	MethodPlanRunCancel             Method = "plan.run.cancel"
	MethodPlanRunJoin               Method = "plan.run.join"
	MethodPlanRunSpawn              Method = "plan.run.spawn"
	MethodPlanRunStart              Method = "plan.run.start"
	MethodPlanRunTree               Method = "plan.run.tree"
	MethodPlanTodoCreate            Method = "plan.todo.create"
	MethodProjectCreate             Method = "project.create"
	MethodProjectDelete             Method = "project.delete"
	MethodProjectList               Method = "project.list"
	MethodProviderCreate            Method = "provider.create"
	MethodProviderCredentialReveal  Method = "provider.credential.reveal"
	MethodProviderCredentialSubmit  Method = "provider.credential.submit"
	MethodProviderDelete            Method = "provider.delete"
	MethodProviderGet               Method = "provider.get"
	MethodProviderList              Method = "provider.list"
	MethodProviderModelSync         Method = "provider.model.sync"
	MethodProviderTest              Method = "provider.test"
	MethodProviderUpdate            Method = "provider.update"
	MethodReviewApprove             Method = "review.approve"
	MethodReviewDecide              Method = "review.decide"
	MethodReviewList                Method = "review.list"
	MethodReviewReject              Method = "review.reject"
	MethodReviewSubmit              Method = "review.submit"
	MethodRunCancel                 Method = "run.cancel"
	MethodRunPlanPut                Method = "run.plan.put"
	MethodRunSend                   Method = "run.send"
	MethodSessionCreate             Method = "session.create"
	MethodSessionDelete             Method = "session.delete"
	MethodSessionList               Method = "session.list"
	MethodSessionUpdate             Method = "session.update"
	MethodSkillCreate               Method = "skill.create"
	MethodSkillDelete               Method = "skill.delete"
	MethodSkillDeprecate            Method = "skill.deprecate"
	MethodSkillDisable              Method = "skill.disable"
	MethodSkillExecute              Method = "skill.execute"
	MethodSkillGet                  Method = "skill.get"
	MethodSkillImportApprove        Method = "skill.import.approve"
	MethodSkillImportDiscover       Method = "skill.import.discover"
	MethodSkillImportInspect        Method = "skill.import.inspect"
	MethodSkillImportReject         Method = "skill.import.reject"
	MethodSkillImportRevoke         Method = "skill.import.revoke"
	MethodSkillImportSubmit         Method = "skill.import.submit"
	MethodSkillInvoke               Method = "skill.invoke"
	MethodSkillList                 Method = "skill.list"
	MethodSkillMatch                Method = "skill.match"
	MethodSkillPublish              Method = "skill.publish"
	MethodSkillUpdate               Method = "skill.update"
	MethodStageCreate               Method = "stage.create"
	MethodStageList                 Method = "stage.list"
	MethodStreamCancel              Method = "stream.cancel"
	MethodSystemHealth              Method = "system.health"
	MethodSystemSettingsOpen        Method = "system.settings.open"
	MethodTerminalClose             Method = "terminal.close"
	MethodTerminalInput             Method = "terminal.input"
	MethodTerminalResize            Method = "terminal.resize"
	MethodTerminalStart             Method = "terminal.start"
	MethodTraceAddEdge              Method = "trace.addEdge"
	MethodTraceMarkStale            Method = "trace.markStale"
	MethodTraceQuery                Method = "trace.query"
	MethodTraceResolveStale         Method = "trace.resolveStale"
	MethodUiThemeSet                Method = "ui.theme.set"
	MethodWebFetch                  Method = "web.fetch"
	MethodWebSearch                 Method = "web.search"
	MethodWorkerDispatch            Method = "worker.dispatch"
	MethodWorkflowCaptureInput      Method = "workflow.captureInput"
	MethodWorkflowCreateCheckpoint  Method = "workflow.createCheckpoint"
	MethodWorkflowCreateVersion     Method = "workflow.createVersion"
	MethodWorkflowEvaluateGate      Method = "workflow.evaluateGate"
	MethodWorkflowPublish           Method = "workflow.publish"
	MethodWorkflowStartStage        Method = "workflow.startStage"
	MethodWorkflowTransitionStage   Method = "workflow.transitionStage"
	MethodWorkspaceConvert          Method = "workspace.convert"
	MethodWorkspaceGrant            Method = "workspace.grant"
	MethodWorkspaceLease            Method = "workspace.lease"
	MethodWorkspaceList             Method = "workspace.list"
	MethodWorkspaceRead             Method = "workspace.read"
	MethodWorkspaceRegister         Method = "workspace.register"
	MethodWorkspaceRootGet          Method = "workspace.root.get"
	MethodWorkspaceRootSelect       Method = "workspace.root.select"
)

type MethodMetadata struct {
	Owner   string
	Enabled bool
}

var MethodMetadataByMethod = map[Method]MethodMetadata{
	MethodAgentRunCancel:            {Owner: "engine", Enabled: true},
	MethodAgentRunGet:               {Owner: "engine", Enabled: true},
	MethodAgentRunReconcile:         {Owner: "engine", Enabled: true},
	MethodAgentRunResume:            {Owner: "engine", Enabled: true},
	MethodAgentRunStart:             {Owner: "engine", Enabled: true},
	MethodAttachmentDelete:          {Owner: "engine", Enabled: true},
	MethodAttachmentGet:             {Owner: "engine", Enabled: true},
	MethodAttachmentIngest:          {Owner: "engine", Enabled: true},
	MethodAttachmentList:            {Owner: "engine", Enabled: true},
	MethodAttachmentUploadAbort:     {Owner: "engine", Enabled: true},
	MethodAttachmentUploadBegin:     {Owner: "engine", Enabled: true},
	MethodAttachmentUploadChunk:     {Owner: "engine", Enabled: true},
	MethodAttachmentUploadCommit:    {Owner: "engine", Enabled: true},
	MethodBarrierArrive:             {Owner: "engine", Enabled: true},
	MethodBrowserAct:                {Owner: "engine", Enabled: true},
	MethodBrowserClose:              {Owner: "host", Enabled: true},
	MethodBrowserOpen:               {Owner: "host", Enabled: true},
	MethodCapabilityList:            {Owner: "engine", Enabled: true},
	MethodChangesetApply:            {Owner: "engine", Enabled: true},
	MethodChangesetPreview:          {Owner: "engine", Enabled: true},
	MethodChangesetRevert:           {Owner: "engine", Enabled: true},
	MethodChatStart:                 {Owner: "engine", Enabled: true},
	MethodChatToolApprove:           {Owner: "engine", Enabled: true},
	MethodCommandCancel:             {Owner: "engine", Enabled: true},
	MethodCommandGet:                {Owner: "engine", Enabled: true},
	MethodCommandReviewRequest:      {Owner: "engine", Enabled: true},
	MethodCommandStart:              {Owner: "engine", Enabled: true},
	MethodComplexityDecide:          {Owner: "engine", Enabled: true},
	MethodConnectorSnapshot:         {Owner: "engine", Enabled: true},
	MethodContextCompactCancel:      {Owner: "engine", Enabled: true},
	MethodContextCompactCommit:      {Owner: "engine", Enabled: true},
	MethodContextCompactPreview:     {Owner: "engine", Enabled: true},
	MethodContextHandoffCreate:      {Owner: "engine", Enabled: true},
	MethodContextHandoffImport:      {Owner: "engine", Enabled: true},
	MethodContextHandoffInspect:     {Owner: "engine", Enabled: true},
	MethodContextHandoffList:        {Owner: "engine", Enabled: true},
	MethodContextHandoffListImports: {Owner: "engine", Enabled: true},
	MethodContextHandoffRevoke:      {Owner: "engine", Enabled: true},
	MethodContextStatus:             {Owner: "engine", Enabled: true},
	MethodDelegationCreate:          {Owner: "engine", Enabled: true},
	MethodDelegationSettle:          {Owner: "engine", Enabled: true},
	MethodDevTaskCreate:             {Owner: "engine", Enabled: true},
	MethodDevTaskTransition:         {Owner: "engine", Enabled: true},
	MethodDiagnosticsExport:         {Owner: "host", Enabled: true},
	MethodEvidenceAttachScan:        {Owner: "engine", Enabled: true},
	MethodEvidenceAttachTest:        {Owner: "engine", Enabled: true},
	MethodEvidenceList:              {Owner: "engine", Enabled: true},
	MethodExtensionInstall:          {Owner: "engine", Enabled: true},
	MethodExtensionLifecycle:        {Owner: "engine", Enabled: true},
	MethodExtensionSearch:           {Owner: "engine", Enabled: true},
	MethodFsGlob:                    {Owner: "engine", Enabled: true},
	MethodFsGrep:                    {Owner: "engine", Enabled: true},
	MethodFsRead:                    {Owner: "engine", Enabled: true},
	MethodFsReadMany:                {Owner: "engine", Enabled: true},
	MethodFsStat:                    {Owner: "engine", Enabled: true},
	MethodFsTree:                    {Owner: "engine", Enabled: true},
	MethodMcpInvoke:                 {Owner: "engine", Enabled: true},
	MethodMcp6Invoke:                {Owner: "engine", Enabled: true},
	MethodMcp6Register:              {Owner: "engine", Enabled: true},
	MethodMcp6Revoke:                {Owner: "engine", Enabled: true},
	MethodMemoryCreate:              {Owner: "engine", Enabled: true},
	MethodMemoryDelete:              {Owner: "engine", Enabled: true},
	MethodMemoryGet:                 {Owner: "engine", Enabled: true},
	MethodMemoryList:                {Owner: "engine", Enabled: true},
	MethodMemorySearch:              {Owner: "engine", Enabled: true},
	MethodMemoryUpdate:              {Owner: "engine", Enabled: true},
	MethodMergeSubmit:               {Owner: "engine", Enabled: true},
	MethodMessageAppend:             {Owner: "engine", Enabled: true},
	MethodMessageList:               {Owner: "engine", Enabled: true},
	MethodMessageRewind:             {Owner: "engine", Enabled: true},
	MethodMigrationInspect:          {Owner: "engine", Enabled: true},
	MethodMigrationRun:              {Owner: "engine", Enabled: true},
	MethodMigrationStatus:           {Owner: "engine", Enabled: true},
	MethodNodeComplete:              {Owner: "engine", Enabled: true},
	MethodNodeCreate:                {Owner: "engine", Enabled: true},
	MethodNodeFail:                  {Owner: "engine", Enabled: true},
	MethodNodeList:                  {Owner: "engine", Enabled: true},
	MethodNodeStart:                 {Owner: "engine", Enabled: true},
	MethodOntologyEdgeCreate:        {Owner: "engine", Enabled: true},
	MethodOntologyEdgeDelete:        {Owner: "engine", Enabled: true},
	MethodOntologyEdgeList:          {Owner: "engine", Enabled: true},
	MethodOntologyEdgeUpdate:        {Owner: "engine", Enabled: true},
	MethodOntologyNodeCreate:        {Owner: "engine", Enabled: true},
	MethodOntologyNodeDelete:        {Owner: "engine", Enabled: true},
	MethodOntologyNodeGet:           {Owner: "engine", Enabled: true},
	MethodOntologyNodeList:          {Owner: "engine", Enabled: true},
	MethodOntologyNodeSearch:        {Owner: "engine", Enabled: true},
	MethodOntologyNodeUpdate:        {Owner: "engine", Enabled: true},
	MethodOpenapiParse:              {Owner: "engine", Enabled: true},
	MethodPlanActivate:              {Owner: "engine", Enabled: true},
	MethodPlanComplete:              {Owner: "engine", Enabled: true},
	MethodPlanCreate:                {Owner: "engine", Enabled: true},
	MethodPlanGet:                   {Owner: "engine", Enabled: true},
	MethodPlanList:                  {Owner: "engine", Enabled: true},
	MethodPlanPause:                 {Owner: "engine", Enabled: true},
	MethodPlanResume:                {Owner: "engine", Enabled: true},
	MethodPlanRunCancel:             {Owner: "engine", Enabled: true},
	MethodPlanRunJoin:               {Owner: "engine", Enabled: true},
	MethodPlanRunSpawn:              {Owner: "engine", Enabled: true},
	MethodPlanRunStart:              {Owner: "engine", Enabled: true},
	MethodPlanRunTree:               {Owner: "engine", Enabled: true},
	MethodPlanTodoCreate:            {Owner: "engine", Enabled: true},
	MethodProjectCreate:             {Owner: "engine", Enabled: true},
	MethodProjectDelete:             {Owner: "engine", Enabled: true},
	MethodProjectList:               {Owner: "engine", Enabled: true},
	MethodProviderCreate:            {Owner: "engine", Enabled: true},
	MethodProviderCredentialReveal:  {Owner: "host", Enabled: true},
	MethodProviderCredentialSubmit:  {Owner: "host", Enabled: true},
	MethodProviderDelete:            {Owner: "engine", Enabled: true},
	MethodProviderGet:               {Owner: "engine", Enabled: true},
	MethodProviderList:              {Owner: "engine", Enabled: true},
	MethodProviderModelSync:         {Owner: "engine", Enabled: true},
	MethodProviderTest:              {Owner: "engine", Enabled: true},
	MethodProviderUpdate:            {Owner: "engine", Enabled: true},
	MethodReviewApprove:             {Owner: "engine", Enabled: true},
	MethodReviewDecide:              {Owner: "engine", Enabled: true},
	MethodReviewList:                {Owner: "engine", Enabled: true},
	MethodReviewReject:              {Owner: "engine", Enabled: true},
	MethodReviewSubmit:              {Owner: "engine", Enabled: true},
	MethodRunCancel:                 {Owner: "engine", Enabled: true},
	MethodRunPlanPut:                {Owner: "engine", Enabled: true},
	MethodRunSend:                   {Owner: "engine", Enabled: true},
	MethodSessionCreate:             {Owner: "engine", Enabled: true},
	MethodSessionDelete:             {Owner: "engine", Enabled: true},
	MethodSessionList:               {Owner: "engine", Enabled: true},
	MethodSessionUpdate:             {Owner: "engine", Enabled: true},
	MethodSkillCreate:               {Owner: "engine", Enabled: true},
	MethodSkillDelete:               {Owner: "engine", Enabled: true},
	MethodSkillDeprecate:            {Owner: "engine", Enabled: true},
	MethodSkillDisable:              {Owner: "engine", Enabled: true},
	MethodSkillExecute:              {Owner: "engine", Enabled: true},
	MethodSkillGet:                  {Owner: "engine", Enabled: true},
	MethodSkillImportApprove:        {Owner: "engine", Enabled: true},
	MethodSkillImportDiscover:       {Owner: "engine", Enabled: true},
	MethodSkillImportInspect:        {Owner: "engine", Enabled: true},
	MethodSkillImportReject:         {Owner: "engine", Enabled: true},
	MethodSkillImportRevoke:         {Owner: "engine", Enabled: true},
	MethodSkillImportSubmit:         {Owner: "engine", Enabled: true},
	MethodSkillInvoke:               {Owner: "engine", Enabled: true},
	MethodSkillList:                 {Owner: "engine", Enabled: true},
	MethodSkillMatch:                {Owner: "engine", Enabled: true},
	MethodSkillPublish:              {Owner: "engine", Enabled: true},
	MethodSkillUpdate:               {Owner: "engine", Enabled: true},
	MethodStageCreate:               {Owner: "engine", Enabled: true},
	MethodStageList:                 {Owner: "engine", Enabled: true},
	MethodStreamCancel:              {Owner: "engine", Enabled: true},
	MethodSystemHealth:              {Owner: "engine", Enabled: true},
	MethodSystemSettingsOpen:        {Owner: "host", Enabled: true},
	MethodTerminalClose:             {Owner: "engine", Enabled: true},
	MethodTerminalInput:             {Owner: "engine", Enabled: true},
	MethodTerminalResize:            {Owner: "engine", Enabled: true},
	MethodTerminalStart:             {Owner: "engine", Enabled: true},
	MethodTraceAddEdge:              {Owner: "engine", Enabled: true},
	MethodTraceMarkStale:            {Owner: "engine", Enabled: true},
	MethodTraceQuery:                {Owner: "engine", Enabled: true},
	MethodTraceResolveStale:         {Owner: "engine", Enabled: true},
	MethodUiThemeSet:                {Owner: "host", Enabled: true},
	MethodWebFetch:                  {Owner: "engine", Enabled: true},
	MethodWebSearch:                 {Owner: "engine", Enabled: true},
	MethodWorkerDispatch:            {Owner: "engine", Enabled: true},
	MethodWorkflowCaptureInput:      {Owner: "engine", Enabled: true},
	MethodWorkflowCreateCheckpoint:  {Owner: "engine", Enabled: true},
	MethodWorkflowCreateVersion:     {Owner: "engine", Enabled: true},
	MethodWorkflowEvaluateGate:      {Owner: "engine", Enabled: true},
	MethodWorkflowPublish:           {Owner: "engine", Enabled: true},
	MethodWorkflowStartStage:        {Owner: "engine", Enabled: true},
	MethodWorkflowTransitionStage:   {Owner: "engine", Enabled: true},
	MethodWorkspaceConvert:          {Owner: "engine", Enabled: true},
	MethodWorkspaceGrant:            {Owner: "engine", Enabled: true},
	MethodWorkspaceLease:            {Owner: "engine", Enabled: true},
	MethodWorkspaceList:             {Owner: "host", Enabled: true},
	MethodWorkspaceRead:             {Owner: "host", Enabled: true},
	MethodWorkspaceRegister:         {Owner: "engine", Enabled: true},
	MethodWorkspaceRootGet:          {Owner: "host", Enabled: true},
	MethodWorkspaceRootSelect:       {Owner: "host", Enabled: true},
}
var Methods = [...]Method{MethodAgentRunCancel, MethodAgentRunGet, MethodAgentRunReconcile, MethodAgentRunResume, MethodAgentRunStart, MethodAttachmentDelete, MethodAttachmentGet, MethodAttachmentIngest, MethodAttachmentList, MethodAttachmentUploadAbort, MethodAttachmentUploadBegin, MethodAttachmentUploadChunk, MethodAttachmentUploadCommit, MethodBarrierArrive, MethodBrowserAct, MethodBrowserClose, MethodBrowserOpen, MethodCapabilityList, MethodChangesetApply, MethodChangesetPreview, MethodChangesetRevert, MethodChatStart, MethodChatToolApprove, MethodCommandCancel, MethodCommandGet, MethodCommandReviewRequest, MethodCommandStart, MethodComplexityDecide, MethodConnectorSnapshot, MethodContextCompactCancel, MethodContextCompactCommit, MethodContextCompactPreview, MethodContextHandoffCreate, MethodContextHandoffImport, MethodContextHandoffInspect, MethodContextHandoffList, MethodContextHandoffListImports, MethodContextHandoffRevoke, MethodContextStatus, MethodDelegationCreate, MethodDelegationSettle, MethodDevTaskCreate, MethodDevTaskTransition, MethodDiagnosticsExport, MethodEvidenceAttachScan, MethodEvidenceAttachTest, MethodEvidenceList, MethodExtensionInstall, MethodExtensionLifecycle, MethodExtensionSearch, MethodFsGlob, MethodFsGrep, MethodFsRead, MethodFsReadMany, MethodFsStat, MethodFsTree, MethodMcpInvoke, MethodMcp6Invoke, MethodMcp6Register, MethodMcp6Revoke, MethodMemoryCreate, MethodMemoryDelete, MethodMemoryGet, MethodMemoryList, MethodMemorySearch, MethodMemoryUpdate, MethodMergeSubmit, MethodMessageAppend, MethodMessageList, MethodMessageRewind, MethodMigrationInspect, MethodMigrationRun, MethodMigrationStatus, MethodNodeComplete, MethodNodeCreate, MethodNodeFail, MethodNodeList, MethodNodeStart, MethodOntologyEdgeCreate, MethodOntologyEdgeDelete, MethodOntologyEdgeList, MethodOntologyEdgeUpdate, MethodOntologyNodeCreate, MethodOntologyNodeDelete, MethodOntologyNodeGet, MethodOntologyNodeList, MethodOntologyNodeSearch, MethodOntologyNodeUpdate, MethodOpenapiParse, MethodPlanActivate, MethodPlanComplete, MethodPlanCreate, MethodPlanGet, MethodPlanList, MethodPlanPause, MethodPlanResume, MethodPlanRunCancel, MethodPlanRunJoin, MethodPlanRunSpawn, MethodPlanRunStart, MethodPlanRunTree, MethodPlanTodoCreate, MethodProjectCreate, MethodProjectDelete, MethodProjectList, MethodProviderCreate, MethodProviderCredentialReveal, MethodProviderCredentialSubmit, MethodProviderDelete, MethodProviderGet, MethodProviderList, MethodProviderModelSync, MethodProviderTest, MethodProviderUpdate, MethodReviewApprove, MethodReviewDecide, MethodReviewList, MethodReviewReject, MethodReviewSubmit, MethodRunCancel, MethodRunPlanPut, MethodRunSend, MethodSessionCreate, MethodSessionDelete, MethodSessionList, MethodSessionUpdate, MethodSkillCreate, MethodSkillDelete, MethodSkillDeprecate, MethodSkillDisable, MethodSkillExecute, MethodSkillGet, MethodSkillImportApprove, MethodSkillImportDiscover, MethodSkillImportInspect, MethodSkillImportReject, MethodSkillImportRevoke, MethodSkillImportSubmit, MethodSkillInvoke, MethodSkillList, MethodSkillMatch, MethodSkillPublish, MethodSkillUpdate, MethodStageCreate, MethodStageList, MethodStreamCancel, MethodSystemHealth, MethodSystemSettingsOpen, MethodTerminalClose, MethodTerminalInput, MethodTerminalResize, MethodTerminalStart, MethodTraceAddEdge, MethodTraceMarkStale, MethodTraceQuery, MethodTraceResolveStale, MethodUiThemeSet, MethodWebFetch, MethodWebSearch, MethodWorkerDispatch, MethodWorkflowCaptureInput, MethodWorkflowCreateCheckpoint, MethodWorkflowCreateVersion, MethodWorkflowEvaluateGate, MethodWorkflowPublish, MethodWorkflowStartStage, MethodWorkflowTransitionStage, MethodWorkspaceConvert, MethodWorkspaceGrant, MethodWorkspaceLease, MethodWorkspaceList, MethodWorkspaceRead, MethodWorkspaceRegister, MethodWorkspaceRootGet, MethodWorkspaceRootSelect}

func ValidMethod(method string) bool { _, ok := MethodMetadataByMethod[Method(method)]; return ok }
