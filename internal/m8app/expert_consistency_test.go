package m8app_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

type brokenPersonaStore struct{}

func (brokenPersonaStore) Put(string, []byte) error { return errors.New("fixture disk full") }
func (brokenPersonaStore) Get(string) ([]byte, bool, error) {
	return nil, false, errors.New("fixture read failed")
}

func TestExpertBodyWriteFailureDoesNotPublishCatalogOrVersion(t *testing.T) {
	store := openSliceStore(t)
	ctx := context.Background()
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", brokenPersonaStore{})
	_, err := svc.Create(ctx, m8app.CreateInput{Source: m8core.ExpertSourceLocal, Frontmatter: fm("failure"), SixSection: sixBody("failure"), RequestID: "create"})
	if err == nil {
		t.Fatal("disk full reported successful expert creation")
	}
	list, err := svc.List(ctx, m8app.ExpertFilter{})
	if err != nil || len(list.Experts) != 0 {
		t.Fatalf("failed body published catalog: %+v %v", list, err)
	}
	persona := &m8app.MemoryPersonaStore{}
	svc.SetPersonaStore(persona)
	created := createExpert(t, svc, "valid")
	svc.SetPersonaStore(brokenPersonaStore{})
	var six map[string]string
	_ = json.Unmarshal([]byte(sixBody("update").CanonicalJSON()), &six)
	if _, err = svc.Update(ctx, m8app.UpdateInput{ExpertID: created.ExpertID, ExpectedVersionID: created.VersionID, SixSection: six, ChangeNote: "fixture"}); err == nil {
		t.Fatal("failed body advanced version")
	}
	svc.SetPersonaStore(persona)
	detail, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID})
	if err != nil || len(detail.Versions) != 1 || detail.Expert["currentVersionId"] != created.VersionID {
		t.Fatalf("failed update advanced metadata: %+v %v", detail, err)
	}
}

type failingEquipmentUOW struct{ base m8app.ExpertUnitOfWork }

func (u failingEquipmentUOW) TransactExpert(ctx context.Context, fn func(m8app.ExpertTx) error) error {
	return u.base.TransactExpert(ctx, func(tx m8app.ExpertTx) error { return fn(failingEquipmentTx{tx}) })
}

type failingEquipmentTx struct{ m8app.ExpertTx }

func (failingEquipmentTx) ReplaceExpertSkillKeys(string, []string) error {
	return errors.New("fixture equipment storage failure")
}

func TestExpertEquipmentFailureRollsBackCatalogAndVersion(t *testing.T) {
	store := openSliceStore(t)
	ctx := context.Background()
	svc := m8app.NewExpertService(failingEquipmentUOW{store.AgentRuntimeRepository()}, "local-user", &m8app.MemoryPersonaStore{})
	_, err := svc.Create(ctx, m8app.CreateInput{Source: m8core.ExpertSourceLocal, Frontmatter: fm("gear"), SixSection: sixBody("gear"), RequestID: "gear", SkillKeys: []string{"fixture"}})
	if err == nil {
		t.Fatal("equipment failure reported success")
	}
	list, err := svc.List(ctx, m8app.ExpertFilter{})
	if err != nil || len(list.Experts) != 0 {
		t.Fatalf("equipment failure left partial expert: %+v %v", list, err)
	}
}

func TestExpertDetailRejectsForeignVersionAndCorruptBody(t *testing.T) {
	store := openSliceStore(t)
	ctx := context.Background()
	persona := &m8app.MemoryPersonaStore{}
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", persona)
	a, b := createExpert(t, svc, "A"), createExpert(t, svc, "B")
	if _, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: a.ExpertID, VersionID: b.VersionID}); !errors.Is(err, m8app.ErrExpertNotFound) {
		t.Fatalf("foreign version accepted: %v", err)
	}
	_ = persona.Put(fm("A").PersonaRef(sixBody("A")), []byte(`{}`))
	if _, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: a.ExpertID}); !errors.Is(err, m8app.ErrExpertBodyUnavailable) {
		t.Fatalf("corrupt body projected as normal: %v", err)
	}
}
