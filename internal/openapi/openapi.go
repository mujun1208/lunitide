// Package openapi implements the M6 legacy-S5 "OpenAPI 3.0 安全 consumer
// 子集" parser (design 02 §06 Legacy S5 Contract): a pure, offline,
// size-bounded decoder for exactly the fields the Integration governance
// model consumes — servers, paths, operationId, parameters, requestBody,
// responses, components.schemas and securitySchemes.
//
// Catalog-coded failures map 1:1 onto M6_ERROR_CATALOG_V2:
//
//	M6-OAS-001 raw or decompressed input >5 MB, decompression bomb, or a
//	           resolved document whose expansion exceeds 5 MB
//	M6-OAS-002 $ref chain deeper than 10 hops, reference cycle, or an
//	           external / out-of-bounds $ref target
//	M6-OAS-003 more than 500 unique paths
//
// Any other conformance failure (malformed JSON, missing operationId,
// duplicate operationId, unsupported HTTP method, security requirement
// naming an undeclared scheme, missing info.version …) is reported as the
// ErrInvalidSpec sentinel; security schemes outside the six-value consumer
// auth enum are reported as ErrAuthUnsupported. Handlers map both onto the
// generic 422 family (BRIDGE_SCHEMA_INVALID) — the OAS codes stay precise
// to their catalog semantics.
//
// Zero network by construction: the parser never dials. External $ref
// targets are refused with M6-OAS-002 because this offline parser has no
// allowlist/SSRF re-check channel; callbacks, links, webhooks and examples
// are resolved (for digest stability) but never followed, and none of
// their URLs are retained in the normalized output.
package openapi

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Wire error codes (M6_ERROR_CATALOG_V2).
const (
	CodeSize  = "M6-OAS-001"
	CodeRef   = "M6-OAS-002"
	CodePaths = "M6-OAS-003"
)

// Frozen consumer-subset limits (design 02 §06 and 04 §05): raw and
// decompressed input ≤5 MB measured before AND after parsing, $ref
// expansion depth ≤10 with cycle detection, unique paths ≤500.
const (
	MaxBytes    = 5 << 20 // 5 MB
	MaxRefDepth = 10
	MaxPaths    = 500
)

var (
	// ErrInvalidSpec marks documents that do not conform to the consumer
	// subset for reasons outside the OAS-001/002/003 catalog codes.
	ErrInvalidSpec = errors.New("openapi: document does not conform to the consumer subset")
	// ErrAuthUnsupported marks securitySchemes outside the six-value
	// consumer auth enum; no CredentialRef may be created for them.
	ErrAuthUnsupported = errors.New("openapi: security scheme outside the supported auth enum")
)

// Error is a catalog-coded parse failure. Code is one of the M6-OAS-00x
// constants above.
type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("openapi: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("openapi: %s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func oasErr(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// AuthType is the normalized six-value consumer auth enum frozen by the
// design: none | apiKeyHeader | apiKeyQuery | bearerToken | basic |
// oauth2ClientCredentials. Everything but none requires a CredentialRef;
// the parser itself never sees or resolves secrets.
type AuthType string

const (
	AuthNone                   AuthType = "none"
	AuthAPIKeyHeader           AuthType = "apiKeyHeader"
	AuthAPIKeyQuery            AuthType = "apiKeyQuery"
	AuthBearerToken            AuthType = "bearerToken"
	AuthBasic                  AuthType = "basic"
	AuthOAuth2ClientCredential AuthType = "oauth2ClientCredentials"
)

// Server is one consumed servers[] entry. The URL is kept for display and
// binding only; nothing is ever dialed at parse time.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// SecurityRequirement is one OR-alternative of the security array:
// scheme name → required scopes.
type SecurityRequirement map[string][]string

// Parameter is one consumed operation (or path-item) parameter.
type Parameter struct {
	Name        string          `json:"name"`
	In          string          `json:"in"`
	Required    bool            `json:"required,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Description string          `json:"description,omitempty"`
}

// RequestBody is one consumed requestBody; Content maps media type →
// schema. Multiple media types are preserved; the governance layer picks
// its preferred representation.
type RequestBody struct {
	Required bool                       `json:"required,omitempty"`
	Content  map[string]json.RawMessage `json:"content,omitempty"`
}

// Response is one consumed responses[] entry keyed by status ("200",
// "default", …).
type Response struct {
	Status      string                     `json:"status"`
	Description string                     `json:"description,omitempty"`
	Content     map[string]json.RawMessage `json:"content,omitempty"`
}

// Operation is one flattened path × method consumer entry. Security is the
// effective requirement list (operation-level override, else global); Auth
// is its normalization onto the auth enum.
type Operation struct {
	ID          string                `json:"operationId"`
	Method      string                `json:"method"`
	Path        string                `json:"path"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	Deprecated  bool                  `json:"deprecated,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   []Response            `json:"responses,omitempty"`
	Security    []SecurityRequirement `json:"security,omitempty"`
	Auth        []AuthType            `json:"auth"`
}

// Spec is the normalized consumer-subset view of one OpenAPI document.
// Digest is the SHA-256 over Canonical, the canonical JSON of the fully
// $ref-resolved document (map keys are marshalled sorted, so the digest is
// stable under key reordering in the source).
type Spec struct {
	OpenAPI     string                     `json:"openapi"`
	Title       string                     `json:"title,omitempty"`
	SpecVersion string                     `json:"specVersion"`
	Description string                     `json:"description,omitempty"`
	Servers     []Server                   `json:"servers,omitempty"`
	Operations  []Operation                `json:"operations"`
	Schemas     map[string]json.RawMessage `json:"schemas,omitempty"`
	AuthTypes   map[string]AuthType        `json:"authTypes,omitempty"`
	Security    []SecurityRequirement      `json:"security,omitempty"`
	Digest      string                     `json:"digest"`
	Canonical   []byte                     `json:"-"`
}

// Parse decodes an OpenAPI consumer-subset document. Content encoding is
// sniffed from the gzip magic bytes; use ParseEncoded when a
// Content-Encoding header is available.
func Parse(raw []byte) (*Spec, error) {
	return parse(raw, "")
}

// ParseEncoded decodes a document together with its HTTP Content-Encoding
// label ("", "identity", "gzip" or "deflate"). A label that contradicts
// the payload magic bytes is tolerated in the safe direction: the actual
// bytes win. Decompressed output is capped at MaxBytes, so a compression
// bomb is refused before JSON decoding.
func ParseEncoded(raw []byte, contentEncoding string) (*Spec, error) {
	return parse(raw, contentEncoding)
}

func parse(raw []byte, enc string) (*Spec, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty document", ErrInvalidSpec)
	}
	if len(raw) > MaxBytes {
		return nil, oasErr(CodeSize, "raw input %d bytes exceeds %d", len(raw), MaxBytes)
	}
	data, err := decodeContent(raw, strings.ToLower(strings.TrimSpace(enc)))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, oasErr(CodeSize, "decompressed input %d bytes exceeds %d", len(data), MaxBytes)
	}
	// UseNumber keeps number literals byte-faithful through the canonical
	// re-marshal, so the digest does not churn on formatting.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	if _, err := dec.Token(); err == nil {
		return nil, fmt.Errorf("%w: trailing content after the document", ErrInvalidSpec)
	}
	resolved, rerr := resolveAll(doc)
	if rerr != nil {
		return nil, rerr
	}
	canonical, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical encode: %v", ErrInvalidSpec, err)
	}
	// Post-parse metering: the fully expanded document must also fit.
	if len(canonical) > MaxBytes {
		return nil, oasErr(CodeSize, "resolved document %d bytes exceeds %d", len(canonical), MaxBytes)
	}
	sum := sha256.Sum256(canonical)
	spec, err := extract(resolved)
	if err != nil {
		return nil, err
	}
	if err := applyGlobalSecurity(spec); err != nil {
		return nil, err
	}
	spec.Digest = hex.EncodeToString(sum[:])
	spec.Canonical = canonical
	return spec, nil
}

// decodeContent returns the plain JSON bytes behind raw, decompressing
// gzip/zlib/deflate payloads (magic-sniffed) under a hard MaxBytes cap.
func decodeContent(raw []byte, enc string) ([]byte, error) {
	switch enc {
	case "", "identity", "gzip", "deflate":
	default:
		return nil, fmt.Errorf("%w: unsupported content-encoding %q", ErrInvalidSpec, enc)
	}
	if enc == "gzip" || (enc != "deflate" && len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b) {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
		}
		return readCapped(zr)
	}
	if enc == "deflate" {
		// HTTP "deflate" is zlib-wrapped in the wild but some servers
		// send raw deflate; try zlib first, fall back to raw.
		if zr, err := zlib.NewReader(bytes.NewReader(raw)); err == nil {
			return readCapped(zr)
		}
		return readCapped(flate.NewReader(bytes.NewReader(raw)))
	}
	return raw, nil
}

func readCapped(r io.Reader) ([]byte, error) {
	// One extra byte proves the payload exceeds the cap.
	buf, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	if len(buf) > MaxBytes {
		return nil, oasErr(CodeSize, "decompressed payload exceeds %d bytes", MaxBytes)
	}
	return buf, nil
}

// ── $ref resolution ─────────────────────────────────────────────────────────

// resolver expands every "$ref" in the document functionally: the original
// tree is never mutated, resolved nodes are shared, and two meters bound
// the work — memoized per-pointer sizes accumulate into totalInlined so an
// expansion bomb is caught mid-resolution, before the final marshal.
type resolver struct {
	doc          map[string]any
	memoSize     map[string]int
	totalInlined int
}

func resolveAll(doc map[string]any) (map[string]any, *Error) {
	r := &resolver{doc: doc, memoSize: map[string]int{}}
	out, err := r.node(doc, 0, nil)
	if err != nil {
		return nil, err
	}
	res, ok := out.(map[string]any)
	if !ok {
		return nil, oasErr(CodeRef, "resolved document is not an object")
	}
	return res, nil
}

// node resolves v encountered after hops reference hops already taken;
// stack holds the pointers currently being chased for cycle detection.
func (r *resolver) node(v any, hops int, stack []string) (any, *Error) {
	switch n := v.(type) {
	case map[string]any:
		raw, isRef := n["$ref"]
		if isRef {
			ref, ok := raw.(string)
			if !ok {
				return nil, &Error{Code: CodeRef, Message: "$ref must be a string", Cause: ErrInvalidSpec}
			}
			if hops >= MaxRefDepth {
				return nil, oasErr(CodeRef, "$ref chain deeper than %d at %q", MaxRefDepth, ref)
			}
			for _, s := range stack {
				if s == ref {
					return nil, oasErr(CodeRef, "$ref cycle at %q", ref)
				}
			}
			target, err := r.deref(ref)
			if err != nil {
				return nil, err
			}
			resolved, rerr := r.node(target, hops+1, append(stack, ref))
			if rerr != nil {
				return nil, rerr
			}
			size, ok := r.memoSize[ref]
			if !ok {
				b, mErr := json.Marshal(resolved)
				if mErr != nil {
					return nil, &Error{Code: CodeRef, Message: "resolved ref is not encodable", Cause: mErr}
				}
				size = len(b)
				r.memoSize[ref] = size
			}
			r.totalInlined += size
			if r.totalInlined > MaxBytes {
				return nil, oasErr(CodeSize, "ref expansion accumulates %d bytes, exceeding %d", r.totalInlined, MaxBytes)
			}
			return resolved, nil
		}
		out := make(map[string]any, len(n))
		for k, val := range n {
			rv, err := r.node(val, hops, stack)
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	case []any:
		out := make([]any, len(n))
		for i, val := range n {
			rv, err := r.node(val, hops, stack)
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	default:
		return v, nil
	}
}

// deref walks an in-document JSON pointer ("#/a/b") against the ORIGINAL
// (unresolved) document. Anything else — absolute URLs, remote fragments,
// pointers without "#", unknown escapes — is refused: this parser is
// offline, so no allowlist/SSRF re-check channel exists here.
func (r *resolver) deref(ref string) (any, *Error) {
	if !strings.HasPrefix(ref, "#/") {
		return nil, oasErr(CodeRef, "external or malformed $ref target %q refused (offline parser)", ref)
	}
	var cur any = r.doc
	for _, tok := range strings.Split(ref[2:], "/") {
		tok = strings.ReplaceAll(tok, "~1", "/")
		tok = strings.ReplaceAll(tok, "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, oasErr(CodeRef, "$ref target %q walks into a non-object", ref)
		}
		cur, ok = m[tok]
		if !ok {
			return nil, oasErr(CodeRef, "$ref target %q not found in document", ref)
		}
	}
	return cur, nil
}

// ── subset extraction ───────────────────────────────────────────────────────

var knownMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

func extract(doc map[string]any) (*Spec, error) {
	spec := &Spec{Operations: []Operation{}}
	ver, _ := doc["openapi"].(string)
	if !strings.HasPrefix(ver, "3.") {
		return nil, fmt.Errorf("%w: openapi field %q is not 3.x", ErrInvalidSpec, ver)
	}
	spec.OpenAPI = ver
	info, ok := doc["info"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: info object missing", ErrInvalidSpec)
	}
	spec.Title, _ = info["title"].(string)
	spec.Description, _ = info["description"].(string)
	v, _ := info["version"].(string)
	if v == "" || len(v) > 64 {
		return nil, fmt.Errorf("%w: info.version must be 1..64 chars", ErrInvalidSpec)
	}
	spec.SpecVersion = v
	for _, s := range listOf(doc["servers"]) {
		m, ok := s.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: servers entries must be objects", ErrInvalidSpec)
		}
		url, _ := m["url"].(string)
		if url == "" || len(url) > 2048 {
			return nil, fmt.Errorf("%w: server url must be 1..2048 chars", ErrInvalidSpec)
		}
		d, _ := m["description"].(string)
		spec.Servers = append(spec.Servers, Server{URL: url, Description: d})
	}
	authTypes, err := mapAuthSchemes(doc)
	if err != nil {
		return nil, err
	}
	spec.AuthTypes = authTypes
	spec.Security, err = readSecurity(doc["security"])
	if err != nil {
		return nil, err
	}
	if comps, ok := doc["components"].(map[string]any); ok {
		if schemas, ok := comps["schemas"].(map[string]any); ok && len(schemas) > 0 {
			spec.Schemas = map[string]json.RawMessage{}
			for name, sc := range schemas {
				raw, mErr := json.Marshal(sc)
				if mErr != nil {
					return nil, fmt.Errorf("%w: schema %q: %v", ErrInvalidSpec, name, mErr)
				}
				spec.Schemas[name] = raw
			}
		}
	}
	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		return spec, nil
	}
	if len(paths) > MaxPaths {
		return nil, oasErr(CodePaths, "%d unique paths exceed %d", len(paths), MaxPaths)
	}
	seenIDs := map[string]string{}
	for p, item := range paths {
		if !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("%w: path %q must start with /", ErrInvalidSpec, p)
		}
		if len(p) > 1024 {
			return nil, fmt.Errorf("%w: path %q exceeds 1024 chars", ErrInvalidSpec, p)
		}
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: path item %q must be an object", ErrInvalidSpec, p)
		}
		pathParams, err := readParameters(m["parameters"], p)
		if err != nil {
			return nil, err
		}
		for method, opv := range m {
			up := strings.ToUpper(method)
			if !knownMethods[up] {
				continue // summary/description/servers/extensions/callbacks…
			}
			op, err := readOperation(up, p, opv, pathParams, authTypes, seenIDs)
			if err != nil {
				return nil, err
			}
			spec.Operations = append(spec.Operations, *op)
		}
	}
	// Map iteration is random; sort for a stable normalized output.
	sort.Slice(spec.Operations, func(i, j int) bool {
		if spec.Operations[i].Path != spec.Operations[j].Path {
			return spec.Operations[i].Path < spec.Operations[j].Path
		}
		return spec.Operations[i].Method < spec.Operations[j].Method
	})
	return spec, nil
}

// readOperation normalizes one operation. Callbacks and their URLs are
// dropped here: "忽略不等于执行" — resolving them for the digest is fine,
// carrying them into the normalized model is not.
func readOperation(method, path string, v any, pathParams []Parameter, authTypes map[string]AuthType, seenIDs map[string]string) (*Operation, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: operation %s %s must be an object", ErrInvalidSpec, method, path)
	}
	op := &Operation{Method: method, Path: path, Parameters: []Parameter{}}
	id, _ := m["operationId"].(string)
	if id == "" || len(id) > 256 {
		return nil, fmt.Errorf("%w: operationId on %s %s must be 1..256 chars", ErrInvalidSpec, method, path)
	}
	if prev, dup := seenIDs[id]; dup {
		return nil, fmt.Errorf("%w: duplicate operationId %q (also on %s)", ErrInvalidSpec, id, prev)
	}
	seenIDs[id] = method + " " + path
	op.ID = id
	op.Summary, _ = m["summary"].(string)
	op.Description, _ = m["description"].(string)
	if d, ok := m["deprecated"].(bool); ok {
		op.Deprecated = d
	}
	opParams, err := readParameters(m["parameters"], path)
	if err != nil {
		return nil, err
	}
	op.Parameters = append(append([]Parameter{}, pathParams...), opParams...)
	if rb, ok := m["requestBody"].(map[string]any); ok {
		body := &RequestBody{Content: map[string]json.RawMessage{}}
		if req, ok := rb["required"].(bool); ok {
			body.Required = req
		}
		if err := readContent(rb["content"], body.Content); err != nil {
			return nil, err
		}
		op.RequestBody = body
	}
	if resp, ok := m["responses"].(map[string]any); ok {
		for status, rv := range resp {
			rm, ok := rv.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%w: response %q must be an object", ErrInvalidSpec, status)
			}
			r := Response{Status: status, Content: map[string]json.RawMessage{}}
			r.Description, _ = rm["description"].(string)
			if err := readContent(rm["content"], r.Content); err != nil {
				return nil, err
			}
			op.Responses = append(op.Responses, r)
		}
		sort.Slice(op.Responses, func(i, j int) bool { return op.Responses[i].Status < op.Responses[j].Status })
	}
	// Effective security: operation-level list overrides the global one;
	// an explicitly empty array means anonymous (none).
	sec, hasSec, err := readSecurityOverride(m["security"])
	if err != nil {
		return nil, err
	}
	op.Security = sec
	op.Auth, err = authEnumsFor(op.Security, authTypes, id)
	if err != nil {
		return nil, err
	}
	_ = hasSec
	return op, nil
}

// applyGlobalSecurity backfills operations that did not override security
// with the document-level default. Operations that overrode (including an
// explicit empty override = anonymous) keep their own list.
func applyGlobalSecurity(spec *Spec) error {
	for i := range spec.Operations {
		if spec.Operations[i].Security == nil {
			spec.Operations[i].Security = spec.Security
			auth, err := authEnumsFor(spec.Security, spec.AuthTypes, spec.Operations[i].ID)
			if err != nil {
				return err
			}
			spec.Operations[i].Auth = auth
		}
	}
	return nil
}

func readParameters(v any, path string) ([]Parameter, error) {
	list := listOf(v)
	if len(list) == 0 {
		return nil, nil
	}
	out := make([]Parameter, 0, len(list))
	for _, p := range list {
		m, ok := p.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: parameters under %s must be objects", ErrInvalidSpec, path)
		}
		param := Parameter{}
		param.Name, _ = m["name"].(string)
		param.In, _ = m["in"].(string)
		if param.Name == "" || len(param.Name) > 256 {
			return nil, fmt.Errorf("%w: parameter name under %s must be 1..256 chars", ErrInvalidSpec, path)
		}
		switch param.In {
		case "query", "header", "path", "cookie":
		default:
			return nil, fmt.Errorf("%w: parameter %q has invalid in %q", ErrInvalidSpec, param.Name, param.In)
		}
		if req, ok := m["required"].(bool); ok {
			param.Required = req
		}
		param.Description, _ = m["description"].(string)
		if sc, ok := m["schema"]; ok && sc != nil {
			raw, err := json.Marshal(sc)
			if err != nil {
				return nil, fmt.Errorf("%w: parameter %q schema: %v", ErrInvalidSpec, param.Name, err)
			}
			param.Schema = raw
		}
		out = append(out, param)
	}
	return out, nil
}

func readContent(v any, into map[string]json.RawMessage) error {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	for mt, cv := range m {
		cm, ok := cv.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: content %q must be an object", ErrInvalidSpec, mt)
		}
		if sc, ok := cm["schema"]; ok && sc != nil {
			raw, err := json.Marshal(sc)
			if err != nil {
				return fmt.Errorf("%w: content %q schema: %v", ErrInvalidSpec, mt, err)
			}
			into[mt] = raw
		}
	}
	return nil
}

// readSecurity reads a global security array (missing → nil, i.e. the
// "nothing declared" default; an empty array is preserved as anonymous).
func readSecurity(v any) ([]SecurityRequirement, error) {
	sec, _, err := readSecurityOverride(v)
	return sec, err
}

func readSecurityOverride(v any) ([]SecurityRequirement, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	list := listOf(v)
	out := make([]SecurityRequirement, 0, len(list))
	for _, s := range list {
		m, ok := s.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("%w: security entries must be objects", ErrInvalidSpec)
		}
		req := SecurityRequirement{}
		for name, scopes := range m {
			strs := make([]string, 0)
			for _, sc := range listOf(scopes) {
				if str, ok := sc.(string); ok {
					strs = append(strs, str)
				}
			}
			req[name] = strs
		}
		out = append(out, req)
	}
	return out, true, nil
}

// authEnumsFor normalizes one effective security list onto the auth enum.
// An empty list is anonymous (none); otherwise every named scheme must be
// declared in securitySchemes.
func authEnumsFor(sec []SecurityRequirement, authTypes map[string]AuthType, opID string) ([]AuthType, error) {
	if len(sec) == 0 {
		return []AuthType{AuthNone}, nil
	}
	seen := map[AuthType]bool{}
	for _, req := range sec {
		for name := range req {
			t, ok := authTypes[name]
			if !ok {
				return nil, fmt.Errorf("%w: operation %q requires undeclared security scheme %q", ErrInvalidSpec, opID, name)
			}
			seen[t] = true
		}
	}
	if len(seen) == 0 {
		return []AuthType{AuthNone}, nil
	}
	out := make([]AuthType, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	// Deterministic order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// mapAuthSchemes maps securitySchemes onto the six-value consumer enum.
// Everything the enum cannot express (openIdConnect, mutualTLS, HTTP
// digest, apiKey in cookie, oauth2 without a clientCredentials flow …) is
// refused: "除 none 外只保存 CredentialRef"，and no CredentialRef can be
// created for an unsupported scheme.
func mapAuthSchemes(doc map[string]any) (map[string]AuthType, error) {
	comps, ok := doc["components"].(map[string]any)
	if !ok {
		return map[string]AuthType{}, nil
	}
	schemes, ok := comps["securitySchemes"].(map[string]any)
	if !ok {
		return map[string]AuthType{}, nil
	}
	out := map[string]AuthType{}
	for name, sv := range schemes {
		m, ok := sv.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: securityScheme %q must be an object", ErrInvalidSpec, name)
		}
		t, _ := m["type"].(string)
		in, _ := m["in"].(string)
		scheme, _ := m["scheme"].(string)
		var auth AuthType
		switch {
		case t == "apiKey" && in == "header":
			auth = AuthAPIKeyHeader
		case t == "apiKey" && in == "query":
			auth = AuthAPIKeyQuery
		case t == "http" && strings.EqualFold(scheme, "bearer"):
			auth = AuthBearerToken
		case t == "http" && strings.EqualFold(scheme, "basic"):
			auth = AuthBasic
		case t == "oauth2" && hasClientCredentials(m["flows"]):
			auth = AuthOAuth2ClientCredential
		default:
			return nil, fmt.Errorf("%w: scheme %q (type %q, in %q, http scheme %q)", ErrAuthUnsupported, name, t, in, scheme)
		}
		out[name] = auth
	}
	return out, nil
}

func hasClientCredentials(flows any) bool {
	m, ok := flows.(map[string]any)
	if !ok {
		return false
	}
	cc, ok := m["clientCredentials"].(map[string]any)
	return ok && len(cc) > 0
}

func listOf(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}
