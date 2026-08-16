// M7 slice 3 domain values (T-7.3.2/T-7.3.3): CR revisions and immutable
// release packages. Submitted revisions and sealed packages are immutable at
// both the service and database layers (M7-REV-002 / M7-PKG-001 triggers in
// migrations/0056). Package documents are content-addressed: the sealed
// canonical document lives in release_blobs under its own sha256 digest and
// every read re-derives the digests so tampering is detectable (M7-PKG-002).
package m7flow

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"
)

// CR revision statuses (cr_revisions.status CHECK set).
const (
	CRRevDraft      = "draft"
	CRRevSubmitted  = "submitted"
	CRRevApproved   = "approved"
	CRRevRejected   = "rejected"
	CRRevSuperseded = "superseded"
)

// Release package states (release_packages.state CHECK set). sealed never
// regresses (PKG-001).
const (
	PkgBuilding = "building"
	PkgSealed   = "sealed"
	PkgFailed   = "failed"
)

// CRRevision is one frozen change-request revision. The manifest carries the
// authorId (SoD anchor), summary and the change/evidence references; the
// digest binds the canonical manifest JSON. A new revision for the same CR
// supersedes the previous open one - edits are always new revisions.
type CRRevision struct {
	ID           string
	CRID         string
	RevisionNo   int64
	ManifestJSON string
	Digest       string
	Status       string
	CreatedAt    time.Time
}

// ReleasePackage is one immutable release package bound to a CR revision.
type ReleasePackage struct {
	ID             string
	CRRevisionID   string
	ManifestDigest string
	BlobDigest     string
	Signature      string
	State          string
	CreatedAt      time.Time
	SealedAt       *time.Time
}

// PackageMember is one content-addressed package member in the R7.1
// canonical form: fixed field order, algorithm pinned to sha256 and members
// sorted by UTF-8 member name.
type PackageMember struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Algorithm string `json:"algorithm"`
}

// EvidenceRef binds a package to concrete quality evidence rows.
type EvidenceRef struct {
	Kind string `json:"kind"` // test_run | scan_run
	ID   string `json:"id"`
}

// SBOMRef references the SBOM that must accompany every sealed package
// (PKG-003 when missing or malformed).
type SBOMRef struct {
	Format string `json:"format"`
	Digest string `json:"digest"`
}

// PackageManifest is the canonical package manifest (members + SBOM bound to
// one revision); ManifestDigest is Digest256 over its serialization.
type PackageManifest struct {
	CRID           string          `json:"crId"`
	RevisionNo     int64           `json:"revisionNo"`
	RevisionDigest string          `json:"revisionDigest"`
	Members        []PackageMember `json:"members"`
	SBOM           *SBOMRef        `json:"sbom,omitempty"`
}

// SealedPackageDoc is the canonical, content-addressed package document
// stored verbatim in release_blobs. Field order is fixed and the manifest
// map serializes with sorted keys, so the same inputs always produce the
// same blob digest.
type SealedPackageDoc struct {
	CRID           string          `json:"crId"`
	RevisionNo     int64           `json:"revisionNo"`
	RevisionDigest string          `json:"revisionDigest"`
	AuthorID       string          `json:"authorId"`
	Manifest       map[string]any  `json:"manifest"`
	Members        []PackageMember `json:"members"`
	SBOM           *SBOMRef        `json:"sbom,omitempty"`
	EvidenceRefs   []EvidenceRef   `json:"evidenceRefs,omitempty"`
	SealedAt       string          `json:"sealedAt"`
}

// crRevisionTransitions is the legal CRRevision state machine:
// draft -> submitted -> approved | rejected | superseded; creating a newer
// revision supersedes any still-open predecessor.
var crRevisionTransitions = map[string]map[string]bool{
	CRRevDraft:      {CRRevSubmitted: true, CRRevSuperseded: true},
	CRRevSubmitted:  {CRRevApproved: true, CRRevRejected: true, CRRevSuperseded: true},
	CRRevApproved:   {},
	CRRevRejected:   {},
	CRRevSuperseded: {},
}

// LegalCRRevisionTransition guards the CRRevision state machine.
func LegalCRRevisionTransition(from, to string) bool { return crRevisionTransitions[from][to] }

// SortMembers canonicalizes the member list ordering (UTF-8 name order) and
// pins the digest algorithm per member (R7.1).
func SortMembers(ms []PackageMember) []PackageMember {
	out := make([]PackageMember, len(ms))
	copy(out, ms)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	for i := range out {
		out[i].Algorithm = "sha256"
	}
	return out
}

// SHA256Hex hashes raw bytes to the 64-hex digest form used across M7.
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}