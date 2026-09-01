// Package people is the this-PC WeChat-style contact and thread store.
// LAN discovery is optional and off by default. Discovered peers are not
// trusted until paired. File offers are never auto-accepted. This package
// never exposes computer-control to a remote machine.
package people

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/identity"
	"github.com/oklog/ulid/v2"
)

var (
	ErrUnavailable  = errors.New("people unavailable")
	ErrNotFound     = errors.New("people not found")
	ErrPairing      = errors.New("people pairing rejected")
	ErrNotTrusted   = errors.New("people peer is not trusted")
	ErrInvalid      = errors.New("people request invalid")
	ErrOfferDecided = errors.New("people file offer already decided")
	ErrTooLarge     = errors.New("people payload too large")
	ErrBlocked      = errors.New("people peer is blocked")
	ErrUnreachable  = errors.New("people peer unreachable")
	ErrCanceled     = errors.New("people picker canceled")
	ErrUnsupported  = errors.New("people picker unsupported")
	ErrOpenFailed   = errors.New("people file open failed")
	ErrLocked       = identity.ErrLocked
	ErrSelfChat     = errors.New("people cannot chat with self")
)

const (
	maxText         = 16384
	maxFileBytes    = 32 << 20
	maxMembers      = 32
	maxList         = 200
	maxMessages     = 200
	maxPreviewBytes = 4 << 20
	chunkSize       = 48 << 10
	defaultTCP      = 36422
	typingTTL       = 4 * time.Second
)

type Contact struct {
	SubjectID   string `json:"subjectId"`
	Nickname    string `json:"nickname"`
	Avatar      string `json:"avatar"`
	Status      string `json:"status"`
	Department  string `json:"department"`
	Title       string `json:"title"`
	OrgName     string `json:"orgName"`
	Bio         string `json:"bio"`
	PublicKey   string `json:"publicKey"`
	PairingHash string `json:"pairingHash,omitempty"`
	TrustState  string `json:"trustState"`
	HostAddr    string `json:"hostAddr"`
	LastSeenAt  string `json:"lastSeenAt"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Remark      string `json:"remark,omitempty"`
	Blocked     bool   `json:"blocked"`
	LastReadAt  string `json:"lastReadAt,omitempty"`
	Self        bool   `json:"self"`
}

type Thread struct {
	ThreadID         string    `json:"threadId"`
	Kind             string    `json:"kind"`
	Title            string    `json:"title"`
	OwnerID          string    `json:"ownerSubjectId"`
	Members          []Contact `json:"members"`
	LastMessage      *Message  `json:"lastMessage,omitempty"`
	UnreadCount      int       `json:"unreadCount"`
	TypingSubjectIDs []string  `json:"typingSubjectIds,omitempty"`
	UpdatedAt        string    `json:"updatedAt"`
	CreatedAt        string    `json:"createdAt"`
}

type Message struct {
	MessageID       string `json:"messageId"`
	ThreadID        string `json:"threadId"`
	SenderID        string `json:"senderSubjectId"`
	Kind            string `json:"kind"`
	Body            string `json:"body"`
	FileName        string `json:"fileName"`
	FileMIME        string `json:"fileMime"`
	FileSize        int64  `json:"fileSize"`
	FileSHA256      string `json:"fileSha256"`
	OfferID         string `json:"offerId,omitempty"`
	OfferStatus     string `json:"offerStatus,omitempty"`
	DestPath        string `json:"destPath,omitempty"`
	TransferPercent int    `json:"transferPercent,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

type FileOffer struct {
	OfferID     string `json:"offerId"`
	MessageID   string `json:"messageId"`
	ThreadID    string `json:"threadId"`
	FromID      string `json:"fromSubjectId"`
	ToID        string `json:"toSubjectId"`
	Status      string `json:"status"`
	FileName    string `json:"fileName"`
	FileMIME    string `json:"fileMime"`
	FileSize    int64  `json:"fileSize"`
	FileSHA256  string `json:"fileSha256"`
	StagingPath string `json:"-"`
	DestPath    string `json:"destPath"`
	CreatedAt   string `json:"createdAt"`
	DecidedAt   string `json:"decidedAt"`
}

type Beacon struct {
	V           int    `json:"v"`
	Kind        string `json:"kind"`
	SubjectID   string `json:"subjectId"`
	Nickname    string `json:"nickname"`
	Department  string `json:"department"`
	Title       string `json:"title"`
	OrgName     string `json:"orgName"`
	Status      string `json:"status"`
	PublicKey   string `json:"publicKey"`
	PairingHash string `json:"pairingHash"`
	Port        int    `json:"port,omitempty"`
}

type PairInput struct {
	PairingCode string
	SubjectID   string
	Nickname    string
	PublicKey   string
	Department  string
	Title       string
	OrgName     string
	PairingHash string
}

type SendInput struct {
	ThreadID      string
	Kind          string
	Body          string
	FileName      string
	FileMIME      string
	ContentBase64 string
	LocalPath     string
}

type ContactPatch struct {
	Remark  *string
	Blocked *bool
}

type StageInput struct {
	UploadID      string
	FileName      string
	FileMIME      string
	Index         int
	Last          bool
	ContentBase64 string
}

type StageResult struct {
	Ready     bool   `json:"ready"`
	LocalPath string `json:"localPath"`
	Bytes     int64  `json:"bytes"`
}

type PickResult struct {
	Path      string `json:"path"`
	FileName  string `json:"fileName"`
	Size      int64  `json:"size"`
	Directory bool   `json:"directory"`
}

type Store interface {
	ListContacts(ctx context.Context) ([]Contact, error)
	UpsertContact(ctx context.Context, c Contact) error
	GetContact(ctx context.Context, subjectID string) (Contact, error)
	ListThreads(ctx context.Context, selfID string) ([]Thread, error)
	GetThread(ctx context.Context, threadID string) (Thread, error)
	FindDirectThread(ctx context.Context, a, b string) (Thread, bool, error)
	InsertThread(ctx context.Context, t Thread, memberIDs []string, ownerID string) error
	ListPeopleMessages(ctx context.Context, threadID string, limit int) ([]Message, error)
	InsertMessage(ctx context.Context, m Message, offer *FileOffer) error
	HasPeopleMessage(ctx context.Context, messageID string) (bool, error)
	GetOffer(ctx context.Context, offerID string) (FileOffer, error)
	GetOfferByMessage(ctx context.Context, messageID string) (FileOffer, error)
	DecideOffer(ctx context.Context, offerID, status, destPath, decidedAt string) error
	MarkThreadRead(ctx context.Context, threadID, subjectID, at string) error
	CountUnread(ctx context.Context, threadID, subjectID string) (int, error)
	ThreadSession(ctx context.Context, threadID string) (string, bool, error)
	BindThreadSession(ctx context.Context, threadID, sessionID, createdAt string) error
	ClearThreadSession(ctx context.Context, threadID string) error
	ListBoundSessionIDs(ctx context.Context) ([]string, error)
}

type Identity interface {
	SubjectID() string
	Locked() bool
	Public() identity.Public
	PairingHash() string
	Snapshot() identity.Record
	Sign(message []byte) ([]byte, error)
	SetDiscovery(ctx context.Context, enabled bool) (identity.Public, error)
}

type Service struct {
	store      Store
	identity   Identity
	receiveDir string
	stagingDir string
	lan        *LAN
	mu         sync.Mutex
	bind       string
	tcpLn      net.Listener
	tcpPort    int
	typing     map[string]map[string]time.Time
	progress   map[string]int
	uploads    map[string]*fileUpload
	incoming   map[string]*incomingFile
}

func New(store Store, ident Identity, receiveDir, stagingDir string) *Service {
	return &Service{
		store: store, identity: ident, receiveDir: receiveDir, stagingDir: stagingDir, lan: NewLAN(),
		bind: ":" + strconv.Itoa(defaultTCP), tcpPort: defaultTCP,
		typing: map[string]map[string]time.Time{}, progress: map[string]int{},
		uploads: map[string]*fileUpload{}, incoming: map[string]*incomingFile{},
	}
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.stopTCP()
	if s.lan != nil {
		s.lan.Stop()
	}
}

func (s *Service) StartDiscoveryIfEnabled() {
	s.RefreshPresence()
}

func (s *Service) RefreshPresence() {
	if s == nil || s.identity == nil {
		return
	}
	if s.identity.Public().DiscoveryEnabled {
		_ = s.startLAN()
		return
	}
	if s.lan != nil {
		s.lan.Stop()
	}
}

func (s *Service) CurrentBeacon() (Beacon, bool) {
	if s == nil || s.identity == nil {
		return Beacon{}, false
	}
	pub := s.identity.Public()
	if pub.Status == identity.StatusInvisible {
		return Beacon{}, false
	}
	return Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: pub.SubjectID, Nickname: pub.Nickname,
		Department: pub.Department, Title: pub.Title, OrgName: pub.OrgName, Status: string(pub.Status),
		PublicKey: pub.PublicKey, PairingHash: s.identity.PairingHash(), Port: s.advertisedPort(),
	}, true
}

func (s *Service) List(ctx context.Context) ([]Contact, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	items, err := s.store.ListContacts(ctx)
	if err != nil {
		return nil, err
	}
	self := s.identity.SubjectID()
	for i := range items {
		items[i].Self = items[i].SubjectID == self
		if items[i].TrustState != "discovered" {
			items[i].PairingHash = ""
		}
	}
	return items, nil
}

func (s *Service) Pair(ctx context.Context, in PairInput) (Contact, error) {
	if err := s.readyUnlocked(); err != nil {
		return Contact{}, err
	}
	code := strings.TrimSpace(in.PairingCode)
	if len(code) != 6 {
		return Contact{}, ErrPairing
	}
	self := s.identity.Public()
	now := nowRFC3339()
	c := Contact{
		SubjectID:  strings.TrimSpace(in.SubjectID),
		Nickname:   strings.TrimSpace(in.Nickname),
		PublicKey:  strings.ToLower(strings.TrimSpace(in.PublicKey)),
		Department: in.Department,
		Title:      in.Title,
		OrgName:    in.OrgName,
		TrustState: "trusted",
		Status:     "offline",
		LastSeenAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if c.SubjectID != "" {
		if existing, err := s.store.GetContact(ctx, c.SubjectID); err == nil {
			if existing.TrustState == "self" {
				return Contact{}, ErrInvalid
			}
			if existing.PairingHash != "" && identity.PairingHash(code, existing.SubjectID) != existing.PairingHash {
				return Contact{}, ErrPairing
			}
			if existing.TrustState == "discovered" && existing.PairingHash == "" {
				return Contact{}, ErrPairing
			}
			if existing.TrustState != "discovered" && identity.PairingHash(code, self.SubjectID) != s.identity.PairingHash() {
				return Contact{}, ErrPairing
			}
			existing.TrustState = "trusted"
			if c.Nickname != "" {
				existing.Nickname = c.Nickname
			}
			if c.PublicKey != "" {
				existing.PublicKey = c.PublicKey
			}
			existing.UpdatedAt = now
			if err := s.store.UpsertContact(ctx, existing); err != nil {
				return Contact{}, err
			}
			_ = s.ensureTCP()
			existing.PairingHash = ""
			return existing, nil
		}
	}
	if identity.PairingHash(code, self.SubjectID) != s.identity.PairingHash() {
		return Contact{}, ErrPairing
	}
	if c.SubjectID == "" {
		c.SubjectID = ulid.Make().String()
	}
	if c.Nickname == "" {
		c.Nickname = "同事"
	}
	if utf8.RuneCountInString(c.Nickname) > 64 {
		return Contact{}, ErrInvalid
	}
	if err := s.store.UpsertContact(ctx, c); err != nil {
		return Contact{}, err
	}
	c.PairingHash = ""
	return c, nil
}

func (s *Service) IngestBeacon(ctx context.Context, b Beacon, host string) error {
	if err := s.ready(); err != nil {
		return err
	}
	if b.V != 1 || b.Kind != "lunitide-people" || b.SubjectID == "" || b.SubjectID == s.identity.SubjectID() {
		return nil
	}
	if b.Status == string(identity.StatusInvisible) {
		return nil
	}
	existing, err := s.store.GetContact(ctx, b.SubjectID)
	now := nowRFC3339()
	addr := joinHostPort(host, b.Port)
	if err == nil {
		if existing.TrustState == "self" {
			return nil
		}
		if existing.Blocked {
			return nil
		}
		existing.Nickname = nonempty(b.Nickname, existing.Nickname)
		existing.Department = b.Department
		existing.Title = b.Title
		existing.OrgName = b.OrgName
		if b.Status != "" {
			existing.Status = b.Status
		}
		if b.PublicKey != "" {
			existing.PublicKey = b.PublicKey
		}
		if b.PairingHash != "" {
			existing.PairingHash = b.PairingHash
		}
		if addr != "" {
			existing.HostAddr = addr
		}
		existing.LastSeenAt = now
		existing.UpdatedAt = now
		return s.store.UpsertContact(ctx, existing)
	}
	nick := strings.TrimSpace(b.Nickname)
	if nick == "" {
		nick = "同事"
	}
	return s.store.UpsertContact(ctx, Contact{
		SubjectID:   b.SubjectID,
		Nickname:    nick,
		Status:      nonempty(b.Status, "online"),
		Department:  b.Department,
		Title:       b.Title,
		OrgName:     b.OrgName,
		PublicKey:   b.PublicKey,
		PairingHash: b.PairingHash,
		TrustState:  "discovered",
		HostAddr:    addr,
		LastSeenAt:  now,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

func (s *Service) stampThreadMembers(t *Thread) {
	if s == nil || t == nil || s.identity == nil {
		return
	}
	self := s.identity.SubjectID()
	for i := range t.Members {
		t.Members[i].Self = t.Members[i].SubjectID == self
	}
}

func collapseListedDirects(items []Thread) []Thread {
	seen := map[string]bool{}
	out := make([]Thread, 0, len(items))
	for _, item := range items {
		if item.Kind != "direct" {
			out = append(out, item)
			continue
		}
		peer := ""
		for _, member := range item.Members {
			if !member.Self {
				peer = member.SubjectID
				break
			}
		}
		if peer == "" || seen[peer] {
			continue
		}
		seen[peer] = true
		out = append(out, item)
	}
	return out
}

func (s *Service) DiscoveryGet() (enabled bool, pairingCode string) {
	if s == nil || s.identity == nil {
		return false, ""
	}
	pub := s.identity.Public()
	return pub.DiscoveryEnabled, pub.PairingCode
}

func (s *Service) DiscoverySet(ctx context.Context, enabled bool) (identity.Public, error) {
	if err := s.readyUnlocked(); err != nil {
		return identity.Public{}, err
	}
	pub, err := s.identity.SetDiscovery(ctx, enabled)
	if err != nil {
		return identity.Public{}, err
	}
	if enabled {
		if err := s.startLAN(); err != nil {
			_, _ = s.identity.SetDiscovery(ctx, false)
			return identity.Public{}, err
		}
	} else if s.lan != nil {
		s.lan.Stop()
	}
	return pub, nil
}

func (s *Service) ListThreads(ctx context.Context) ([]Thread, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	items, err := s.store.ListThreads(ctx, s.identity.SubjectID())
	if err != nil {
		return nil, err
	}
	self := s.identity.SubjectID()
	for i := range items {
		s.stampThreadMembers(&items[i])
		n, countErr := s.store.CountUnread(ctx, items[i].ThreadID, self)
		if countErr != nil {
			return nil, countErr
		}
		items[i].UnreadCount = n
		items[i].TypingSubjectIDs = s.typingFor(items[i].ThreadID)
		if items[i].LastMessage != nil {
			items[i].LastMessage.TransferPercent = s.progressFor(items[i].LastMessage.OfferID)
		}
	}
	return collapseListedDirects(items), nil
}

func (s *Service) OpenDirect(ctx context.Context, peerSubjectID string) (Thread, []Message, error) {
	if err := s.readyUnlocked(); err != nil {
		return Thread{}, nil, err
	}
	self := s.identity.SubjectID()
	peerSubjectID = strings.TrimSpace(peerSubjectID)
	if peerSubjectID == "" || peerSubjectID == self {
		return Thread{}, nil, ErrSelfChat
	}
	if _, err := s.store.GetContact(ctx, peerSubjectID); err != nil {
		return Thread{}, nil, ErrNotFound
	}
	if t, ok, err := s.store.FindDirectThread(ctx, self, peerSubjectID); err != nil {
		return Thread{}, nil, err
	} else if ok {
		return s.openExisting(ctx, t)
	}
	now := nowRFC3339()
	t := Thread{ThreadID: ulid.Make().String(), Kind: "direct", OwnerID: self, CreatedAt: now, UpdatedAt: now}
	members := []string{self, peerSubjectID}
	if err := s.store.InsertThread(ctx, t, members, self); err != nil {
		return Thread{}, nil, err
	}
	opened, err := s.store.GetThread(ctx, t.ThreadID)
	if err != nil {
		return Thread{}, nil, err
	}
	go s.deliverThread(opened)
	return s.openExisting(ctx, opened)
}

func (s *Service) OpenThread(ctx context.Context, threadID string) (Thread, []Message, error) {
	if err := s.ready(); err != nil {
		return Thread{}, nil, err
	}
	t, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return Thread{}, nil, err
	}
	return s.openExisting(ctx, t)
}

func (s *Service) ListMessages(ctx context.Context, threadID string, limit int) ([]Message, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > maxMessages {
		limit = maxMessages
	}
	return s.store.ListPeopleMessages(ctx, threadID, limit)
}

func (s *Service) PeekThread(ctx context.Context, threadID string) (Thread, error) {
	if err := s.ready(); err != nil {
		return Thread{}, err
	}
	t, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return Thread{}, err
	}
	s.stampThreadMembers(&t)
	return t, nil
}

func (s *Service) openExisting(ctx context.Context, t Thread) (Thread, []Message, error) {
	msgs, err := s.store.ListPeopleMessages(ctx, t.ThreadID, maxMessages)
	if err != nil {
		return Thread{}, nil, err
	}
	readAt := nowRFC3339()
	for _, msg := range msgs {
		if msg.CreatedAt > readAt {
			readAt = msg.CreatedAt
		}
	}
	if err := s.store.MarkThreadRead(ctx, t.ThreadID, s.identity.SubjectID(), readAt); err != nil {
		return Thread{}, nil, err
	}
	s.stampThreadMembers(&t)
	t.UnreadCount = 0
	t.TypingSubjectIDs = s.typingFor(t.ThreadID)
	for i := range msgs {
		msgs[i].TransferPercent = s.progressFor(msgs[i].OfferID)
	}
	go s.deliverRead(t, readAt)
	return t, msgs, nil
}

func (s *Service) CreateGroup(ctx context.Context, title, ownerSubjectID string, memberIDs []string) (Thread, error) {
	if err := s.readyUnlocked(); err != nil {
		return Thread{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 128 {
		return Thread{}, ErrInvalid
	}
	self := s.identity.SubjectID()
	if ownerSubjectID == "" {
		ownerSubjectID = self
	}
	seen := map[string]bool{self: true, ownerSubjectID: true}
	ids := []string{self}
	if ownerSubjectID != self {
		ids = append(ids, ownerSubjectID)
	}
	for _, id := range memberIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		contact, err := s.store.GetContact(ctx, id)
		if err != nil {
			return Thread{}, ErrNotFound
		}
		if contact.TrustState == "discovered" {
			return Thread{}, ErrNotTrusted
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) < 1 || len(ids) > maxMembers {
		return Thread{}, ErrInvalid
	}
	if _, err := s.store.GetContact(ctx, ownerSubjectID); err != nil {
		return Thread{}, ErrNotFound
	}
	now := nowRFC3339()
	t := Thread{ThreadID: ulid.Make().String(), Kind: "group", Title: title, OwnerID: ownerSubjectID, CreatedAt: now, UpdatedAt: now}
	if err := s.store.InsertThread(ctx, t, ids, ownerSubjectID); err != nil {
		return Thread{}, err
	}
	opened, err := s.store.GetThread(ctx, t.ThreadID)
	if err != nil {
		return Thread{}, err
	}
	go s.deliverThread(opened)
	s.stampThreadMembers(&opened)
	return opened, nil
}

func (s *Service) Send(ctx context.Context, in SendInput) (Message, *FileOffer, error) {
	if err := s.readyUnlocked(); err != nil {
		return Message{}, nil, err
	}
	t, err := s.store.GetThread(ctx, in.ThreadID)
	if err != nil {
		return Message{}, nil, ErrNotFound
	}
	kind := in.Kind
	if kind == "" {
		kind = "text"
	}
	switch kind {
	case "text", "emoji", "image", "file":
	default:
		return Message{}, nil, ErrInvalid
	}
	self := s.identity.SubjectID()
	now := nowRFC3339()
	msg := Message{
		MessageID: ulid.Make().String(),
		ThreadID:  t.ThreadID,
		SenderID:  self,
		Kind:      kind,
		Body:      in.Body,
		FileName:  in.FileName,
		FileMIME:  in.FileMIME,
		CreatedAt: now,
	}
	if kind == "text" || kind == "emoji" {
		if strings.TrimSpace(in.Body) == "" || utf8.RuneCountInString(in.Body) > maxText {
			return Message{}, nil, ErrInvalid
		}
		if err := s.store.InsertMessage(ctx, msg, nil); err != nil {
			return Message{}, nil, err
		}
		go s.deliverMessage(t, msg, "")
		return msg, nil, nil
	}
	stagePath, size, sumHex, err := s.materializeFile(in)
	if err != nil {
		return Message{}, nil, err
	}
	msg.FileSize = size
	msg.FileSHA256 = sumHex
	if msg.FileName == "" {
		msg.FileName = nonempty(in.FileName, filepath.Base(in.LocalPath))
		if msg.FileName == "" || msg.FileName == "." {
			msg.FileName = "file"
		}
	}
	if strings.EqualFold(filepath.Ext(stagePath), ".zip") && !strings.EqualFold(filepath.Ext(msg.FileName), ".zip") {
		if msg.FileName == "" || msg.FileName == "file" {
			msg.FileName = "folder.zip"
		} else {
			msg.FileName += ".zip"
		}
		if msg.FileMIME == "" {
			msg.FileMIME = "application/zip"
		}
	}
	offer := FileOffer{
		OfferID:     ulid.Make().String(),
		MessageID:   msg.MessageID,
		ThreadID:    t.ThreadID,
		FromID:      self,
		ToID:        peerOf(t, self),
		Status:      "pending",
		FileName:    msg.FileName,
		FileMIME:    msg.FileMIME,
		FileSize:    msg.FileSize,
		FileSHA256:  msg.FileSHA256,
		StagingPath: stagePath,
		CreatedAt:   now,
	}
	if err := s.store.InsertMessage(ctx, msg, &offer); err != nil {
		return Message{}, nil, err
	}
	msg.OfferID = offer.OfferID
	msg.OfferStatus = "pending"
	msg.DestPath = stagePath
	go s.deliverMessage(t, msg, stagePath)
	return msg, &offer, nil
}

func (s *Service) DecideFile(ctx context.Context, offerID string, accept bool) (FileOffer, error) {
	if err := s.readyUnlocked(); err != nil {
		return FileOffer{}, err
	}
	offer, err := s.store.GetOffer(ctx, offerID)
	if err != nil {
		return FileOffer{}, ErrNotFound
	}
	if offer.Status != "pending" {
		return FileOffer{}, ErrOfferDecided
	}
	now := nowRFC3339()
	if !accept {
		_ = os.Remove(offer.StagingPath)
		if err := s.store.DecideOffer(ctx, offerID, "rejected", "", now); err != nil {
			return FileOffer{}, err
		}
		offer.Status = "rejected"
		offer.DecidedAt = now
		offer.DestPath = ""
		return offer, nil
	}
	if s.receiveDir == "" {
		return FileOffer{}, fmt.Errorf("receive directory missing")
	}
	if err := os.MkdirAll(s.receiveDir, 0o700); err != nil {
		return FileOffer{}, err
	}
	safe := sanitizeName(offer.FileName)
	dest := filepath.Join(s.receiveDir, offer.OfferID+"-"+safe)
	if offer.StagingPath != "" {
		if err := copyFile(offer.StagingPath, dest); err != nil {
			return FileOffer{}, err
		}
		_ = os.Remove(offer.StagingPath)
	}
	if err := s.store.DecideOffer(ctx, offerID, "accepted", dest, now); err != nil {
		return FileOffer{}, err
	}
	offer.Status = "accepted"
	offer.DestPath = dest
	offer.DecidedAt = now
	return offer, nil
}

func (s *Service) ready() error {
	if s == nil || s.store == nil || s.identity == nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Service) readyUnlocked() error {
	if err := s.ready(); err != nil {
		return err
	}
	if s.identity.Locked() {
		return ErrLocked
	}
	return nil
}

func (s *Service) startLAN() error {
	_ = s.ensureTCP()
	if s.lan == nil {
		s.lan = NewLAN()
	}
	return s.lan.Start(func() Beacon {
		b, ok := s.CurrentBeacon()
		if !ok {
			return Beacon{}
		}
		return b
	}, func(b Beacon, host string) {
		_ = s.IngestBeacon(context.Background(), b, host)
	})
}

func decodeFile(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if i := strings.Index(b64, ","); i >= 0 && strings.Contains(b64[:i], "base64") {
		b64 = b64[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(b64)
	}
	return raw, err
}

func peerOf(t Thread, self string) string {
	for _, m := range t.Members {
		if m.SubjectID != self {
			return m.SubjectID
		}
	}
	return self
}

func nonempty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func sanitizeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/|?*`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if utf8.RuneCountInString(name) > 80 {
		runes := []rune(name)
		name = string(runes[:80])
	}
	return name
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Service) SetListenAddr(addr string) {
	if s == nil || strings.TrimSpace(addr) == "" {
		return
	}
	s.mu.Lock()
	s.bind = strings.TrimSpace(addr)
	s.mu.Unlock()
}

func (s *Service) LocalAddr() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tcpLn == nil {
		return ""
	}
	return s.tcpLn.Addr().String()
}

func (s *Service) StartTCP() error { return s.ensureTCP() }

func (s *Service) AddPeer(ctx context.Context, hostAddr string) (Contact, error) {
	if err := s.readyUnlocked(); err != nil {
		return Contact{}, err
	}
	addr, err := parsePeerAddr(hostAddr)
	if err != nil {
		return Contact{}, err
	}
	if err := s.ensureTCP(); err != nil {
		return Contact{}, err
	}
	hello, err := s.dialHello(addr)
	if err != nil {
		return Contact{}, err
	}
	host, _, _ := net.SplitHostPort(addr)
	if err := s.IngestBeacon(ctx, hello.beacon(), host); err != nil {
		return Contact{}, err
	}
	if existing, getErr := s.store.GetContact(ctx, hello.SubjectID); getErr == nil {
		if existing.Blocked {
			return Contact{}, ErrBlocked
		}
		existing.HostAddr = addr
		existing.UpdatedAt = nowRFC3339()
		_ = s.store.UpsertContact(ctx, existing)
		existing.Self = existing.SubjectID == s.identity.SubjectID()
		existing.PairingHash = ""
		return existing, nil
	}
	return s.store.GetContact(ctx, hello.SubjectID)
}

func (s *Service) UpdateContact(ctx context.Context, subjectID string, patch ContactPatch) (Contact, error) {
	if err := s.readyUnlocked(); err != nil {
		return Contact{}, err
	}
	c, err := s.store.GetContact(ctx, subjectID)
	if err != nil {
		return Contact{}, ErrNotFound
	}
	if c.TrustState == "self" && patch.Blocked != nil && *patch.Blocked {
		return Contact{}, ErrInvalid
	}
	if patch.Remark != nil {
		remark := strings.TrimSpace(*patch.Remark)
		if utf8.RuneCountInString(remark) > 64 {
			return Contact{}, ErrInvalid
		}
		c.Remark = remark
	}
	if patch.Blocked != nil {
		c.Blocked = *patch.Blocked
	}
	c.UpdatedAt = nowRFC3339()
	if err := s.store.UpsertContact(ctx, c); err != nil {
		return Contact{}, err
	}
	c.Self = c.SubjectID == s.identity.SubjectID()
	c.PairingHash = ""
	return c, nil
}

func (s *Service) NoteTyping(ctx context.Context, threadID string) error {
	if err := s.readyUnlocked(); err != nil {
		return err
	}
	t, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return ErrNotFound
	}
	go s.deliverTyping(t)
	return nil
}

func (s *Service) StageFile(ctx context.Context, in StageInput) (StageResult, error) {
	if err := s.readyUnlocked(); err != nil {
		return StageResult{}, err
	}
	id := strings.TrimSpace(in.UploadID)
	if len(id) != 26 {
		return StageResult{}, ErrInvalid
	}
	raw, err := decodeFile(in.ContentBase64)
	if err != nil || len(raw) == 0 {
		return StageResult{}, ErrInvalid
	}
	if s.stagingDir == "" {
		return StageResult{}, fmt.Errorf("staging directory missing")
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return StageResult{}, err
	}
	s.mu.Lock()
	up := s.uploads[id]
	if up == nil {
		path := filepath.Join(s.stagingDir, "up-"+id)
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if openErr != nil {
			s.mu.Unlock()
			return StageResult{}, openErr
		}
		up = &fileUpload{name: in.FileName, mime: in.FileMIME, path: path, file: f}
		s.uploads[id] = up
	}
	s.mu.Unlock()
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.size+int64(len(raw)) > maxFileBytes {
		return StageResult{}, ErrTooLarge
	}
	if _, err := up.file.Write(raw); err != nil {
		return StageResult{}, err
	}
	up.size += int64(len(raw))
	if !in.Last {
		return StageResult{Bytes: up.size}, nil
	}
	_ = up.file.Close()
	up.file = nil
	s.mu.Lock()
	delete(s.uploads, id)
	s.mu.Unlock()
	return StageResult{Ready: true, LocalPath: up.path, Bytes: up.size}, nil
}

func (s *Service) PickFile(folder bool) (PickResult, error) {
	if err := s.readyUnlocked(); err != nil {
		return PickResult{}, err
	}
	path, err := pickLocalPath(folder)
	if err != nil {
		return PickResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return PickResult{}, err
	}
	return PickResult{Path: path, FileName: info.Name(), Size: info.Size(), Directory: info.IsDir()}, nil
}

func (s *Service) OpenFile(destPath string) (string, error) {
	if err := s.readyUnlocked(); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(strings.TrimSpace(destPath))
	if err != nil || abs == "" {
		return "", ErrInvalid
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", ErrInvalid
	}
	if info.IsDir() {
		return "", ErrInvalid
	}
	if !pathUnderRoot(s.receiveDir, abs) && !pathUnderRoot(s.stagingDir, abs) {
		return "", ErrInvalid
	}
	if err := openPathFn(abs); err != nil {
		return "", ErrOpenFailed
	}
	return abs, nil
}

func ReplaceOpenPathForTest(fn func(string) error) func() {
	prev := openPathFn
	openPathFn = fn
	return func() { openPathFn = prev }
}

func pathUnderRoot(root, target string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func (s *Service) materializeFile(in SendInput) (string, int64, string, error) {
	if s.stagingDir == "" {
		return "", 0, "", fmt.Errorf("staging directory missing")
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return "", 0, "", err
	}
	stage := filepath.Join(s.stagingDir, ulid.Make().String())
	srcPath := strings.TrimSpace(in.LocalPath)
	if srcPath != "" {
		info, err := os.Stat(srcPath)
		if err != nil {
			return "", 0, "", ErrInvalid
		}
		if info.IsDir() {
			zipPath := stage + ".zip"
			if err := zipDirectory(srcPath, zipPath, maxFileBytes); err != nil {
				return "", 0, "", err
			}
			srcPath = zipPath
			defer func() {
				if srcPath == zipPath {
					// zip stays as staging payload; caller uses returned path
				}
			}()
			stage = zipPath
		} else {
			if err := copyFileLimited(srcPath, stage, maxFileBytes); err != nil {
				return "", 0, "", err
			}
		}
		sum, size, err := hashFile(stage)
		return stage, size, sum, err
	}
	raw, err := decodeFile(in.ContentBase64)
	if err != nil {
		return "", 0, "", err
	}
	if len(raw) == 0 || int64(len(raw)) > maxFileBytes {
		return "", 0, "", ErrTooLarge
	}
	if err := os.WriteFile(stage, raw, 0o600); err != nil {
		return "", 0, "", err
	}
	sum := sha256.Sum256(raw)
	return stage, int64(len(raw)), hex.EncodeToString(sum[:]), nil
}

func (s *Service) typingFor(threadID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	by := s.typing[threadID]
	var ids []string
	for id, until := range by {
		if until.After(now) && id != s.identity.SubjectID() {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Service) progressFor(offerID string) int {
	if offerID == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress[offerID]
}

func (s *Service) advertisedPort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tcpPort > 0 {
		return s.tcpPort
	}
	return defaultTCP
}

func joinHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if port <= 0 {
		port = defaultTCP
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func (s *Service) ThreadSession(ctx context.Context, threadID string) (string, bool, error) {
	if s == nil || s.store == nil {
		return "", false, ErrUnavailable
	}
	if !canonicalPeopleULID(threadID) {
		return "", false, ErrInvalid
	}
	return s.store.ThreadSession(ctx, threadID)
}

func (s *Service) BindThreadSession(ctx context.Context, threadID, sessionID string) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	if !canonicalPeopleULID(threadID) || !canonicalPeopleULID(sessionID) {
		return ErrInvalid
	}
	return s.store.BindThreadSession(ctx, threadID, sessionID, nowRFC3339())
}

func (s *Service) ClearThreadSession(ctx context.Context, threadID string) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	if !canonicalPeopleULID(threadID) {
		return ErrInvalid
	}
	return s.store.ClearThreadSession(ctx, threadID)
}

func (s *Service) ListBoundSessionIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	return s.store.ListBoundSessionIDs(ctx)
}

func canonicalPeopleULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

func parsePeerAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") {
		return "", ErrInvalid
	}
	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw, nil
	}
	if ip := net.ParseIP(raw); ip != nil {
		return net.JoinHostPort(raw, strconv.Itoa(defaultTCP)), nil
	}
	return "", ErrInvalid
}

func copyFile(src, dest string) error { return copyFileLimited(src, dest, maxFileBytes) }

func copyFileLimited(src, dest string, limit int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		_ = os.Remove(dest)
		return ErrTooLarge
	}
	return nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
