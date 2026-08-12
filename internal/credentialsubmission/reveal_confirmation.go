package credentialsubmission

import "context"

type RevealTarget struct {
	ProviderID string
	Protocol   string
	Origin     string
}

// RevealConfirmer is the Host-owned user-presence boundary for credential
// reveal. Production uses a platform-native prompt; tests inject a confirmer.
type RevealConfirmer interface {
	ConfirmCredentialReveal(context.Context, RevealTarget) (bool, error)
}

type RevealConfirmFunc func(context.Context, RevealTarget) (bool, error)

func (f RevealConfirmFunc) ConfirmCredentialReveal(ctx context.Context, target RevealTarget) (bool, error) {
	return f(ctx, target)
}

type nativeRevealConfirmer struct{}

func (nativeRevealConfirmer) ConfirmCredentialReveal(ctx context.Context, target RevealTarget) (bool, error) {
	return confirmCredentialRevealNative(ctx, target)
}
