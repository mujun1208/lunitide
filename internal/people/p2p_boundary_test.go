package people

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type boundaryStore struct {
	Store
	thread   Thread
	contacts map[string]Contact
	inserted []Message
	reads    int
}

func (s *boundaryStore) GetContact(_ context.Context, id string) (Contact, error) {
	if c, ok := s.contacts[id]; ok {
		return c, nil
	}
	return Contact{}, ErrNotFound
}
func (s *boundaryStore) HasPeopleMessage(context.Context, string) (bool, error) { return false, nil }
func (s *boundaryStore) FindDirectThread(context.Context, string, string) (Thread, bool, error) {
	return s.thread, true, nil
}
func (s *boundaryStore) GetThread(context.Context, string) (Thread, error) { return s.thread, nil }
func (s *boundaryStore) InsertMessage(_ context.Context, m Message, _ *FileOffer) error {
	s.inserted = append(s.inserted, m)
	return nil
}
func (s *boundaryStore) MarkThreadRead(context.Context, string, string, string) error {
	s.reads++
	return nil
}

type boundaryIdentity struct {
	Identity
	locked bool
}

func (i *boundaryIdentity) Locked() bool { return i.locked }

func (*boundaryIdentity) SubjectID() string { return "self" }

func boundaryService(t *testing.T) (*Service, *boundaryStore, Contact, Contact) {
	t.Helper()
	peer := Contact{SubjectID: "peer", TrustState: "trusted", PublicKey: "authenticated-key"}
	other := Contact{SubjectID: "other", TrustState: "trusted", PublicKey: "other-key"}
	st := &boundaryStore{thread: Thread{ThreadID: "group", Kind: "group", Members: []Contact{{SubjectID: "self"}, peer}}, contacts: map[string]Contact{peer.SubjectID: peer, other.SubjectID: other}}
	s := New(st, &boundaryIdentity{}, t.TempDir(), t.TempDir())
	t.Cleanup(s.Close)
	return s, st, peer, other
}

func TestAuthenticatedPeerCannotForgeSenderOrEnterAnotherGroup(t *testing.T) {
	s, st, peer, other := boundaryService(t)
	frame := p2pFrame{Typ: "msg", Thread: &wireThread{ThreadID: "group", Kind: "group"}, Message: &Message{MessageID: "message", ThreadID: "group", SenderID: "self", Kind: "text", Body: "test"}}
	s.handleFrame(nil, nil, nil, peer, frame)
	frame.Message.SenderID = other.SubjectID
	s.handleFrame(nil, nil, nil, other, frame)
	if len(st.inserted) != 0 {
		t.Fatal("forged sender or group outsider persisted")
	}
	frame.Message.SenderID = peer.SubjectID
	s.handleFrame(nil, nil, nil, peer, frame)
	if len(st.inserted) != 1 || st.inserted[0].SenderID != peer.SubjectID {
		t.Fatal("authorized member message was lost")
	}
}

func TestEveryFrameRechecksTrustAndThreadMembership(t *testing.T) {
	s, st, peer, other := boundaryService(t)
	for _, typ := range []string{"typing", "read"} {
		s.handleFrame(nil, nil, nil, other, p2pFrame{Typ: typ, ThreadID: "group", SubjectID: other.SubjectID})
	}
	if st.reads != 0 || len(s.typing) != 0 {
		t.Fatal("outsider updated group activity")
	}
	blocked := peer
	blocked.Blocked = true
	st.contacts[peer.SubjectID] = blocked
	s.handleFrame(nil, nil, nil, peer, p2pFrame{Typ: "msg", Message: &Message{MessageID: "late", ThreadID: "group", SenderID: peer.SubjectID, Kind: "text", Body: "revoked"}})
	if len(st.inserted) != 0 {
		t.Fatal("stale socket grant survived blocking")
	}
}

func TestRemoteThreadCannotRelabelAnExistingGroupAsDirect(t *testing.T) {
	s, _, peer, other := boundaryService(t)
	for _, from := range []Contact{peer, other} {
		if _, err := s.ensureRemoteThread(context.Background(), wireThread{ThreadID: "group", Kind: "direct"}, from); err == nil {
			t.Fatal("remote thread kind overrode authoritative group")
		}
	}
}

func TestCloseTerminatesTrackedConnectionAndRejectsLateFrames(t *testing.T) {
	s, st, peer, _ := boundaryService(t)
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()
	if !s.trackConnection(local) {
		t.Fatal("live service rejected connection")
	}
	s.Close()
	_ = remote.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := remote.Read(make([]byte, 1)); err == nil {
		t.Fatal("closed service left socket open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("close did not terminate active socket")
	}
	if s.trackConnection(local) {
		t.Fatal("closed service accepted connection")
	}
	if err := s.StartTCP(); err == nil {
		t.Fatal("closed service restarted listener")
	}
	s.handleFrame(nil, nil, nil, peer, p2pFrame{Typ: "msg", Thread: &wireThread{ThreadID: "group", Kind: "group"}, Message: &Message{MessageID: "late-after-close", ThreadID: "group", SenderID: peer.SubjectID, Kind: "text", Body: "late"}})
	if len(st.inserted) != 0 {
		t.Fatal("closed service persisted late frame")
	}
}

func TestIncomingFileBindsOwnerSequenceSizeAndDigest(t *testing.T) {
	s, st, peer, other := boundaryService(t)
	payload := []byte("abc")
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	msg := Message{MessageID: "file-message", ThreadID: "group", SenderID: peer.SubjectID, Kind: "file", OfferID: "../sender-controlled", FileSize: 3, FileSHA256: digest}
	begin := p2pFrame{Typ: "file-begin", OfferID: msg.OfferID, Size: 3, SHA256: digest, Message: &msg, Thread: &wireThread{ThreadID: "group", Kind: "group"}}
	s.handleFrame(nil, nil, nil, peer, begin)
	in := s.incoming[msg.OfferID]
	if in == nil {
		t.Fatal("valid offer rejected")
	}
	if filepath.Dir(in.path) != s.stagingDir {
		t.Fatal("offer ID escaped staging root")
	}
	chunk := p2pFrame{Typ: "file-chunk", OfferID: msg.OfferID, Seq: 1, Data: base64.StdEncoding.EncodeToString(payload)}
	s.handleFrame(nil, nil, nil, other, chunk)
	chunk.Seq = 2
	s.handleFrame(nil, nil, nil, peer, chunk)
	if in.got != 0 {
		t.Fatal("foreign or out-of-order chunk accepted")
	}
	chunk.Seq = 1
	s.handleFrame(nil, nil, nil, peer, chunk)
	s.handleFrame(nil, nil, nil, peer, chunk) // duplicate frame must not append twice
	s.handleFrame(nil, nil, nil, peer, begin) // duplicate begin must not truncate
	end := p2pFrame{Typ: "file-end", OfferID: msg.OfferID, SHA256: digest, Last: true}
	s.handleFrame(nil, nil, nil, other, end)
	if s.incoming[msg.OfferID] != in || in.got != 3 {
		t.Fatal("foreign completion or replay corrupted transfer")
	}
	s.handleFrame(nil, nil, nil, peer, end)
	if len(st.inserted) != 1 || st.inserted[0].SenderID != peer.SubjectID {
		t.Fatal("valid file not persisted")
	}
	got, err := os.ReadFile(in.path)
	if err != nil || string(got) != "abc" {
		t.Fatalf("payload changed: %q %v", got, err)
	}
}

func TestIncompleteIncomingFileNeverBecomesAnOffer(t *testing.T) {
	s, st, peer, _ := boundaryService(t)
	hash := sha256.Sum256([]byte("abc"))
	digest := hex.EncodeToString(hash[:])
	msg := Message{MessageID: "file-message", ThreadID: "group", SenderID: peer.SubjectID, Kind: "file", OfferID: "offer", FileSize: 3, FileSHA256: digest}
	s.handleFrame(nil, nil, nil, peer, p2pFrame{Typ: "file-begin", OfferID: "offer", Size: 3, SHA256: digest, Message: &msg, Thread: &wireThread{ThreadID: "group", Kind: "group"}})
	in := s.incoming["offer"]
	if in == nil {
		t.Fatal("valid begin rejected")
	}
	s.handleFrame(nil, nil, nil, peer, p2pFrame{Typ: "file-end", OfferID: "offer", SHA256: digest, Last: true})
	if len(st.inserted) != 0 {
		t.Fatal("incomplete file became usable")
	}
	if _, err := os.Stat(in.path); !os.IsNotExist(err) {
		t.Fatalf("incomplete staging retained: %v", err)
	}
}
