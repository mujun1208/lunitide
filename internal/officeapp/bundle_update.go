package officeapp

import (
	"context"
	"errors"
	"strings"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

type BundleMemberUpdate struct {
	VersionID        string
	ExpectedRevision int64
	Request          content.PatchRequest
}

type BundleUpdateResult struct {
	Bundle     domain.Bundle
	Published  bool
	Candidates []domain.Version
}

type stagedBundleMember struct {
	member BundleMemberUpdate
	base   domain.Version
	result content.PatchResult
}

func (s *Service) UpdateBundle(ctx context.Context, taskID, bundleID string, members []BundleMemberUpdate, key string) (BundleUpdateResult, error) {
	d, err := s.deliveries()
	if err != nil {
		return BundleUpdateResult{}, err
	}
	formal, err := d.GetOfficeBundle(ctx, bundleID)
	if err != nil {
		return BundleUpdateResult{}, err
	}
	out := BundleUpdateResult{Bundle: formal}
	if formal.TaskID != taskID {
		return out, domain.ErrScope
	}
	if len(members) == 0 || len(members) > 32 {
		return out, domain.ErrInvalid
	}
	inFormal := map[string]domain.BundleFile{}
	for _, f := range formal.Files {
		inFormal[f.VersionID] = f
	}
	seen := map[string]bool{}
	for _, m := range members {
		if seen[m.VersionID] || inFormal[m.VersionID].VersionID == "" {
			return out, domain.ErrInvalid
		}
		seen[m.VersionID] = true
	}

	staged := make([]stagedBundleMember, 0, len(members))
	for _, m := range members {
		v, data, e := s.ReadVersion(ctx, taskID, m.VersionID)
		if e != nil {
			return out, e
		}
		if e = s.assertPatchCAS(ctx, v, data, m.ExpectedRevision, m.Request); e != nil {
			return out, asOfficeConflict(e)
		}
		result, e := content.Patch(data, m.Request)
		if e != nil {
			return out, asOfficeConflict(e)
		}
		staged = append(staged, stagedBundleMember{member: m, base: v, result: result})
	}

	published := make([]domain.Version, 0, len(staged))
	replace := map[string]string{}
	for _, st := range staged {
		report := encode(map[string]any{"changedParts": st.result.ChangedParts, "calculation": st.result.Calculation})
		next, e := s.publish(ctx, taskID, st.base.ArtifactID, st.base.Name, st.base.Kind, "imported", st.result.Data, report, st.base.ID, st.member.ExpectedRevision, bundleMemberKey(key, st.base.ID))
		if e != nil {
			out.Candidates = published
			return out, asOfficeConflict(e)
		}
		published = append(published, next)
		replace[st.base.ID] = next.ID
	}
	out.Candidates = published

	ids := make([]string, 0, len(formal.Files))
	for _, f := range formal.Files {
		if next, ok := replace[f.VersionID]; ok {
			ids = append(ids, next)
			continue
		}
		ids = append(ids, f.VersionID)
	}
	bundle, err := s.CreateBundle(ctx, taskID, formal.Title, ids, key)
	if err != nil {
		return out, err
	}
	out.Bundle = bundle
	out.Published = true
	return out, nil
}

func (s *Service) assertPatchCAS(ctx context.Context, v domain.Version, data []byte, revision int64, req content.PatchRequest) error {
	if err := s.assertPatchBase(v, data, req); err != nil {
		return err
	}
	return s.assertHeadRevision(ctx, v.TaskID, v.ArtifactID, revision)
}

func (s *Service) assertPatchBase(v domain.Version, data []byte, req content.PatchRequest) error {
	if req.BaseSHA256 != v.SHA256 || string(req.Kind) != v.Kind {
		return domain.ErrConflict
	}
	i, err := content.Inspect(content.Kind(v.Kind), data)
	if err != nil {
		return err
	}
	for _, op := range req.Operations {
		ok := false
		for _, n := range i.Nodes {
			if n.ID == op.NodeID && n.Digest == op.ExpectedDigest {
				ok = true
				break
			}
		}
		if !ok {
			return domain.ErrConflict
		}
	}
	for _, r := range req.Ranges {
		ok := false
		for _, p := range i.Parts {
			if p.Name == r.Part && p.SHA256 == r.ExpectedDigest {
				ok = true
				break
			}
		}
		if !ok {
			return domain.ErrConflict
		}
	}
	return nil
}

func (s *Service) assertHeadRevision(ctx context.Context, taskID, artifactID string, revision int64) error {
	heads, err := s.Store.ListOfficeHeads(ctx, taskID)
	if err != nil {
		return err
	}
	for _, h := range heads {
		if h.ArtifactID == artifactID {
			if h.Revision != revision {
				return domain.ErrConflict
			}
			return nil
		}
	}
	return domain.ErrConflict
}

func asOfficeConflict(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, content.ErrConflict) {
		return domain.ErrConflict
	}
	return err
}

func bundleMemberKey(key, versionID string) string {
	joined := key + "/" + versionID
	if len(joined) <= 128 && !strings.ContainsAny(joined, "\x00\r\n") {
		return joined
	}
	return digest([]byte(key + "\n" + versionID))
}
