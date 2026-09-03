package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mroapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func mroMutationRequest(method, payload, key string) bridge.Request {
	return bridge.Request{
		Version: bridge.Version, Kind: "request",
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Method: method, SentAt: time.Now().UTC(),
		Payload: json.RawMessage(payload), DeadlineMS: 3000, IdempotencyKey: key,
	}
}

func newMROEngineWithKB(t *testing.T) (*Engine, *m8app.KBService) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "mro-reg.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	kb := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	e := NewEngine(nil, "test")
	e.SetM8SliceServices(kb, nil, nil)
	e.SetMROService(mroapp.New(store))
	return e, kb
}

func TestMROManualRegisterRejectsUnindexedDocument(t *testing.T) {
	e, _ := newMROEngineWithKB(t)
	payload := `{"docType":"AMM","revision":"42","status":"controlled","documents":[{"documentId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","partNo":1}]}`
	resp := e.Handle(context.Background(), mroMutationRequest("mro.manual.register", payload, "idem-reject"))
	if resp.OK {
		t.Fatalf("register with a dangling document must fail, got OK: %+v", resp.Payload)
	}
	if resp.Error == nil || resp.Error.Code != "MRO-DOC-NOT-READY" {
		t.Fatalf("want MRO-DOC-NOT-READY, got %+v", resp.Error)
	}
}

func TestExpertKnowledgeIngestProducesReadyDocForRegister(t *testing.T) {
	e, _ := newMROEngineWithKB(t)
	ctx := context.Background()
	expertID := ulid.Make().String()
	path := filepath.Join(t.TempDir(), "amm-42.md")
	body := []byte("# ATA 32\n\nGear retraction fault isolation procedure.\n\n# ATA 33\n\nCabin lights.")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	ingestPayload := `{"expertId":"` + expertID + `","path":` + jsonString(path) + `,"sourceLocator":"mro://AMM/42?ata=32&status=uncontrolled","mediaType":"text/markdown"}`
	ing := e.Handle(ctx, nominationRequest("expert.knowledge.ingest", ingestPayload))
	if !ing.OK {
		t.Fatalf("ingest should succeed: %+v", ing.Error)
	}
	var body2 struct {
		CollectionID string `json:"collectionId"`
		Documents    []struct {
			DocumentID string `json:"documentId"`
			IndexState string `json:"indexState"`
		} `json:"documents"`
	}
	raw, _ := json.Marshal(ing.Payload)
	if err := json.Unmarshal(raw, &body2); err != nil {
		t.Fatal(err)
	}
	if len(body2.CollectionID) != 26 || len(body2.Documents) == 0 {
		t.Fatalf("ingest payload = %+v", body2)
	}
	if body2.Documents[0].IndexState != "ready" {
		t.Fatalf("want ready doc, got %+v", body2.Documents[0])
	}
	regPayload := `{"docType":"AMM","revision":"42","status":"uncontrolled","documents":[{"documentId":"` + body2.Documents[0].DocumentID + `","partNo":1}]}`
	reg := e.Handle(ctx, mroMutationRequest("mro.manual.register", regPayload, "idem-chain"))
	if !reg.OK {
		t.Fatalf("register with ingested doc should succeed: %+v", reg.Error)
	}
}

func TestMROManualRegisterAcceptsReadyDocument(t *testing.T) {
	e, kb := newMROEngineWithKB(t)
	ctx := context.Background()
	expertID := ulid.Make().String()
	coll, err := kb.EnsureExpertCollection(ctx, expertID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "amm.md")
	body := []byte("# ATA 32\n\nGear retraction fault isolation procedure.")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	docID := ulid.Make().String()
	if _, err := kb.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: coll.CollectionID, DocumentID: docID,
		MediaType: "text/markdown", ContentRef: path, SHA256: hex.EncodeToString(sum[:]),
		SourceLocator: "mro://AMM/42?ata=32&status=controlled",
		Projector:     m8app.ParseBodyIndexer,
	}); err != nil {
		t.Fatal(err)
	}
	payload := `{"docType":"AMM","revision":"42","status":"controlled","documents":[{"documentId":"` + docID + `","partNo":1}]}`
	resp := e.Handle(ctx, mroMutationRequest("mro.manual.register", payload, "idem-ok"))
	if !resp.OK {
		t.Fatalf("register with a ready document should succeed: %+v", resp.Error)
	}
}

func TestMROChecklistBuildDropsUncitedSteps(t *testing.T) {
	e := NewEngine(nil, "test")
	resp := e.Handle(context.Background(), nominationRequest("mro.checklist.build", `{
		"steps":["隔离液压","无引用"],
		"cites":[{"revision":"42","ata":"32"}]
	}`))
	if !resp.OK {
		t.Fatalf("checklist.build = %+v", resp.Error)
	}
	var body struct {
		Banner string `json:"banner"`
		Steps  []struct {
			N        int    `json:"n"`
			Text     string `json:"text"`
			Revision string `json:"revision"`
		} `json:"steps"`
	}
	raw, _ := json.Marshal(resp.Payload)
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Banner != "辅助建议，不构成放行" || len(body.Steps) != 1 || body.Steps[0].Text != "隔离液压" {
		t.Fatalf("payload = %+v", body)
	}
}

func TestMROAuditListEmptyWhenKBMissing(t *testing.T) {
	e := NewEngine(nil, "test")
	resp := e.Handle(context.Background(), nominationRequest("mro.audit.list", `{}`))
	if !resp.OK {
		t.Fatalf("audit.list = %+v", resp.Error)
	}
}

func TestMROAuditListReturnsKBEvents(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "mro-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	e := NewEngine(nil, "test")
	e.SetM8SliceServices(m8app.NewKBService(store.AgentRuntimeRepository(), "local-user"), nil, nil)
	resp := e.Handle(context.Background(), nominationRequest("mro.audit.list", `{"limit":10}`))
	if !resp.OK {
		t.Fatalf("audit.list = %+v", resp.Error)
	}
}
