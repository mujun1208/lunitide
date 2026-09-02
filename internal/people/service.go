// Package people is the this-PC WeChat-style contact and thread store.
// LAN discovery is optional and off by default. Discovered peers are not
// trusted until paired. File offers are never auto-accepted. This package
// never exposes computer-control to a remote machine.
package people

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

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

func (s *Service) stampThreadMembers(t *Thread) {
	if s == nil || t == nil || s.identity == nil {
		return
	}
	self := s.identity.SubjectID()
	for i := range t.Members {
		t.Members[i].Self = t.Members[i].SubjectID == self
	}
}

// forDelivery returns t with an independent copy of its Members slice. A
// delivery goroutine reads the thread while the caller keeps stamping Self
// flags onto its own copy; a plain struct copy shares one backing array, so
// the read and the stamp race. Copying the slice hands the goroutine a value
// nobody else mutates.
func (t Thread) forDelivery() Thread {
	if t.Members != nil {
		t.Members = append([]Contact(nil), t.Members...)
	}
	return t
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
	go s.deliverThread(opened.forDelivery())
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
	go s.deliverThread(opened.forDelivery())
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

func peerOf(t Thread, self string) string {
	for _, m := range t.Members {
		if m.SubjectID != self {
			return m.SubjectID
		}
	}
	return self
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
