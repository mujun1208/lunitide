package people

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/identity"
)

const maxFrame = 96 << 10

type fileUpload struct {
	mu       sync.Mutex
	name     string
	mime     string
	path     string
	file     *os.File
	size     int64
	chunks   []stageChunkReceipt
	updated  time.Time
	complete bool
}

type stageChunkReceipt struct {
	hash   [32]byte
	last   bool
	result StageResult
}

type incomingFile struct {
	file    *os.File
	path    string
	size    int64
	got     int64
	msg     Message
	thread  wireThread
	owner   string
	seq     int
	updated time.Time
}

type p2pFrame struct {
	Typ         string      `json:"typ"`
	V           int         `json:"v"`
	SubjectID   string      `json:"subjectId,omitempty"`
	Nickname    string      `json:"nickname,omitempty"`
	Department  string      `json:"department,omitempty"`
	Title       string      `json:"title,omitempty"`
	OrgName     string      `json:"orgName,omitempty"`
	Status      string      `json:"status,omitempty"`
	PublicKey   string      `json:"publicKey,omitempty"`
	PairingHash string      `json:"pairingHash,omitempty"`
	Port        int         `json:"port,omitempty"`
	Eph         string      `json:"eph,omitempty"`
	Nonce       string      `json:"nonce,omitempty"`
	Sig         string      `json:"sig,omitempty"`
	Thread      *wireThread `json:"thread,omitempty"`
	Message     *Message    `json:"message,omitempty"`
	OfferID     string      `json:"offerId,omitempty"`
	Seq         int         `json:"seq,omitempty"`
	Data        string      `json:"data,omitempty"`
	SHA256      string      `json:"sha256,omitempty"`
	Last        bool        `json:"last,omitempty"`
	ThreadID    string      `json:"threadId,omitempty"`
	At          string      `json:"at,omitempty"`
	Size        int64       `json:"size,omitempty"`
	FileName    string      `json:"fileName,omitempty"`
	FileMIME    string      `json:"fileMime,omitempty"`
	BodyEnc     int         `json:"bodyEnc,omitempty"`
	BodyCipher  string      `json:"bodyCipher,omitempty"`
	BodyNonce   string      `json:"bodyNonce,omitempty"`
}

type wireThread struct {
	ThreadID  string   `json:"threadId"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	OwnerID   string   `json:"ownerSubjectId"`
	MemberIDs []string `json:"memberIds"`
}

func (f p2pFrame) beacon() Beacon {
	return Beacon{
		V: 1, Kind: "lunitide-people", SubjectID: f.SubjectID, Nickname: f.Nickname,
		Department: f.Department, Title: f.Title, OrgName: f.OrgName, Status: f.Status,
		PublicKey: f.PublicKey, PairingHash: f.PairingHash, Port: f.Port,
	}
}

func (s *Service) ensureTCP() error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrUnavailable
	}
	if s.tcpLn != nil {
		s.mu.Unlock()
		return nil
	}
	bind := s.bind
	if bind == "" {
		bind = ":" + strconv.Itoa(defaultTCP)
	}
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.tcpLn = ln
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		s.tcpPort = addr.Port
	}
	s.mu.Unlock()
	go s.acceptLoop(ln)
	return nil
}

func (s *Service) stopTCP() {
	s.mu.Lock()
	ln := s.tcpLn
	s.tcpLn = nil
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
}

func (s *Service) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.serveConn(conn, true)
	}
}

func (s *Service) serveConn(conn net.Conn, inbound bool) {
	defer conn.Close()
	if !s.trackConnection(conn) {
		return
	}
	defer s.untrackConnection(conn)
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	peer, aead, err := s.handshake(conn, inbound)
	if err != nil {
		return
	}
	if s.ready() != nil {
		return
	}
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	_ = s.IngestBeacon(context.Background(), peer.beacon(), host)
	contact, err := s.store.GetContact(context.Background(), peer.SubjectID)
	if err != nil || contact.Blocked {
		return
	}
	// The same AEAD key protects both directions. Reserve the high half of
	// nonce counters for receiver replies so ACKs never reuse sender nonces.
	seq := uint64(1) << 63
	for {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
		frame, err := readEncFrame(conn, aead)
		if err != nil {
			return
		}
		s.handleFrame(conn, aead, &seq, contact, frame)
	}
}

func (s *Service) handshake(conn net.Conn, inbound bool) (p2pFrame, cipher.AEAD, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return p2pFrame{}, nil, err
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return p2pFrame{}, nil, err
	}
	local, err := s.helloFrame(priv.PublicKey().Bytes(), nonce)
	if err != nil {
		return p2pFrame{}, nil, err
	}
	var peer p2pFrame
	if inbound {
		peer, err = readPlainFrame(conn)
		if err != nil {
			return p2pFrame{}, nil, err
		}
		if err := s.verifyHello(peer); err != nil {
			return p2pFrame{}, nil, err
		}
		if err := writePlainFrame(conn, local); err != nil {
			return p2pFrame{}, nil, err
		}
	} else {
		if err := writePlainFrame(conn, local); err != nil {
			return p2pFrame{}, nil, err
		}
		peer, err = readPlainFrame(conn)
		if err != nil {
			return p2pFrame{}, nil, err
		}
		if err := s.verifyHello(peer); err != nil {
			return p2pFrame{}, nil, err
		}
	}
	peerEph, err := hex.DecodeString(peer.Eph)
	if err != nil {
		return p2pFrame{}, nil, err
	}
	peerPub, err := ecdh.X25519().NewPublicKey(peerEph)
	if err != nil {
		return p2pFrame{}, nil, err
	}
	secret, err := priv.ECDH(peerPub)
	if err != nil {
		return p2pFrame{}, nil, err
	}
	peerNonce, err := hex.DecodeString(peer.Nonce)
	if err != nil {
		return p2pFrame{}, nil, err
	}
	aead, err := deriveAEAD(secret, nonce, peerNonce)
	return peer, aead, err
}

func (s *Service) helloFrame(eph, nonce []byte) (p2pFrame, error) {
	pub := s.identity.Public()
	msg := helloBytes(pub.SubjectID, eph, nonce)
	sig, err := s.identity.Sign(msg)
	if err != nil {
		return p2pFrame{}, err
	}
	return p2pFrame{
		Typ: "hello", V: 1, SubjectID: pub.SubjectID, Nickname: pub.Nickname,
		Department: pub.Department, Title: pub.Title, OrgName: pub.OrgName,
		Status: string(pub.Status), PublicKey: pub.PublicKey, PairingHash: s.identity.PairingHash(),
		Port: s.advertisedPort(), Eph: hex.EncodeToString(eph), Nonce: hex.EncodeToString(nonce),
		Sig: hex.EncodeToString(sig),
	}, nil
}

func (s *Service) verifyHello(f p2pFrame) error {
	if f.Typ != "hello" && f.Typ != "hello-ok" {
		f.Typ = "hello"
	}
	if f.V != 1 || f.SubjectID == "" || f.SubjectID == s.identity.SubjectID() {
		return ErrInvalid
	}
	eph, err := hex.DecodeString(f.Eph)
	if err != nil || len(eph) != 32 {
		return ErrInvalid
	}
	nonce, err := hex.DecodeString(f.Nonce)
	if err != nil || len(nonce) != 24 {
		return ErrInvalid
	}
	sig, err := hex.DecodeString(f.Sig)
	if err != nil {
		return ErrInvalid
	}
	if !identity.Verify(f.PublicKey, helloBytes(f.SubjectID, eph, nonce), sig) {
		return ErrInvalid
	}
	if existing, err := s.store.GetContact(context.Background(), f.SubjectID); err == nil {
		if existing.Blocked {
			return ErrBlocked
		}
		if existing.PublicKey != "" && existing.PublicKey != f.PublicKey {
			return ErrInvalid
		}
	}
	return nil
}

func helloBytes(subjectID string, eph, nonce []byte) []byte {
	buf := make([]byte, 0, 16+len(subjectID)+len(eph)+len(nonce))
	buf = append(buf, []byte("lunitide-p2p-v1")...)
	buf = append(buf, subjectID...)
	buf = append(buf, eph...)
	buf = append(buf, nonce...)
	return buf
}

func deriveAEAD(secret, nonceA, nonceB []byte) (cipher.AEAD, error) {
	salt := sha256.Sum256(append(append([]byte{}, minBytes(nonceA, nonceB)...), maxBytes(nonceA, nonceB)...))
	key, err := hkdf.Key(sha256.New, secret, salt[:], "lunitide-p2p", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func minBytes(a, b []byte) []byte {
	if string(a) <= string(b) {
		return a
	}
	return b
}

func maxBytes(a, b []byte) []byte {
	if string(a) >= string(b) {
		return a
	}
	return b
}

func (s *Service) dialHello(addr string) (p2pFrame, error) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return p2pFrame{}, ErrUnreachable
	}
	defer conn.Close()
	if !s.trackConnection(conn) {
		return p2pFrame{}, ErrUnavailable
	}
	defer s.untrackConnection(conn)
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	peer, _, err := s.handshake(conn, false)
	if err != nil {
		return p2pFrame{}, ErrUnreachable
	}
	return peer, nil
}

func (s *Service) deliverMessage(t Thread, msg Message, filePath string) {
	for _, member := range t.Members {
		if member.SubjectID == s.identity.SubjectID() || member.Blocked || strings.TrimSpace(member.HostAddr) == "" {
			continue
		}
		outMsg := msg
		frameEnc := 0
		var cipherB64, nonceB64 string
		// F-08: E2E-encrypt the text body for this specific member. Falls back
		// to plaintext Body when the peer public key is unknown or derivation
		// fails, so delivery is never blocked.
		if outMsg.Kind == "text" && outMsg.Body != "" {
			if c, n, ok := s.sealBody(member.PublicKey, outMsg.Body); ok {
				cipherB64, nonceB64, frameEnc = c, n, bodyEncV
				outMsg.Body = ""
			}
		}
		_ = s.pushTo(member, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			frame := p2pFrame{Typ: "msg", V: 1, Thread: wireOf(t), Message: &outMsg,
				BodyEnc: frameEnc, BodyCipher: cipherB64, BodyNonce: nonceB64}
			if err := writeEncFrame(conn, aead, seq, frame); err != nil {
				return err
			}
			if filePath == "" || (msg.Kind != "file" && msg.Kind != "image") {
				return nil
			}
			return s.pushFile(conn, aead, seq, t, msg, filePath)
		})
	}
}

func (s *Service) deliverThread(t Thread) {
	for _, member := range t.Members {
		if member.SubjectID == s.identity.SubjectID() || member.Blocked || strings.TrimSpace(member.HostAddr) == "" {
			continue
		}
		_ = s.pushTo(member, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			return writeEncFrame(conn, aead, seq, p2pFrame{Typ: "thread", V: 1, Thread: wireOf(t)})
		})
	}
}

func (s *Service) deliverTyping(t Thread) {
	for _, member := range t.Members {
		if member.SubjectID == s.identity.SubjectID() || member.Blocked || strings.TrimSpace(member.HostAddr) == "" {
			continue
		}
		_ = s.pushTo(member, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			return writeEncFrame(conn, aead, seq, p2pFrame{Typ: "typing", V: 1, ThreadID: t.ThreadID, Thread: wireOf(t), SubjectID: s.identity.SubjectID()})
		})
	}
}

func (s *Service) deliverRead(t Thread, at string) {
	for _, member := range t.Members {
		if member.SubjectID == s.identity.SubjectID() || member.Blocked || strings.TrimSpace(member.HostAddr) == "" {
			continue
		}
		_ = s.pushTo(member, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			return writeEncFrame(conn, aead, seq, p2pFrame{Typ: "read", V: 1, ThreadID: t.ThreadID, Thread: wireOf(t), SubjectID: s.identity.SubjectID(), At: at})
		})
	}
}

func (s *Service) pushExpected(addr, subjectID, publicKey string, fn func(net.Conn, cipher.AEAD, *uint64) error) error {
	if err := s.readyUnlocked(); err != nil {
		return err
	}
	addr, err := parsePeerAddr(addr)
	if err != nil {
		return err
	}
	if err := s.ensureTCP(); err != nil {
		return err
	}
	ctx := s.deliveryCtx
	if ctx == nil {
		ctx = context.Background()
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return ErrUnreachable
	}
	defer conn.Close()
	if !s.trackConnection(conn) {
		return ErrUnavailable
	}
	defer s.untrackConnection(conn)
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	peer, aead, err := s.handshake(conn, false)
	if err != nil {
		return err
	}
	if subjectID != "" && (peer.SubjectID != subjectID || peer.PublicKey != publicKey) {
		return ErrNotTrusted
	}
	if subjectID != "" {
		current, err := s.store.GetContact(ctx, subjectID)
		if err != nil || current.Blocked || current.TrustState != "trusted" || current.PublicKey != publicKey {
			return ErrNotTrusted
		}
	}
	seq := uint64(0)
	guarded := peerGrantConn{Conn: conn, check: func() error {
		if err := s.readyUnlocked(); err != nil {
			return err
		}
		current, err := s.store.GetContact(ctx, subjectID)
		if err != nil || current.Blocked || current.TrustState != "trusted" || current.PublicKey != publicKey {
			return ErrNotTrusted
		}
		return nil
	}}
	return fn(guarded, aead, &seq)
}

func (s *Service) pushFile(conn net.Conn, aead cipher.AEAD, seq *uint64, t Thread, msg Message, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := writeEncFrame(conn, aead, seq, p2pFrame{
		Typ: "file-begin", V: 1, OfferID: msg.OfferID, Message: &msg, Thread: wireOf(t),
		Size: msg.FileSize, FileName: msg.FileName, FileMIME: msg.FileMIME, SHA256: msg.FileSHA256,
	}); err != nil {
		return err
	}
	buf := make([]byte, chunkSize)
	n := 0
	var sent int64
	for {
		c, err := f.Read(buf)
		if c > 0 {
			n++
			sent += int64(c)
			if err := writeEncFrame(conn, aead, seq, p2pFrame{
				Typ: "file-chunk", V: 1, OfferID: msg.OfferID, Seq: n,
				Data: base64.StdEncoding.EncodeToString(buf[:c]),
			}); err != nil {
				return err
			}
			if msg.FileSize > 0 {
				s.setProgress(msg.OfferID, int(sent*100/msg.FileSize))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	s.setProgress(msg.OfferID, 100)
	return writeEncFrame(conn, aead, seq, p2pFrame{Typ: "file-end", V: 1, OfferID: msg.OfferID, SHA256: msg.FileSHA256, Last: true})
}

func (s *Service) handleFrame(conn net.Conn, aead cipher.AEAD, seq *uint64, from Contact, frame p2pFrame) {
	if s.readyUnlocked() != nil {
		return
	}
	ctx := context.Background()
	// The authenticated subject stays fixed for this connection, but its grant
	// can be revoked while the socket remains open. Never trust a stale snapshot.
	current, err := s.store.GetContact(ctx, from.SubjectID)
	if err != nil || current.Blocked || current.TrustState != "trusted" || current.SubjectID == s.identity.SubjectID() || current.PublicKey != from.PublicKey {
		return
	}
	from = current
	switch frame.Typ {
	case "msg":
		if from.TrustState != "trusted" && from.TrustState != "self" {
			return
		}
		if frame.Message == nil {
			return
		}
		if s.receiveMessage(ctx, from, frame) {
			s.acknowledgeIncoming(ctx, conn, aead, seq, from, frame.Message.MessageID, "")
		}
	case "thread":
		if from.TrustState != "trusted" && from.TrustState != "self" {
			return
		}
		if frame.Thread != nil {
			_, _ = s.ensureRemoteThread(ctx, *frame.Thread, from)
		}
	case "file-begin":
		if from.TrustState != "trusted" {
			return
		}
		s.beginIncoming(from, frame)
	case "file-chunk":
		s.writeIncoming(from, frame)
	case "file-end":
		s.finishIncoming(ctx, from, frame)
		if _, ok := s.store.(DeliveryStore); ok {
			if offer, err := s.store.GetOffer(ctx, frame.OfferID); err == nil && offer.FromID == from.SubjectID && offer.FileSHA256 == frame.SHA256 && frame.Last {
				s.acknowledgeIncoming(ctx, conn, aead, seq, from, offer.MessageID, frame.OfferID)
			}
		}
	case "typing":
		threadID, ok := s.remoteEventThread(ctx, frame, from)
		if !ok {
			return
		}
		s.noteRemoteTyping(threadID, from.SubjectID)
	case "read":
		threadID, ok := s.remoteEventThread(ctx, frame, from)
		if !ok {
			return
		}
		_ = s.store.MarkThreadRead(ctx, threadID, from.SubjectID, nonempty(frame.At, nowRFC3339()))
	}
}

func (s *Service) receiveMessage(ctx context.Context, from Contact, frame p2pFrame) bool {
	msg := *frame.Message
	if msg.SenderID != from.SubjectID || msg.MessageID == "" || (msg.Kind != "text" && msg.Kind != "emoji") || (frame.Thread != nil && frame.Thread.ThreadID != msg.ThreadID) {
		return false
	}
	// F-08: if the body was E2E-sealed, decrypt with the shared key derived
	// from the sender's Ed25519 public key. Auth failure => discard (tamper
	// protection). Absent cipher fields => backward-compatible plaintext.
	if frame.BodyEnc == bodyEncV {
		plain, ok := s.openBody(from.PublicKey, frame.BodyCipher, frame.BodyNonce)
		if !ok {
			return false
		}
		msg.Body = plain
	}
	if !utf8.ValidString(msg.Body) || len(msg.Body) > 4*maxWireText || utf8.RuneCountInString(msg.Body) > maxWireText {
		return false
	}
	thread, err := s.ensureRemoteThread(ctx, derefThread(frame.Thread, msg.ThreadID), from)
	if err != nil {
		return false
	}
	msg.ThreadID = thread.ThreadID
	if store, ok := s.store.(DeliveryStore); ok {
		if saved, err := store.GetPeopleMessage(ctx, msg.MessageID); err == nil {
			return saved.SenderID == msg.SenderID && saved.ThreadID == msg.ThreadID && saved.Kind == msg.Kind && saved.Body == msg.Body
		} else if !errors.Is(err, ErrNotFound) {
			return false
		}
	} else if exists, err := s.store.HasPeopleMessage(ctx, msg.MessageID); err != nil || exists {
		return false
	}
	return s.store.InsertMessage(ctx, msg, nil) == nil
}

func (s *Service) beginIncoming(from Contact, frame p2pFrame) {
	if frame.OfferID == "" || len(frame.OfferID) > 128 || frame.Size < 0 || frame.Size > maxFileBytes || frame.Message == nil {
		return
	}
	msg := *frame.Message
	if msg.SenderID != from.SubjectID || msg.MessageID == "" || msg.OfferID != frame.OfferID || msg.FileSize != frame.Size || msg.FileSHA256 != frame.SHA256 || (msg.Kind != "file" && msg.Kind != "image") {
		return
	}
	if digest, err := hex.DecodeString(frame.SHA256); err != nil || len(digest) != sha256.Size {
		return
	}
	th := derefThread(frame.Thread, msg.ThreadID)
	if th.ThreadID != msg.ThreadID {
		return
	}
	if _, err := s.ensureRemoteThread(context.Background(), th, from); err != nil {
		return
	}
	if exists, err := s.store.HasPeopleMessage(context.Background(), msg.MessageID); err != nil || exists {
		return
	}
	if s.stagingDir == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	for id, in := range s.incoming {
		if time.Since(in.updated) > 30*time.Second {
			_ = in.file.Close()
			_ = os.Remove(in.path)
			delete(s.incoming, id)
		}
	}
	if s.incoming[frame.OfferID] != nil || len(s.incoming) >= 8 {
		return
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return
	}
	// The sender's offer ID is metadata, never a filesystem path.
	f, err := os.CreateTemp(s.stagingDir, "incoming-*")
	if err != nil {
		return
	}
	s.incoming[frame.OfferID] = &incomingFile{file: f, path: f.Name(), size: frame.Size, msg: msg, thread: th, owner: from.SubjectID, updated: time.Now()}
}

func (s *Service) trackConnection(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	if s.connections == nil {
		s.connections = make(map[net.Conn]struct{})
	}
	s.connections[conn] = struct{}{}
	return true
}

func (s *Service) untrackConnection(conn net.Conn) {
	s.mu.Lock()
	delete(s.connections, conn)
	s.mu.Unlock()
}

func (s *Service) writeIncoming(from Contact, frame p2pFrame) {
	raw, err := base64.StdEncoding.DecodeString(frame.Data)
	if err != nil || len(raw) == 0 || len(raw) > chunkSize {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	in := s.incoming[frame.OfferID]
	if in == nil || in.file == nil || in.owner != from.SubjectID || frame.Seq != in.seq+1 {
		return
	}
	if in.got+int64(len(raw)) > in.size {
		return
	}
	if _, err := in.file.Write(raw); err != nil {
		return
	}
	in.got += int64(len(raw))
	in.seq = frame.Seq
	in.updated = time.Now()
	if in.size > 0 {
		s.progress[frame.OfferID] = int(in.got * 100 / in.size)
	}
}

func (s *Service) finishIncoming(ctx context.Context, from Contact, frame p2pFrame) {
	s.mu.Lock()
	in := s.incoming[frame.OfferID]
	if in == nil || in.owner != from.SubjectID {
		s.mu.Unlock()
		return
	}
	delete(s.incoming, frame.OfferID)
	s.mu.Unlock()
	retained := false
	defer func() {
		if !retained {
			_ = os.Remove(in.path)
		}
	}()
	if in.file != nil {
		if err := in.file.Sync(); err != nil {
			_ = in.file.Close()
			return
		}
		if err := in.file.Close(); err != nil {
			return
		}
	}
	sum, size, err := hashFile(in.path)
	if err != nil || size != in.size || in.got != in.size || sum != in.msg.FileSHA256 || frame.SHA256 != sum || !frame.Last {
		return
	}
	if in.msg.FileSize == 0 {
		in.msg.FileSize = size
	}
	ok, err := s.store.HasPeopleMessage(ctx, in.msg.MessageID)
	if err != nil || ok {
		return
	}
	thread, err := s.ensureRemoteThread(ctx, in.thread, from)
	if err != nil {
		return
	}
	in.msg.ThreadID = thread.ThreadID
	in.msg.OfferStatus = "pending"
	offer := FileOffer{
		OfferID: nonempty(in.msg.OfferID, frame.OfferID), MessageID: in.msg.MessageID, ThreadID: thread.ThreadID,
		FromID: from.SubjectID, ToID: s.identity.SubjectID(), Status: "pending",
		FileName: nonempty(in.msg.FileName, "file"), FileMIME: in.msg.FileMIME, FileSize: in.msg.FileSize,
		FileSHA256: sum, StagingPath: in.path, CreatedAt: nonempty(in.msg.CreatedAt, nowRFC3339()),
	}
	in.msg.OfferID = offer.OfferID
	if err := s.store.InsertMessage(ctx, in.msg, &offer); err != nil {
		return
	}
	retained = true
	s.setProgress(offer.OfferID, 100)
}

func (s *Service) ensureRemoteThread(ctx context.Context, spec wireThread, from Contact) (Thread, error) {
	self := s.identity.SubjectID()
	kind := nonempty(spec.Kind, "direct")
	if spec.ThreadID == "" || (kind != "direct" && kind != "group") {
		return Thread{}, ErrInvalid
	}
	// Resolve the supplied identity before choosing a direct/group path. A
	// peer cannot relabel an existing group as a direct thread to join it.
	if existing, err := s.store.GetThread(ctx, spec.ThreadID); err == nil {
		if existing.Kind != kind || !threadHasMember(existing, self) || !threadHasMember(existing, from.SubjectID) {
			return Thread{}, ErrNotTrusted
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Thread{}, err
	}
	if kind == "direct" {
		if t, ok, err := s.store.FindDirectThread(ctx, self, from.SubjectID); err != nil {
			return Thread{}, err
		} else if ok {
			if t.Kind != "direct" || !threadHasMember(t, self) || !threadHasMember(t, from.SubjectID) {
				return Thread{}, ErrNotTrusted
			}
			return t, nil
		}
	}
	now := nowRFC3339()
	ids := spec.MemberIDs
	if kind == "direct" {
		ids = []string{self, from.SubjectID}
	} else if kind != "group" || spec.OwnerID != from.SubjectID || !containsID(ids, self) || !containsID(ids, from.SubjectID) || len(ids) > maxMembers {
		return Thread{}, ErrNotTrusted
	}
	for _, id := range ids {
		if _, err := s.store.GetContact(ctx, id); err != nil {
			nick := "同事"
			if id == from.SubjectID {
				nick = nonempty(from.Nickname, nick)
			}
			_ = s.store.UpsertContact(ctx, Contact{
				SubjectID: id, Nickname: nick, TrustState: "discovered", Status: "offline",
				CreatedAt: now, UpdatedAt: now,
			})
		}
	}
	owner := nonempty(spec.OwnerID, from.SubjectID)
	t := Thread{ThreadID: spec.ThreadID, Kind: kind, Title: spec.Title, OwnerID: owner, CreatedAt: now, UpdatedAt: now}
	if err := s.store.InsertThread(ctx, t, ids, owner); err != nil {
		// A thread announcement and its first typing/message frame can race.
		// Accept only the already committed, identically scoped identity.
		existing, readErr := s.store.GetThread(ctx, t.ThreadID)
		if readErr != nil || existing.Kind != kind || existing.OwnerID != owner || !threadHasMember(existing, self) || !threadHasMember(existing, from.SubjectID) {
			return Thread{}, err
		}
		return existing, nil
	}
	created, err := s.store.GetThread(ctx, t.ThreadID)
	if err != nil {
		return Thread{}, err
	}
	if created.Kind != kind || !threadHasMember(created, self) || !threadHasMember(created, from.SubjectID) {
		return Thread{}, ErrNotTrusted
	}
	return created, nil
}

func (s *Service) remoteEventThread(ctx context.Context, frame p2pFrame, from Contact) (string, bool) {
	if frame.ThreadID == "" || (frame.SubjectID != "" && frame.SubjectID != from.SubjectID) {
		return "", false
	}
	var thread Thread
	var err error
	if frame.Thread != nil {
		if frame.Thread.ThreadID != frame.ThreadID {
			return "", false
		}
		thread, err = s.ensureRemoteThread(ctx, *frame.Thread, from)
	} else {
		thread, err = s.store.GetThread(ctx, frame.ThreadID)
	}
	return thread.ThreadID, err == nil && threadHasMember(thread, from.SubjectID) && threadHasMember(thread, s.identity.SubjectID())
}

func (s *Service) noteRemoteTyping(threadID, subjectID string) {
	if threadID == "" || subjectID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.typing[threadID] == nil {
		s.typing[threadID] = map[string]time.Time{}
	}
	s.typing[threadID][subjectID] = time.Now().Add(typingTTL)
}

func (s *Service) setProgress(offerID string, pct int) {
	if offerID == "" {
		return
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	s.mu.Lock()
	s.progress[offerID] = pct
	s.mu.Unlock()
}

func wireOf(t Thread) *wireThread {
	ids := make([]string, 0, len(t.Members))
	for _, m := range t.Members {
		ids = append(ids, m.SubjectID)
	}
	return &wireThread{ThreadID: t.ThreadID, Kind: t.Kind, Title: t.Title, OwnerID: t.OwnerID, MemberIDs: ids}
}

func derefThread(t *wireThread, threadID string) wireThread {
	if t != nil {
		return *t
	}
	return wireThread{ThreadID: threadID, Kind: "direct"}
}

func containsID(ids []string, id string) bool {
	for _, item := range ids {
		if item == id {
			return true
		}
	}
	return false
}

func writePlainFrame(conn net.Conn, f p2pFrame) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return writeRaw(conn, raw)
}

func readPlainFrame(conn net.Conn) (p2pFrame, error) {
	raw, err := readRaw(conn)
	if err != nil {
		return p2pFrame{}, err
	}
	var f p2pFrame
	if err := json.Unmarshal(raw, &f); err != nil {
		return p2pFrame{}, err
	}
	return f, nil
}

func writeEncFrame(conn net.Conn, aead cipher.AEAD, seq *uint64, f p2pFrame) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	*seq++
	nonce := make([]byte, aead.NonceSize())
	binary.BigEndian.PutUint64(nonce[len(nonce)-8:], *seq)
	return writeRaw(conn, append(nonce, aead.Seal(nil, nonce, raw, nil)...))
}

func readEncFrame(conn net.Conn, aead cipher.AEAD) (p2pFrame, error) {
	raw, err := readRaw(conn)
	if err != nil {
		return p2pFrame{}, err
	}
	ns := aead.NonceSize()
	if len(raw) < ns {
		return p2pFrame{}, ErrInvalid
	}
	plain, err := aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return p2pFrame{}, err
	}
	var f p2pFrame
	if err := json.Unmarshal(plain, &f); err != nil {
		return p2pFrame{}, err
	}
	return f, nil
}

func writeRaw(conn net.Conn, raw []byte) error {
	if len(raw) == 0 || len(raw) > maxFrame {
		return ErrTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(raw)))
	if _, err := conn.Write(hdr[:]); err != nil {
		return err
	}
	_, err := conn.Write(raw)
	return err
}

func readRaw(conn net.Conn) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > maxFrame {
		return nil, ErrTooLarge
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(conn, buf)
	return buf, err
}
