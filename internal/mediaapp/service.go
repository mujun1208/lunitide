package mediaapp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/domain/media"
	"github.com/oklog/ulid/v2"
)

const PlaybackOrigin = "https://media.lunitide.local/v1/assets/"

const (
	ticketInitialTTL = 60 * time.Second
	ticketIdleTTL    = 30 * time.Minute
	ticketHardCap    = 12 * time.Hour
	playerLeaseTTL   = 30 * time.Second
)

var ErrTicketExpired = errors.New("MEDIA_TICKET_EXPIRED")

type ticket struct {
	Token     string
	AssetID   string
	SessionID string
	Path      string
	MIME      string
	Identity  string
	Owner     string
	Created   time.Time
	Expires   time.Time
}

type watchSub struct {
	owner     string
	scopeKind string
	scopeID   string
	sessionID string
	notify    func(sessionID string, revision int64)
}

type Service struct {
	store    Store
	mu       sync.Mutex
	tickets  map[string]ticket
	paths    map[string]string
	watchers map[string]watchSub
}

func New(store Store) *Service {
	return &Service{store: store, tickets: map[string]ticket{}, paths: map[string]string{}, watchers: map[string]watchSub{}}
}

func (s *Service) SubscribeWatch(streamID, owner, scopeKind, scopeID, sessionID string, notify func(sessionID string, revision int64)) func() {
	s.mu.Lock()
	s.watchers[streamID] = watchSub{owner: owner, scopeKind: scopeKind, scopeID: scopeID, sessionID: sessionID, notify: notify}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.watchers, streamID)
		s.mu.Unlock()
	}
}

func (s *Service) publishWatch(snap media.Snapshot) {
	if snap.MediaSessionID == "" {
		return
	}
	s.mu.Lock()
	subs := make([]watchSub, 0, len(s.watchers))
	for _, sub := range s.watchers {
		subs = append(subs, sub)
	}
	s.mu.Unlock()
	for _, sub := range subs {
		if sub.owner != snap.OwnerSubjectID || sub.scopeKind != snap.ScopeKind || sub.scopeID != snap.ScopeID {
			continue
		}
		if sub.sessionID != "" && sub.sessionID != snap.MediaSessionID {
			continue
		}
		if sub.notify != nil {
			sub.notify(snap.MediaSessionID, snap.Revision)
		}
	}
}

func (s *Service) CreateSession(ctx context.Context, owner, scopeKind, scopeID, assetID, operationID string) (media.Snapshot, media.Operation, error) {
	snap, op, err := s.store.CreateMediaSession(ctx, owner, scopeKind, internalScopeID(owner, scopeKind, scopeID), "owned", assetID, operationID)
	if err == nil {
		s.publishWatch(snap)
	}
	return snap, op, err
}

func (s *Service) GetSession(ctx context.Context, owner, sessionID string) (media.Snapshot, error) {
	return s.store.GetMediaSessionForOwner(ctx, owner, sessionID)
}

func (s *Service) ListSessions(ctx context.Context, owner, scopeKind, scopeID string, limit int) ([]media.Snapshot, error) {
	return s.store.ListMediaSessions(ctx, owner, scopeKind, internalScopeID(owner, scopeKind, scopeID), limit)
}

func (s *Service) Command(ctx context.Context, owner, sessionID, action, operationID string, expectedRevision int64, positionMs, volume int) (media.Snapshot, media.Operation, error) {
	snap, op, err := s.store.ApplyMediaSessionCommand(ctx, owner, sessionID, action, operationID, expectedRevision, positionMs, volume)
	if err == nil {
		s.publishWatch(snap)
	}
	return snap, op, err
}

func (s *Service) QueueCommand(ctx context.Context, owner, sessionID, action, itemID, operationID string, beforeItemID *string, expectedQueueRevision int64) (media.Snapshot, media.Operation, error) {
	snap, op, err := s.store.ApplyMediaQueueCommand(ctx, owner, sessionID, action, itemID, operationID, beforeItemID, expectedQueueRevision)
	if err == nil {
		s.publishWatch(snap)
	}
	return snap, op, err
}

func (s *Service) ListAssets(ctx context.Context, owner, scopeKind, scopeID, sourceKind string, limit int) ([]media.Asset, error) {
	return s.store.ListMediaAssetRecords(ctx, owner, scopeKind, internalScopeID(owner, scopeKind, scopeID), sourceKind, limit)
}

func (s *Service) ListQueueAssets(ctx context.Context, owner, sessionID string) ([]media.Asset, error) {
	return s.store.ListMediaQueueAssets(ctx, owner, sessionID)
}

func (s *Service) GetOperation(ctx context.Context, owner, operationID string) (media.Operation, error) {
	return s.store.GetMediaOperationByID(ctx, owner, operationID)
}

func (s *Service) ListOperations(ctx context.Context, owner, scopeKind, scopeID string, limit int) ([]media.Operation, error) {
	return s.store.ListMediaOperations(ctx, owner, scopeKind, internalScopeID(owner, scopeKind, scopeID), limit)
}

func (s *Service) RegisterAsset(ctx context.Context, owner, scopeKind, scopeID, sourceKind, path, mime, kind, title string, size int64) (media.Asset, error) {
	if err := AuthorizeLocalAsset(sourceKind, path, kind); err != nil {
		return media.Asset{}, err
	}
	id, err := s.store.InsertMediaAsset(ctx, owner, scopeKind, internalScopeID(owner, scopeKind, scopeID), sourceKind, path, mime, kind, title, size)
	if err != nil {
		return media.Asset{}, err
	}
	s.mu.Lock()
	s.paths[id] = path
	s.mu.Unlock()
	return s.store.GetMediaAsset(ctx, owner, id)
}

func (s *Service) OpenAsset(ctx context.Context, owner, assetID, sessionID string) (playbackURL, expiresAt string, err error) {
	if _, err := s.store.GetMediaSessionForOwner(ctx, owner, sessionID); err != nil {
		return "", "", err
	}
	asset, err := s.store.GetMediaAsset(ctx, owner, assetID)
	if err != nil {
		return "", "", err
	}
	if asset.State != "ready" {
		return "", "", media.ErrAssetChanged
	}
	token, err := randomToken()
	if err != nil {
		return "", "", err
	}
	now := time.Now().UTC()
	exp := now.Add(ticketInitialTTL)
	s.mu.Lock()
	s.tickets[token] = ticket{Token: token, AssetID: asset.AssetID, SessionID: sessionID, Path: asset.SourceRef, MIME: asset.MIME, Identity: asset.FileIdentity, Owner: owner, Created: now, Expires: exp}
	s.mu.Unlock()
	return PlaybackOrigin + token, exp.Format(time.RFC3339), nil
}

func (s *Service) ResolveTicket(token, owner string) (path, mime string, err error) {
	s.mu.Lock()
	row, ok := s.tickets[token]
	now := time.Now().UTC()
	if !ok || now.After(row.Expires) || now.After(row.Created.Add(ticketHardCap)) {
		if ok {
			delete(s.tickets, token)
		}
		s.mu.Unlock()
		return "", "", ErrTicketExpired
	}
	if owner != "" && row.Owner != owner {
		s.mu.Unlock()
		return "", "", media.ErrScopeDenied
	}
	next := now.Add(ticketIdleTTL)
	hard := row.Created.Add(ticketHardCap)
	if next.After(hard) {
		next = hard
	}
	row.Expires = next
	s.tickets[token] = row
	s.mu.Unlock()
	if err := s.verifyLiveIdentity(row); err != nil {
		return "", "", err
	}
	return row.Path, row.MIME, nil
}

func (s *Service) verifyLiveIdentity(row ticket) error {
	if row.Identity == "" {
		return nil
	}
	info, err := os.Stat(row.Path)
	if err != nil {
		if s.store != nil && row.AssetID != "" {
			_ = s.store.MarkMediaAssetState(context.Background(), row.Owner, row.AssetID, "missing")
		}
		if os.IsNotExist(err) {
			return media.ErrAssetNotFound
		}
		return err
	}
	live := media.FileIdentity(info.Size(), info.ModTime().UnixNano())
	if live != row.Identity {
		if s.store != nil && row.AssetID != "" {
			_ = s.store.MarkMediaAssetState(context.Background(), row.Owner, row.AssetID, "changed")
		}
		return media.ErrAssetChanged
	}
	return nil
}

func (s *Service) AttachPlayer(ctx context.Context, owner, sessionID, windowInstanceID string, navigationEpoch int64) (token string, generation int64, expiresAt string, err error) {
	if _, err := s.store.GetMediaSessionForOwner(ctx, owner, sessionID); err != nil {
		return "", 0, "", err
	}
	token, err = randomToken()
	if err != nil {
		return "", 0, "", err
	}
	expiresAt = time.Now().UTC().Add(playerLeaseTTL).Format(time.RFC3339Nano)
	digest := sha256Hex(token)
	generation, err = s.store.AttachMediaPlayerLease(ctx, sessionID, windowInstanceID, navigationEpoch, digest, expiresAt)
	if err != nil {
		return "", 0, "", err
	}
	return token, generation, expiresAt, nil
}

func (s *Service) NextPlayerCommand(ctx context.Context, owner, sessionID, windowInstanceID, leaseToken string, generation int64) (operationID, desired string, err error) {
	if _, err := s.store.GetMediaSessionForOwner(ctx, owner, sessionID); err != nil {
		return "", "", err
	}
	if err := s.store.VerifyMediaPlayerLease(ctx, sessionID, windowInstanceID, leaseToken, generation); err != nil {
		return "", "", err
	}
	return s.store.NextMediaPlayerCommand(ctx, sessionID, generation)
}

func (s *Service) ReportPlayer(ctx context.Context, owner, sessionID, windowInstanceID, leaseToken, operationID, event string, generation, positionMs, durationMs int64) error {
	if err := s.store.VerifyMediaPlayerLease(ctx, sessionID, windowInstanceID, leaseToken, generation); err != nil {
		return err
	}
	if err := s.store.AckMediaPlayerCommandForOwner(ctx, owner, sessionID, operationID, event, positionMs, durationMs); err != nil {
		return err
	}
	if snap, err := s.store.GetMediaSessionForOwner(ctx, owner, sessionID); err == nil {
		s.publishWatch(snap)
	}
	return nil
}

func (s *Service) ReplaceQueue(ctx context.Context, owner, scopeKind, scopeID, sessionID string, expectedQueueRevision int64, assetIDs []string) (int64, error) {
	return s.store.ReplaceMediaQueue(ctx, owner, scopeKind, internalScopeID(owner, scopeKind, scopeID), sessionID, expectedQueueRevision, assetIDs)
}

func internalScopeID(owner, kind, scopeID string) string {
	if kind == "user" {
		return owner
	}
	return scopeID
}

func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func sha256Hex(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func NewULID() string { return ulid.Make().String() }
