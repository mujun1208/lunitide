package modelfit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const (
	purposeAliasCoding = "coding"
	purposeCodingPlan  = "coding_plan"
)

type ModeParameters struct {
	ThinkingType  string `json:"thinkingType"`
	Effort        string `json:"effort"`
	ClearThinking *bool  `json:"clearThinking"`
}

type ModelProfile struct {
	SchemaVersion            int                       `json:"schemaVersion"`
	ProfileID                string                    `json:"profileId"`
	Family                   string                    `json:"family"`
	CodecVersion             string                    `json:"codecVersion"`
	ModelContract            string                    `json:"modelContract"`
	EndpointPurpose          string                    `json:"endpointPurpose"`
	Protocol                 string                    `json:"protocol"`
	ModelIDs                 []string                  `json:"modelIds"`
	Modes                    map[string]ModeParameters `json:"modes"`
	ReplayPolicy             string                    `json:"replayPolicy"`
	StrictTools              bool                      `json:"strictTools"`
	StrictEndpointSuffix     string                    `json:"strictEndpointSuffix"`
	ContextWindow            int64                     `json:"contextWindow"`
	MaxOutputTokens          int64                     `json:"maxOutputTokens"`
	SourceURLs               []string                  `json:"sourceUrls"`
	SourceCheckedAt          string                    `json:"sourceCheckedAt"`
	AllowedReturnedModels    []string                  `json:"allowedReturnedModels"`
	RevisionPolicy           string                    `json:"revisionPolicy"`
	IdentityEvidenceTTLHours int64                     `json:"identityEvidenceTTLHours"`
}

type TargetIdentity struct {
	ProviderID            string  `json:"providerId"`
	Protocol              string  `json:"protocol"`
	EndpointURL           string  `json:"endpointUrl"`
	EndpointPurpose       string  `json:"endpointPurpose"`
	ModelRequested        string  `json:"modelRequested"`
	Family                string  `json:"family"`
	ModelContract         string  `json:"modelContract"`
	CredentialBindingID   string  `json:"credentialBindingId"`
	ProfileDigest         string  `json:"profileDigest"`
	CodecVersion          string  `json:"codecVersion"`
	ExpectedModelRevision *string `json:"expectedModelRevision"`
}

func TargetDigest(target TargetIdentity) (string, error) {
	normalized, err := canonicalizeEndpointURL(target.EndpointURL)
	if err != nil {
		return "", err
	}
	target.EndpointURL = normalized
	return digestCanonical(target)
}

func ProfileDigest(p ModelProfile) (string, error) {
	return digestCanonical(p)
}

func FindProfile(profiles []ModelProfile, family, purpose string) ModelProfile {
	wantPurpose := resolvePurposeAlias(purpose)
	for _, p := range profiles {
		if p.Family == family && resolvePurposeAlias(p.EndpointPurpose) == wantPurpose {
			return p
		}
	}
	return ModelProfile{}
}

func resolvePurposeAlias(purpose string) string {
	if purpose == purposeAliasCoding {
		return purposeCodingPlan
	}
	return purpose
}

func canonicalizeEndpointURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.User != nil {
		return "", fmt.Errorf("endpoint userinfo is not allowed")
	}
	query := u.Query()
	for key := range query {
		if secretQueryKey(key) {
			return "", fmt.Errorf("endpoint query key %q looks like a secret", key)
		}
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
	}
	u.Scheme = scheme
	u.Fragment = ""
	u.RawFragment = ""
	return u.String(), nil
}

func secretQueryKey(key string) bool {
	switch strings.ToLower(key) {
	case "api_key", "key", "token", "secret":
		return true
	default:
		return false
	}
}

func digestCanonical(v any) (string, error) {
	body, err := canonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalJSON encodes v with UTF-8-sorted object keys, array order
// preserved, integers as decimal json.Number, and no Unicode NFC.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}
