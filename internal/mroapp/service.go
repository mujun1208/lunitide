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
)

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
