package messageapp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrIdempotencyKeyRequired     = errors.New("idempotency key is required")
	ErrIdempotencyConflict        = errors.New("idempotency key reused with different request")
	ErrSessionNotFound            = errors.New("session not found")
	ErrMessageStorageQuotaReached = errors.New("message storage quota reached")
	ErrDataInvariantViolation     = errors.New("message data invariant violation")
	ErrCursorInvalid              = errors.New("message cursor invalid")
	ErrPageBudgetTooSmall         = errors.New("message page byte budget too small")
	ErrAssistantResponseTooLarge  = errors.New("assistant response too large")
	ErrMessageNotFound            = errors.New("message not found")
	ErrRewindRequiresUserMessage  = errors.New("rewind requires user message")
)

type Searcher interface {
	SearchMessages(context.Context, SearchQuery) ([]SearchHit, error)
}

const (
	DefaultSearchLimit    = 16
	MaxSearchLimit        = 32
	MaxSearchQueryRunes   = 200
	MaxSearchSnippetRunes = 180
)

type SearchQuery struct {
	Query, ProjectID, SessionID string
	Limit                       int
}

type SearchHit struct {
	SessionID    string `json:"sessionId"`
	MessageID    string `json:"messageId"`
	Role         string `json:"role"`
	Sequence     int64  `json:"sequence"`
	Snippet      string `json:"snippet"`
	SessionTitle string `json:"sessionTitle"`
}

type SearchRequest struct {
	Query, ProjectID, SessionID string
	Limit                       int
}

type SearchResult struct {
	Items []SearchHit `json:"items"`
}

type Tx interface {
	AppendMessage(context.Context, message.Message) (message.Message, error)
	Message(context.Context, string) (message.Message, error)
	Idempotency(context.Context, string, string, time.Time) (providerapp.Record, bool, error)
	PutIdempotency(context.Context, providerapp.Record) error
	PutAudit(context.Context, providerapp.Audit) error
	PutTokenLedgerEntry(context.Context, token.LedgerEntry) error
}
type rewindTx interface {
	RewindMessages(context.Context, string, string) (RewindResult, error)
}
type UnitOfWork interface {
	DoMessage(context.Context, func(Tx) error) error
}
type Reader interface {
	ListMessages(context.Context, PageQuery) ([]message.Message, int64, bool, error)
}
type historyRevisionReader interface {
	MessageHistoryRevision(context.Context, string) (int64, error)
}
type Direction string

const (
	Forward           Direction = "forward"
	Backward          Direction = "backward"
	DefaultLimit                = 64
	DefaultByteBudget           = 131072
	MinByteBudget               = 16384
	MaxByteBudget               = 245760
)

type PageRequest struct {
	SessionID         string
	Cursor            string
	Direction         Direction
	Limit, ByteBudget int
	RequestID         string
}
type PageQuery struct {
	SessionID          string
	Direction          Direction
	Snapshot, Boundary int64
	Limit              int
	Revision           int64
}
type Page struct {
	Items            []message.Message `json:"items"`
	HasMore          bool              `json:"hasMore"`
	NextCursor       *string           `json:"nextCursor"`
	SnapshotSequence int64             `json:"snapshotSequence"`
}
type RewindResult struct {
	SessionID       string `json:"sessionId"`
	MessageID       string `json:"messageId"`
	DeletedCount    int64  `json:"deletedCount"`
	LastSequence    int64  `json:"lastSequence"`
	HistoryRevision int64  `json:"historyRevision"`
}
type cursorV1 struct {
	Version   int       `json:"v"`
	SessionID string    `json:"s"`
	Direction Direction `json:"d"`
	Snapshot  int64     `json:"h"`
	Boundary  int64     `json:"b"`
	Digest    string    `json:"x"`
	Revision  int64     `json:"r"`
}

const CursorKeySize = 32

func DeriveCursorKey(secret []byte) []byte {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte("lunitide/message-cursor/v1"))
	return h.Sum(nil)
}
func cursorMAC(key []byte, c cursorV1) []byte {
	h := hmac.New(sha256.New, key)
	b, _ := json.Marshal(struct {
		Version   int       `json:"v"`
		SessionID string    `json:"s"`
		Direction Direction `json:"d"`
		Snapshot  int64     `json:"h"`
		Boundary  int64     `json:"b"`
		Revision  int64     `json:"r"`
	}{c.Version, c.SessionID, c.Direction, c.Snapshot, c.Boundary, c.Revision})
	_, _ = h.Write(b)
	return h.Sum(nil)
}
func encodeCursor(key []byte, c cursorV1) string {
	c.Version = 1
	c.Digest = hex.EncodeToString(cursorMAC(key, c))
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(key []byte, raw string) (cursorV1, error) {
	var c cursorV1
	b, e := base64.RawURLEncoding.Strict().DecodeString(raw)
	if e != nil {
		return c, ErrCursorInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(&struct{}{}) != io.EOF {
		return c, ErrCursorInvalid
	}
	tag, tagErr := hex.DecodeString(c.Digest)
	if c.Version != 1 || !message.CanonicalULID(c.SessionID) || (c.Direction != Forward && c.Direction != Backward) || c.Snapshot < 0 || c.Snapshot > message.MaxSafeSequence || c.Boundary < 0 || c.Boundary > c.Snapshot || c.Revision < 0 || tagErr != nil || len(tag) != sha256.Size || !hmac.Equal(tag, cursorMAC(key, c)) {
		return c, ErrCursorInvalid
	}
	return c, nil
}

type Service struct {
	read      Reader
	uow       UnitOfWork
	now       func() time.Time
	cursorKey [CursorKeySize]byte
}

func New(r Reader, u UnitOfWork, key []byte) (*Service, error) {
	if len(key) != CursorKeySize {
		return nil, errors.New("message cursor key must be exactly 32 bytes")
	}
	s := &Service{read: r, uow: u, now: func() time.Time { return time.Now().UTC() }}
	copy(s.cursorKey[:], key)
	return s, nil
}
func available(v any) bool {
	if v == nil {
		return false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !rv.IsNil()
	}
	return true
}

func decodeReplay(raw []byte, result any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing idempotency response data")
	}
	return nil
}
func (s *Service) List(ctx context.Context, r PageRequest) (Page, error) {
	if s == nil || !available(s.read) {
		return Page{}, errors.New("message reader unavailable")
	}
	if r.Direction == "" {
		r.Direction = Forward
	}
	if r.Limit == 0 {
		r.Limit = DefaultLimit
	}
	if r.ByteBudget == 0 {
		r.ByteBudget = DefaultByteBudget
	}
	q := PageQuery{SessionID: r.SessionID, Direction: r.Direction, Limit: r.Limit}
	var cursorRevision *int64
	if r.Cursor != "" {
		c, e := decodeCursor(s.cursorKey[:], r.Cursor)
		if e != nil || c.SessionID != r.SessionID || c.Direction != r.Direction {
			return Page{}, ErrCursorInvalid
		}
		cursorRevision = &c.Revision
		q.Snapshot, q.Boundary = c.Snapshot, c.Boundary
	}
	var revision int64
	if reader, ok := s.read.(historyRevisionReader); ok {
		var revErr error
		revision, revErr = reader.MessageHistoryRevision(ctx, r.SessionID)
		if revErr != nil {
			return Page{}, revErr
		}
	}
	if cursorRevision != nil && *cursorRevision != revision {
		return Page{}, ErrCursorInvalid
	}
	q.Revision = revision
	items, snapshot, more, e := s.read.ListMessages(ctx, q)
	if e != nil {
		return Page{}, e
	}
	p := Page{Items: items, HasMore: more, SnapshotSequence: snapshot}
	for {
		if p.HasMore && len(p.Items) > 0 {
			b := p.Items[len(p.Items)-1].Sequence
			c := encodeCursor(s.cursorKey[:], cursorV1{SessionID: r.SessionID, Direction: r.Direction, Snapshot: snapshot, Boundary: b, Revision: revision})
			p.NextCursor = &c
		} else {
			p.NextCursor = nil
		}
		envelope := struct {
			Version   string `json:"v"`
			Kind      string `json:"kind"`
			ID        string `json:"id"`
			RequestID string `json:"requestId"`
			OK        bool   `json:"ok"`
			Payload   Page   `json:"payload"`
		}{"1.0", "response", "00000000000000000000000000", r.RequestID, true, p}
		raw, _ := json.Marshal(envelope)
		if len(raw) < r.ByteBudget {
			break
		}
		if len(p.Items) == 0 {
			return Page{}, ErrPageBudgetTooSmall
		}
		p.Items = p.Items[:len(p.Items)-1]
		p.HasMore = true
	}
	if len(p.Items) == 0 && p.HasMore {
		return Page{}, ErrPageBudgetTooSmall
	}
	return p, nil
}

func (s *Service) Search(ctx context.Context, q SearchRequest) (SearchResult, error) {
	if s == nil || !available(s.read) {
		return SearchResult{}, errors.New("message reader unavailable")
	}
	searcher, ok := s.read.(Searcher)
	if !ok {
		return SearchResult{Items: []SearchHit{}}, nil
	}
	query := strings.TrimSpace(q.Query)
	if query == "" {
		return SearchResult{Items: []SearchHit{}}, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	hits, err := searcher.SearchMessages(ctx, SearchQuery{Query: query, ProjectID: q.ProjectID, SessionID: q.SessionID, Limit: limit})
	if err != nil {
		return SearchResult{}, err
	}
	if hits == nil {
		hits = []SearchHit{}
	}
	return SearchResult{Items: hits}, nil
}
func (s *Service) Append(ctx context.Context, key, actor string, request any, value message.Message) (message.Message, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return message.Message{}, ErrIdempotencyKeyRequired
	}
	if s == nil || !available(s.uow) {
		return message.Message{}, errors.New("message unit of work unavailable")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return message.Message{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var result message.Message
	err = s.uow.DoMessage(ctx, func(tx Tx) error {
		now := s.now().UTC()
		record, found, e := tx.Idempotency(ctx, "message.append", key, now)
		if e != nil {
			return e
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			if decodeReplay(record.Response, &result) != nil || result.Validate() != nil || result.SessionID != value.SessionID || result.Text != value.Text {
				return ErrDataInvariantViolation
			}
			authoritative, getErr := tx.Message(ctx, result.ID)
			if getErr != nil || authoritative != result {
				return ErrDataInvariantViolation
			}
			return nil
		}
		result, e = tx.AppendMessage(ctx, value)
		if e != nil {
			return e
		}
		response, e := json.Marshal(result)
		if e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"sessionId": result.SessionID, "sequence": result.Sequence})
		eventSum := sha256.Sum256([]byte("message-audit\x00" + digest + "\x00" + result.ID))
		var event ulid.ULID
		copy(event[:], eventSum[:16])
		if e = tx.PutAudit(ctx, providerapp.Audit{ID: event.String(), Action: "message.appended", AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); e != nil {
			return e
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "message.append", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

func (s *Service) Rewind(ctx context.Context, key, actor, sessionID, messageID string) (RewindResult, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return RewindResult{}, ErrIdempotencyKeyRequired
	}
	body, _ := json.Marshal(struct{ SessionID, MessageID string }{sessionID, messageID})
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var result RewindResult
	err := s.uow.DoMessage(ctx, func(tx Tx) error {
		now := s.now().UTC()
		record, found, err := tx.Idempotency(ctx, "message.rewind", key, now)
		if err != nil {
			return err
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return decodeReplay(record.Response, &result)
		}
		rewinder, ok := tx.(rewindTx)
		if !ok {
			return ErrDataInvariantViolation
		}
		result, err = rewinder.RewindMessages(ctx, sessionID, messageID)
		if err != nil {
			return err
		}
		response, _ := json.Marshal(result)
		meta, _ := json.Marshal(result)
		if err = tx.PutAudit(ctx, providerapp.Audit{ID: ulid.Make().String(), Action: "message.rewound", AggregateID: messageID, Actor: actor, Metadata: meta, CreatedAt: now}); err != nil {
			return err
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "message.rewind", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

// AssistantUsage carries provider-reported token usage for an assistant message.
type AssistantUsage struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	OutputTokens int64  `json:"outputTokens"`
}

// assistantRequest is the idempotency digest input for AppendAssistant.
type assistantRequest struct {
	SessionID string         `json:"sessionId"`
	Text      string         `json:"text"`
	Usage     AssistantUsage `json:"usage"`
}

// AppendAssistant durably persists a completed assistant response with
// provider-reported token usage. The streamID serves as the idempotency key:
// if the stream completes but the durable write fails, a retry with the same
// streamID replays the original result. A different request with the same
// streamID returns ErrIdempotencyConflict.
func (s *Service) AppendAssistant(ctx context.Context, streamID, actor, sessionID, text string, usage AssistantUsage) (message.Message, error) {
	if !providerapp.ValidIdempotencyKey(streamID) {
		return message.Message{}, ErrIdempotencyKeyRequired
	}
	if s == nil || !available(s.uow) {
		return message.Message{}, errors.New("message unit of work unavailable")
	}
	// Pre-validate assistant text size before entering the transaction.
	normalized, err := message.NormalizeAssistantText(text)
	if err != nil {
		return message.Message{}, ErrAssistantResponseTooLarge
	}
	// Build the idempotency request digest from the semantic inputs.
	request := assistantRequest{SessionID: sessionID, Text: normalized, Usage: usage}
	body, err := json.Marshal(request)
	if err != nil {
		return message.Message{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var result message.Message
	err = s.uow.DoMessage(ctx, func(tx Tx) error {
		now := s.now().UTC()
		record, found, e := tx.Idempotency(ctx, "message.append-assistant", streamID, now)
		if e != nil {
			return e
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			if decodeReplay(record.Response, &result) != nil || result.Validate() != nil || result.SessionID != sessionID || result.Text != normalized {
				return ErrDataInvariantViolation
			}
			authoritative, getErr := tx.Message(ctx, result.ID)
			if getErr != nil || authoritative != result {
				return ErrDataInvariantViolation
			}
			return nil
		}
		// Build the assistant message for storage. AppendMessage allocates
		// ID, sequence, and created_at; role and status are set here.
		value := message.Message{
			SessionID: sessionID,
			Role:      message.RoleAssistant,
			Status:    message.StatusCompleted,
			Text:      normalized,
		}
		result, e = tx.AppendMessage(ctx, value)
		if e != nil {
			return e
		}
		// Always write a canonical token ledger entry for the assistant
		// message. The canonical tokenizer is the stable capacity gauge
		// (architecture doc §12.1.1): it persists tokenizer ID/version so
		// that context.status can report canonicalTokenizerVersion and the
		// ledger can be queried by the frozen revision. This entry uses
		// empty provider/model to distinguish it from provider-specific
		// entries.
		canonicalEntry := token.LedgerEntry{
			ID:                ulid.Make().String(),
			MessageID:         result.ID,
			Provider:          "",
			Model:             "",
			TokenizerID:       token.CanonicalTokenizerID,
			TokenizerRevision: token.CanonicalTokenizerRevision,
			TokenCount:        token.EstimateTokens(normalized),
			EstimationMethod:  token.CharRatio,
			UTF8Bytes:         int64(len(normalized)),
			ComputedAt:        now,
		}
		if e = tx.PutTokenLedgerEntry(ctx, canonicalEntry); e != nil {
			return e
		}
		// Write provider-reported assistant output tokens when the gateway
		// reports them. Request totals include input/history and therefore must
		// never be attributed to this single assistant message.
		if usage.OutputTokens > 0 {
			providerEntry := token.LedgerEntry{
				ID:                ulid.Make().String(),
				MessageID:         result.ID,
				Provider:          usage.Provider,
				Model:             usage.Model,
				TokenizerID:       token.ProviderReportTokenizerID,
				TokenizerRevision: token.ProviderReportTokenizerRevision,
				TokenCount:        usage.OutputTokens,
				EstimationMethod:  token.ProviderReport,
				UTF8Bytes:         int64(len(normalized)),
				ComputedAt:        now,
			}
			if e = tx.PutTokenLedgerEntry(ctx, providerEntry); e != nil {
				return e
			}
		}
		// Persist the audit event and idempotency record.
		response, e := json.Marshal(result)
		if e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"sessionId": result.SessionID, "sequence": result.Sequence, "streamId": streamID})
		eventSum := sha256.Sum256([]byte("message-audit\x00" + digest + "\x00" + result.ID))
		var event ulid.ULID
		copy(event[:], eventSum[:16])
		if e = tx.PutAudit(ctx, providerapp.Audit{ID: event.String(), Action: "message.assistant.appended", AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); e != nil {
			return e
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "message.append-assistant", Key: streamID, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
