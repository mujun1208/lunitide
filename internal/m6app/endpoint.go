package m6app

import (
	"context"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

// EndpointService persists mcp6 endpoint lifecycle changes onto
// m6_mcp_endpoint. The in-memory mcp6.Registry owns transitions; this
// adapter mirrors them durably so a restart can rebuild the registry (the
// rebuild pass is part of the M6 startup wiring).
type EndpointService struct {
	uow   UnitOfWork
	clock Clock
}

func NewEndpointService(uow UnitOfWork) *EndpointService {
	return &EndpointService{uow: uow, clock: systemClock{}}
}

// PersistRegister stores a freshly registered endpoint (register path).
func (s *EndpointService) PersistRegister(ctx context.Context, e *mcp6.Endpoint) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	pinJSON, err := json.Marshal(e.Pin)
	if err != nil {
		return err
	}
	now := s.clock.Now().UTC()
	return s.uow.TransactM6(ctx, func(tx Tx) error {
		if err := tx.PutM6Endpoint(m6supply.Endpoint{
			ID: e.ID, Transport: e.Transport, URL: e.URL, AuthRef: e.AuthRef,
			CapabilityPinJSON: string(pinJSON), State: e.State, Version: e.Version,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "mcp6.endpoint.registered",
			AggregateID: e.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: endpointAuditMeta(e.ID, e.Transport, e.State),
		})
	})
}

// PersistState mirrors a state change (probe/invoke drift/revoke paths).
func (s *EndpointService) PersistState(ctx context.Context, endpointID, state string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	at := s.clock.Now().UTC()
	return s.uow.TransactM6(ctx, func(tx Tx) error {
		if err := tx.UpdateM6EndpointState(endpointID, state, at); err != nil {
			return err
		}
		// routine probe/ready transitions record no audit event
		if action := endpointAuditAction(state); action != "" {
			return tx.PutAudit(providerapp.Audit{
				ID: ulid.Make().String(), Action: action,
				AggregateID: endpointID, Actor: delegationActor, CreatedAt: at,
				Metadata: endpointAuditMeta(endpointID, "", state),
			})
		}
		return nil
	})
}

// LoadEndpoints rehydrates the durable endpoint rows into descriptor slices
// for the mcp6.Registry startup rebuild.
func (s *EndpointService) LoadEndpoints(ctx context.Context) ([]m6supply.Endpoint, error) {
	if s == nil || s.uow == nil {
		return nil, ErrServiceUnavailable
	}
	var out []m6supply.Endpoint
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		rows, err := tx.ListM6Endpoints()
		out = rows
		return err
	})
	return out, err
}

// endpointAuditMeta shapes the audit metadata for endpoint lifecycle rows.
func endpointAuditMeta(endpointID, transport, state string) []byte {
	type meta struct {
		EndpointID string `json:"endpointId"`
		Transport  string `json:"transport,omitempty"`
		State      string `json:"state"`
	}
	return marshalJSON(meta{EndpointID: endpointID, Transport: transport, State: state})
}

// endpointAuditAction maps durable endpoint states onto audit actions;
// routine probe/ready transitions record no audit event.
func endpointAuditAction(state string) string {
	switch state {
	case "degraded":
		return "mcp6.endpoint.degraded"
	case "revoked":
		return "mcp6.endpoint.revoked"
	}
	return ""
}
