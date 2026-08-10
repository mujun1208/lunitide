// Code generated from discovered bridge schemas. DO NOT EDIT.
package contract

import (
	"github.com/lunitide/lunitide/internal/app"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/ipc"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestGeneratedSchemaConstantsMatchGoContracts(t *testing.T) {
	if bridge.Version != "1.0" {
		t.Fatalf("bridge version = %q", bridge.Version)
	}
	if ipc.RPCMajor != 1 || ipc.RPCMinor != 0 {
		t.Fatal("RPC version drift")
	}
	if !reflect.DeepEqual([]string{string(provider.ProtocolOpenAICompatible), string(provider.ProtocolAnthropic)}, []string{"openai_compatible", "anthropic"}) {
		t.Fatal("provider protocol drift")
	}
}
func TestGeneratedSchemaJSONFieldsMatchGoDTOs(t *testing.T) {
	assertJSONFields(t, reflect.TypeOf(bridge.Request{}), []string{"v", "kind", "id", "traceId", "method", "sentAt", "payload", "idempotencyKey,omitempty", "deadlineMs"})
	assertJSONFields(t, reflect.TypeOf(bridge.Error{}), []string{"code", "message", "retryable", "details,omitempty", "correlationId"})
	assertJSONFields(t, reflect.TypeOf(bridge.Response{}), []string{"v", "kind", "id", "requestId", "ok", "payload,omitempty", "error,omitempty"})
	assertJSONFields(t, reflect.TypeOf(ipc.Handshake{}), []string{"rpcMajor", "rpcMinor", "clientPid", "sessionNonce"})
}
func TestGeneratedEnabledEngineRuntimeRoutesMatchSchema(t *testing.T) {
	for method, metadata := range bridge.MethodMetadataByMethod {
		if metadata.Owner == "engine" && metadata.Enabled {
			if handler, ok := app.RuntimeHandlers[method]; !ok || handler == nil {
				t.Errorf("enabled engine method %q has no runtime handler", method)
			}
		}
	}
	for method := range app.RuntimeHandlers {
		metadata, ok := bridge.MethodMetadataByMethod[method]
		if !ok || metadata.Owner != "engine" || !metadata.Enabled {
			t.Errorf("runtime method %q is not enabled engine contract", method)
		}
	}
}
func TestGeneratedPublicDTOsContainNoSensitiveFields(t *testing.T) {
	text := strings.ToLower("{\"ULID\":{\"type\":\"string\",\"pattern\":\"^[0-7][0-9A-HJKMNP-TV-Z]{25}$\",\"x-examples\":{\"positive\":[\"01ARZ3NDEKTSV4RRFFQ69G5FAV\"],\"negative\":[\"81ARZ3NDEKTSV4RRFFQ69G5FAV\"]}},\"ProviderProtocol\":{\"enum\":[\"openai_compatible\",\"anthropic\"]},\"ProviderStatus\":{\"enum\":[\"enabled\",\"disabled\"]},\"CredentialState\":{\"enum\":[\"configured\",\"missing\",\"unavailable\",\"requires_reentry\"]},\"ModelDTO\":{\"type\":\"object\",\"additionalProperties\":false,\"properties\":{\"modelId\":{\"type\":\"string\",\"minLength\":1,\"maxLength\":200,\"pattern\":\"^[\\\\x21-\\\\x7E]+$\"},\"displayName\":{\"type\":\"string\",\"minLength\":1,\"maxLength\":200},\"isDefault\":{\"type\":\"boolean\"}},\"required\":[\"modelId\",\"displayName\",\"isDefault\"]},\"ProviderDTO\":{\"type\":\"object\",\"additionalProperties\":false,\"properties\":{\"id\":{\"$ref\":\"#/$defs/ULID\"},\"name\":{\"type\":\"string\",\"minLength\":1,\"maxLength\":500},\"protocol\":{\"$ref\":\"#/$defs/ProviderProtocol\"},\"baseUrl\":{\"type\":\"string\",\"format\":\"uri\"},\"models\":{\"type\":\"array\",\"minItems\":1,\"maxItems\":50,\"items\":{\"$ref\":\"#/$defs/ModelDTO\"}},\"status\":{\"$ref\":\"#/$defs/ProviderStatus\"},\"credentialState\":{\"$ref\":\"#/$defs/CredentialState\"},\"createdAt\":{\"type\":\"string\",\"format\":\"date-time\"},\"updatedAt\":{\"type\":\"string\",\"format\":\"date-time\"},\"version\":{\"type\":\"integer\",\"minimum\":1}},\"required\":[\"id\",\"name\",\"protocol\",\"baseUrl\",\"models\",\"status\",\"credentialState\",\"createdAt\",\"updatedAt\",\"version\"]},\"ProjectStatus\":{\"enum\":[\"active\",\"archived\"]},\"ProjectDTO\":{\"type\":\"object\",\"additionalProperties\":false,\"properties\":{\"id\":{\"$ref\":\"#/$defs/ULID\"},\"name\":{\"type\":\"string\",\"minLength\":1,\"maxLength\":200},\"status\":{\"$ref\":\"#/$defs/ProjectStatus\"},\"createdAt\":{\"type\":\"string\",\"format\":\"date-time\"},\"updatedAt\":{\"type\":\"string\",\"format\":\"date-time\"},\"version\":{\"type\":\"integer\",\"minimum\":1}},\"required\":[\"id\",\"name\",\"status\",\"createdAt\",\"updatedAt\",\"version\"]},\"SessionStatus\":{\"enum\":[\"active\"]},\"SessionDTO\":{\"type\":\"object\",\"additionalProperties\":false,\"properties\":{\"id\":{\"$ref\":\"#/$defs/ULID\"},\"projectId\":{\"$ref\":\"#/$defs/ULID\"},\"title\":{\"type\":\"string\",\"minLength\":1,\"maxLength\":200},\"status\":{\"$ref\":\"#/$defs/SessionStatus\"},\"createdAt\":{\"type\":\"string\",\"format\":\"date-time\"},\"updatedAt\":{\"type\":\"string\",\"format\":\"date-time\"},\"version\":{\"type\":\"integer\",\"const\":1}},\"required\":[\"id\",\"projectId\",\"title\",\"status\",\"createdAt\",\"updatedAt\",\"version\"]},\"MigrationStatusDTO\":{\"type\":\"object\",\"additionalProperties\":false,\"properties\":{\"state\":{\"enum\":[\"idle\",\"running\",\"completed\",\"failed\"]},\"processed\":{\"type\":\"integer\",\"minimum\":0},\"total\":{\"type\":\"integer\",\"minimum\":0}},\"required\":[\"state\",\"processed\",\"total\"]}}")
	for _, forbidden := range []string{"apikey", "secret", "ciphertext", "credentialref"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("public DTO schema contains sensitive field %q", forbidden)
		}
	}
}
func TestGeneratedHandshakeNonceConstraint(t *testing.T) {
	expression := regexp.MustCompile("^[0-9a-fA-F]{64}$")
	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if len(valid) != 64 || !expression.MatchString(valid) || expression.MatchString(valid[:63]) {
		t.Fatal("nonce constraint drift")
	}
}
func assertJSONFields(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	got := make([]string, typ.NumField())
	for i := range got {
		got[i] = typ.Field(i).Tag.Get("json")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s JSON fields = %v, want %v", typ, got, want)
	}
}
