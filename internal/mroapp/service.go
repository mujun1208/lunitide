package mroapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	ErrServiceUnavailable = errors.New("mroapp: service unavailable")
	ErrPayloadInvalid     = errors.New("mroapp: payload invalid")
	ErrDuplicateTail      = errors.New("mroapp: duplicate tail")
	ErrCheckoutBlocked    = errors.New("mroapp: checkout blocked")
	ErrNotFound           = errors.New("mroapp: not found")
)

type CheckoutBlockedError struct{ Reason string }

func (e *CheckoutBlockedError) Error() string {
	if e == nil || e.Reason == "" {
		return ErrCheckoutBlocked.Error()
	}
	return ErrCheckoutBlocked.Error() + ": " + e.Reason
}

func (e *CheckoutBlockedError) Is(target error) bool {
	return target == ErrCheckoutBlocked
}

var legalDocTypes = map[string]struct{}{
	"AMM": {}, "IPC": {}, "TSM": {}, "FIM": {}, "WDM": {}, "CMM": {},
	"MEL": {}, "SB": {}, "AD": {}, "EO": {}, "POLICY": {},
}

var legalManualStatus = map[string]struct{}{
	"controlled": {}, "uncontrolled": {}, "superseded": {},
}

type Aircraft struct {
	AircraftID string `json:"aircraftId"`
	TailNo     string `json:"tailNo"`
	MSN        string `json:"msn"`
	Model      string `json:"model"`
	Config     string `json:"config"`
	CreatedAt  string `json:"createdAt"`
}

type AircraftInput struct {
	AircraftID string
	TailNo     string
	MSN        string
	Model      string
	Config     string
}

type ManualDocInput struct {
	DocumentID string
	PartNo     int
}

type ManualInput struct {
	ManualID  string
	Title     string
	DocType   string
	Revision  string
	Status    string
	ATA       string
	Documents []ManualDocInput
}

type Manual struct {
	ManualID     string `json:"manualId"`
	Title        string `json:"title"`
	DocType      string `json:"docType"`
	Revision     string `json:"revision"`
	Status       string `json:"status"`
	ATA          string `json:"ata"`
	SectionCount int    `json:"sectionCount"`
	CreatedAt    string `json:"createdAt"`
}

type Store interface {
	UpsertAircraft(ctx context.Context, row Aircraft) error
	ListAircraft(ctx context.Context) ([]Aircraft, error)
	RegisterManual(ctx context.Context, row Manual, docs []ManualDocInput) error
	ListManuals(ctx context.Context) ([]Manual, error)
}

// OpsStore is optional. sqlite implements it; unit fakes do not.
type OpsStore interface {
	ListDueItems(ctx context.Context) ([]DueItem, error)
	UpsertDueItem(ctx context.Context, row DueItem) error
	ListTools(ctx context.Context) ([]Tool, error)
	UpsertTool(ctx context.Context, row Tool) error
	InsertToolLoan(ctx context.Context, row ToolLoan) error
	ListChemLots(ctx context.Context) ([]ChemLot, error)
	ListChemUses(ctx context.Context) ([]ChemUse, error)
	UpsertChemLot(ctx context.Context, row ChemLot) error
	InsertChemUse(ctx context.Context, row ChemUse) error
	ListKits(ctx context.Context) ([]Kit, error)
	ListKitItems(ctx context.Context) ([]KitItem, error)
	UpsertKit(ctx context.Context, row Kit) error
	UpsertKitItem(ctx context.Context, row KitItem) error
	ListPartsStock(ctx context.Context) ([]PartsStock, error)
	ListAlternates(ctx context.Context) ([]Alternate, error)
	UpsertPartsStock(ctx context.Context, row PartsStock) error
	UpsertAlternate(ctx context.Context, row Alternate) error
	ListWorkPackages(ctx context.Context) ([]WorkPackage, error)
	UpsertWorkPackage(ctx context.Context, row WorkPackage) error
	ListScheduleAssignments(ctx context.Context) ([]ScheduleAssignment, error)
	ListCapacitySlots(ctx context.Context) ([]CapacitySlot, error)
	ListIntervalRules(ctx context.Context) ([]IntervalRule, error)
	UpsertIntervalRule(ctx context.Context, row IntervalRule) error
	InsertIntervalChangeDraft(ctx context.Context, taskKey, mpdCite, fleetCite, createdAt string) error
	ListAOGTails(ctx context.Context) ([]string, error)
	InsertOpsTodos(ctx context.Context, rows []OpsTodo) error
	ListOpsTodos(ctx context.Context) ([]OpsTodo, error)
	// P1 write-path additions.
	RecordUtilization(ctx context.Context, row UtilizationEvent) error
	ListUtilizationEvents(ctx context.Context) ([]UtilizationEvent, error)
	CloseOpenToolLoan(ctx context.Context, toolID, inAt string) error
	InsertScheduleAssignment(ctx context.Context, id string, row ScheduleAssignment) error
	UpsertCapacitySlot(ctx context.Context, skill string, hours float64) error
	InsertWorkPackageTasks(ctx context.Context, packageID string, taskKeys []string) error
	ListWorkPackageTasks(ctx context.Context, packageID string) ([]string, error)
	// P2 low-altitude / AOG / PO additions.
	UpsertComponent(ctx context.Context, row Component) error
	ListComponents(ctx context.Context) ([]Component, error)
	InsertLifeEvent(ctx context.Context, row LifeEvent) error
	ListLifeEvents(ctx context.Context) ([]LifeEvent, error)
	InsertPirepDraft(ctx context.Context, row PirepDraft) error
	ListPirepDrafts(ctx context.Context) ([]PirepDraft, error)
	UpdatePirepState(ctx context.Context, id, state string) error
	InsertAOGCase(ctx context.Context, row AOGCase) error
	ListAOGCases(ctx context.Context) ([]AOGCase, error)
	UpdateAOGState(ctx context.Context, id, state string) error
	InsertPODraft(ctx context.Context, row PODraft) error
	ListPODrafts(ctx context.Context) ([]PODraft, error)
	UpdatePOState(ctx context.Context, id, state string) error
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	store Store
	clock Clock
}

func New(store Store) *Service { return &Service{store: store, clock: systemClock{}} }

func (s *Service) UpsertAircraft(ctx context.Context, in AircraftInput) (Aircraft, error) {
	if s == nil || s.store == nil {
		return Aircraft{}, ErrServiceUnavailable
	}
	tail := strings.TrimSpace(in.TailNo)
	model := strings.TrimSpace(in.Model)
	if tail == "" || len(tail) > 32 || model == "" || len(model) > 64 {
		return Aircraft{}, ErrPayloadInvalid
	}
	msn := strings.TrimSpace(in.MSN)
	if len(msn) > 32 {
		return Aircraft{}, ErrPayloadInvalid
	}
	config := strings.TrimSpace(in.Config)
	if len(config) > 128 {
		return Aircraft{}, ErrPayloadInvalid
	}
	id := strings.TrimSpace(in.AircraftID)
	if id == "" {
		id = ulid.Make().String()
	}
	if len(id) != 26 {
		return Aircraft{}, ErrPayloadInvalid
	}
	row := Aircraft{
		AircraftID: id, TailNo: tail, MSN: msn, Model: model, Config: config,
		CreatedAt: s.clock.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.store.UpsertAircraft(ctx, row); err != nil {
		return Aircraft{}, err
	}
	return row, nil
}

func (s *Service) ListAircraft(ctx context.Context) ([]Aircraft, error) {
	if s == nil || s.store == nil {
		return nil, ErrServiceUnavailable
	}
	return s.store.ListAircraft(ctx)
}

func (s *Service) RegisterManual(ctx context.Context, in ManualInput) (Manual, error) {
	if s == nil || s.store == nil {
		return Manual{}, ErrServiceUnavailable
	}
	docType := strings.TrimSpace(in.DocType)
	rev := strings.TrimSpace(in.Revision)
	status := strings.TrimSpace(in.Status)
	if _, ok := legalDocTypes[docType]; !ok {
		return Manual{}, ErrPayloadInvalid
	}
	if _, ok := legalManualStatus[status]; !ok {
		return Manual{}, ErrPayloadInvalid
	}
	if rev == "" || len(rev) > 64 {
		return Manual{}, ErrPayloadInvalid
	}
	title := strings.TrimSpace(in.Title)
	if len(title) > 256 {
		return Manual{}, ErrPayloadInvalid
	}
	ata := strings.TrimSpace(in.ATA)
	if len(ata) > 16 {
		return Manual{}, ErrPayloadInvalid
	}
	if len(in.Documents) == 0 {
		return Manual{}, ErrPayloadInvalid
	}
	for _, d := range in.Documents {
		if len(strings.TrimSpace(d.DocumentID)) != 26 || d.PartNo < 1 {
			return Manual{}, ErrPayloadInvalid
		}
	}
	id := strings.TrimSpace(in.ManualID)
	if id == "" {
		id = ulid.Make().String()
	}
	if len(id) != 26 {
		return Manual{}, ErrPayloadInvalid
	}
	row := Manual{
		ManualID: id, Title: title, DocType: docType, Revision: rev,
		Status: status, ATA: ata, SectionCount: len(in.Documents),
		CreatedAt: s.clock.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := s.store.RegisterManual(ctx, row, in.Documents); err != nil {
		return Manual{}, err
	}
	return row, nil
}

func (s *Service) ListManuals(ctx context.Context) ([]Manual, error) {
	if s == nil || s.store == nil {
		return nil, ErrServiceUnavailable
	}
	return s.store.ListManuals(ctx)
}
