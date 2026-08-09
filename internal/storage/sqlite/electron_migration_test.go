package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

const sanitizedElectronFixture = `{"version":"0.2.1","providers":[{"id":"legacy-example","name":"Example","protocol":"openai","baseUrl":"https://example.test/v1","models":["model-a","model-b"],"defaultModel":"model-b","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z","encryptedApiKey":"sanitized-not-a-secret"}]}`

type electronMetadataGolden struct {
	Version           string              `json:"version"`
	SourceFingerprint string              `json:"sourceFingerprint"`
	ItemFingerprints  []string            `json:"itemFingerprints"`
	Providers         []provider.Provider `json:"providers"`
}

func TestSanitizedElectronMetadataFixtures(t *testing.T) {
	want := map[string]string{
		"electron-provider-0.1.json":       "0.1",
		"electron-provider-0.2.json":       "0.2",
		"electron-provider-0.2.1.json":     "0.2.1",
		"electron-provider-numeric-1.json": "0.1",
		"electron-provider-numeric-2.json": "0.2",
	}
	for name, version := range want {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(string(raw)), "sk-") || strings.Contains(string(raw), "api-key-leak-canary") {
				t.Fatal("fixture contains a secret canary")
			}
			got, err := inspectElectronBytes(raw, name)
			if err != nil || got.version != version || len(got.providers) != 1 {
				t.Fatalf("version=%q providers=%d err=%v", got.version, len(got.providers), err)
			}
			if len(got.encrypted) != 1 || got.encrypted[0] != "SANITIZED-NOT-CIPHERTEXT" {
				t.Fatal("fixture contains a non-sanitized encrypted credential")
			}
		})
	}
}

func TestElectronMigrationPreservesLegacyProtocolBinding(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "electron-provider-numeric-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	inspected, err := inspectElectronBytes(raw, "electron-provider-numeric-1.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(inspected.providers) != 1 || len(inspected.legacyProtocols) != 1 {
		t.Fatalf("unexpected inspected provider count: %d/%d", len(inspected.providers), len(inspected.legacyProtocols))
	}
	if inspected.providers[0].Protocol != provider.ProtocolOpenAICompatible || inspected.legacyProtocols[0] != "openai" {
		t.Fatalf("legacy binding was not preserved: destination=%q legacy=%q", inspected.providers[0].Protocol, inspected.legacyProtocols[0])
	}
}

func TestElectronMetadataGoldenCompatibility(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "electron-provider-*.json"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("fixtures=%v err=%v", fixtures, err)
	}
	for _, fixture := range fixtures {
		if strings.HasSuffix(fixture, ".golden.json") {
			continue
		}
		fixture := fixture
		t.Run(filepath.Base(fixture), func(t *testing.T) {
			raw, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			inspected, err := inspectElectronBytes(raw, filepath.Base(fixture))
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(electronMetadataGolden{
				Version:           inspected.version,
				SourceFingerprint: inspected.fingerprint,
				ItemFingerprints:  inspected.itemFP,
				Providers:         inspected.providers,
			}, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			goldenPath := strings.TrimSuffix(fixture, ".json") + ".golden.json"
			if os.Getenv("LUNITIDE_UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(goldenPath, got, 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v (set LUNITIDE_UPDATE_GOLDEN=1 to create it)", goldenPath, err)
			}
			if string(got) != string(want) {
				t.Fatalf("normalized migration output drifted for %s\n--- want\n%s--- got\n%s", fixture, want, got)
			}
		})
	}
}

func TestElectronMetadataMigrationPreservesSourceAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "providers.json")
	if err := os.WriteFile(source, []byte(sanitizedElectronFixture), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(source)
	beforeSum := sha256.Sum256(before)
	s, err := Open(ctx, filepath.Join(dir, "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	inspect, err := s.InspectElectronProviderMetadata(ctx, source)
	if err != nil || inspect.State != "idle" || inspect.Total != 1 {
		t.Fatalf("inspect=%#v err=%v", inspect, err)
	}
	first, err := s.RunElectronProviderMetadata(ctx, source)
	if err != nil || first.State != "completed" || first.Imported != 1 || first.Processed != 1 {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := s.RunElectronProviderMetadata(ctx, source)
	if err != nil || second != first {
		t.Fatalf("replay=%#v err=%v want %#v", second, err, first)
	}
	after, _ := os.ReadFile(source)
	afterSum := sha256.Sum256(after)
	if beforeSum != afterSum {
		t.Fatal("source file changed")
	}
	items, err := s.List(ctx, provider.Filter{})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
	if items[0].LegacyID != "legacy-example" || items[0].CredentialState != provider.CredentialRequiresReentry || items[0].CredentialRef != "" || !items[0].Models[1].IsDefault {
		t.Fatalf("unsafe import: %#v", items[0])
	}
	var auditCount int
	if err = s.db.QueryRow(`SELECT count(*) FROM audit_events WHERE actor='electron-metadata-migration'`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit=%d err=%v", auditCount, err)
	}
}

func TestElectronMetadataMigrationDuplicateConflictAndResume(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	source := filepath.Join(dir, "providers.json")
	os.WriteFile(source, []byte(sanitizedElectronFixture), 0600)
	s, err := Open(ctx, filepath.Join(dir, "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	in, err := inspectElectronFile(source)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := in.providers[0]
	if _, err = s.Create(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	st, err := s.RunElectronProviderMetadata(ctx, source)
	if err != nil || st.Duplicates != 1 || st.Imported != 0 {
		t.Fatalf("duplicate=%#v err=%v", st, err)
	}
	// A changed source fingerprint with the same legacy ID but different metadata is a conflict.
	changed := []byte(`{"version":"0.2","providers":[{"id":"legacy-example","name":"Changed","protocol":"openai","baseUrl":"https://example.test/v1","model":"model-a","createdAt":"2025-01-01T00:00:00Z","updatedAt":"2025-01-02T00:00:00Z"}]}`)
	os.WriteFile(source, changed, 0600)
	st, err = s.RunElectronProviderMetadata(ctx, source)
	if err != nil || st.Conflicts != 1 || st.Imported != 0 {
		t.Fatalf("conflict=%#v err=%v", st, err)
	}
	// Simulate interruption after a committed item; rerun must not double-count it.
	if _, err = s.db.Exec(`UPDATE provider_metadata_migrations SET state='running' WHERE source_fingerprint=?`, st.SourceFingerprint); err != nil {
		t.Fatal(err)
	}
	resumed, err := s.RunElectronProviderMetadata(ctx, source)
	if err != nil || resumed.State != "completed" || resumed.Processed != 1 || resumed.Conflicts != 1 {
		t.Fatalf("resume=%#v err=%v", resumed, err)
	}
}

func TestElectronMetadataMigrationRejectsCorruptAndUnboundedInput(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(ctx, filepath.Join(dir, "target.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cases := map[string][]byte{
		"corrupt":       []byte(`{"version":"0.2.1","providers":[`),
		"unknown-field": []byte(`{"version":"0.2.1","providers":[],"secret":"no"}`),
		"unsupported":   []byte(`{"version":"9","providers":[]}`),
		"too-many":      append([]byte(`{"version":"0.2.1","providers":[]}`), make([]byte, maxElectronProviderFile)...),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name, "providers.json")
			os.MkdirAll(filepath.Dir(p), 0700)
			os.WriteFile(p, raw, 0600)
			if _, err := s.RunElectronProviderMetadata(ctx, p); err == nil {
				t.Fatal("corrupt input accepted")
			}
		})
	}
	var n int
	if err = s.db.QueryRow(`SELECT count(*) FROM provider_metadata_migrations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("corrupt input wrote status: %d %v", n, err)
	}
}
