package identity

import (
	"context"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memStore struct {
	rec Record
	ok  bool
}

func (m *memStore) LoadIdentity(context.Context) (Record, bool, error) { return m.rec, m.ok, nil }
func (m *memStore) InsertIdentity(_ context.Context, rec Record) error {
	m.rec, m.ok = rec, true
	return nil
}
func (m *memStore) UpdateIdentity(_ context.Context, rec Record) error { m.rec = rec; return nil }
func (m *memStore) RebindLegacySubject(context.Context, string, string) error {
	return nil
}
func (m *memStore) UpsertSelfContact(context.Context, Record) error { return nil }

func TestEnsureCreatesUnlockedIdentity(t *testing.T) {
	svc := New(&memStore{})
	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	pub := svc.Public()
	if pub.Nickname != DefaultNickname || pub.Locked || pub.DiscoveryEnabled || len(pub.PairingCode) != 6 {
		t.Fatalf("public = %+v", pub)
	}
	if svc.SubjectID() == legacySubject {
		t.Fatal("subject should be a ULID")
	}
}

func TestUpdateRejectsEmptyNickname(t *testing.T) {
	svc := New(&memStore{})
	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	empty := ""
	if _, err := svc.Update(context.Background(), ProfilePatch{Nickname: &empty}); err != ErrInvalidProfile {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateAvatarFromJPEGPath(t *testing.T) {
	svc := New(&memStore{})
	if err := svc.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "face.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	pub, err := svc.Update(context.Background(), ProfilePatch{Avatar: &path})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub.Avatar, "data:image/jpeg;base64,") {
		t.Fatalf("avatar = %q", pub.Avatar)
	}
	if len(pub.Avatar) > maxAvatar {
		t.Fatalf("avatar too large: %d", len(pub.Avatar))
	}
}
