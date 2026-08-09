//go:build windows

package credentialsubmission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/electronsafestorage"
	"github.com/lunitide/lunitide/internal/providerapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

// Discovery accepts at most 100 providers from each of the two allowlisted
// Electron data directories. Keep the aggregate bounded without starving the
// second source on every startup.
const maxElectronAdoptionCandidates = 200

type electronCredentialVisitor func(context.Context, func(storage.ElectronCredentialCandidate) error) error

// RunElectronCredentialAdoption is Host-only startup wiring. Discovery yields
// opaque ciphertext to this process; Engine receives fingerprints/bindings only.
func (h *HostHandler) RunElectronCredentialAdoption(ctx context.Context) error {
	return h.runElectronCredentialAdoption(ctx, storage.VisitDiscoveredElectronCredentials)
}

func (h *HostHandler) runElectronCredentialAdoption(ctx context.Context, discover electronCredentialVisitor) error {
	var candidates []storage.ElectronCredentialCandidate
	err := discover(ctx, func(candidate storage.ElectronCredentialCandidate) error {
		if len(candidates) >= maxElectronAdoptionCandidates {
			return errors.New("Electron credential candidate limit exceeded")
		}
		candidates = append(candidates, candidate)
		return nil
	})
	if err != nil && len(candidates) == 0 {
		return err
	}
	tuples := make([]providerapp.ElectronCredentialTuple, 0, len(candidates))
	for _, c := range candidates {
		tuples = append(tuples, providerapp.ElectronCredentialTuple{SourceFingerprint: c.SourceFingerprint, ItemFingerprint: c.ItemFingerprint})
	}
	response, callErr := h.internalCall(ctx, "internal.electron-credential.plan", "", map[string]any{"tuples": tuples})
	if callErr != nil {
		return errors.Join(err, callErr)
	}
	if !response.OK {
		return errors.Join(err, errors.New("Electron credential plan rejected"))
	}
	b, _ := json.Marshal(response.Payload)
	var plans []providerapp.ElectronCredentialPlan
	if json.Unmarshal(b, &plans) != nil {
		return errors.Join(err, errors.New("invalid Electron credential plan"))
	}
	byTuple := make(map[string]storage.ElectronCredentialCandidate, len(candidates))
	for _, c := range candidates {
		byTuple[c.SourceFingerprint+"\x00"+c.ItemFingerprint] = c
	}
	for _, plan := range plans {
		candidate, found := byTuple[plan.SourceFingerprint+"\x00"+plan.ItemFingerprint]
		if !found || candidate.ProviderID != plan.ProviderID || candidate.Origin != plan.Origin || candidate.Protocol != plan.Protocol {
			_ = h.disposition(ctx, plan.ElectronCredentialTuple, "superseded")
			continue
		}
		// The destination protocol is normalized (legacy "openai" becomes
		// "openai_compatible"), while the safeStorage envelope remains bound to
		// the exact protocol written by Electron. Validate both independently.
		decryptErr := electronsafestorage.WithAPIKeyAndEncryptedKey(candidate.EncryptedBlob, candidate.OSCryptEncryptedKey, plan.Origin, candidate.LegacyProtocol, func(apiKey []byte) error {
			return h.adoptElectronCredential(ctx, plan, apiKey)
		})
		if errors.Is(decryptErr, electronsafestorage.ErrInvalidInput) || errors.Is(decryptErr, electronsafestorage.ErrBindingMismatch) {
			_ = h.disposition(ctx, plan.ElectronCredentialTuple, "rejected")
		} else if decryptErr != nil {
			err = errors.Join(err, decryptErr)
		}
	}
	return err
}

func (h *HostHandler) adoptElectronCredential(ctx context.Context, plan providerapp.ElectronCredentialPlan, credential []byte) error {
	// Coordinator Existing scope validates the secret against the exact ordinary
	// provider-update target. The separate internal adoption payload carries the
	// migration tuple and later receives the reserved credential ref.
	boundRequest, _ := json.Marshal(map[string]any{"id": plan.ProviderID, "expectedVersion": plan.Version, "protocol": plan.Protocol, "baseUrl": plan.Origin})
	request := providerapp.ElectronCredentialAdoption{ElectronCredentialPlan: plan}
	sub, err := h.Coordinator.Submit(ctx, SubmitInput{Scope: Existing(plan.ProviderID), Protocol: plan.Protocol, Origin: plan.Origin, Request: boundRequest, Credential: credential})
	if err != nil {
		return err
	}
	reservation, err := h.Coordinator.Reserve(ctx, sub.SubmissionID, boundRequest)
	if err != nil {
		return err
	}
	request.CredentialRef = reservation.Ref.CredentialRef
	payload, _ := json.Marshal(request)
	keyHash := sha256.Sum256([]byte("electron-credential-adoption\x00" + plan.SourceFingerprint + "\x00" + plan.ItemFingerprint))
	response, err := h.internalCall(ctx, "internal.electron-credential.adopt", hex.EncodeToString(keyHash[:]), json.RawMessage(payload))
	if err != nil {
		return err
	}
	if !response.OK {
		_ = h.Coordinator.CleanupExpired(context.Background())
		if response.Error != nil && response.Error.Code == "PROVIDER_VERSION_CONFLICT" {
			return h.disposition(ctx, plan.ElectronCredentialTuple, "superseded")
		}
		return errors.New("Electron credential adoption rejected")
	}
	if _, err = h.Coordinator.Adopt(ctx, sub.SubmissionID, boundRequest); err != nil {
		return err
	}
	_, err = h.Coordinator.Consume(ctx, sub.SubmissionID, boundRequest)
	return err
}

func (h *HostHandler) disposition(ctx context.Context, tuple providerapp.ElectronCredentialTuple, value string) error {
	response, err := h.internalCall(ctx, "internal.electron-credential.disposition", "", map[string]string{"sourceFingerprint": tuple.SourceFingerprint, "itemFingerprint": tuple.ItemFingerprint, "disposition": value})
	if err == nil && !response.OK {
		return errors.New("Electron credential disposition rejected")
	}
	return err
}
func (h *HostHandler) internalCall(ctx context.Context, method, key string, payload any) (bridge.Response, error) {
	raw, _ := json.Marshal(payload)
	now := time.Now().UTC()
	return h.Engine.Call(ctx, bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: method, SentAt: now, DeadlineMS: 10000, IdempotencyKey: key, Payload: raw})
}
