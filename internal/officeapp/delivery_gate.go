package officeapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

var ErrDraftRequired = errors.New("OFFICE_DRAFT_REQUIRED")
var errDeliveryModeConflict = errors.New("INVALID_ARGUMENT")

func deliveryPolicyForTask(task domain.Task) domain.DeliveryPolicy {
	return DeliveryPolicyFromCheckpoint(task.Checkpoint)
}

func DeliveryPolicyFromCheckpoint(raw json.RawMessage) domain.DeliveryPolicy {
	policy := domain.DeliveryPolicy{Tier: "basic"}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return policy
	}
	if raw, ok := fields["deliveryPolicy"]; ok {
		_ = json.Unmarshal(raw, &policy)
	}
	if policy.Tier == "" {
		policy.Tier = "basic"
	}
	return policy
}

func WithTaskDeliveryPolicy(previous json.RawMessage, policy domain.DeliveryPolicy) json.RawMessage {
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(previous, &fields)
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	if policy.Tier == "" {
		policy.Tier = "basic"
	}
	fields["deliveryPolicy"] = encode(policy)
	return encode(fields)
}

func ResolveDeliveryMode(deliveryMode string, draft *bool) (string, error) {
	mode := strings.TrimSpace(deliveryMode)
	if mode != "" && mode != "copy" && mode != "formal" {
		return "", errDeliveryModeConflict
	}
	if mode == "" {
		if draft != nil && !*draft {
			return "formal", nil
		}
		return "copy", nil
	}
	if draft == nil {
		return mode, nil
	}
	implied := "formal"
	if *draft {
		implied = "copy"
	}
	if implied != mode {
		return "", errDeliveryModeConflict
	}
	return mode, nil
}

func (s *Service) AssessDelivery(ctx context.Context, taskID, versionID string, policy domain.DeliveryPolicy) (domain.FormalDecision, error) {
	revision, tier := domain.ResolveRegisteredPolicy(policy)
	v, err := s.Store.GetOfficeVersion(ctx, versionID)
	if err != nil || v.TaskID != taskID || v.SHA256 == "" {
		return domain.FormalDecision{
			VersionID:      versionID,
			PolicyRevision: revision,
			Tier:           tier,
			Allowed:        false,
			State:          "needs_review",
			MissingChecks:  domain.RegisteredRequiredCheckIDs(revision, ""),
		}, nil
	}
	reports, err := s.Store.ListOfficeValidations(ctx, v.ID)
	if err != nil {
		return domain.FormalDecision{}, err
	}
	status := map[string]string{}
	refs := []string{}
	for _, report := range reports {
		if report.SHA256 != v.SHA256 {
			continue
		}
		refs = append(refs, report.ID)
		collapsed := map[string]string{}
		fromCanonical := map[string]bool{}
		for _, check := range report.Checks {
			mergeCanonicalStatus(collapsed, fromCanonical, check)
		}
		for id, st := range collapsed {
			if _, exists := status[id]; exists {
				continue
			}
			status[id] = st
		}
	}
	required := domain.RegisteredRequiredCheckIDs(revision, v.Kind)
	dec := domain.FormalDecision{
		VersionID:      v.ID,
		SourceSHA256:   v.SHA256,
		PolicyRevision: revision,
		Tier:           tier,
		EvidenceRefs:   refs,
	}
	failed := false
	incomplete := false
	for _, id := range required {
		st, ok := status[id]
		if !ok {
			dec.MissingChecks = append(dec.MissingChecks, id)
			incomplete = true
			continue
		}
		switch st {
		case "passed":
		case "failed":
			dec.BlockingCodes = append(dec.BlockingCodes, id)
			failed = true
		default:
			dec.MissingChecks = append(dec.MissingChecks, id)
			incomplete = true
		}
	}
	switch {
	case failed:
		dec.State = "blocked"
	case incomplete:
		dec.State = "needs_review"
	default:
		dec.State = "verified"
		dec.Allowed = true
	}
	return s.persistFormalDecision(ctx, taskID, evidenceDigest(v.SHA256, status), dec)
}

func (s *Service) persistFormalDecision(ctx context.Context, taskID, digest string, dec domain.FormalDecision) (domain.FormalDecision, error) {
	found, ok, err := s.Store.FindOfficeDeliveryDecision(ctx, dec.VersionID, dec.SourceSHA256, dec.PolicyRevision, digest)
	if err != nil {
		return domain.FormalDecision{}, err
	}
	if ok {
		return found, nil
	}
	return s.Store.SaveOfficeDeliveryDecision(ctx, dec, taskID, digest)
}

func mergeCanonicalStatus(status map[string]string, fromCanonical map[string]bool, check domain.Check) {
	id := domain.CanonicalCheckID(check.ID)
	canonical := check.ID == id
	prev, exists := status[id]
	if !exists {
		status[id] = check.Status
		fromCanonical[id] = canonical
		return
	}
	pr, nr := checkStatusRank(prev), checkStatusRank(check.Status)
	if nr > pr || (nr == pr && canonical && !fromCanonical[id]) {
		status[id] = check.Status
		fromCanonical[id] = canonical
	}
}

func checkStatusRank(st string) int {
	switch st {
	case "failed":
		return 3
	case "passed":
		return 1
	default:
		return 2
	}
}

func evidenceDigest(sourceSHA string, status map[string]string) string {
	keys := make([]string, 0, len(status))
	for id := range status {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(sourceSHA)
	for _, id := range keys {
		b.WriteByte('\n')
		b.WriteString(id)
		b.WriteByte('=')
		b.WriteString(status[id])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func (s *Service) AcceptFormal(ctx context.Context, taskID, artifactID, versionID string, expectedRevision int64, policy domain.DeliveryPolicy) (domain.Head, domain.FormalDecision, error) {
	dec, err := s.AssessDelivery(ctx, taskID, versionID, policy)
	if err != nil {
		return domain.Head{}, dec, err
	}
	if !dec.Allowed {
		return domain.Head{}, dec, ErrDraftRequired
	}
	head, err := s.Store.AcceptOfficeVersion(ctx, taskID, artifactID, versionID, expectedRevision)
	return head, dec, err
}

func (s *Service) ExportFormal(ctx context.Context, taskID, versionID, dir string, policy domain.DeliveryPolicy, exportNames ...string) (string, domain.FormalDecision, error) {
	dec, err := s.AssessDelivery(ctx, taskID, versionID, policy)
	if err != nil {
		return "", dec, err
	}
	if !dec.Allowed {
		return "", dec, ErrDraftRequired
	}
	path, err := s.Export(ctx, taskID, versionID, dir, exportNames...)
	return path, dec, err
}

func (s *Service) ExportBundleFormal(ctx context.Context, taskID, bundleID, dir string, policy domain.DeliveryPolicy) (BundleExport, error) {
	d, err := s.deliveries()
	if err != nil {
		return BundleExport{}, err
	}
	b, err := d.GetOfficeBundle(ctx, bundleID)
	if err != nil {
		return BundleExport{}, err
	}
	if b.TaskID != taskID {
		return BundleExport{}, domain.ErrScope
	}
	var decision domain.FormalDecision
	for _, f := range b.Files {
		dec, e := s.AssessDelivery(ctx, taskID, f.VersionID, policy)
		if e != nil {
			return BundleExport{Decision: dec}, e
		}
		if !dec.Allowed {
			return BundleExport{BundleID: bundleID, Decision: dec, Complete: false}, ErrDraftRequired
		}
		if decision.DecisionID == "" {
			decision = dec
		}
	}
	out, err := s.ExportBundle(ctx, taskID, bundleID, dir)
	out.Decision = decision
	return out, err
}
