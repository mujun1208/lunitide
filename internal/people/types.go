package people

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/identity"
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
