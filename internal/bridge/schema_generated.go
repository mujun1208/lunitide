// Code generated from discovered bridge schemas. DO NOT EDIT.
package bridge

const Version = "1.0"

type Method string

const (
	MethodChatStart                Method = "chat.start"
	MethodDiagnosticsExport        Method = "diagnostics.export"
	MethodMessageAppend            Method = "message.append"
	MethodMessageList              Method = "message.list"
	MethodMigrationInspect         Method = "migration.inspect"
	MethodMigrationRun             Method = "migration.run"
	MethodMigrationStatus          Method = "migration.status"
	MethodProjectCreate            Method = "project.create"
	MethodProjectList              Method = "project.list"
	MethodProviderCreate           Method = "provider.create"
	MethodProviderCredentialSubmit Method = "provider.credential.submit"
	MethodProviderDelete           Method = "provider.delete"
	MethodProviderGet              Method = "provider.get"
	MethodProviderList             Method = "provider.list"
	MethodProviderModelSync        Method = "provider.model.sync"
	MethodProviderTest             Method = "provider.test"
	MethodProviderUpdate           Method = "provider.update"
	MethodSessionCreate            Method = "session.create"
	MethodSessionList              Method = "session.list"
	MethodStreamCancel             Method = "stream.cancel"
	MethodSystemHealth             Method = "system.health"
)

type MethodMetadata struct {
	Owner   string
	Enabled bool
}

var MethodMetadataByMethod = map[Method]MethodMetadata{
	MethodChatStart:                {Owner: "engine", Enabled: true},
	MethodDiagnosticsExport:        {Owner: "host", Enabled: false},
	MethodMessageAppend:            {Owner: "engine", Enabled: true},
	MethodMessageList:              {Owner: "engine", Enabled: true},
	MethodMigrationInspect:         {Owner: "engine", Enabled: false},
	MethodMigrationRun:             {Owner: "engine", Enabled: false},
	MethodMigrationStatus:          {Owner: "engine", Enabled: false},
	MethodProjectCreate:            {Owner: "engine", Enabled: true},
	MethodProjectList:              {Owner: "engine", Enabled: true},
	MethodProviderCreate:           {Owner: "engine", Enabled: true},
	MethodProviderCredentialSubmit: {Owner: "host", Enabled: true},
	MethodProviderDelete:           {Owner: "engine", Enabled: true},
	MethodProviderGet:              {Owner: "engine", Enabled: true},
	MethodProviderList:             {Owner: "engine", Enabled: true},
	MethodProviderModelSync:        {Owner: "engine", Enabled: true},
	MethodProviderTest:             {Owner: "engine", Enabled: true},
	MethodProviderUpdate:           {Owner: "engine", Enabled: true},
	MethodSessionCreate:            {Owner: "engine", Enabled: true},
	MethodSessionList:              {Owner: "engine", Enabled: true},
	MethodStreamCancel:             {Owner: "engine", Enabled: true},
	MethodSystemHealth:             {Owner: "engine", Enabled: true},
}
var Methods = [...]Method{MethodChatStart, MethodDiagnosticsExport, MethodMessageAppend, MethodMessageList, MethodMigrationInspect, MethodMigrationRun, MethodMigrationStatus, MethodProjectCreate, MethodProjectList, MethodProviderCreate, MethodProviderCredentialSubmit, MethodProviderDelete, MethodProviderGet, MethodProviderList, MethodProviderModelSync, MethodProviderTest, MethodProviderUpdate, MethodSessionCreate, MethodSessionList, MethodStreamCancel, MethodSystemHealth}

func ValidMethod(method string) bool { _, ok := MethodMetadataByMethod[Method(method)]; return ok }
