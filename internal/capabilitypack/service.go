// Package capabilitypack coordinates recoverable installs of existing skills,
// MCP endpoints and plugin gates. Component ownership is stored server-side.
package capabilitypack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var ErrNotFound = errors.New("capability pack not found")
var ErrConflict = errors.New("capability pack manifest or operation changed")
var ErrUnavailable = errors.New("capability pack service unavailable")

type Spec struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Skills       []string `json:"skills"`
	McpPresetIDs []string `json:"mcpPresetIds"`
	ToolGates    []string `json:"toolGates"`
}
type Record struct {
	Spec       Spec        `json:"spec"`
	Digest     string      `json:"digest"`
	State      string      `json:"state"`
	Desired    string      `json:"desired"`
	Version    int64       `json:"version"`
	Error      string      `json:"error"`
	CreatedAt  string      `json:"createdAt"`
	UpdatedAt  string      `json:"updatedAt"`
	Components []Component `json:"components"`
}
type Resource struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	TargetID string `json:"targetId"`
	Managed  bool   `json:"managed"`
}
type Component struct {
	Resource
	Ordinal int    `json:"ordinal"`
	State   string `json:"state"`
}
type Tx interface {
	GetPack(string) (Record, error)
	ListPacks() ([]Record, error)
	SavePack(Record, int64) error
	PlanResource(Resource) (Resource, error)
	PutReference(string, Component) error
	References(string) ([]Component, error)
	SetReferenceState(string, string, string, string) error
	SetResourceTarget(string, string, string) error
	MayRelease(string, string, string) (bool, error)
}
type OwnershipTx interface {
	ClaimPackResourceIndependent(string, string) error
	MayRelease(string, string, string) (bool, error)
}
type Store interface {
	TransactPack(context.Context, func(Tx) error) error
}
type Executor interface {
	Describe(context.Context, string, string) (Resource, error)
	Ensure(context.Context, Resource) (string, error)
	Release(context.Context, Resource) error
	Mount(context.Context, Spec) error
	Unmount(context.Context, string) error
}
type Service struct {
	store     Store
	executor  Executor
	operation chan struct{}
}

func New(store Store, executor Executor) *Service {
	return &Service{store: store, executor: executor, operation: make(chan struct{}, 1)}
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func (s Spec) Validate() error {
	if !idPattern.MatchString(s.ID) || len(s.Name) == 0 || len(s.Name) > 256 || len(s.Description) > 4096 {
		return ErrConflict
	}
	seen := map[string]bool{}
	for kind, ids := range map[string][]string{"skill": s.Skills, "mcp": s.McpPresetIDs, "gate": s.ToolGates} {
		if len(ids) > 32 {
			return ErrConflict
		}
		for _, id := range ids {
			key := kind + ":" + id
			if !idPattern.MatchString(id) || seen[key] {
				return ErrConflict
			}
			seen[key] = true
		}
	}
	if len(seen) == 0 {
		return ErrConflict
	}
	return nil
}
func digest(spec Spec) string {
	raw, _ := json.Marshal(spec)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (s *Service) List(ctx context.Context) ([]Record, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	var out []Record
	err := s.store.TransactPack(ctx, func(tx Tx) error {
		var err error
		out, err = tx.ListPacks()
		if err != nil {
			return err
		}
		for i := range out {
			out[i].Components, err = tx.References(out[i].Spec.ID)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}
func (s *Service) get(ctx context.Context, id string) (Record, error) {
	var out Record
	err := s.store.TransactPack(ctx, func(tx Tx) error {
		var err error
		out, err = tx.GetPack(id)
		if err == nil {
			out.Components, err = tx.References(id)
		}
		return err
	})
	return out, err
}

func (s *Service) Install(ctx context.Context, spec Spec, repair bool) (Record, error) {
	if s == nil || s.store == nil || s.executor == nil {
		return Record{}, ErrUnavailable
	}
	if spec.Skills == nil {
		spec.Skills = []string{}
	}
	if spec.McpPresetIDs == nil {
		spec.McpPresetIDs = []string{}
	}
	if spec.ToolGates == nil {
		spec.ToolGates = []string{}
	}
	if err := spec.Validate(); err != nil {
		return Record{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	select {
	case s.operation <- struct{}{}:
		defer func() { <-s.operation }()
	case <-ctx.Done():
		return Record{}, ctx.Err()
	}
	record, err := s.get(ctx, spec.ID)
	if err == nil {
		if record.Digest != digest(spec) || record.State == "uninstalling" || (record.State == "failed" && record.Desired == "uninstalled") {
			return record, ErrConflict
		}
		if record.State == "installed" && !repair {
			return record, nil
		}
	} else if !errors.Is(err, ErrNotFound) {
		return record, err
	}
	// Resolve every dependency before starting effects; a missing service or
	// unknown component cannot be reported as an installed pack.
	var planned []Component
	for _, group := range []struct {
		kind string
		ids  []string
	}{{"gate", spec.ToolGates}, {"skill", spec.Skills}, {"mcp", spec.McpPresetIDs}} {
		for _, key := range group.ids {
			resource, err := s.executor.Describe(ctx, group.kind, key)
			if err != nil {
				return record, err
			}
			if resource.Kind != group.kind || resource.Key != key || resource.TargetID == "" {
				return record, ErrConflict
			}
			planned = append(planned, Component{Resource: resource, Ordinal: len(planned), State: "planned"})
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	observedVersion := record.Version
	err = s.store.TransactPack(ctx, func(tx Tx) error {
		current, readErr := tx.GetPack(spec.ID)
		expected := int64(0)
		if readErr == nil {
			if current.Digest != digest(spec) || current.Version != observedVersion {
				return ErrConflict
			}
			expected = current.Version
			record = current
		} else if !errors.Is(readErr, ErrNotFound) {
			return readErr
		} else {
			existing, err := tx.ListPacks()
			if err != nil {
				return err
			}
			if len(existing) >= 256 {
				return fmt.Errorf("capability pack limit reached (256)")
			}
			record = Record{Spec: spec, Digest: digest(spec), CreatedAt: now}
		}
		record.State, record.Desired, record.Error, record.UpdatedAt, record.Version = "installing", "installed", "", now, expected+1
		if err := tx.SavePack(record, expected); err != nil {
			return err
		}
		for _, component := range planned {
			resource, err := tx.PlanResource(component.Resource)
			if err != nil {
				return err
			}
			component.Resource = resource
			if err = tx.PutReference(spec.ID, component); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return record, err
	}
	record, err = s.get(ctx, spec.ID)
	if err != nil {
		return record, err
	}
	for _, component := range record.Components {
		operationCtx := WithOperation(ctx, Operation{PackID: spec.ID, Kind: component.Kind, Key: component.Key})
		target, applyErr := s.executor.Ensure(operationCtx, component.Resource)
		if applyErr != nil {
			return s.failed(ctx, record, fmt.Errorf("%s %s: %w", component.Kind, component.Key, applyErr))
		}
		if err = s.store.TransactPack(ctx, func(tx Tx) error {
			if err := tx.SetResourceTarget(component.Kind, component.Key, target); err != nil {
				return err
			}
			return tx.SetReferenceState(spec.ID, component.Kind, component.Key, "ready")
		}); err != nil {
			return s.failed(ctx, record, err)
		}
	}
	if err = s.executor.Mount(WithOperation(ctx, Operation{PackID: spec.ID}), spec); err != nil {
		return s.failed(ctx, record, err)
	}
	return s.finish(ctx, record, "installed")
}

func (s *Service) Uninstall(ctx context.Context, id string, expected int64) (Record, error) {
	if s == nil || s.store == nil || s.executor == nil {
		return Record{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	select {
	case s.operation <- struct{}{}:
		defer func() { <-s.operation }()
	case <-ctx.Done():
		return Record{}, ctx.Err()
	}
	record, err := s.get(ctx, id)
	if err != nil {
		return record, err
	}
	if record.State == "uninstalled" {
		return record, nil
	}
	if record.Version != expected {
		return record, ErrConflict
	}
	record.State, record.Desired, record.Error = "uninstalling", "uninstalled", ""
	if record, err = s.save(ctx, record); err != nil {
		return record, err
	}
	// Reverse install order keeps gates available while their MCP dependencies
	// are being retired. The resource's release gate is checked again inside
	// the component mutation transaction to protect concurrent manual claims.
	for i := len(record.Components) - 1; i >= 0; i-- {
		component := record.Components[i]
		if component.State == "released" {
			continue
		}
		release := false
		if err = s.store.TransactPack(ctx, func(tx Tx) error {
			var err error
			release, err = tx.MayRelease(component.Kind, component.Key, id)
			return err
		}); err != nil {
			return s.failed(ctx, record, err)
		}
		if release && component.Kind != "skill" {
			if err = s.executor.Release(WithOperation(ctx, Operation{PackID: id, Kind: component.Kind, Key: component.Key, Removing: true}), component.Resource); err != nil {
				return s.failed(ctx, record, err)
			}
		}
		if err = s.store.TransactPack(ctx, func(tx Tx) error { return tx.SetReferenceState(id, component.Kind, component.Key, "released") }); err != nil {
			return s.failed(ctx, record, err)
		}
	}
	if err = s.executor.Unmount(WithOperation(ctx, Operation{PackID: id, Removing: true}), id); err != nil {
		return s.failed(ctx, record, err)
	}
	return s.finish(ctx, record, "uninstalled")
}
func (s *Service) save(ctx context.Context, r Record) (Record, error) {
	expected := r.Version
	r.Version++
	r.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	err := s.store.TransactPack(ctx, func(tx Tx) error { return tx.SavePack(r, expected) })
	if err != nil {
		return r, err
	}
	return s.get(ctx, r.Spec.ID)
}
func (s *Service) finish(ctx context.Context, r Record, state string) (Record, error) {
	r.State, r.Error = state, ""
	return s.save(ctx, r)
}
func (s *Service) failed(ctx context.Context, r Record, cause error) (Record, error) {
	r.State = "failed"
	r.Error = cause.Error()
	if len(r.Error) > 4096 {
		r.Error = r.Error[:4096]
	}
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	out, err := s.save(saveCtx, r)
	if err != nil {
		return out, errors.Join(cause, err)
	}
	return out, cause
}

type operationKey struct{}
type Operation struct {
	PackID, Kind, Key string
	Removing          bool
}

func WithOperation(ctx context.Context, op Operation) context.Context {
	return context.WithValue(ctx, operationKey{}, op)
}
func CurrentOperation(ctx context.Context) (Operation, bool) {
	op, ok := ctx.Value(operationKey{}).(Operation)
	return op, ok
}

// GuardMutation is used inside each existing component's SQLite mutation.
// Renderer-supplied actor strings cannot forge this private context scope.
func GuardMutation(ctx context.Context, tx any, kind, target string) (bool, error) {
	ownership, ok := tx.(OwnershipTx)
	if !ok {
		return true, nil
	}
	if op, inside := CurrentOperation(ctx); inside {
		if op.Removing && op.Kind == kind {
			return ownership.MayRelease(kind, op.Key, op.PackID)
		}
		return true, nil
	}
	return true, ownership.ClaimPackResourceIndependent(kind, target)
}
