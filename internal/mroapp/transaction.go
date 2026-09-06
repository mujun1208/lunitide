package mroapp

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrScope = errors.New("mroapp: record belongs to another organization")
var ErrConflict = errors.New("mroapp: request payload or source evidence changed")
var ErrConstraints = errors.New("mroapp: current scheduling constraints are not satisfied")
var ErrCapacity = errors.New("mroapp: record, relationship or source verification budget exceeded")

type scopeKey struct{}
type transactionKey struct{}

// WithScope is called only by the trusted Engine binding resolver, never from
// a renderer-provided organization field. Empty scope is the personal space.
func WithScope(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, scopeKey{}, orgID)
}
func Scope(ctx context.Context) string { value, _ := ctx.Value(scopeKey{}).(string); return value }
func InTransaction(ctx context.Context) bool {
	value, _ := ctx.Value(transactionKey{}).(bool)
	return value
}

type TransactionalStore interface {
	TransactMRO(context.Context, func(context.Context) error) error
}
type RequestStore interface {
	ExecuteMRORequest(context.Context, string, string, string, func(context.Context) (json.RawMessage, error), func(context.Context) error) (json.RawMessage, error)
	ListMROAudit(context.Context, int) ([]AuditRow, error)
}
type AuditRow struct {
	ID           string `json:"id"`
	Action       string `json:"action"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	CreatedAt    string `json:"createdAt"`
}

func atomicValue[T any](s *Service, ctx context.Context, fn func(context.Context) (T, error)) (out T, err error) {
	if s == nil || s.store == nil {
		return out, ErrServiceUnavailable
	}
	if InTransaction(ctx) {
		return fn(ctx)
	}
	if tx, ok := s.store.(TransactionalStore); ok {
		err = tx.TransactMRO(ctx, func(txCtx context.Context) error {
			out, err = fn(context.WithValue(txCtx, transactionKey{}, true))
			return err
		})
		return out, err
	}
	return fn(ctx)
}
func atomicError(s *Service, ctx context.Context, fn func(context.Context) error) error {
	_, err := atomicValue(s, ctx, func(txCtx context.Context) (struct{}, error) { return struct{}{}, fn(txCtx) })
	return err
}

func (s *Service) ExecuteRequest(ctx context.Context, method, key, digest string, fn func(context.Context) (json.RawMessage, error), replay func(context.Context) error) (json.RawMessage, error) {
	store, ok := s.store.(RequestStore)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return store.ExecuteMRORequest(ctx, method, key, digest, func(txCtx context.Context) (json.RawMessage, error) {
		return fn(context.WithValue(txCtx, transactionKey{}, true))
	}, func(txCtx context.Context) error { return replay(context.WithValue(txCtx, transactionKey{}, true)) })
}
func (s *Service) ListAudit(ctx context.Context, limit int) ([]AuditRow, error) {
	store, ok := s.store.(RequestStore)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return store.ListMROAudit(ctx, limit)
}
