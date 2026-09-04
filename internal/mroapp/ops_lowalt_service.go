package mroapp

import (
	"context"
	"strings"

	"github.com/oklog/ulid/v2"
)

var legalLifeKinds = map[string]struct{}{
	"install": {}, "remove": {}, "transfer": {}, "repair": {}, "scrap": {},
}

// UpsertComponent registers or updates a serialized component.
func (s *Service) UpsertComponent(ctx context.Context, sn, pn string, life float64) (Component, error) {
	ops, err := s.ops()
	if err != nil {
		return Component{}, err
	}
	sn = strings.TrimSpace(sn)
	pn = strings.TrimSpace(pn)
	if sn == "" || len(sn) > 64 || pn == "" || len(pn) > 64 || life < 0 {
		return Component{}, ErrPayloadInvalid
	}
	row := Component{
		ID: ulid.Make().String(), SN: sn, PN: pn, LifeCount: life,
		CreatedAt: s.clock.Now().UTC().Format(opsTimeLayout),
	}
	if err := ops.UpsertComponent(ctx, row); err != nil {
		return Component{}, err
	}
	return row, nil
}

// RecordLifeEvent appends one genealogy event to a component.
func (s *Service) RecordLifeEvent(ctx context.Context, componentID, kind, occurredAt, note string) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	componentID = strings.TrimSpace(componentID)
	kind = strings.TrimSpace(kind)
	occurredAt = strings.TrimSpace(occurredAt)
	if len(componentID) != 26 || occurredAt == "" || len(note) > 512 {
		return ErrPayloadInvalid
	}
	if _, ok := legalLifeKinds[kind]; !ok {
		return ErrPayloadInvalid
	}
	return ops.InsertLifeEvent(ctx, LifeEvent{
		ID: ulid.Make().String(), ComponentID: componentID, Kind: kind,
		OccurredAt: occurredAt, Note: strings.TrimSpace(note),
	})
}

// ListGenealogies resolves every component's life history and install state.
func (s *Service) ListGenealogies(ctx context.Context) ([]GenealogyView, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	components, err := ops.ListComponents(ctx)
	if err != nil {
		return nil, err
	}
	events, err := ops.ListLifeEvents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]GenealogyView, 0, len(components))
	for _, c := range components {
		out = append(out, Genealogy(c, events))
	}
	return out, nil
}

// DraftPirep stores a PIREP body draft for later human confirmation.
func (s *Service) DraftPirep(ctx context.Context, tailNo, bodyJSON string) (PirepDraft, error) {
	ops, err := s.ops()
	if err != nil {
		return PirepDraft{}, err
	}
	tailNo = strings.TrimSpace(tailNo)
	bodyJSON = strings.TrimSpace(bodyJSON)
	if tailNo == "" || len(tailNo) > 32 || len(bodyJSON) < 2 {
		return PirepDraft{}, ErrPayloadInvalid
	}
	row := PirepDraft{
		ID: ulid.Make().String(), TailNo: tailNo, BodyJSON: bodyJSON,
		State: "draft", CreatedAt: s.clock.Now().UTC().Format(opsTimeLayout),
	}
	if err := ops.InsertPirepDraft(ctx, row); err != nil {
		return PirepDraft{}, err
	}
	return row, nil
}

// ListPireps returns the PIREP drafts.
func (s *Service) ListPireps(ctx context.Context) ([]PirepDraft, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	return ops.ListPirepDrafts(ctx)
}

// IntakeAOG parses an AOG paste into a draft case. It never auto-purchases;
// the case stays in draft until a human confirms.
func (s *Service) IntakeAOG(ctx context.Context, text string) (AOGCase, error) {
	ops, err := s.ops()
	if err != nil {
		return AOGCase{}, err
	}
	draft := ParseAOGPaste(text)
	if strings.TrimSpace(draft.TailNo) == "" || len(draft.TailNo) > 32 {
		return AOGCase{}, ErrPayloadInvalid
	}
	row := AOGCase{
		ID: ulid.Make().String(), TailNo: strings.TrimSpace(draft.TailNo),
		PN: clip(draft.PN, 64), Qty: clip(draft.Qty, 32), Note: clip(draft.Note, 512),
		State: "draft", CreatedAt: s.clock.Now().UTC().Format(opsTimeLayout),
	}
	if err := ops.InsertAOGCase(ctx, row); err != nil {
		return AOGCase{}, err
	}
	return row, nil
}

// ListAOG returns the AOG intake cases.
func (s *Service) ListAOG(ctx context.Context) ([]AOGCase, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	return ops.ListAOGCases(ctx)
}

// DraftPO stores a purchase-order draft. Confirmation stays human.
func (s *Service) DraftPO(ctx context.Context, pn, qty, price string) (PODraft, error) {
	ops, err := s.ops()
	if err != nil {
		return PODraft{}, err
	}
	pn = strings.TrimSpace(pn)
	if pn == "" || len(pn) > 64 {
		return PODraft{}, ErrPayloadInvalid
	}
	row := PODraft{
		ID: ulid.Make().String(), PN: pn, Qty: clip(qty, 32), Price: clip(price, 32),
		State: "draft", CreatedAt: s.clock.Now().UTC().Format(opsTimeLayout),
	}
	if err := ops.InsertPODraft(ctx, row); err != nil {
		return PODraft{}, err
	}
	return row, nil
}

// ListPO returns the purchase-order drafts.
func (s *Service) ListPO(ctx context.Context) ([]PODraft, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	return ops.ListPODrafts(ctx)
}

// Triggers derives the five airworthiness trigger categories from due state.
func (s *Service) Triggers(ctx context.Context) ([]TriggerRow, error) {
	dues, err := s.ListDue(ctx)
	if err != nil {
		return nil, err
	}
	return TriggerStatus(dues), nil
}

var legalDraftStates = map[string]struct{}{"confirmed": {}, "rejected": {}}

func legalDraftState(state string) bool {
	_, ok := legalDraftStates[strings.TrimSpace(state)]
	return ok
}

// ConfirmPirep flips a PIREP draft to confirmed or rejected. Confirming writes
// an advisory CHECK due on the same tail (source=pirep:<id>) so the due rail
// can schedule a human inspection. It never writes a production defect.
func (s *Service) ConfirmPirep(ctx context.Context, id, state string) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(id)) != 26 || !legalDraftState(state) {
		return ErrPayloadInvalid
	}
	if err := ops.UpdatePirepState(ctx, id, state); err != nil {
		return err
	}
	if state != "confirmed" {
		return nil
	}
	drafts, err := ops.ListPirepDrafts(ctx)
	if err != nil {
		return err
	}
	var tail string
	for _, d := range drafts {
		if d.ID == id {
			tail = d.TailNo
			break
		}
	}
	if tail == "" {
		return ErrPayloadInvalid
	}
	return s.UpsertDueItem(ctx, DueItem{
		ScopeID: tail,
		Kind:    "CHECK",
		DueAt:   s.clock.Now().UTC().Format("2006-01-02"),
		Source:  "pirep:" + id,
	})
}

// ConfirmAOG flips an AOG intake case. It never auto-purchases.
func (s *Service) ConfirmAOG(ctx context.Context, id, state string) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(id)) != 26 || !legalDraftState(state) {
		return ErrPayloadInvalid
	}
	return ops.UpdateAOGState(ctx, id, state)
}

// ConfirmPO flips a purchase-order draft. Ordering stays human.
func (s *Service) ConfirmPO(ctx context.Context, id, state string) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(id)) != 26 || !legalDraftState(state) {
		return ErrPayloadInvalid
	}
	return ops.UpdatePOState(ctx, id, state)
}

func clip(v string, max int) string {
	v = strings.TrimSpace(v)
	if len(v) > max {
		return v[:max]
	}
	return v
}
