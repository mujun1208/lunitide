package skill

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// testCanonical is an INDEPENDENT re-implementation of the loader's
// canonicalization. Signing fixtures through it (instead of reusing
// canonicalManifestBytes) proves the wire format: if the loader's
// canonicalization ever drifts, previously signed manifests stop verifying
// and these tests fail instead of silently re-signing.
func testCanonical(t *testing.T, m SkillManifest) []byte {
	t.Helper()
	payload := map[string]any{
		"id":           m.ID,
		"version":      m.Version,
		"name":         m.Name,
		"description":  m.Description,
		"capabilities": m.Capabilities,
		"steps":        m.Steps,
		"signedAt":     m.SignedAt,
		"expiresAt":    m.ExpiresAt,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// buildManifest produces a fully signed manifest fixture; mutate runs
// before digesting/signing (for legitimately varied fixtures).
func buildManifest(t *testing.T, priv ed25519.PrivateKey, now time.Time, mutate func(*SkillManifest)) (SkillManifest, []byte) {
	t.Helper()
	m := SkillManifest{
		ID:           "builtin.hello",
		Version:      "1.0.0",
		Name:         "Hello",
		Description:  "greet",
		Capabilities: []string{"fs.read"},
		Steps: []Step{{
			Name:        "greet",
			Action:      "fs.read",
			ParamSchema: map[string]any{"target": "string"},
		}},
		SignedAt:  now.Add(-24 * time.Hour),
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	if mutate != nil {
		mutate(&m)
	}
	canonical := testCanonical(t, m)
	sum := sha256.Sum256(canonical)
	m.Digest = hex.EncodeToString(sum[:])
	m.Signature = ed25519.Sign(priv, canonical)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return m, data
}

func mapFS(t *testing.T, data []byte) fs.FS {
	t.Helper()
	return fstest.MapFS{"manifests/hello.json": &fstest.MapFile{Data: data}}
}

// TestBuiltinSkillOnly covers T-5.3.5: only product-root-signed manifests
// embedded in the binary ever load.
func TestBuiltinSkillOnly(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	t.Run("valid signed manifest loads with capabilities intact", func(t *testing.T) {
		want, data := buildManifest(t, priv, now, nil)
		got, err := loadFromFS(mapFS(t, data), pub, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("loaded %d manifests, want 1", len(got))
		}
		if got[0].ID != want.ID || got[0].Name != want.Name || got[0].Version != want.Version {
			t.Fatalf("manifest = %+v, want id/name/version of %+v", got[0], want)
		}
		if len(got[0].Capabilities) != 1 || got[0].Capabilities[0] != "fs.read" {
			t.Fatalf("capabilities = %v, want [fs.read]", got[0].Capabilities)
		}
		if len(got[0].Steps) != 1 || got[0].Steps[0].Action != "fs.read" {
			t.Fatalf("steps = %+v", got[0].Steps)
		}
	})

	t.Run("single tampered byte is rejected", func(t *testing.T) {
		_, data := buildManifest(t, priv, now, nil)
		// Flip one byte of the description: "greet" -> "grief".
		tampered := strings.Replace(string(data), "greet", "grief", 1)
		if tampered == string(data) {
			t.Fatal("fixture did not tamper")
		}
		_, err := loadFromFS(mapFS(t, []byte(tampered)), pub, now)
		if !errors.Is(err, ErrSkillRejected) {
			t.Fatalf("err = %v, want ErrSkillRejected", err)
		}
		if !strings.Contains(err.Error(), "builtin.hello") {
			t.Fatalf("rejection must name the manifest id: %v", err)
		}
	})

	t.Run("recomputed digest with stale signature is rejected", func(t *testing.T) {
		// Re-sign content with a DIFFERENT key and keep the stale digest:
		// digest matches, root signature does not.
		_, other, _ := ed25519.GenerateKey(rand.Reader)
		m, _ := buildManifest(t, other, now, func(m *SkillManifest) { m.Name = "Impostor" })
		canonical := testCanonical(t, m)
		sum := sha256.Sum256(canonical)
		m.Digest = hex.EncodeToString(sum[:])
		m.Signature = ed25519.Sign(other, canonical)
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadFromFS(mapFS(t, data), pub, now); !errors.Is(err, ErrSkillRejected) {
			t.Fatalf("err = %v, want ErrSkillRejected", err)
		}
	})

	t.Run("expired manifest is rejected", func(t *testing.T) {
		_, data := buildManifest(t, priv, now, func(m *SkillManifest) {
			m.ExpiresAt = now.Add(-time.Minute)
		})
		_, err := loadFromFS(mapFS(t, data), pub, now)
		if !errors.Is(err, ErrSkillRejected) || !strings.Contains(err.Error(), "expired") {
			t.Fatalf("err = %v, want expired rejection", err)
		}
	})

	t.Run("digest mismatch is rejected even with a valid signature", func(t *testing.T) {
		m, data := buildManifest(t, priv, now, nil)
		m.Digest = strings.Repeat("0", 64) // signature covers the canonical
		// bytes only, so it stays valid: the digest check must catch it.
		bad, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if string(bad) == string(data) {
			t.Fatal("fixture did not change")
		}
		_, err = loadFromFS(mapFS(t, bad), pub, now)
		if !errors.Is(err, ErrSkillRejected) || !strings.Contains(err.Error(), "digest") {
			t.Fatalf("err = %v, want digest rejection", err)
		}
	})

	t.Run("malformed version is rejected", func(t *testing.T) {
		_, data := buildManifest(t, priv, now, func(m *SkillManifest) { m.Version = "1.0" })
		if _, err := loadFromFS(mapFS(t, data), pub, now); !errors.Is(err, ErrSkillRejected) {
			t.Fatalf("err = %v, want ErrSkillRejected", err)
		}
	})

	t.Run("any rejection fails the whole registry", func(t *testing.T) {
		_, good := buildManifest(t, priv, now, nil)
		_, bad := buildManifest(t, priv, now, func(m *SkillManifest) { m.ID = "builtin.evil" })
		eviled := strings.Replace(string(bad), "greet", "grief", 1)
		fsys := fstest.MapFS{
			"manifests/hello.json": &fstest.MapFile{Data: good},
			"manifests/evil.json":  &fstest.MapFile{Data: []byte(eviled)},
		}
		got, err := loadFromFS(fsys, pub, now)
		if !errors.Is(err, ErrSkillRejected) {
			t.Fatalf("err = %v, want ErrSkillRejected", err)
		}
		if got != nil {
			t.Fatalf("partial registry leaked: %+v", got)
		}
	})

	// (e) No third-party loading entry. This is a compile-time property:
	// LoadBuiltins takes only the root key and a clock — no path, no
	// caller filesystem. The two checks below pin it so a future refactor
	// cannot quietly widen the surface.
	t.Run("no third-party loading entry", func(t *testing.T) {
		lt := reflect.TypeOf(LoadBuiltins)
		if lt.NumIn() != 2 {
			t.Fatalf("LoadBuiltins takes %d args, want exactly (ed25519.PublicKey, time.Time)", lt.NumIn())
		}
		if lt.In(0) != reflect.TypeOf(ed25519.PublicKey{}) || lt.In(1) != reflect.TypeOf(time.Time{}) {
			t.Fatalf("LoadBuiltins signature = %v, want (ed25519.PublicKey, time.Time)", lt)
		}
		for i := 0; i < lt.NumIn(); i++ {
			switch lt.In(i).Kind() {
			case reflect.String:
				t.Fatalf("LoadBuiltins arg %d is a string path: third-party entry", i)
			}
			if lt.In(i) == reflect.TypeOf((*fs.FS)(nil)).Elem() {
				t.Fatalf("LoadBuiltins arg %d accepts a caller filesystem: third-party entry", i)
			}
		}
		// The only filesystem the package touches is the embedded one:
		// assert builtin.go contains no runtime disk/network API.
		src, err := os.ReadFile("builtin.go")
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"os.Open(", "os.ReadDir(", "os.ReadFile(", "filepath.Walk", "http.", "net.Listen"} {
			if strings.Contains(string(src), banned) {
				t.Fatalf("builtin.go references %q: third-party I/O entry", banned)
			}
		}
	})

	t.Run("nil root key is a wiring bug", func(t *testing.T) {
		if _, err := LoadBuiltins(nil, now); !errors.Is(err, ErrSkillRegistryEmpty) {
			t.Fatalf("err = %v, want ErrSkillRegistryEmpty", err)
		}
	})
}

// TestEmbeddedHelloManifest checks the shipped fixture: structure, digest
// self-consistency and (with any key) signature-shaped fields. The embedded
// signature is a placeholder until the product signing pipeline issues the
// real one, so only the digest is verified here.
func TestEmbeddedHelloManifest(t *testing.T) {
	data, err := builtinFS.ReadFile("manifests/hello.json")
	if err != nil {
		t.Fatal(err)
	}
	var m SkillManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "builtin.hello" || m.Name != "Hello" || m.Version != "1.0.0" || m.Description != "greet" {
		t.Fatalf("manifest header = %+v", m)
	}
	if len(m.Capabilities) != 1 || m.Capabilities[0] != "fs.read" {
		t.Fatalf("capabilities = %v", m.Capabilities)
	}
	canonical, err := canonicalManifestBytes(m)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canonical)
	if actual := hex.EncodeToString(sum[:]); m.Digest != actual {
		t.Fatalf("embedded digest = %s, canonical digest = %s (re-sign the fixture)", m.Digest, actual)
	}
	if len(m.Signature) != ed25519.SignatureSize {
		t.Fatalf("placeholder signature must still be %d bytes, got %d", ed25519.SignatureSize, len(m.Signature))
	}
	// The placeholder signature cannot verify: loading must refuse the
	// registry rather than accept an unsigned builtin.
	if _, err := LoadBuiltins(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)), time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("LoadBuiltins must reject placeholder-signed embedded manifests")
	} else if !errors.Is(err, ErrSkillRejected) {
		t.Fatalf("err = %v, want ErrSkillRejected", err)
	}
}
