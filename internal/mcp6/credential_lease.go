package mcp6

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/lunitide/lunitide/internal/secret"
)

// CredentialBinding binds a credential to the exact endpoint identity and
// HTTPS origin. Stdio gets an inert synthetic HTTPS origin; no request is made
// to it. This prevents another endpoint from borrowing the same SecretRef.
func CredentialBinding(e *Endpoint, ref string) (secret.Ref, error) {
	origin := ""
	if e == nil {
		return secret.Ref{}, ErrCredentialRevoked
	}
	switch e.Transport {
	case "https":
		u, err := url.Parse(e.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return secret.Ref{}, ErrCredentialRevoked
		}
		origin = "https://" + u.Host
	case "stdio":
		sum := sha256.Sum256([]byte(e.ID))
		origin = "https://" + hex.EncodeToString(sum[:]) + ".stdio.invalid"
	default:
		return secret.Ref{}, ErrCredentialRevoked
	}
	return (secret.Ref{CredentialRef: ref, ProviderID: "mcp:" + e.ID, Origin: origin, Protocol: "mcp"}).Validate()
}

func SecretCredentialLease(service secret.Service) CredentialLease {
	return func(parent context.Context, e *Endpoint, fn func(Credentials) error) error {
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		credentials := Credentials{Context: ctx, Env: map[string][]byte{}}
		keys := make([]string, 0, len(e.EnvSecretRefs))
		for k := range e.EnvSecretRefs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var resolve func(int) error
		resolve = func(index int) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if index >= len(keys) {
				return fn(credentials)
			}
			name := keys[index]
			binding, err := CredentialBinding(e, e.EnvSecretRefs[name])
			if err != nil {
				return err
			}
			if service == nil {
				return ErrCredentialRevoked
			}
			return service.WithSecret(ctx, binding, func(value []byte) error {
				if len(value) == 0 || len(value) > 16384 {
					return ErrCredentialRevoked
				}
				credentials.Env[name] = value
				defer delete(credentials.Env, name)
				return resolve(index + 1)
			})
		}
		var err error
		if e.AuthRef == "" {
			err = resolve(0)
		} else {
			binding, bindingErr := CredentialBinding(e, e.AuthRef)
			if bindingErr != nil {
				return bindingErr
			}
			if service == nil {
				return ErrCredentialRevoked
			}
			err = service.WithSecret(ctx, binding, func(value []byte) error {
				if len(value) == 0 || len(value) > 16384 {
					return ErrCredentialRevoked
				}
				credentials.Bearer = value
				defer func() { credentials.Bearer = nil }()
				return resolve(0)
			})
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return ErrCredentialRevoked
			}
			return fmt.Errorf("MCP credential lease: %w", err)
		}
		return nil
	}
}
