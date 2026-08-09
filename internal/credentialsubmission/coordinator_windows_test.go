//go:build windows

package credentialsubmission

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
)

type memorySecrets struct {
	mu      sync.Mutex
	values  map[secret.Ref][]byte
	deletes []secret.Ref
}

func newMemorySecrets() *memorySecrets { return &memorySecrets{values: make(map[secret.Ref][]byte)} }

func (s *memorySecrets) Put(ctx context.Context, ref secret.Ref, value []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[ref] = append([]byte(nil), value...)
	return nil
}
func (s *memorySecrets) WithSecret(ctx context.Context, ref secret.Ref, fn func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	value, ok := s.values[ref]
	copyValue := append([]byte(nil), value...)
	s.mu.Unlock()
	if !ok {
		return os.ErrNotExist
	}
	defer secret.Zero(copyValue)
	return fn(copyValue)
}
func (s *memorySecrets) Delete(ctx context.Context, ref secret.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, ref)
	s.deletes = append(s.deletes, ref)
	return nil
}
func (s *memorySecrets) has(ref secret.Ref) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.values[ref]
	return ok
}
func (s *memorySecrets) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.values) }

// testReferenceResolver is intentionally explicit: production construction
// must never silently assume that a reference is unadopted.
type testReferenceResolver struct{}

func (testReferenceResolver) IsCredentialReferenceAdopted(context.Context, secret.Ref) (bool, error) {
	return false, nil
}

func testCoordinator(t *testing.T) (*Coordinator, *memorySecrets, *datadir.SecureRoot) {
	t.Helper()
	root, err := datadir.PrepareForTest(filepath.Join(t.TempDir(), "secure"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	store := newMemorySecrets()
	c, err := New(root, store, testReferenceResolver{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, store, root
}

func hash(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

func draftInput(t *testing.T, requestHash string, credential []byte) SubmitInput {
	t.Helper()
	origin := "https://api.example.test"
	fingerprint, err := provider.OriginFingerprint(provider.ProtocolOpenAICompatible, origin)
	if err != nil {
		t.Fatal(err)
	}
	return SubmitInput{Scope: Draft(fingerprint), Protocol: provider.ProtocolOpenAICompatible, Origin: origin, RequestHash: requestHash, Credential: credential}
}

func TestReserveConcurrentSingleLogicalWinnerAndReplay(t *testing.T) {
	c, _, _ := testCoordinator(t)
	ownerHash := hash("owner")
	sub, err := c.Submit(context.Background(), draftInput(t, ownerHash, []byte("credential")))
	if err != nil {
		t.Fatal(err)
	}

	const workers = 100
	start := make(chan struct{})
	var wg sync.WaitGroup
	var ownerSuccess, conflicts int
	var resultMu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			attemptHash := hash("loser")
			if i == 37 {
				attemptHash = ownerHash
			}
			_, reserveErr := c.Reserve(context.Background(), sub.SubmissionID, attemptHash)
			resultMu.Lock()
			defer resultMu.Unlock()
			switch {
			case reserveErr == nil:
				ownerSuccess++
			case errors.Is(reserveErr, ErrConflict):
				conflicts++
			default:
				t.Errorf("unexpected reserve error: %v", reserveErr)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if ownerSuccess != 1 || conflicts != workers-1 {
		t.Fatalf("success=%d conflicts=%d", ownerSuccess, conflicts)
	}
	if _, err = c.Reserve(context.Background(), sub.SubmissionID, ownerHash); err != nil {
		t.Fatalf("same-hash reserve replay: %v", err)
	}
	if _, err = c.Adopt(context.Background(), sub.SubmissionID, ownerHash); err != nil {
		t.Fatal(err)
	}
	first, err := c.Consume(context.Background(), sub.SubmissionID, ownerHash)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := c.Consume(context.Background(), sub.SubmissionID, ownerHash)
	if err != nil || replay.Ref != first.Ref {
		t.Fatalf("consume replay = %#v, %v", replay, err)
	}
	if _, err = c.Consume(context.Background(), sub.SubmissionID, hash("wrong")); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong hash: %v", err)
	}
}

func TestRecoveryDeletesUncommittedButNeverConsumed(t *testing.T) {
	c, store, root := testCoordinator(t)
	h1, h2 := hash("orphan"), hash("adopted")
	orphan, err := c.Submit(context.Background(), draftInput(t, h1, []byte("orphan-secret")))
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := c.Submit(context.Background(), draftInput(t, h2, []byte("adopted-secret")))
	if err != nil {
		t.Fatal(err)
	}
	orphanRef := c.entries[orphan.SubmissionID].Ref
	if _, err = c.Reserve(context.Background(), adopted.SubmissionID, h2); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Adopt(context.Background(), adopted.SubmissionID, h2); err != nil {
		t.Fatal(err)
	}
	consumed, err := c.Consume(context.Background(), adopted.SubmissionID, h2)
	if err != nil {
		t.Fatal(err)
	}

	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(root, store, testReferenceResolver{})
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if store.has(orphanRef) {
		t.Fatal("recovery retained uncommitted orphan")
	}
	if !store.has(consumed.Ref) {
		t.Fatal("recovery deleted consumed credential")
	}
	if _, err = recovered.Consume(context.Background(), adopted.SubmissionID, h2); err != nil {
		t.Fatalf("uncertain consume replay failed: %v", err)
	}
}

func TestCompletedReplayExpiresAndRecoveryCompactsWithoutDeletingSecret(t *testing.T) {
	c, store, root := testCoordinator(t)
	now := time.Now().UTC().Add(-2 * time.Minute)
	c.now = func() time.Time { return now }
	h := hash("bounded-replay")
	input := draftInput(t, h, []byte("adopted-secret"))
	input.TTL = time.Minute
	sub, err := c.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Reserve(context.Background(), sub.SubmissionID, h); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Adopt(context.Background(), sub.SubmissionID, h); err != nil {
		t.Fatal(err)
	}
	consumed, err := c.Consume(context.Background(), sub.SubmissionID, h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Consume(context.Background(), sub.SubmissionID, h); err != nil {
		t.Fatalf("consume replay during replay period: %v", err)
	}

	now = now.Add(time.Minute)
	if _, err = c.Consume(context.Background(), sub.SubmissionID, h); !errors.Is(err, ErrExpired) {
		t.Fatalf("consume replay after replay period: %v", err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := New(root, store, testReferenceResolver{})
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if len(recovered.entries) != 0 {
		t.Fatalf("completed journal entries after recovery = %d", len(recovered.entries))
	}
	if !store.has(consumed.Ref) {
		t.Fatal("recovery compaction deleted adopted credential")
	}
	if _, err = recovered.Consume(context.Background(), sub.SubmissionID, h); !errors.Is(err, ErrNotFound) {
		t.Fatalf("compacted consume = %v", err)
	}
}

func TestCompletedJournalStressBeyondEntryLimit(t *testing.T) {
	c, store, _ := testCoordinator(t)
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	c.now = func() time.Time { return now }
	const submissions = maxJournalEntries + 1
	for i := 0; i < submissions; i++ {
		h := hash(fmt.Sprintf("stress-%d", i))
		input := draftInput(t, h, []byte("secret"))
		input.TTL = time.Second
		sub, err := c.Submit(context.Background(), input)
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		if _, err = c.Reserve(context.Background(), sub.SubmissionID, h); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
		if _, err = c.Adopt(context.Background(), sub.SubmissionID, h); err != nil {
			t.Fatalf("adopt %d: %v", i, err)
		}
		if _, err = c.Consume(context.Background(), sub.SubmissionID, h); err != nil {
			t.Fatalf("consume %d: %v", i, err)
		}
		now = now.Add(time.Second)
		if err = c.Recover(context.Background()); err != nil {
			t.Fatalf("recover %d: %v", i, err)
		}
	}
	if len(c.entries) != 0 {
		t.Fatalf("completed journal entries = %d", len(c.entries))
	}
	if store.count() != submissions {
		t.Fatalf("adopted secrets = %d, want %d", store.count(), submissions)
	}
}

func TestOutstandingLimitRejectsBeforeSecretPut(t *testing.T) {
	c, store, _ := testCoordinator(t)
	c.mu.Lock()
	for i := 0; i < maxJournalEntries; i++ {
		id := ulid.Make().String()
		c.entries[id] = journalEntry{SubmissionID: id, State: StateReady}
	}
	c.mu.Unlock()
	input := draftInput(t, hash("bounded"), []byte("must-not-be-stored"))
	if _, err := c.Submit(context.Background(), input); !errors.Is(err, ErrBusy) {
		t.Fatalf("full coordinator submit error=%v want ErrBusy", err)
	}
	if store.count() != 0 {
		t.Fatalf("Secrets.Put occurred despite admission rejection: count=%d", store.count())
	}
}

func TestReplacementAlwaysAllocatesNewReference(t *testing.T) {
	c, _, _ := testCoordinator(t)
	providerID := ulid.Make().String()
	input := draftInput(t, hash("one"), []byte("first"))
	input.Scope = Draft(input.Scope.DraftFingerprint)
	first, err := c.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	firstRef := c.entries[first.SubmissionID].Ref
	input = draftInput(t, hash("two"), []byte("second"))
	input.Scope = Draft(input.Scope.DraftFingerprint)
	second, err := c.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	secondRef := c.entries[second.SubmissionID].Ref
	_ = providerID
	if firstRef.CredentialRef == secondRef.CredentialRef {
		t.Fatal("replacement reused credential reference")
	}
}

func TestExpiryDeletesUnconsumedAndRejectsLateConsume(t *testing.T) {
	c, store, _ := testCoordinator(t)
	now := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	c.now = func() time.Time { return now }
	h := hash("expiry")
	input := draftInput(t, h, []byte("short-lived"))
	input.TTL = time.Minute
	sub, err := c.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := c.Reserve(context.Background(), sub.SubmissionID, h)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	if _, err = c.Consume(context.Background(), sub.SubmissionID, h); !errors.Is(err, ErrExpired) {
		t.Fatalf("late consume: %v", err)
	}
	if err = c.CleanupExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.has(reserved.Ref) || store.count() != 0 {
		t.Fatal("expired credential was not deleted")
	}
	tooLong := draftInput(t, hash("long"), []byte("x"))
	tooLong.TTL = MaxTTL + time.Nanosecond
	if _, err = c.Submit(context.Background(), tooLong); err == nil {
		t.Fatal("accepted TTL over five minutes")
	}
	if !bytes.Equal(tooLong.Credential, []byte{0}) {
		t.Fatal("rejected input was not zeroed")
	}
}

func TestJournalContainsMetadataOnlyAndInputIsZeroed(t *testing.T) {
	c, _, root := testCoordinator(t)
	canary := []byte("PLAINTEXT-CREDENTIAL-CANARY")
	input := draftInput(t, hash("journal"), canary)
	sub, err := c.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canary, make([]byte, len(canary))) {
		t.Fatal("credential input was not zeroed")
	}
	path, err := root.FilePath(journalName)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(contents, []byte("PLAINTEXT-CREDENTIAL-CANARY")) || bytes.Contains(bytes.ToLower(contents), []byte("ciphertext")) {
		t.Fatal("journal contains credential material")
	}
	var raw struct {
		Version int                          `json:"version"`
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err = json.Unmarshal(contents, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Version != 1 || len(raw.Entries) != 1 {
		t.Fatalf("unexpected journal: %s", contents)
	}
	allowed := map[string]bool{"submissionId": true, "providerId": true, "ref": true, "requestDigest": true, "state": true, "expiry": true}
	for key := range raw.Entries[0] {
		if !allowed[key] {
			t.Fatalf("unexpected journal field %q", key)
		}
	}
	if string(raw.Entries[0]["submissionId"]) != `"`+sub.SubmissionID+`"` {
		t.Fatal("submission metadata missing")
	}
}

func TestValidationAndMaximumCredentialSize(t *testing.T) {
	c, _, _ := testCoordinator(t)
	oversize := make([]byte, MaxCredentialSize+1)
	if _, err := c.Submit(context.Background(), draftInput(t, hash("large"), oversize)); err == nil {
		t.Fatal("accepted oversized credential")
	}
	if !bytes.Equal(oversize, make([]byte, len(oversize))) {
		t.Fatal("oversized credential was not zeroed")
	}
	bad := draftInput(t, hash("bad-origin"), []byte("x"))
	bad.Origin = "HTTPS://API.EXAMPLE.TEST/"
	if _, err := c.Submit(context.Background(), bad); err == nil {
		t.Fatal("accepted non-normalized origin")
	}
	bad = draftInput(t, hash("bad-fingerprint"), []byte("x"))
	bad.Scope = Draft(hash("different"))
	if _, err := c.Submit(context.Background(), bad); err == nil {
		t.Fatal("accepted mismatched draft fingerprint")
	}
}

func TestDraftCreateTupleMismatchDoesNotStoreCredential(t *testing.T) {
	c, store, _ := testCoordinator(t)
	origin := "https://api.example.test"
	fingerprint, err := provider.OriginFingerprint(provider.ProtocolOpenAICompatible, origin)
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string][]byte{
		"origin":   []byte(`{"protocol":"openai_compatible","baseUrl":"https://other.example/v1"}`),
		"protocol": []byte(`{"protocol":"anthropic","baseUrl":"https://api.example.test/v1"}`),
	} {
		t.Run(name, func(t *testing.T) {
			input := SubmitInput{Scope: Draft(fingerprint), Protocol: provider.ProtocolOpenAICompatible, Origin: origin, Request: request, Credential: []byte("one-use")}
			if _, err := c.Submit(context.Background(), input); err == nil {
				t.Fatal("accepted draft create tuple mismatch")
			}
			if store.count() != 0 {
				t.Fatal("tuple mismatch stored credential")
			}
		})
	}
}

func TestNewRequiresAuthoritativeReferenceResolver(t *testing.T) {
	root, err := datadir.PrepareForTest(filepath.Join(t.TempDir(), "secure"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if c, err := New(root, newMemorySecrets(), nil); err == nil || c != nil {
		t.Fatalf("New without authoritative resolver = %#v, %v", c, err)
	}
}
