package openapi

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// The acceptance matrix (design 04 §05 "Legacy S5 Verification") requires:
// 5 MB/5 MB+1, ref 10/11, 500/501 paths; 循环 ref、外部 ref SSRF、callback
// URL、压缩炸弹 — 边界内稳定解析，+1 与攻击输入返回 OAS 精确码且零网络
// 副作用。

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *openapi.Error, got %v", err)
	}
	return e.Code
}

const happySpec = `{
  "openapi": "3.0.3",
  "info": {"title": "Pets", "version": "1.2.0", "description": "demo"},
  "servers": [{"url": "https://api.example.com/v1", "description": "prod"}],
  "security": [{"bearer": []}],
  "paths": {
    "/pets": {
      "parameters": [{"name": "trace", "in": "query", "schema": {"type": "string"}}],
      "get": {
        "operationId": "listPets",
        "summary": "list",
        "parameters": [{"name": "limit", "in": "query", "required": true, "schema": {"type": "integer"}}],
        "responses": {
          "200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Pets"}}}},
          "default": {"description": "err"}
        }
      },
      "post": {
        "operationId": "createPet",
        "requestBody": {"required": true, "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Pet"}}}},
        "responses": {"201": {"description": "created"}},
        "security": [{"apiKey": []}]
      }
    },
    "/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}},
        "callbacks": {
          "onEvent": {"https://callback.example.com/hook": {"post": {"operationId": "cbOp", "responses": {"200": {"description": "ok"}}}}}
        }
      }
    }
  },
  "components": {
    "schemas": {
      "Pet": {"type": "object", "properties": {"id": {"type": "string"}, "name": {"type": "string"}}},
      "Pets": {"type": "array", "items": {"$ref": "#/components/schemas/Pet"}}
    },
    "securitySchemes": {
      "bearer": {"type": "http", "scheme": "bearer"},
      "apiKey": {"type": "apiKey", "in": "header", "name": "X-API-Key"}
    }
  }
}`

func TestParseHappyPath(t *testing.T) {
	spec, err := Parse([]byte(happySpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.OpenAPI != "3.0.3" || spec.SpecVersion != "1.2.0" || spec.Title != "Pets" {
		t.Fatalf("metadata: %+v", spec)
	}
	if len(spec.Servers) != 1 || spec.Servers[0].URL != "https://api.example.com/v1" {
		t.Fatalf("servers: %+v", spec.Servers)
	}
	if got := spec.AuthTypes["bearer"]; got != AuthBearerToken {
		t.Fatalf("bearer auth: %v", got)
	}
	if got := spec.AuthTypes["apiKey"]; got != AuthAPIKeyHeader {
		t.Fatalf("apiKey auth: %v", got)
	}
	if len(spec.Operations) != 3 {
		t.Fatalf("operations: %d", len(spec.Operations))
	}
	byID := map[string]Operation{}
	for _, op := range spec.Operations {
		byID[op.ID] = op
	}
	list := byID["listPets"]
	if list.Method != "GET" || list.Path != "/pets" {
		t.Fatalf("listPets: %+v", list)
	}
	// Path-level parameter merges ahead of operation-level ones.
	if len(list.Parameters) != 2 || list.Parameters[0].Name != "trace" || list.Parameters[1].Name != "limit" {
		t.Fatalf("listPets params: %+v", list.Parameters)
	}
	if list.Auth[0] != AuthBearerToken {
		t.Fatalf("listPets inherited auth: %v", list.Auth)
	}
	create := byID["createPet"]
	if create.Auth[0] != AuthAPIKeyHeader {
		t.Fatalf("createPet override auth: %v", create.Auth)
	}
	if create.RequestBody == nil || create.RequestBody.Content["application/json"] == nil {
		t.Fatalf("createPet body: %+v", create.RequestBody)
	}
	// $ref inside components resolved into the response content.
	if list.Responses[0].Status != "200" || !bytes.Contains(list.Responses[0].Content["application/json"], []byte(`"type":"object"`)) {
		t.Fatalf("listPets responses: %+v", list.Responses)
	}
	if len(spec.Schemas) != 2 {
		t.Fatalf("schemas: %d", len(spec.Schemas))
	}
	if len(spec.Digest) != 64 {
		t.Fatalf("digest: %q", spec.Digest)
	}
}

func TestAnonymousOverride(t *testing.T) {
	// Replace the GLOBAL security (first occurrence, right after servers)
	// with an anonymous default; the post operation keeps its apiKey
	// override and must NOT become anonymous.
	doc := strings.Replace(happySpec, `"security": [{"bearer": []}],`, `"security": [],`, 1)
	spec, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, op := range spec.Operations {
		switch op.ID {
		case "createPet":
			if len(op.Auth) != 1 || op.Auth[0] != AuthAPIKeyHeader {
				t.Fatalf("createPet override auth: %v", op.Auth)
			}
		default:
			if len(op.Auth) != 1 || op.Auth[0] != AuthNone {
				t.Fatalf("op %s auth: %v", op.ID, op.Auth)
			}
		}
	}
}

func TestRawSizeBoundary(t *testing.T) {
	base := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{}}`
	exact := append([]byte(base), bytes.Repeat([]byte(" "), MaxBytes-len(base))...)
	if len(exact) != MaxBytes {
		t.Fatalf("padding math: %d", len(exact))
	}
	if _, err := Parse(exact); err != nil {
		t.Fatalf("exact 5MB must parse: %v", err)
	}
	over := append(append([]byte{}, exact...), ' ')
	if _, err := Parse(over); err == nil || codeOf(t, err) != CodeSize {
		t.Fatalf("5MB+1 must be M6-OAS-001, got %v", err)
	}
}

func TestUniquePathsBoundary(t *testing.T) {
	build := func(n int) string {
		var b strings.Builder
		b.WriteString(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{`)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"/p%04d":{"get":{"operationId":"op%04d","responses":{"200":{"description":"ok"}}}}`, i, i)
		}
		b.WriteString(`}}`)
		return b.String()
	}
	if _, err := Parse([]byte(build(MaxPaths))); err != nil {
		t.Fatalf("500 paths must parse: %v", err)
	}
	_, err := Parse([]byte(build(MaxPaths + 1)))
	if err == nil || codeOf(t, err) != CodePaths {
		t.Fatalf("501 paths must be M6-OAS-003, got %v", err)
	}
}

// refChain builds a document whose parameter schema chases exactly n
// $ref nodes (schema → C1 → … → C(n-1), with Cn concrete) before reaching
// the concrete schema.
func refChain(n int) string {
	var b strings.Builder
	b.WriteString(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","parameters":[{"name":"p","in":"query","schema":{"$ref":"#/components/schemas/C1"}}],"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{`)
	for i := 1; i < n; i++ {
		if i > 1 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"C%d":{"$ref":"#/components/schemas/C%d"}`, i, i+1)
	}
	if n > 1 {
		b.WriteByte(',')
	}
	fmt.Fprintf(&b, `"C%d":{"type":"object"}}}}}`, n)
	return b.String()
}

func TestRefDepthBoundary(t *testing.T) {
	if _, err := Parse([]byte(refChain(MaxRefDepth))); err != nil {
		t.Fatalf("chain of 10 refs must parse: %v", err)
	}
	_, err := Parse([]byte(refChain(MaxRefDepth + 1)))
	if err == nil || codeOf(t, err) != CodeRef {
		t.Fatalf("chain of 11 refs must be M6-OAS-002, got %v", err)
	}
}

func TestRefCycle(t *testing.T) {
	doc := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/A"}}}}}}}},"components":{"schemas":{"A":{"$ref":"#/components/schemas/B"},"B":{"$ref":"#/components/schemas/A"}}}}`
	_, err := Parse([]byte(doc))
	if err == nil || codeOf(t, err) != CodeRef {
		t.Fatalf("cycle must be M6-OAS-002, got %v", err)
	}
}

func TestExternalRefRefused(t *testing.T) {
	for _, ref := range []string{
		"https://evil.example.com/schemas.yaml#/Pet",
		"petstore.json#/Pet",
		"#components/schemas/Pet", // malformed pointer (no slash)
	} {
		doc := fmt.Sprintf(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"%s"}}}}}}}}},"components":{"schemas":{}}}`, ref)
		_, err := Parse([]byte(doc))
		if err == nil || codeOf(t, err) != CodeRef {
			t.Fatalf("external ref %q must be M6-OAS-002, got %v", ref, err)
		}
	}
}

func TestRefMissingTarget(t *testing.T) {
	doc := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok","content":{"application/json":{"schema":{"$ref":"#/components/schemas/Nope"}}}}}}}},"components":{"schemas":{}}}`
	_, err := Parse([]byte(doc))
	if err == nil || codeOf(t, err) != CodeRef {
		t.Fatalf("missing target must be M6-OAS-002, got %v", err)
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zlibBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func flateBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, _ := flate.NewWriter(&buf, 1)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCompressionBomb(t *testing.T) {
	base := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{}}`
	// Exactly MaxBytes decompresses fine; MaxBytes+1 is a bomb.
	ok := append([]byte(base), bytes.Repeat([]byte(" "), MaxBytes-len(base))...)
	if len(ok) != MaxBytes {
		t.Fatalf("padding math: %d", len(ok))
	}
	spec, err := ParseEncoded(gzipBytes(t, ok), "gzip")
	if err != nil {
		t.Fatalf("5MB decompressed must parse: %v", err)
	}
	if spec.SpecVersion != "1" {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	bomb := append([]byte(base), bytes.Repeat([]byte(" "), MaxBytes-len(base)+1)...)
	if _, err := ParseEncoded(gzipBytes(t, bomb), "gzip"); err == nil || codeOf(t, err) != CodeSize {
		t.Fatalf("gzip bomb must be M6-OAS-001, got %v", err)
	}
	// Magic sniffing catches a bomb even when the label says identity.
	if _, err := Parse(gzipBytes(t, bomb)); err == nil || codeOf(t, err) != CodeSize {
		t.Fatalf("sniffed bomb must be M6-OAS-001, got %v", err)
	}
	// deflate: zlib-wrapped and raw both decode.
	if _, err := ParseEncoded(zlibBytes(t, []byte(base)), "deflate"); err != nil {
		t.Fatalf("zlib deflate must parse: %v", err)
	}
	if _, err := ParseEncoded(flateBytes(t, []byte(base)), "deflate"); err != nil {
		t.Fatalf("raw deflate must parse: %v", err)
	}
	// Corrupt gzip is a conformance failure, not a catalog code.
	if _, err := ParseEncoded([]byte{0x1f, 0x8b, 0x00, 'g', 'a', 'r', 'b'}, "gzip"); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("corrupt gzip must be ErrInvalidSpec, got %v", err)
	}
	// Unknown encoding label refused.
	if _, err := ParseEncoded([]byte(base), "br"); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("br must be ErrInvalidSpec, got %v", err)
	}
}

func TestExpansionBomb(t *testing.T) {
	// A ~140KB schema referenced by 100 parameters: raw document stays
	// well under 5MB but the expansion is ~14MB — refused mid-resolution.
	var blob strings.Builder
	blob.WriteString(`{"type":"object","properties":{`)
	for i := 0; i < 1500; i++ {
		if i > 0 {
			blob.WriteByte(',')
		}
		fmt.Fprintf(&blob, `"p%04d":{"type":"string","description":"%s"}`, i, strings.Repeat("x", 60))
	}
	blob.WriteString(`}}`)
	var b strings.Builder
	b.WriteString(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","parameters":[`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"name":"q%03d","in":"query","schema":{"$ref":"#/components/schemas/Blob"}}`, i)
	}
	b.WriteString(`],"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{"Blob":`)
	b.WriteString(blob.String())
	b.WriteString(`}}}`)
	raw := []byte(b.String())
	if len(raw) >= MaxBytes {
		t.Fatalf("raw doc unexpectedly large: %d", len(raw))
	}
	_, err := Parse(raw)
	if err == nil || codeOf(t, err) != CodeSize {
		t.Fatalf("expansion bomb must be M6-OAS-001, got %v", err)
	}
}

func TestAuthEnum(t *testing.T) {
	supported := []struct {
		scheme string
		body   string
		want   AuthType
	}{
		{"hdr", `{"type":"apiKey","in":"header","name":"X-Key"}`, AuthAPIKeyHeader},
		{"qry", `{"type":"apiKey","in":"query","name":"k"}`, AuthAPIKeyQuery},
		{"b", `{"type":"http","scheme":"bearer"}`, AuthBearerToken},
		{"ba", `{"type":"http","scheme":"basic"}`, AuthBasic},
		{"o", `{"type":"oauth2","flows":{"clientCredentials":{"tokenUrl":"https://x","scopes":{"s":"d"}}}}`, AuthOAuth2ClientCredential},
	}
	var schemes strings.Builder
	for i, tc := range supported {
		if i > 0 {
			schemes.WriteByte(',')
		}
		fmt.Fprintf(&schemes, `"%s":%s`, tc.scheme, tc.body)
	}
	doc := fmt.Sprintf(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{},"components":{"securitySchemes":{%s}}}`, schemes.String())
	spec, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("supported schemes must parse: %v", err)
	}
	for _, tc := range supported {
		if spec.AuthTypes[tc.scheme] != tc.want {
			t.Fatalf("scheme %s: got %s want %s", tc.scheme, spec.AuthTypes[tc.scheme], tc.want)
		}
	}
	for _, bad := range []string{
		`{"type":"apiKey","in":"cookie","name":"c"}`,
		`{"type":"http","scheme":"digest"}`,
		`{"type":"openIdConnect","openIdConnectUrl":"https://x"}`,
		`{"type":"oauth2","flows":{"authorizationCode":{"authorizationUrl":"https://x","tokenUrl":"https://x","scopes":{"s":"d"}}}}`,
		`{"type":"mutualTLS"}`,
	} {
		doc := fmt.Sprintf(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{},"components":{"securitySchemes":{"bad":%s}}}`, bad)
		_, err := Parse([]byte(doc))
		if !errors.Is(err, ErrAuthUnsupported) {
			t.Fatalf("scheme %s must be ErrAuthUnsupported, got %v", bad, err)
		}
	}
}

func TestNoSchemesMeansNone(t *testing.T) {
	doc := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok"}}}}}}`
	spec, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if spec.Operations[0].Auth[0] != AuthNone {
		t.Fatalf("auth: %v", spec.Operations[0].Auth)
	}
}

func TestUndeclaredSecurityScheme(t *testing.T) {
	doc := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"security":[{"ghost":[]}],"paths":{"/a":{"get":{"operationId":"a","responses":{"200":{"description":"ok"}}}}},"components":{"securitySchemes":{}}}`
	if _, err := Parse([]byte(doc)); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("undeclared scheme must be ErrInvalidSpec, got %v", err)
	}
}

func TestOperationConformance(t *testing.T) {
	cases := map[string]string{
		"missing operationId":   `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"responses":{"200":{"description":"ok"}}}}}}`,
		"duplicate operationId": `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"dup","responses":{"200":{"description":"ok"}}}},"/b":{"get":{"operationId":"dup","responses":{"200":{"description":"ok"}}}}}}`,
		"bad path":              `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"a":{"get":{"operationId":"a","responses":{"200":{"description":"ok"}}}}}}`,
		"bad parameter":         `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","parameters":[{"name":"p","in":"body"}],"responses":{"200":{"description":"ok"}}}}}}`,
		"missing info":          `{"openapi":"3.0.3","paths":{}}`,
		"bad version":           `{"openapi":"2.0","info":{"title":"t","version":"1"},"paths":{}}`,
		"empty version":         `{"openapi":"3.0.3","info":{"title":"t","version":""},"paths":{}}`,
		"trailing junk":         `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{}} {"x":1}`,
		"array root":            `[1,2,3]`,
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); !errors.Is(err, ErrInvalidSpec) {
			t.Fatalf("%s must be ErrInvalidSpec, got %v", name, err)
		}
	}
	if _, err := Parse(nil); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("empty input must be ErrInvalidSpec, got %v", err)
	}
}

func TestTraceMethodIgnored(t *testing.T) {
	// TRACE is outside the seven-method consumer enum; the operation is
	// skipped rather than imported.
	doc := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"trace":{"operationId":"tr","responses":{"200":{"description":"ok"}}},"get":{"operationId":"ok","responses":{"200":{"description":"ok"}}}}}}`
	spec, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(spec.Operations) != 1 || spec.Operations[0].ID != "ok" {
		t.Fatalf("operations: %+v", spec.Operations)
	}
}

func TestCallbackURLsIgnored(t *testing.T) {
	// callback/webhook/link/example URLs must never trigger network and
	// never reach the normalized model; resolving them for the digest is
	// fine. Zero network is by construction: this package imports no net
	// machinery at all.
	spec, err := Parse([]byte(happySpec))
	if err != nil {
		t.Fatalf("parse with callbacks: %v", err)
	}
	for _, op := range spec.Operations {
		if op.ID == "cbOp" {
			t.Fatalf("callback operation leaked into the model")
		}
	}
	if !bytes.Contains(spec.Canonical, []byte("callback.example.com")) {
		t.Fatalf("canonical should still cover the resolved document for digest stability")
	}
}

func TestDigestDeterminism(t *testing.T) {
	a, err := Parse([]byte(happySpec))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse([]byte(happySpec))
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("digest churn across parses: %s vs %s", a.Digest, b.Digest)
	}
	// Same document with reordered keys must keep the digest stable.
	reordered := `{"info":{"version":"1.2.0","title":"Pets","description":"demo"},"openapi":"3.0.3","paths":{"/pets":{"get":{"responses":{"200":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pets"}}},"description":"ok"},"default":{"description":"err"}},"parameters":[{"in":"query","required":true,"schema":{"type":"integer"},"name":"limit"}],"operationId":"listPets","summary":"list"},"post":{"responses":{"201":{"description":"created"}},"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/Pet"}}},"required":true},"operationId":"createPet","security":[{"apiKey":[]}]},"parameters":[{"in":"query","schema":{"type":"string"},"name":"trace"}]},"/pets/{id}":{"get":{"callbacks":{"onEvent":{"https://callback.example.com/hook":{"post":{"operationId":"cbOp","responses":{"200":{"description":"ok"}}}}}},"operationId":"getPet","parameters":[{"in":"path","name":"id","required":true,"schema":{"type":"string"}}],"responses":{"200":{"description":"ok"}}}}},"components":{"securitySchemes":{"apiKey":{"in":"header","name":"X-API-Key","type":"apiKey"},"bearer":{"scheme":"bearer","type":"http"}},"schemas":{"Pets":{"items":{"$ref":"#/components/schemas/Pet"},"type":"array"},"Pet":{"properties":{"id":{"type":"string"},"name":{"type":"string"}},"type":"object"}}},"servers":[{"description":"prod","url":"https://api.example.com/v1"}],"security":[{"bearer":[]}]}`
	c, err := Parse([]byte(reordered))
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != c.Digest {
		t.Fatalf("digest must be key-order independent: %s vs %s", a.Digest, c.Digest)
	}
}

func TestNumberFidelityInDigest(t *testing.T) {
	// UseNumber: 10000000000 must not round-trip as 1e+10.
	doc := `{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"operationId":"a","parameters":[{"name":"p","in":"query","schema":{"type":"integer","maximum":10000000000}}],"responses":{"200":{"description":"ok"}}}}}}`
	spec, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(spec.Canonical, []byte("10000000000")) {
		t.Fatalf("number literal lost in canonical form: %s", spec.Canonical)
	}
}
