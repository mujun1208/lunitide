package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
)

type createCommitSpy struct{ commits int }

func (s *createCommitSpy) Get(context.Context, string) (provider.Provider, error) {
	return provider.Provider{}, provider.ErrNotFound
}
func (s *createCommitSpy) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return nil, nil
}
func (s *createCommitSpy) CreateRequest(_ context.Context, _, _ string, _ any, p provider.Provider) (provider.Provider, error) {
	s.commits++
	return p, nil
}
func (s *createCommitSpy) UpdateRequest(context.Context, string, string, any, string, int64, func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return provider.Provider{}, nil
}
func (s *createCommitSpy) DeleteRequest(context.Context, string, string, any, string, int64) (provider.Provider, error) {
	return provider.Provider{}, nil
}

func TestCredentialCreateTupleMismatchDoesNotCommit(t *testing.T) {
	for name, tuple := range map[string]string{
		"origin":       `"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","credentialRef":"01ARZ3NDEKTSV4RRFFQ69G5FAW","origin":"https://other.example","protocol":"openai_compatible"`,
		"protocol":     `"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","credentialRef":"01ARZ3NDEKTSV4RRFFQ69G5FAW","origin":"https://example.com","protocol":"anthropic"`,
		"provider-ref": `"providerId":"not-authoritative","credentialRef":"01ARZ3NDEKTSV4RRFFQ69G5FAW","origin":"https://example.com","protocol":"openai_compatible"`,
		"secret-ref":   `"providerId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","credentialRef":"not-authoritative","origin":"https://example.com","protocol":"openai_compatible"`,
	} {
		t.Run(name, func(t *testing.T) {
			spy := &createCommitSpy{}
			payload := `{` + tuple + `,"create":{"name":"Example","protocol":"openai_compatible","baseUrl":"https://example.com/v1","models":[{"modelId":"m","displayName":"M","isDefault":true}]}}`
			request := validRequest("internal.provider.create.with-credential", payload)
			request.IdempotencyKey = "tuple-" + name
			response := handleProviderCreateWithCredential(NewEngine(spy, "test"), context.Background(), request)
			if response.OK || spy.commits != 0 {
				t.Fatalf("tuple mismatch response=%#v commits=%d", response, spy.commits)
			}
		})
	}
}
