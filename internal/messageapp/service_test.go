package messageapp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/providerapp"
)

type nilReader struct{}

func (*nilReader) ListMessages(context.Context, PageQuery) ([]message.Message, int64, bool, error) {
	panic("typed nil called")
}

type nilUOW struct{}

func (*nilUOW) DoMessage(context.Context, func(Tx) error) error { panic("typed nil called") }

func TestTypedNilDependenciesDoNotPanic(t *testing.T) {
	var r *nilReader
	var u *nilUOW
	s, err := New(r, u, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(context.Background(), PageRequest{SessionID: "x"}); err == nil {
		t.Fatal("typed-nil reader accepted")
	}
	if _, err := s.Append(context.Background(), "key", "actor", map[string]string{}, message.Message{}); err == nil {
		t.Fatal("typed-nil unit of work accepted")
	}
	// AppendAssistant on typed-nil uow must not panic.
	if _, err := s.AppendAssistant(context.Background(), "stream-1", "engine", "01ARZ3NDEKTSV4RRFFQ69G5FAV", "hi", AssistantUsage{}); err == nil {
		t.Fatal("typed-nil uow accepted by AppendAssistant")
	}
}

// assistantTx is a controllable Tx mock for AppendAssistant tests.
type assistantTx struct {
	mu               sync.Mutex
	idempotency      map[string]providerapp.Record // key: operation+"\x00"+key
	appendCallCount  int
	tokenLedgerCalls int
	ledgerEntries    []token.LedgerEntry
	auditCalls       int
	idempotencyPuts  int
	appendErr        error
	lastAppended     message.Message // returned by Message() for the last append
	now              time.Time
}

func newAssistantTx(now time.Time) *assistantTx {
	return &assistantTx{
		idempotency: make(map[string]providerapp.Record),
		now:         now,
	}
}

func (t *assistantTx) AppendMessage(_ context.Context, v message.Message) (message.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.appendErr != nil {
		return message.Message{}, t.appendErr
	}
	t.appendCallCount++
	v.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	v.Sequence = int64(t.appendCallCount)
	v.CreatedAt = t.now
	t.lastAppended = v
	return v, nil
}

func (t *assistantTx) Message(_ context.Context, id string) (message.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if id == t.lastAppended.ID {
		return t.lastAppended, nil
	}
	return message.Message{}, errors.New("not found")
}

func (t *assistantTx) Idempotency(_ context.Context, operation, key string, _ time.Time) (providerapp.Record, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.idempotency[operation+"\x00"+key]
	return r, ok, nil
}

func (t *assistantTx) PutIdempotency(_ context.Context, r providerapp.Record) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.idempotencyPuts++
	t.idempotency[r.Operation+"\x00"+r.Key] = r
	return nil
}

func (t *assistantTx) PutAudit(_ context.Context, _ providerapp.Audit) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.auditCalls++
	return nil
}

func (t *assistantTx) PutTokenLedgerEntry(_ context.Context, entry token.LedgerEntry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tokenLedgerCalls++
	t.ledgerEntries = append(t.ledgerEntries, entry)
	return nil
}

// assistantUOW wraps a Tx and executes the callback synchronously.
type assistantUOW struct {
	tx Tx
}

func (u *assistantUOW) DoMessage(_ context.Context, fn func(Tx) error) error {
	return fn(u.tx)
}

func newAssistantService(t *testing.T, tx Tx) *Service {
	t.Helper()
	s, err := New(nil, &assistantUOW{tx: tx}, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAppendAssistantSuccess(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	tx := newAssistantTx(now)
	s := newAssistantService(t, tx)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	usage := AssistantUsage{Provider: "openai_compatible", Model: "gpt-4", OutputTokens: 42}

	msg, err := s.AppendAssistant(context.Background(), "stream-001", "engine", sessionID, "Hello!\r\nWorld.", usage)
	if err != nil {
		t.Fatalf("AppendAssistant failed: %v", err)
	}
	// Verify message fields.
	if msg.Role != message.RoleAssistant || msg.Status != message.StatusCompleted {
		t.Fatalf("unexpected role/status: %s/%s", msg.Role, msg.Status)
	}
	if msg.Text != "Hello!\nWorld." {
		t.Fatalf("CRLF not normalized: %q", msg.Text)
	}
	if msg.SessionID != sessionID || msg.Sequence != 1 || msg.ID == "" {
		t.Fatalf("unexpected message metadata: %+v", msg)
	}
	// Verify side effects: 1 append, 2 token ledger (canonical + provider-reported),
	// 1 audit, 1 idempotency put.
	if tx.appendCallCount != 1 {
		t.Fatalf("expected 1 AppendMessage call, got %d", tx.appendCallCount)
	}
	if tx.tokenLedgerCalls != 2 {
		t.Fatalf("expected 2 PutTokenLedgerEntry calls (canonical + provider), got %d", tx.tokenLedgerCalls)
	}
	// Verify the canonical entry uses the frozen tokenizer revision.
	canonicalFound := false
	providerFound := false
	for _, e := range tx.ledgerEntries {
		if e.TokenizerRevision == token.CanonicalTokenizerRevision && e.Provider == "" && e.EstimationMethod == token.CharRatio {
			if e.TokenizerID != token.CanonicalTokenizerID {
				t.Fatalf("canonical tokenizer ID = %q", e.TokenizerID)
			}
			canonicalFound = true
		}
		if e.Provider == "openai_compatible" && e.Model == "gpt-4" && e.EstimationMethod == token.ProviderReport {
			providerFound = true
			if e.TokenizerID != token.ProviderReportTokenizerID || e.TokenizerRevision != token.ProviderReportTokenizerRevision {
				t.Fatalf("provider report identity = %q/%q", e.TokenizerID, e.TokenizerRevision)
			}
			if e.TokenCount != 42 {
				t.Fatalf("provider entry attributed %d tokens, want assistant output 42", e.TokenCount)
			}
		}
	}
	if !canonicalFound {
		t.Fatal("canonical token ledger entry not found")
	}
	if !providerFound {
		t.Fatal("provider-reported token ledger entry not found")
	}
	if tx.auditCalls != 1 {
		t.Fatalf("expected 1 PutAudit call, got %d", tx.auditCalls)
	}
	if tx.idempotencyPuts != 1 {
		t.Fatalf("expected 1 PutIdempotency call, got %d", tx.idempotencyPuts)
	}
}

func TestAppendAssistantIdempotentReplay(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	tx := newAssistantTx(now)
	s := newAssistantService(t, tx)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	usage := AssistantUsage{Provider: "openai_compatible", Model: "gpt-4", OutputTokens: 10}

	// First call persists.
	first, err := s.AppendAssistant(context.Background(), "stream-replay", "engine", sessionID, "Answer.", usage)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	// Second call with same streamID + same request must replay without new append.
	second, err := s.AppendAssistant(context.Background(), "stream-replay", "engine", sessionID, "Answer.", usage)
	if err != nil {
		t.Fatalf("replay call failed: %v", err)
	}
	if first != second {
		t.Fatalf("replay returned different message: first=%+v second=%+v", first, second)
	}
	if tx.appendCallCount != 1 {
		t.Fatalf("expected 1 AppendMessage call after replay, got %d", tx.appendCallCount)
	}
}

func TestAppendAssistantIdempotencyConflict(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	tx := newAssistantTx(now)
	s := newAssistantService(t, tx)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"

	// First call with text "A".
	if _, err := s.AppendAssistant(context.Background(), "stream-conflict", "engine", sessionID, "A", AssistantUsage{}); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	// Second call with same streamID but different text must conflict.
	_, err := s.AppendAssistant(context.Background(), "stream-conflict", "engine", sessionID, "B", AssistantUsage{})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestAppendAssistantRejectsOversizedResponse(t *testing.T) {
	tx := newAssistantTx(time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))
	s := newAssistantService(t, tx)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	oversized := strings.Repeat("a", message.MaxRunesAssistant+1)
	_, err := s.AppendAssistant(context.Background(), "stream-big", "engine", sessionID, oversized, AssistantUsage{})
	if !errors.Is(err, ErrAssistantResponseTooLarge) {
		t.Fatalf("expected ErrAssistantResponseTooLarge, got %v", err)
	}
	if tx.appendCallCount != 0 {
		t.Fatal("oversized response should not reach AppendMessage")
	}
}

func TestAppendAssistantRejectsInvalidStreamID(t *testing.T) {
	tx := newAssistantTx(time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))
	s := newAssistantService(t, tx)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for _, bad := range []string{"", "has space", "has\x00null"} {
		_, err := s.AppendAssistant(context.Background(), bad, "engine", sessionID, "text", AssistantUsage{})
		if !errors.Is(err, ErrIdempotencyKeyRequired) {
			t.Fatalf("expected ErrIdempotencyKeyRequired for %q, got %v", bad, err)
		}
	}
}

func TestAppendAssistantCanonicalAlwaysWrittenEvenWithZeroUsage(t *testing.T) {
	tx := newAssistantTx(time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC))
	s := newAssistantService(t, tx)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	// OutputTokens = 0 → no provider-reported entry, but canonical entry is
	// always written (architecture doc §12.1.1: persist tokenizer ID/version).
	if _, err := s.AppendAssistant(context.Background(), "stream-zero", "engine", sessionID, "text", AssistantUsage{OutputTokens: 0}); err != nil {
		t.Fatalf("AppendAssistant failed: %v", err)
	}
	if tx.tokenLedgerCalls != 1 {
		t.Fatalf("expected 1 token ledger call (canonical only), got %d", tx.tokenLedgerCalls)
	}
	// Verify the single entry is the canonical one.
	if len(tx.ledgerEntries) != 1 {
		t.Fatalf("expected 1 ledger entry, got %d", len(tx.ledgerEntries))
	}
	e := tx.ledgerEntries[0]
	if e.TokenizerRevision != token.CanonicalTokenizerRevision {
		t.Fatalf("expected canonical revision %q, got %q", token.CanonicalTokenizerRevision, e.TokenizerRevision)
	}
	if e.EstimationMethod != token.CharRatio {
		t.Fatalf("expected char-ratio method, got %s", e.EstimationMethod)
	}
	if e.Provider != "" || e.Model != "" {
		t.Fatalf("canonical entry should have empty provider/model, got %q/%q", e.Provider, e.Model)
	}
}

func TestAppendAssistantReplayDetectsCorruptedIdempotencyResponse(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	tx := newAssistantTx(now)
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	streamID := "stream-corrupt"
	text := "original"
	usage := AssistantUsage{Provider: "p", Model: "m", OutputTokens: 5}
	// Manually compute the digest that AppendAssistant would use.
	request := assistantRequest{SessionID: sessionID, Text: text, Usage: usage}
	body, _ := json.Marshal(request)
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	// Plant a corrupt idempotency record with correct digest but garbage response.
	tx.idempotency["message.append-assistant\x00"+streamID] = providerapp.Record{
		Operation: "message.append-assistant",
		Key:       streamID,
		Digest:    digest,
		Response:  []byte("not-json"),
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
	}
	s := newAssistantService(t, tx)
	_, err := s.AppendAssistant(context.Background(), streamID, "engine", sessionID, text, usage)
	if !errors.Is(err, ErrDataInvariantViolation) {
		t.Fatalf("expected ErrDataInvariantViolation for corrupt replay, got %v", err)
	}
}

type pageReader struct {
	items    []message.Message
	snapshot int64
	more     bool
}

func (r pageReader) ListMessages(context.Context, PageQuery) ([]message.Message, int64, bool, error) {
	return append([]message.Message(nil), r.items...), r.snapshot, r.more, nil
}

func TestCursorBindingsTamperAndBudget(t *testing.T) {
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	item := message.Message{ID: id, SessionID: id, Role: message.RoleUser, Status: message.StatusCompleted, Sequence: 1, Text: strings.Repeat("\u0001", 2048), CreatedAt: now}
	s, e := New(pageReader{items: []message.Message{item}, snapshot: 2, more: true}, nil, []byte("0123456789abcdef0123456789abcdef"))
	if e != nil {
		t.Fatal(e)
	}
	page, err := s.List(context.Background(), PageRequest{SessionID: id, Direction: Forward, Limit: 1, ByteBudget: MaxByteBudget, RequestID: id})
	if err != nil || page.NextCursor == nil || !page.HasMore {
		t.Fatalf("page = %#v, %v", page, err)
	}
	if _, err = s.List(context.Background(), PageRequest{SessionID: id, Direction: Backward, Cursor: *page.NextCursor}); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("direction binding: %v", err)
	}
	tampered := *page.NextCursor
	if tampered[len(tampered)-1] == 'A' {
		tampered = tampered[:len(tampered)-1] + "B"
	} else {
		tampered = tampered[:len(tampered)-1] + "A"
	}
	if _, err = s.List(context.Background(), PageRequest{SessionID: id, Direction: Forward, Cursor: tampered}); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("tamper: %v", err)
	}
	if _, err = s.List(context.Background(), PageRequest{SessionID: id, Direction: Forward, Limit: 1, ByteBudget: MinByteBudget, RequestID: id}); err != nil {
		t.Fatalf("escaped item should fit minimum budget: %v", err)
	}
}

func TestCursorRejectsWrongKeyAndRecomputedPublicSHA(t *testing.T) {
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	item := message.Message{ID: id, SessionID: id, Role: message.RoleUser, Status: message.StatusCompleted, Sequence: 1, Text: "x", CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
	s, _ := New(pageReader{items: []message.Message{item}, snapshot: 2, more: true}, nil, []byte("0123456789abcdef0123456789abcdef"))
	p, err := s.List(context.Background(), PageRequest{SessionID: id, Direction: Forward, Limit: 1, ByteBudget: MaxByteBudget})
	if err != nil || p.NextCursor == nil {
		t.Fatalf("cursor: %v", err)
	}
	wrong, _ := New(pageReader{}, nil, []byte("abcdef0123456789abcdef0123456789"))
	if _, err = wrong.List(context.Background(), PageRequest{SessionID: id, Direction: Forward, Cursor: *p.NextCursor}); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("wrong key: %v", err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(*p.NextCursor)
	var c map[string]any
	_ = json.Unmarshal(raw, &c)
	c["b"] = float64(2)
	old := sha256.Sum256([]byte("lunitide/message-cursor/v1\x00" + id + "\x00forward\x002\x002"))
	c["x"] = hex.EncodeToString(old[:])
	raw, _ = json.Marshal(c)
	forged := base64.RawURLEncoding.EncodeToString(raw)
	if _, err = s.List(context.Background(), PageRequest{SessionID: id, Direction: Forward, Cursor: forged}); !errors.Is(err, ErrCursorInvalid) {
		t.Fatalf("public SHA forgery: %v", err)
	}
}
