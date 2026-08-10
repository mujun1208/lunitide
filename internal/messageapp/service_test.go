package messageapp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
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
