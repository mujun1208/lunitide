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
	MethodDiagnosticsExport         Method = "diagnostics.export"
	MethodEvidenceList              Method = "evidence.list"
	MethodFsGlob                    Method = "fs.glob"
	MethodFsGrep                    Method = "fs.grep"
	MethodFsRead                    Method = "fs.read"
	MethodFsReadMany                Method = "fs.readMany"
	MethodFsStat                    Method = "fs.stat"
	MethodFsTree                    Method = "fs.tree"
	MethodMemoryCreate              Method = "memory.create"
	MethodMemoryDelete              Method = "memory.delete"
	MethodMemoryGet                 Method = "memory.get"
	MethodMemoryList                Method = "memory.list"
	MethodMemorySearch              Method = "memory.search"
	MethodMemoryUpdate              Method = "memory.update"
	MethodMessageAppend             Method = "message.append"
	MethodMessageList               Method = "message.list"
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
	MethodRunPlanPut                Method = "run.plan.put"
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
	MethodSkillInvoke               Method = "skill.invoke"
	MethodSkillList                 Method = "skill.list"
	MethodSkillMatch                Method = "skill.match"
	MethodSkillPublish              Method = "skill.publish"
	MethodSkillUpdate               Method = "skill.update"
	MethodStageCreate               Method = "stage.create"
	MethodStageList                 Method = "stage.list"
	MethodStreamCancel              Method = "stream.cancel"
	MethodSystemHealth              Method = "system.health"
	MethodTerminalClose             Method = "terminal.close"
	MethodTerminalInput             Method = "terminal.input"
	MethodTerminalResize            Method = "terminal.resize"
	MethodTerminalStart             Method = "terminal.start"
	MethodUiThemeSet                Method = "ui.theme.set"
	MethodWebFetch                  Method = "web.fetch"
	MethodWebSearch                 Method = "web.search"
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
	MethodDiagnosticsExport:         {Owner: "host", Enabled: true},
	MethodEvidenceList:              {Owner: "engine", Enabled: true},
	MethodFsGlob:                    {Owner: "engine", Enabled: true},
	MethodFsGrep:                    {Owner: "engine", Enabled: true},
	MethodFsRead:                    {Owner: "engine", Enabled: true},
	MethodFsReadMany:                {Owner: "engine", Enabled: true},
	MethodFsStat:                    {Owner: "engine", Enabled: true},
	MethodFsTree:                    {Owner: "engine", Enabled: true},
	MethodMemoryCreate:              {Owner: "engine", Enabled: true},
	MethodMemoryDelete:              {Owner: "engine", Enabled: true},
	MethodMemoryGet:                 {Owner: "engine", Enabled: true},
	MethodMemoryList:                {Owner: "engine", Enabled: true},
	MethodMemorySearch:              {Owner: "engine", Enabled: true},
	MethodMemoryUpdate:              {Owner: "engine", Enabled: true},
	MethodMessageAppend:             {Owner: "engine", Enabled: true},
	MethodMessageList:               {Owner: "engine", Enabled: true},
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
	MethodRunPlanPut:                {Owner: "engine", Enabled: true},
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
	MethodSkillInvoke:               {Owner: "engine", Enabled: true},
	MethodSkillList:                 {Owner: "engine", Enabled: true},
	MethodSkillMatch:                {Owner: "engine", Enabled: true},
	MethodSkillPublish:              {Owner: "engine", Enabled: true},
	MethodSkillUpdate:               {Owner: "engine", Enabled: true},
	MethodStageCreate:               {Owner: "engine", Enabled: true},
	MethodStageList:                 {Owner: "engine", Enabled: true},
	MethodStreamCancel:              {Owner: "engine", Enabled: true},
	MethodSystemHealth:              {Owner: "engine", Enabled: true},
	MethodTerminalClose:             {Owner: "engine", Enabled: true},
	MethodTerminalInput:             {Owner: "engine", Enabled: true},
	MethodTerminalResize:            {Owner: "engine", Enabled: true},
	MethodTerminalStart:             {Owner: "engine", Enabled: true},
	MethodUiThemeSet:                {Owner: "host", Enabled: true},
	MethodWebFetch:                  {Owner: "engine", Enabled: true},
	MethodWebSearch:                 {Owner: "engine", Enabled: true},
	MethodWorkspaceGrant:            {Owner: "engine", Enabled: true},
	MethodWorkspaceLease:            {Owner: "engine", Enabled: true},
	MethodWorkspaceList:             {Owner: "host", Enabled: true},
	MethodWorkspaceRead:             {Owner: "host", Enabled: true},
	MethodWorkspaceRegister:         {Owner: "engine", Enabled: true},
	MethodWorkspaceRootGet:          {Owner: "host", Enabled: true},
	MethodWorkspaceRootSelect:       {Owner: "host", Enabled: true},
}
var Methods = [...]Method{MethodAgentRunCancel, MethodAgentRunGet, MethodAgentRunReconcile, MethodAgentRunResume, MethodAgentRunStart, MethodAttachmentDelete, MethodAttachmentGet, MethodAttachmentIngest, MethodAttachmentList, MethodAttachmentUploadAbort, MethodAttachmentUploadBegin, MethodAttachmentUploadChunk, MethodAttachmentUploadCommit, MethodBrowserClose, MethodBrowserOpen, MethodCapabilityList, MethodChangesetApply, MethodChangesetPreview, MethodChangesetRevert, MethodChatStart, MethodChatToolApprove, MethodCommandCancel, MethodCommandGet, MethodCommandReviewRequest, MethodCommandStart, MethodContextCompactCancel, MethodContextCompactCommit, MethodContextCompactPreview, MethodContextHandoffCreate, MethodContextHandoffImport, MethodContextHandoffInspect, MethodContextHandoffList, MethodContextHandoffListImports, MethodContextHandoffRevoke, MethodContextStatus, MethodDiagnosticsExport, MethodEvidenceList, MethodFsGlob, MethodFsGrep, MethodFsRead, MethodFsReadMany, MethodFsStat, MethodFsTree, MethodMemoryCreate, MethodMemoryDelete, MethodMemoryGet, MethodMemoryList, MethodMemorySearch, MethodMemoryUpdate, MethodMessageAppend, MethodMessageList, MethodMigrationInspect, MethodMigrationRun, MethodMigrationStatus, MethodNodeComplete, MethodNodeCreate, MethodNodeFail, MethodNodeList, MethodNodeStart, MethodOntologyEdgeCreate, MethodOntologyEdgeDelete, MethodOntologyEdgeList, MethodOntologyEdgeUpdate, MethodOntologyNodeCreate, MethodOntologyNodeDelete, MethodOntologyNodeGet, MethodOntologyNodeList, MethodOntologyNodeSearch, MethodOntologyNodeUpdate, MethodPlanActivate, MethodPlanComplete, MethodPlanCreate, MethodPlanGet, MethodPlanList, MethodPlanPause, MethodPlanResume, MethodPlanRunCancel, MethodPlanRunJoin, MethodPlanRunSpawn, MethodPlanRunStart, MethodPlanRunTree, MethodPlanTodoCreate, MethodProjectCreate, MethodProjectDelete, MethodProjectList, MethodProviderCreate, MethodProviderCredentialReveal, MethodProviderCredentialSubmit, MethodProviderDelete, MethodProviderGet, MethodProviderList, MethodProviderModelSync, MethodProviderTest, MethodProviderUpdate, MethodReviewApprove, MethodReviewDecide, MethodReviewList, MethodReviewReject, MethodRunPlanPut, MethodSessionCreate, MethodSessionDelete, MethodSessionList, MethodSessionUpdate, MethodSkillCreate, MethodSkillDelete, MethodSkillDeprecate, MethodSkillDisable, MethodSkillExecute, MethodSkillGet, MethodSkillInvoke, MethodSkillList, MethodSkillMatch, MethodSkillPublish, MethodSkillUpdate, MethodStageCreate, MethodStageList, MethodStreamCancel, MethodSystemHealth, MethodTerminalClose, MethodTerminalInput, MethodTerminalResize, MethodTerminalStart, MethodUiThemeSet, MethodWebFetch, MethodWebSearch, MethodWorkspaceGrant, MethodWorkspaceLease, MethodWorkspaceList, MethodWorkspaceRead, MethodWorkspaceRegister, MethodWorkspaceRootGet, MethodWorkspaceRootSelect}

func ValidMethod(method string) bool { _, ok := MethodMetadataByMethod[Method(method)]; return ok }
