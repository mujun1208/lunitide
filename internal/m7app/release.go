// M7 slice 3 application service (T-7.3.2/T-7.3.3): CR revisions and
// immutable release packages. createRevision freezes a canonical manifest
// digest and supersedes the previous open revision; buildPackage seals a
// content-addressed package document (members + SBOM + signature) and binds
// it to the revision plus every referenced Test/Scan through trace edges;
// the read projections re-derive every digest so tampering is detectable.
package m7app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

var (
	// ErrRevisionNotFound / ErrPackageNotFound: referenced rows missing.
	ErrRevisionNotFound = errors.New("m7app: cr revision not found")
	ErrPackageNotFound  = errors.New("m7app: release package not found")
	// ErrDigestMismatch: expected or recomputed digest differs from the
	// stored one (PKG-002 - isolate the object, never reuse).
	ErrDigestMismatch = errors.New("m7app: package digest mismatch")
	// ErrEvidenceMissing: the manifest references test/scan rows that do
	// not exist; the package cannot be traced to its evidence (PKG-002).
	ErrEvidenceMissing = errors.New("m7app: referenced evidence missing")
	// ErrPackageInvalid: members/SBOM/signature malformed (PKG-003).
	ErrPackageInvalid = errors.New("m7app: package manifest invalid")
	// ErrSignatureInvalid: the stored signature does not verify over the
	// stored blob (PKG-003 - tamper or wrong key).
	ErrSignatureInvalid = errors.New("m7app: package signature invalid")
	// ErrRevisionFrozen: the revision is not open for packaging/review
	// transitions anymore (REV-002 - create a new revision instead).
	ErrRevisionFrozen = errors.New("m7app: cr revision frozen")
	// ErrIllegalRevisionTransition: the requested status change is not in
	// the canonical CRRevision state machine.
	ErrIllegalRevisionTransition = errors.New("m7app: illegal cr revision transition")
	// ErrAuthorMismatch: the review declared an author that does not match
	// the frozen revision manifest (REV-001 semantics for CR revisions).
	ErrAuthorMismatch = errors.New("m7app: review author does not match revision author")
)

// ReleaseTx is the slice-3 single-writer transaction (sqlite agentRuntimeTx
// satisfies it alongside WorkflowTx/EvidenceTx).
type ReleaseTx interface {
	PutCRRevision(m7flow.CRRevision) error
	GetCRRevision(id string) (m7flow.CRRevision, error)
	MaxCRRevisionNo(crID string) (int64, error)
	FindOpenCRRevision(crID string) (m7flow.CRRevision, error)
	ListCRRevisions(crID string) ([]m7flow.CRRevision, error)
	UpdateCRRevisionStatus(id, from, to string) error
	PutReleasePackage(m7flow.ReleasePackage) error
	GetReleasePackage(id string) (m7flow.ReleasePackage, error)
	FindPackageByRevision(crRevisionID string) (m7flow.ReleasePackage, error)
	PutReleaseBlob(digest, content string, createdAt time.Time) error
	GetReleaseBlob(digest string) (string, error)
	ListSubjectReviews(subjectType, subjectID string) ([]m7flow.Review, error)
	NodeExists(nodeType, nodeID string) (bool, error)
	EvidenceReportDigest(kind, id string) (string, error)
	PutEdge(m7flow.TraceEdge) error
}

// ReleaseUnitOfWork is the slice-3 single-writer boundary.
type ReleaseUnitOfWork interface {
	TransactRelease(ctx context.Context, fn func(ReleaseTx) error) error
}

// ReleaseSigner produces and verifies integrity signatures over sealed
// package documents. The default implementation is a local HMAC-SHA256 MAC
// keyed by a versioned derivation: it detects any tampering with the stored
// blob, but it is not a non-repudiation signature - a real signing service
// can be injected without touching storage or the projections.
type ReleaseSigner interface {
	KeyID() string
	Sign(canonicalDoc string) string
	Verify(canonicalDoc, signature string) bool
}

type localMACSigner struct {
	key []byte
}

// NewLocalMACSigner returns the default integrity signer (key id
// local-mac-v1). The key is deterministic on purpose: signatures must stay
// verifiable across restarts on the same install.
func NewLocalMACSigner() ReleaseSigner {
	sum := sha256.Sum256([]byte("lunitide:m7:release-signing:local-mac-v1"))
	return localMACSigner{key: sum[:]}
}

func (localMACSigner) KeyID() string { return "local-mac-v1" }

func (s localMACSigner) Sign(doc string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(doc))
	return "local-mac-v1:" + hex.EncodeToString(mac.Sum(nil))
}

func (s localMACSigner) Verify(doc, signature string) bool {
	const prefix = "local-mac-v1:"
	if len(signature) <= len(prefix) || signature[:len(prefix)] != prefix {
		return false
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(doc))
	want := mac.Sum(nil)
	got, err := hex.DecodeString(signature[len(prefix):])
	if err != nil || len(want) != len(got) {
		return false
	}
	return hmac.Equal(want, got)
}

// ReleaseService implements release.createRevision / buildPackage /
// getRevision / getPackage (slice 3).
type ReleaseService struct {
	uow    ReleaseUnitOfWork
	clock  Clock
	signer ReleaseSigner
}

func NewReleaseService(uow ReleaseUnitOfWork) *ReleaseService {
	return &ReleaseService{uow: uow, clock: systemClock{}, signer: NewLocalMACSigner()}
}

// SetClock substitutes the wall clock (tests).
func (s *ReleaseService) SetClock(c Clock) { s.clock = c }

// SetSigner substitutes the package signer (tests / future signing service).
func (s *ReleaseService) SetSigner(sig ReleaseSigner) { s.signer = sig }

// RevisionView is the release.getRevision projection.
type RevisionView struct {
	Revisions []RevisionSummary `json:"revisions"`
	Manifest  map[string]any    `json:"manifest"`
	Reviews   []m7flow.Review   `json:"reviews"`
}

// RevisionSummary is one row of the revision list.
type RevisionSummary struct {
	RevisionNo int64  `json:"revisionNo"`
	Status     string `json:"status"`
	Digest     string `json:"digest"`
	CreatedAt  string `json:"createdAt"`
}

// PackageView is the release.getPackage projection (verified=true only when
// every digest and the signature re-verified against the stored blob).
type PackageView struct {
	ID             string                 `json:"id"`
	CRRevisionID   string                 `json:"crRevisionId"`
	ManifestDigest string                 `json:"manifestDigest"`
	BlobDigest     string                 `json:"blobDigest"`
	Signature      string                 `json:"signature"`
	State          string                 `json:"state"`
	SealedAt       string                 `json:"sealedAt"`
	MemberDigests  []m7flow.PackageMember `json:"memberDigests"`
	SBOM           *m7flow.SBOMRef        `json:"sbom"`
	Verified       bool                   `json:"verified"`
}

// CreateRevision freezes a new CR revision. The manifest must carry authorId
// (the SoD anchor) and summary; the digest binds the canonical manifest
// JSON. Any still-open predecessor revision is superseded - edits are new
// revisions, never in-place updates (REV-002).
func (s *ReleaseService) CreateRevision(ctx context.Context, crID string, manifest map[string]any) (m7flow.CRRevision, error) {
	if s == nil || s.uow == nil {
		return m7flow.CRRevision{}, ErrServiceUnavailable
	}
	if len(crID) < 1 || len(crID) > 128 {
		return m7flow.CRRevision{}, ErrPackageInvalid
	}
	authorID, _ := manifest["authorId"].(string)
	summary, _ := manifest["summary"].(string)
	if len(authorID) < 1 || len(authorID) > 128 || len(summary) < 1 || len(summary) > 2000 {
		return m7flow.CRRevision{}, fmt.Errorf("%w: manifest requires authorId and summary", ErrPackageInvalid)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return m7flow.CRRevision{}, err
	}
	digest := m7flow.SHA256Hex(canonical)
	var out m7flow.CRRevision
	err = s.uow.TransactRelease(ctx, func(tx ReleaseTx) error {
		next, err := tx.MaxCRRevisionNo(crID)
		if err != nil {
			return err
		}
		if prev, err := tx.FindOpenCRRevision(crID); err == nil {
			if err := tx.UpdateCRRevisionStatus(prev.ID, prev.Status, m7flow.CRRevSuperseded); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		out = m7flow.CRRevision{
			ID: ulid.Make().String(), CRID: crID, RevisionNo: next + 1,
			ManifestJSON: string(canonical), Digest: digest,
			Status: m7flow.CRRevSubmitted, CreatedAt: s.clock.Now().UTC(),
		}
		return tx.PutCRRevision(out)
	})
	if err != nil {
		return m7flow.CRRevision{}, err
	}
	return out, nil
}

// BuildPackage seals the immutable release package for one revision. The
// caller passes the digest it saw when reading the revision; a mismatch is
// PKG-002 before anything is written. Packaging is idempotent per revision:
// an existing package is returned unchanged. The sealed document is stored
// content-addressed in release_blobs and traced to the revision and every
// referenced Test/Scan.
func (s *ReleaseService) BuildPackage(ctx context.Context, crRevisionID, expectedDigest string) (m7flow.ReleasePackage, error) {
	if s == nil || s.uow == nil {
		return m7flow.ReleasePackage{}, ErrServiceUnavailable
	}
	var out m7flow.ReleasePackage
	err := s.uow.TransactRelease(ctx, func(tx ReleaseTx) error {
		rev, err := tx.GetCRRevision(crRevisionID)
		if err != nil {
			return ErrRevisionNotFound
		}
		if existing, err := tx.FindPackageByRevision(crRevisionID); err == nil {
			out = existing
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		if expectedDigest != rev.Digest {
			return fmt.Errorf("%w: revision digest changed", ErrDigestMismatch)
		}
		if rev.Status != m7flow.CRRevSubmitted && rev.Status != m7flow.CRRevApproved {
			return fmt.Errorf("%w: status %s", ErrRevisionFrozen, rev.Status)
		}
		var manifest map[string]any
		if err := json.Unmarshal([]byte(rev.ManifestJSON), &manifest); err != nil {
			return err
		}
		members, err := parseMembers(manifest["members"])
		if err != nil {
			return err
		}
		sbom, err := parseSBOM(manifest["sbom"])
		if err != nil {
			return err
		}
		refs, err := parseEvidenceRefs(manifest["evidenceRefs"])
		if err != nil {
			return err
		}
		// Traceability precondition: every referenced Test/Scan must exist
		// (and provide its report digest for the trace edge).
		reportDigests := make(map[string]string, len(refs))
		for _, ref := range refs {
			ok, err := tx.NodeExists(ref.Kind, ref.ID)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: %s/%s", ErrEvidenceMissing, ref.Kind, ref.ID)
			}
			d, err := tx.EvidenceReportDigest(ref.Kind, ref.ID)
			if err != nil {
				return err
			}
			reportDigests[ref.ID] = d
		}
		now := s.clock.Now().UTC()
		authorID, _ := manifest["authorId"].(string)
		doc := m7flow.SealedPackageDoc{
			CRID: rev.CRID, RevisionNo: rev.RevisionNo, RevisionDigest: rev.Digest,
			AuthorID: authorID, Manifest: manifest, Members: members, SBOM: sbom,
			EvidenceRefs: refs, SealedAt: now.Format(time.RFC3339Nano),
		}
		canonicalDoc, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		blobDigest := m7flow.SHA256Hex(canonicalDoc)
		manifestDigest := m7flow.Digest256(m7flow.PackageManifest{
			CRID: rev.CRID, RevisionNo: rev.RevisionNo, RevisionDigest: rev.Digest,
			Members: members, SBOM: sbom,
		})
		out = m7flow.ReleasePackage{
			ID: ulid.Make().String(), CRRevisionID: rev.ID,
			ManifestDigest: manifestDigest, BlobDigest: blobDigest,
			Signature: s.signer.Sign(string(canonicalDoc)),
			State:     m7flow.PkgSealed, CreatedAt: now, SealedAt: &now,
		}
		if err := tx.PutReleaseBlob(blobDigest, string(canonicalDoc), now); err != nil {
			return err
		}
		if err := tx.PutReleasePackage(out); err != nil {
			return err
		}
		if err := tx.PutEdge(m7flow.TraceEdge{
			ID: ulid.Make().String(), FromType: "cr_revision", FromID: rev.ID,
			FromDigest: rev.Digest, Relation: m7flow.RelProduces,
			ToType: "release_package", ToID: out.ID, ToDigest: blobDigest, CreatedAt: now,
		}); err != nil {
			return err
		}
		for _, ref := range refs {
			if err := tx.PutEdge(m7flow.TraceEdge{
				ID: ulid.Make().String(), FromType: "release_package", FromID: out.ID,
				FromDigest: blobDigest, Relation: m7flow.RelVerifies,
				ToType: ref.Kind, ToID: ref.ID, ToDigest: reportDigests[ref.ID], CreatedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return m7flow.ReleasePackage{}, err
	}
	return out, nil
}

// GetRevision renders the release.getRevision projection: every revision of
// the CR (ordered), the selected revision manifest (revisionNo 0 = latest)
// and the reviews bound to it.
func (s *ReleaseService) GetRevision(ctx context.Context, crID string, revisionNo int64) (RevisionView, error) {
	if s == nil || s.uow == nil {
		return RevisionView{}, ErrServiceUnavailable
	}
	var view RevisionView
	err := s.uow.TransactRelease(ctx, func(tx ReleaseTx) error {
		revs, err := tx.ListCRRevisions(crID)
		if err != nil {
			return err
		}
		if len(revs) == 0 {
			return ErrRevisionNotFound
		}
		view.Revisions = make([]RevisionSummary, 0, len(revs))
		var selected m7flow.CRRevision
		for _, r := range revs {
			view.Revisions = append(view.Revisions, RevisionSummary{
				RevisionNo: r.RevisionNo, Status: r.Status, Digest: r.Digest,
				CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339Nano),
			})
			if revisionNo == 0 || r.RevisionNo == revisionNo {
				selected = r
			}
		}
		if selected.ID == "" {
			return ErrRevisionNotFound
		}
		if err := json.Unmarshal([]byte(selected.ManifestJSON), &view.Manifest); err != nil {
			return err
		}
		view.Reviews, err = tx.ListSubjectReviews("cr_revision", selected.ID)
		return err
	})
	if err != nil {
		return RevisionView{}, err
	}
	return view, nil
}

// GetPackage renders the release.getPackage projection. It re-derives the
// blob digest, manifest digest, signature and revision binding from the
// stored blob - any mismatch fails closed (PKG-002/PKG-003) so a tampered
// package can never be consumed by promotion.
func (s *ReleaseService) GetPackage(ctx context.Context, packageID string) (PackageView, error) {
	if s == nil || s.uow == nil {
		return PackageView{}, ErrServiceUnavailable
	}
	var view PackageView
	err := s.uow.TransactRelease(ctx, func(tx ReleaseTx) error {
		pkg, err := tx.GetReleasePackage(packageID)
		if err != nil {
			return ErrPackageNotFound
		}
		blob, err := tx.GetReleaseBlob(pkg.BlobDigest)
		if err != nil {
			return fmt.Errorf("%w: blob missing", ErrDigestMismatch)
		}
		if m7flow.SHA256Hex([]byte(blob)) != pkg.BlobDigest {
			return fmt.Errorf("%w: blob content digest", ErrDigestMismatch)
		}
		if !s.signer.Verify(blob, pkg.Signature) {
			return ErrSignatureInvalid
		}
		var doc m7flow.SealedPackageDoc
		if err := json.Unmarshal([]byte(blob), &doc); err != nil {
			return fmt.Errorf("%w: blob is not a sealed document", ErrDigestMismatch)
		}
		if m7flow.Digest256(m7flow.PackageManifest{
			CRID: doc.CRID, RevisionNo: doc.RevisionNo, RevisionDigest: doc.RevisionDigest,
			Members: doc.Members, SBOM: doc.SBOM,
		}) != pkg.ManifestDigest {
			return fmt.Errorf("%w: manifest digest", ErrDigestMismatch)
		}
		rev, err := tx.GetCRRevision(pkg.CRRevisionID)
		if err != nil {
			return ErrRevisionNotFound
		}
		if rev.Digest != doc.RevisionDigest || m7flow.Digest256(doc.Manifest) != rev.Digest {
			return fmt.Errorf("%w: revision binding", ErrDigestMismatch)
		}
		sealedAt := ""
		if pkg.SealedAt != nil {
			sealedAt = pkg.SealedAt.UTC().Format(time.RFC3339Nano)
		}
		view = PackageView{
			ID: pkg.ID, CRRevisionID: pkg.CRRevisionID,
			ManifestDigest: pkg.ManifestDigest, BlobDigest: pkg.BlobDigest,
			Signature: pkg.Signature, State: pkg.State, SealedAt: sealedAt,
			MemberDigests: doc.Members, SBOM: doc.SBOM, Verified: true,
		}
		return nil
	})
	if err != nil {
		return PackageView{}, err
	}
	return view, nil
}

// CheckReviewAuthor verifies that a review submitted for a CR revision
// declares the author frozen in the revision manifest, closing the
// forged-author hole in the SoD rule (REV-001).
func (s *ReleaseService) CheckReviewAuthor(ctx context.Context, revisionID, declaredAuthor string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return s.uow.TransactRelease(ctx, func(tx ReleaseTx) error {
		rev, err := tx.GetCRRevision(revisionID)
		if err != nil {
			return ErrRevisionNotFound
		}
		var manifest map[string]any
		if err := json.Unmarshal([]byte(rev.ManifestJSON), &manifest); err != nil {
			return err
		}
		authorID, _ := manifest["authorId"].(string)
		if authorID == "" || authorID != declaredAuthor {
			return ErrAuthorMismatch
		}
		return nil
	})
}

// ApplyReview moves a submitted revision to approved/rejected after a
// review landed. It reports whether the transition was applied; closed
// revisions (superseded/approved/rejected) are left untouched - the review
// itself is already recorded as append-only evidence.
func (s *ReleaseService) ApplyReview(ctx context.Context, revisionID, verdict string) (bool, error) {
	if s == nil || s.uow == nil {
		return false, ErrServiceUnavailable
	}
	to := m7flow.CRRevApproved
	if verdict == m7flow.VerdictReject {
		to = m7flow.CRRevRejected
	}
	applied := false
	err := s.uow.TransactRelease(ctx, func(tx ReleaseTx) error {
		rev, err := tx.GetCRRevision(revisionID)
		if err != nil {
			return ErrRevisionNotFound
		}
		if rev.Status == to {
			return nil // idempotent
		}
		if !m7flow.LegalCRRevisionTransition(rev.Status, to) {
			return nil // closed revision: review stays, status does not move
		}
		applied = true
		return tx.UpdateCRRevisionStatus(revisionID, rev.Status, to)
	})
	return applied, err
}

// parseMembers validates and canonicalizes the manifest member list (R7.1).
func parseMembers(v any) ([]m7flow.PackageMember, error) {
	raw, ok := v.([]any)
	if !ok || len(raw) < 1 || len(raw) > 128 {
		return nil, fmt.Errorf("%w: members must be a non-empty array", ErrPackageInvalid)
	}
	members := make([]m7flow.PackageMember, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: member must be an object", ErrPackageInvalid)
		}
		name, _ := m["name"].(string)
		size, _ := m["size"].(float64)
		digest, _ := m["sha256"].(string)
		if len(name) < 1 || len(name) > 256 || seen[name] || size < 0 ||
			len(digest) != 64 || !isLowerHex(digest) {
			return nil, fmt.Errorf("%w: member %q malformed", ErrPackageInvalid, name)
		}
		seen[name] = true
		members = append(members, m7flow.PackageMember{Name: name, Size: int64(size), SHA256: digest})
	}
	return m7flow.SortMembers(members), nil
}

// parseSBOM validates the mandatory SBOM reference (PKG-003 when missing).
func parseSBOM(v any) (*m7flow.SBOMRef, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: sbom {format,digest} required", ErrPackageInvalid)
	}
	format, _ := m["format"].(string)
	digest, _ := m["digest"].(string)
	if len(format) < 1 || len(format) > 32 || len(digest) != 64 || !isLowerHex(digest) {
		return nil, fmt.Errorf("%w: sbom malformed", ErrPackageInvalid)
	}
	return &m7flow.SBOMRef{Format: format, Digest: digest}, nil
}

// parseEvidenceRefs validates the optional test/scan reference list.
func parseEvidenceRefs(v any) ([]m7flow.EvidenceRef, error) {
	if v == nil {
		return nil, nil
	}
	raw, ok := v.([]any)
	if !ok || len(raw) > 64 {
		return nil, fmt.Errorf("%w: evidenceRefs malformed", ErrPackageInvalid)
	}
	refs := make([]m7flow.EvidenceRef, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: evidenceRef must be an object", ErrPackageInvalid)
		}
		kind, _ := m["kind"].(string)
		id, _ := m["id"].(string)
		if (kind != "test_run" && kind != "scan_run") || len(id) < 1 || len(id) > 64 {
			return nil, fmt.Errorf("%w: evidenceRef kind/id malformed", ErrPackageInvalid)
		}
		refs = append(refs, m7flow.EvidenceRef{Kind: kind, ID: id})
	}
	return refs, nil
}
