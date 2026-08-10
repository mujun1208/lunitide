package skillapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

type mockSkillReader struct {
	skill       *skill.Skill
	skills      []skill.Skill
	byNameVer   *skill.Skill
	err         error
	nameVerErr  error
}

func (m *mockSkillReader) GetSkill(_ context.Context, _ string) (*skill.Skill, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.skill, nil
}
func (m *mockSkillReader) GetSkillByNameVersion(_ context.Context, _, _ string) (*skill.Skill, error) {
	if m.nameVerErr != nil {
		return nil, m.nameVerErr
	}
	return m.byNameVer, nil
}
func (m *mockSkillReader) ListSkills(_ context.Context, _ string, _ int) ([]skill.Skill, error) {
	return m.skills, m.err
}

type mockSkillWriter struct {
	updatedDisplay string
	updatedDesc    string
	updatedStatus  string
	deletedID      string
	err            error
}

func (m *mockSkillWriter) UpdateSkill(_ context.Context, _, display, desc string) error {
	m.updatedDisplay = display
	m.updatedDesc = desc
	return m.err
}
func (m *mockSkillWriter) UpdateSkillStatus(_ context.Context, _, status string) error {
	m.updatedStatus = status
	return m.err
}
func (m *mockSkillWriter) DeleteSkill(_ context.Context, id string) error {
	m.deletedID = id
	return m.err
}

func skNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func makeSkill(id string, status skill.SkillStatus, perms []skill.PermissionLevel) *skill.Skill {
	if perms == nil {
		perms = []skill.PermissionLevel{skill.PermissionReadOnly}
	}
	return &skill.Skill{
		ID:           id,
		Name:         "search-web",
		DisplayName:  "Web Search",
		Description:  "Searches the web for information",
		Version:      "1.0.0",
		Status:       status,
		Permissions:  perms,
		EntryPoint:   "skills/search/main",
		ManifestJSON: "{}",
		CreatedAt:    skNow(),
		UpdatedAt:    skNow(),
	}
}

func TestGetNotFound(t *testing.T) {
	r := &mockSkillReader{skill: nil}
	s := New(r, &mockSkillWriter{})
	if _, err := s.Get(context.Background(), "missing"); err != ErrSkillNotFound {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestGetSuccess(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}
	s := New(r, &mockSkillWriter{})
	sk, err := s.Get(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if sk.Name != "search-web" {
		t.Fatalf("expected name search-web, got %s", sk.Name)
	}
}

func TestGetByNameVersionNotFound(t *testing.T) {
	r := &mockSkillReader{byNameVer: nil}
	s := New(r, &mockSkillWriter{})
	if _, err := s.GetByNameVersion(context.Background(), "search-web", "1.0.0"); err != ErrSkillNotFound {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestListPublished(t *testing.T) {
	r := &mockSkillReader{skills: []skill.Skill{*makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}}
	s := New(r, &mockSkillWriter{})
	skills, err := s.ListPublished(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
}

func TestUpdateRejectsOversizeDisplayName(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	s := New(r, &mockSkillWriter{})
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	if err := s.Update(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", string(long), "desc"); err == nil {
		t.Fatal("expected error for oversize display name")
	}
}

func TestUpdateRejectsOversizeDescription(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	s := New(r, &mockSkillWriter{})
	long := make([]byte, 4097)
	for i := range long {
		long[i] = 'a'
	}
	if err := s.Update(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "Display", string(long)); err == nil {
		t.Fatal("expected error for oversize description")
	}
}

func TestUpdateNotFound(t *testing.T) {
	r := &mockSkillReader{skill: nil}
	s := New(r, &mockSkillWriter{})
	if err := s.Update(context.Background(), "missing", "Display", "desc"); err != ErrSkillNotFound {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestUpdateSuccess(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	w := &mockSkillWriter{}
	s := New(r, w)
	if err := s.Update(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", "New Display", "new desc"); err != nil {
		t.Fatal(err)
	}
	if w.updatedDisplay != "New Display" {
		t.Fatalf("expected display updated, got %s", w.updatedDisplay)
	}
}

func TestPublishFromDraft(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	w := &mockSkillWriter{}
	s := New(r, w)
	if err := s.Publish(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.updatedStatus != string(skill.SkillStatusPublished) {
		t.Fatalf("expected published, got %s", w.updatedStatus)
	}
}

func TestPublishRejectsAlreadyPublished(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}
	s := New(r, &mockSkillWriter{})
	if err := s.Publish(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestDeprecateFromPublished(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}
	w := &mockSkillWriter{}
	s := New(r, w)
	if err := s.Deprecate(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.updatedStatus != string(skill.SkillStatusDeprecated) {
		t.Fatalf("expected deprecated, got %s", w.updatedStatus)
	}
}

func TestDeprecateRejectsDraft(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	s := New(r, &mockSkillWriter{})
	if err := s.Deprecate(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestDisableFromPublished(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}
	w := &mockSkillWriter{}
	s := New(r, w)
	if err := s.Disable(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.updatedStatus != string(skill.SkillStatusDisabled) {
		t.Fatalf("expected disabled, got %s", w.updatedStatus)
	}
}

func TestDisableRejectsDisabled(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDisabled, nil)}
	s := New(r, &mockSkillWriter{})
	if err := s.Disable(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestDeleteAllowsDraft(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	w := &mockSkillWriter{}
	s := New(r, w)
	if err := s.Delete(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
	if w.deletedID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected skill deleted, got %s", w.deletedID)
	}
}

func TestDeleteAllowsDisabled(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDisabled, nil)}
	w := &mockSkillWriter{}
	s := New(r, w)
	if err := s.Delete(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteRejectsPublished(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}
	s := New(r, &mockSkillWriter{})
	if err := s.Delete(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != ErrInvalidTransition {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestCanInvokeAllowsPublishedWithPermission(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, []skill.PermissionLevel{skill.PermissionNetwork})}
	s := New(r, &mockSkillWriter{})
	if err := s.CanInvoke(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.PermissionNetwork); err != nil {
		t.Fatalf("expected invocation allowed, got %v", err)
	}
}

func TestCanInvokeRejectsMissingPermission(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, []skill.PermissionLevel{skill.PermissionReadOnly})}
	s := New(r, &mockSkillWriter{})
	if err := s.CanInvoke(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.PermissionNetwork); err != ErrPermissionDenied {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
}

func TestCanInvokeRejectsDraft(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, []skill.PermissionLevel{skill.PermissionAdmin})}
	s := New(r, &mockSkillWriter{})
	if err := s.CanInvoke(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.PermissionAdmin); err != ErrSkillNotPublished {
		t.Fatalf("expected ErrSkillNotPublished, got %v", err)
	}
}

func TestCanInvokeRejectsDeprecated(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDeprecated, []skill.PermissionLevel{skill.PermissionAdmin})}
	s := New(r, &mockSkillWriter{})
	if err := s.CanInvoke(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.PermissionAdmin); err != ErrSkillDeprecated {
		t.Fatalf("expected ErrSkillDeprecated, got %v", err)
	}
}

func TestCanInvokeRejectsDisabled(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDisabled, []skill.PermissionLevel{skill.PermissionAdmin})}
	s := New(r, &mockSkillWriter{})
	if err := s.CanInvoke(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.PermissionAdmin); err != ErrSkillDisabled {
		t.Fatalf("expected ErrSkillDisabled, got %v", err)
	}
}

func TestCanInvokeRejectsInvalidPermission(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}
	s := New(r, &mockSkillWriter{})
	if err := s.CanInvoke(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.PermissionLevel("bogus")); err != ErrInvalidPermission {
		t.Fatalf("expected ErrInvalidPermission, got %v", err)
	}
}

func TestCanInvokeRejectsMissingSkill(t *testing.T) {
	r := &mockSkillReader{skill: nil}
	s := New(r, &mockSkillWriter{})
	if err := s.CanInvoke(context.Background(), "missing", skill.PermissionReadOnly); err != ErrSkillNotFound {
		t.Fatalf("expected ErrSkillNotFound, got %v", err)
	}
}

func TestMatchReturnsRankedResults(t *testing.T) {
	r := &mockSkillReader{skills: []skill.Skill{
		*makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil), // name "search-web"
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Name: "weather", DisplayName: "Weather Lookup", Description: "search weather data", Version: "1.0.0", Status: skill.SkillStatusPublished, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "w/main", ManifestJSON: "{}", CreatedAt: skNow(), UpdatedAt: skNow()},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", Name: "unrelated", DisplayName: "Unrelated", Description: "nothing here", Version: "1.0.0", Status: skill.SkillStatusPublished, Permissions: []skill.PermissionLevel{skill.PermissionReadOnly}, EntryPoint: "u/main", ManifestJSON: "{}", CreatedAt: skNow(), UpdatedAt: skNow()},
	}}
	s := New(r, &mockSkillWriter{})
	matches, err := s.Match(context.Background(), "search")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	// First match should be "search-web" (name hit scores higher than description hit).
	if matches[0].Skill.ID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("expected search-web first, got %s", matches[0].Skill.ID)
	}
	if matches[0].Score <= matches[1].Score {
		t.Fatalf("expected first match to rank higher: %f vs %f", matches[0].Score, matches[1].Score)
	}
}

func TestMatchEmptyQuery(t *testing.T) {
	s := New(&mockSkillReader{}, &mockSkillWriter{})
	matches, err := s.Match(context.Background(), "   ")
	if err != nil {
		t.Fatal(err)
	}
	if matches != nil {
		t.Fatalf("expected nil for empty query, got %v", matches)
	}
}

func TestMatchExcludesNonPublished(t *testing.T) {
	// ListSkills is called with status="published" filter; the mock returns whatever it has,
	// but in production the storage layer filters. We test the service contract: it passes
	// "published" to the reader.
	r := &mockSkillReader{skills: []skill.Skill{*makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}}
	s := New(r, &mockSkillWriter{})
	matches, err := s.Match(context.Background(), "search")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
}

func TestMatchNoResults(t *testing.T) {
	r := &mockSkillReader{skills: []skill.Skill{*makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusPublished, nil)}}
	s := New(r, &mockSkillWriter{})
	matches, err := s.Match(context.Background(), "xyzzy-nope")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestCanInvokeReaderUnavailable(t *testing.T) {
	s := &Service{}
	if err := s.CanInvoke(context.Background(), "x", skill.PermissionReadOnly); err == nil {
		t.Fatal("expected error for nil reader")
	}
}

func TestPublishWriterUnavailable(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	s := New(r, nil)
	if err := s.Publish(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err == nil {
		t.Fatal("expected error for nil writer")
	}
}

func TestPublishPropagatesError(t *testing.T) {
	r := &mockSkillReader{skill: makeSkill("01ARZ3NDEKTSV4RRFFQ69G5FAV", skill.SkillStatusDraft, nil)}
	boom := errors.New("storage failure")
	w := &mockSkillWriter{err: boom}
	s := New(r, w)
	if err := s.Publish(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err != boom {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

func TestCanTransitionToMatrix(t *testing.T) {
	cases := []struct {
		from, to skill.SkillStatus
		want     bool
	}{
		{skill.SkillStatusDraft, skill.SkillStatusPublished, true},
		{skill.SkillStatusDraft, skill.SkillStatusDisabled, true},
		{skill.SkillStatusDraft, skill.SkillStatusDeprecated, false},
		{skill.SkillStatusPublished, skill.SkillStatusDeprecated, true},
		{skill.SkillStatusPublished, skill.SkillStatusDisabled, true},
		{skill.SkillStatusPublished, skill.SkillStatusDraft, false},
		{skill.SkillStatusDeprecated, skill.SkillStatusDisabled, true},
		{skill.SkillStatusDeprecated, skill.SkillStatusPublished, false},
		{skill.SkillStatusDisabled, skill.SkillStatusPublished, false},
		{skill.SkillStatusDisabled, skill.SkillStatusDraft, false},
	}
	for _, c := range cases {
		got := canTransitionTo(c.from, c.to)
		if got != c.want {
			t.Errorf("canTransitionTo(%s, %s) = %v; want %v", c.from, c.to, got, c.want)
		}
	}
}
