// Code generated from discovered bridge schemas. DO NOT EDIT.
package bridge

const Version = "1.0"

type Method string

const (
	MethodChatStart                Method = "chat.start"
	MethodContextCompactCancel     Method = "context.compact.cancel"
	MethodContextCompactCommit     Method = "context.compact.commit"
	MethodContextCompactPreview    Method = "context.compact.preview"
	MethodContextStatus            Method = "context.status"
	MethodDiagnosticsExport        Method = "diagnostics.export"
	MethodMemoryCreate             Method = "memory.create"
	MethodMemoryDelete             Method = "memory.delete"
	MethodMemoryGet                Method = "memory.get"
	MethodMemoryList               Method = "memory.list"
	MethodMemorySearch             Method = "memory.search"
	MethodMemoryUpdate             Method = "memory.update"
	MethodMessageAppend            Method = "message.append"
	MethodMessageList              Method = "message.list"
	MethodMigrationInspect         Method = "migration.inspect"
	MethodMigrationRun             Method = "migration.run"
	MethodMigrationStatus          Method = "migration.status"
	MethodNodeComplete             Method = "node.complete"
	MethodNodeCreate               Method = "node.create"
	MethodNodeFail                 Method = "node.fail"
	MethodNodeList                 Method = "node.list"
	MethodNodeStart                Method = "node.start"
	MethodOntologyEdgeCreate       Method = "ontology.edge.create"
	MethodOntologyEdgeDelete       Method = "ontology.edge.delete"
	MethodOntologyEdgeList         Method = "ontology.edge.list"
	MethodOntologyEdgeUpdate       Method = "ontology.edge.update"
	MethodOntologyNodeCreate       Method = "ontology.node.create"
	MethodOntologyNodeDelete       Method = "ontology.node.delete"
	MethodOntologyNodeGet          Method = "ontology.node.get"
	MethodOntologyNodeList         Method = "ontology.node.list"
	MethodOntologyNodeSearch       Method = "ontology.node.search"
	MethodOntologyNodeUpdate       Method = "ontology.node.update"
	MethodPlanActivate             Method = "plan.activate"
	MethodPlanComplete             Method = "plan.complete"
	MethodPlanCreate               Method = "plan.create"
	MethodPlanGet                  Method = "plan.get"
	MethodPlanList                 Method = "plan.list"
	MethodPlanPause                Method = "plan.pause"
	MethodPlanResume               Method = "plan.resume"
	MethodProjectCreate            Method = "project.create"
	MethodProjectDelete            Method = "project.delete"
	MethodProjectList              Method = "project.list"
	MethodProviderCreate           Method = "provider.create"
	MethodProviderCredentialSubmit Method = "provider.credential.submit"
	MethodProviderDelete           Method = "provider.delete"
	MethodProviderGet              Method = "provider.get"
	MethodProviderList             Method = "provider.list"
	MethodProviderModelSync        Method = "provider.model.sync"
	MethodProviderTest             Method = "provider.test"
	MethodProviderUpdate           Method = "provider.update"
	MethodReviewApprove            Method = "review.approve"
	MethodReviewList               Method = "review.list"
	MethodReviewReject             Method = "review.reject"
	MethodSessionCreate            Method = "session.create"
	MethodSessionDelete            Method = "session.delete"
	MethodSessionList              Method = "session.list"
	MethodSkillCreate              Method = "skill.create"
	MethodSkillDelete              Method = "skill.delete"
	MethodSkillDeprecate           Method = "skill.deprecate"
	MethodSkillDisable             Method = "skill.disable"
	MethodSkillGet                 Method = "skill.get"
	MethodSkillList                Method = "skill.list"
	MethodSkillMatch               Method = "skill.match"
	MethodSkillPublish             Method = "skill.publish"
	MethodSkillUpdate              Method = "skill.update"
	MethodStageCreate              Method = "stage.create"
	MethodStageList                Method = "stage.list"
	MethodStreamCancel             Method = "stream.cancel"
	MethodSystemHealth             Method = "system.health"
)

type MethodMetadata struct {
	Owner   string
	Enabled bool
}

var MethodMetadataByMethod = map[Method]MethodMetadata{
	MethodChatStart:                {Owner: "engine", Enabled: true},
	MethodContextCompactCancel:     {Owner: "engine", Enabled: true},
	MethodContextCompactCommit:     {Owner: "engine", Enabled: true},
	MethodContextCompactPreview:    {Owner: "engine", Enabled: true},
	MethodContextStatus:            {Owner: "engine", Enabled: true},
	MethodDiagnosticsExport:        {Owner: "host", Enabled: true},
	MethodMemoryCreate:             {Owner: "engine", Enabled: true},
	MethodMemoryDelete:             {Owner: "engine", Enabled: true},
	MethodMemoryGet:                {Owner: "engine", Enabled: true},
	MethodMemoryList:               {Owner: "engine", Enabled: true},
	MethodMemorySearch:             {Owner: "engine", Enabled: true},
	MethodMemoryUpdate:             {Owner: "engine", Enabled: true},
	MethodMessageAppend:            {Owner: "engine", Enabled: true},
	MethodMessageList:              {Owner: "engine", Enabled: true},
	MethodMigrationInspect:         {Owner: "engine", Enabled: true},
	MethodMigrationRun:             {Owner: "engine", Enabled: true},
	MethodMigrationStatus:          {Owner: "engine", Enabled: true},
	MethodNodeComplete:             {Owner: "engine", Enabled: true},
	MethodNodeCreate:               {Owner: "engine", Enabled: true},
	MethodNodeFail:                 {Owner: "engine", Enabled: true},
	MethodNodeList:                 {Owner: "engine", Enabled: true},
	MethodNodeStart:                {Owner: "engine", Enabled: true},
	MethodOntologyEdgeCreate:       {Owner: "engine", Enabled: true},
	MethodOntologyEdgeDelete:       {Owner: "engine", Enabled: true},
	MethodOntologyEdgeList:         {Owner: "engine", Enabled: true},
	MethodOntologyEdgeUpdate:       {Owner: "engine", Enabled: true},
	MethodOntologyNodeCreate:       {Owner: "engine", Enabled: true},
	MethodOntologyNodeDelete:       {Owner: "engine", Enabled: true},
	MethodOntologyNodeGet:          {Owner: "engine", Enabled: true},
	MethodOntologyNodeList:         {Owner: "engine", Enabled: true},
	MethodOntologyNodeSearch:       {Owner: "engine", Enabled: true},
	MethodOntologyNodeUpdate:       {Owner: "engine", Enabled: true},
	MethodPlanActivate:             {Owner: "engine", Enabled: true},
	MethodPlanComplete:             {Owner: "engine", Enabled: true},
	MethodPlanCreate:               {Owner: "engine", Enabled: true},
	MethodPlanGet:                  {Owner: "engine", Enabled: true},
	MethodPlanList:                 {Owner: "engine", Enabled: true},
	MethodPlanPause:                {Owner: "engine", Enabled: true},
	MethodPlanResume:               {Owner: "engine", Enabled: true},
	MethodProjectCreate:            {Owner: "engine", Enabled: true},
	MethodProjectDelete:            {Owner: "engine", Enabled: true},
	MethodProjectList:              {Owner: "engine", Enabled: true},
	MethodProviderCreate:           {Owner: "engine", Enabled: true},
	MethodProviderCredentialSubmit: {Owner: "host", Enabled: true},
	MethodProviderDelete:           {Owner: "engine", Enabled: true},
	MethodProviderGet:              {Owner: "engine", Enabled: true},
	MethodProviderList:             {Owner: "engine", Enabled: true},
	MethodProviderModelSync:        {Owner: "engine", Enabled: true},
	MethodProviderTest:             {Owner: "engine", Enabled: true},
	MethodProviderUpdate:           {Owner: "engine", Enabled: true},
	MethodReviewApprove:            {Owner: "engine", Enabled: true},
	MethodReviewList:               {Owner: "engine", Enabled: true},
	MethodReviewReject:             {Owner: "engine", Enabled: true},
	MethodSessionCreate:            {Owner: "engine", Enabled: true},
	MethodSessionDelete:            {Owner: "engine", Enabled: true},
	MethodSessionList:              {Owner: "engine", Enabled: true},
	MethodSkillCreate:              {Owner: "engine", Enabled: true},
	MethodSkillDelete:              {Owner: "engine", Enabled: true},
	MethodSkillDeprecate:           {Owner: "engine", Enabled: true},
	MethodSkillDisable:             {Owner: "engine", Enabled: true},
	MethodSkillGet:                 {Owner: "engine", Enabled: true},
	MethodSkillList:                {Owner: "engine", Enabled: true},
	MethodSkillMatch:               {Owner: "engine", Enabled: true},
	MethodSkillPublish:             {Owner: "engine", Enabled: true},
	MethodSkillUpdate:              {Owner: "engine", Enabled: true},
	MethodStageCreate:              {Owner: "engine", Enabled: true},
	MethodStageList:                {Owner: "engine", Enabled: true},
	MethodStreamCancel:             {Owner: "engine", Enabled: true},
	MethodSystemHealth:             {Owner: "engine", Enabled: true},
}
var Methods = [...]Method{MethodChatStart, MethodContextCompactCancel, MethodContextCompactCommit, MethodContextCompactPreview, MethodContextStatus, MethodDiagnosticsExport, MethodMemoryCreate, MethodMemoryDelete, MethodMemoryGet, MethodMemoryList, MethodMemorySearch, MethodMemoryUpdate, MethodMessageAppend, MethodMessageList, MethodMigrationInspect, MethodMigrationRun, MethodMigrationStatus, MethodNodeComplete, MethodNodeCreate, MethodNodeFail, MethodNodeList, MethodNodeStart, MethodOntologyEdgeCreate, MethodOntologyEdgeDelete, MethodOntologyEdgeList, MethodOntologyEdgeUpdate, MethodOntologyNodeCreate, MethodOntologyNodeDelete, MethodOntologyNodeGet, MethodOntologyNodeList, MethodOntologyNodeSearch, MethodOntologyNodeUpdate, MethodPlanActivate, MethodPlanComplete, MethodPlanCreate, MethodPlanGet, MethodPlanList, MethodPlanPause, MethodPlanResume, MethodProjectCreate, MethodProjectDelete, MethodProjectList, MethodProviderCreate, MethodProviderCredentialSubmit, MethodProviderDelete, MethodProviderGet, MethodProviderList, MethodProviderModelSync, MethodProviderTest, MethodProviderUpdate, MethodReviewApprove, MethodReviewList, MethodReviewReject, MethodSessionCreate, MethodSessionDelete, MethodSessionList, MethodSkillCreate, MethodSkillDelete, MethodSkillDeprecate, MethodSkillDisable, MethodSkillGet, MethodSkillList, MethodSkillMatch, MethodSkillPublish, MethodSkillUpdate, MethodStageCreate, MethodStageList, MethodStreamCancel, MethodSystemHealth}

func ValidMethod(method string) bool { _, ok := MethodMetadataByMethod[Method(method)]; return ok }
