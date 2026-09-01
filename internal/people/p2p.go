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
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/identity"
)

const maxFrame = 96 << 10

type fileUpload struct {
	mu   sync.Mutex
	name string
	mime string
	path string
	file *os.File
	size int64
}

type incomingFile struct {
	file   *os.File
	path   string
	size   int64
	got    int64
	msg    Message
	thread wireThread
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
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	peer, aead, err := s.handshake(conn, inbound)
	if err != nil {
		return
	}
	host, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	_ = s.IngestBeacon(context.Background(), peer.beacon(), host)
	contact, err := s.store.GetContact(context.Background(), peer.SubjectID)
	if err != nil || contact.Blocked {
		return
	}
	seq := uint64(0)
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
		_ = s.push(member.HostAddr, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			frame := p2pFrame{Typ: "msg", V: 1, Thread: wireOf(t), Message: &msg}
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
		_ = s.push(member.HostAddr, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			return writeEncFrame(conn, aead, seq, p2pFrame{Typ: "thread", V: 1, Thread: wireOf(t)})
		})
	}
}

func (s *Service) deliverTyping(t Thread) {
	for _, member := range t.Members {
		if member.SubjectID == s.identity.SubjectID() || member.Blocked || strings.TrimSpace(member.HostAddr) == "" {
			continue
		}
		_ = s.push(member.HostAddr, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			return writeEncFrame(conn, aead, seq, p2pFrame{Typ: "typing", V: 1, ThreadID: t.ThreadID, SubjectID: s.identity.SubjectID()})
		})
	}
}

func (s *Service) deliverRead(t Thread, at string) {
	for _, member := range t.Members {
		if member.SubjectID == s.identity.SubjectID() || member.Blocked || strings.TrimSpace(member.HostAddr) == "" {
			continue
		}
		_ = s.push(member.HostAddr, func(conn net.Conn, aead cipher.AEAD, seq *uint64) error {
			return writeEncFrame(conn, aead, seq, p2pFrame{Typ: "read", V: 1, ThreadID: t.ThreadID, SubjectID: s.identity.SubjectID(), At: at})
		})
	}
}

func (s *Service) push(addr string, fn func(net.Conn, cipher.AEAD, *uint64) error) error {
	addr, err := parsePeerAddr(addr)
	if err != nil {
		return err
	}
	_ = s.ensureTCP()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return ErrUnreachable
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))
	_, aead, err := s.handshake(conn, false)
	if err != nil {
		return err
	}
	seq := uint64(0)
	return fn(conn, aead, &seq)
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
	ctx := context.Background()
	switch frame.Typ {
	case "msg":
		if from.TrustState != "trusted" && from.TrustState != "self" {
			return
		}
		if frame.Message == nil {
			return
		}
		s.receiveMessage(ctx, from, frame)
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
		s.writeIncoming(frame)
	case "file-end":
		s.finishIncoming(ctx, from, frame)
	case "typing":
		if from.TrustState != "trusted" {
			return
		}
		s.noteRemoteTyping(frame.ThreadID, from.SubjectID)
	case "read":
		if from.TrustState != "trusted" || frame.ThreadID == "" {
			return
		}
		_ = s.store.MarkThreadRead(ctx, frame.ThreadID, from.SubjectID, nonempty(frame.At, nowRFC3339()))
	}
}

func (s *Service) receiveMessage(ctx context.Context, from Contact, frame p2pFrame) {
	msg := *frame.Message
	if msg.Kind == "file" || msg.Kind == "image" {
		return
	}
	ok, err := s.store.HasPeopleMessage(ctx, msg.MessageID)
	if err != nil || ok {
		return
	}
	thread, err := s.ensureRemoteThread(ctx, derefThread(frame.Thread, msg.ThreadID), from)
	if err != nil {
		return
	}
	msg.ThreadID = thread.ThreadID
	_ = s.store.InsertMessage(ctx, msg, nil)
}

func (s *Service) beginIncoming(from Contact, frame p2pFrame) {
	if frame.OfferID == "" || frame.Size <= 0 || frame.Size > maxFileBytes {
		return
	}
	if s.stagingDir == "" {
		return
	}
	_ = os.MkdirAll(s.stagingDir, 0o700)
	path := filepath.Join(s.stagingDir, frame.OfferID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	msg := Message{OfferID: frame.OfferID, Kind: "file", FileName: frame.FileName, FileMIME: frame.FileMIME, FileSize: frame.Size, FileSHA256: frame.SHA256}
	if frame.Message != nil {
		msg = *frame.Message
	}
	th := wireThread{}
	if frame.Thread != nil {
		th = *frame.Thread
	} else if frame.Message != nil {
		th.ThreadID = frame.Message.ThreadID
	}
	s.mu.Lock()
	s.incoming[frame.OfferID] = &incomingFile{file: f, path: path, size: frame.Size, msg: msg, thread: th}
	s.mu.Unlock()
}

func (s *Service) writeIncoming(frame p2pFrame) {
	raw, err := base64.StdEncoding.DecodeString(frame.Data)
	if err != nil {
		return
	}
	s.mu.Lock()
	in := s.incoming[frame.OfferID]
	s.mu.Unlock()
	if in == nil || in.file == nil {
		return
	}
	if in.got+int64(len(raw)) > maxFileBytes {
		return
	}
	if _, err := in.file.Write(raw); err != nil {
		return
	}
	in.got += int64(len(raw))
	if in.size > 0 {
		s.setProgress(frame.OfferID, int(in.got*100/in.size))
	}
}

func (s *Service) finishIncoming(ctx context.Context, from Contact, frame p2pFrame) {
	s.mu.Lock()
	in := s.incoming[frame.OfferID]
	delete(s.incoming, frame.OfferID)
	s.mu.Unlock()
	if in == nil {
		return
	}
	if in.file != nil {
		_ = in.file.Close()
	}
	sum, size, err := hashFile(in.path)
	if err != nil || (in.msg.FileSHA256 != "" && sum != in.msg.FileSHA256) {
		_ = os.Remove(in.path)
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
	_ = s.store.InsertMessage(ctx, in.msg, &offer)
	s.setProgress(offer.OfferID, 100)
}

func (s *Service) ensureRemoteThread(ctx context.Context, spec wireThread, from Contact) (Thread, error) {
	self := s.identity.SubjectID()
	if spec.Kind != "group" {
		if t, ok, err := s.store.FindDirectThread(ctx, self, from.SubjectID); err != nil {
			return Thread{}, err
		} else if ok {
			return t, nil
		}
	} else if spec.ThreadID != "" {
		if t, err := s.store.GetThread(ctx, spec.ThreadID); err == nil {
			return t, nil
		}
	}
	if spec.ThreadID == "" {
		return Thread{}, ErrInvalid
	}
	kind := spec.Kind
	if kind == "" {
		kind = "direct"
	}
	now := nowRFC3339()
	ids := spec.MemberIDs
	if len(ids) == 0 {
		ids = []string{self, from.SubjectID}
	}
	if !containsID(ids, self) {
		ids = append(ids, self)
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
		return Thread{}, err
	}
	return s.store.GetThread(ctx, t.ThreadID)
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
