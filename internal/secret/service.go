// Package secret defines the Host-only credential storage boundary.
package secret

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

type Ref struct {
	CredentialRef string
	ProviderID    string
	Origin        string
	Protocol      string
}

type Service interface {
	Put(context.Context, Ref, []byte) error
	WithSecret(context.Context, Ref, func([]byte) error) error
	Delete(context.Context, Ref) error
}

func (r Ref) Validate() (Ref, error) {
	if r.CredentialRef == "" || len(r.CredentialRef) > 256 || r.ProviderID == "" || len(r.ProviderID) > 256 || r.Protocol == "" || len(r.Protocol) > 64 {
		return Ref{}, errors.New("invalid secret reference")
	}
	origin, err := provider.NormalizeOrigin(r.Origin)
	if err != nil {
		return Ref{}, errors.New("invalid secret origin")
	}
	r.Origin = origin
	return r, nil
}

func Zero(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
