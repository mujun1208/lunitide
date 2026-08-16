package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

// newM7ReleaseHarness wires the slice-3 release service (plus the slice-2
// review service review.submit depends on) onto a fresh store.
func newM7ReleaseHarness(t *testing.T) (*Engine, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "m7rel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	traceSvc := m7app.NewTraceService(repo)
	e := NewEngine(nil, "test")
	e.SetM7EvidenceServices(traceSvc, m7app.NewGateService(repo), m7app.NewReviewService(repo, traceSvc))
	e.SetM7ReleaseServices(m7app.NewReleaseService(repo))
	return e, store
}

// createRevisionForTest creates one revision and returns id+digest.
func createRevisionForTest(t *testing.T, e *Engine, manifest string) (string, string) {
	t.Helper()
	resp := e.Handle(context.Background(), m7Request(bridge.MethodReleaseCreateRevision, manifest, "idem-cr"))
	var out struct {
		CRRevisionID string `json:"crRevisionId"`
		Digest       string `json:"digest"`
		RevisionNo   int64  `json:"revisionNo"`
	}
	m7Decode(t, resp, &out)
	if out.RevisionNo < 1 || len(out.Digest) != 64 {
		t.Fatalf("unexpected revision: %+v", out)
	}
	return out.CRRevisionID, out.Digest
}

func TestReleaseCreateRevisionSupersedesOpenPredecessor(t *testing.T) {
	e, _ := newM7ReleaseHarness(t)
	ctx := context.Background()
	m1 := `{"crId":"CR-1","manifest":{"authorId":"author-1","summary":"first"},"requestId":"r1"}`
	m2 := `{"crId":"CR-1","manifest":{"authorId":"author-1","summary":"second"},"requestId":"r2"}`
	createRevisionForTest(t, e, m1)
	createRevisionForTest(t, e, m2)
	view := e.Handle(ctx, m7Request(bridge.MethodReleaseGetRevision, `{"crId":"CR-1"}`, ""))
	var rv struct {
		Revisions []struct {
			RevisionNo int64  `json:"revisionNo"`
			Status     string `json:"status"`
		} `json:"revisions"`
	}
	m7Decode(t, view, &rv)
	if len(rv.Revisions) != 2 {
		t.Fatalf("want 2 revisions, got %d", len(rv.Revisions))
	}
	if rv.Revisions[0].Status != "superseded" || rv.Revisions[1].Status != "submitted" {
		t.Fatalf("want superseded+submitted, got %+v", rv.Revisions)
	}
}

func TestReleaseReviewFlowApprovesRevision(t *testing.T) {
	e, _ := newM7ReleaseHarness(t)
	ctx := context.Background()
	revID, digest := createRevisionForTest(t, e,
		`{"crId":"CR-2","manifest":{"authorId":"author-1","summary":"s","members":[{"name":"a.bin","size":3,"sha256":"`+
			strings.Repeat("aa", 32)+`"}],"sbom":{"format":"cyclonedx","digest":"`+strings.Repeat("bb", 32)+`"}},"requestId":"r1"}`)
	// Forged author (REV-001): declared author differs from the manifest.
	forged := e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"cr_revision","subjectId":"`+revID+`","verdict":"approve","reviewerId":"rev-1","authorId":"someone-else","reason":"ok"}`, ""))
	if forged.OK || forged.Error.Code != "M7-REV-001" {
		t.Fatalf("forged author want M7-REV-001, got %+v", forged.Error)
	}
	// Self review (REV-001).
	self := e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"cr_revision","subjectId":"`+revID+`","verdict":"approve","reviewerId":"author-1","authorId":"author-1","reason":"ok"}`, ""))
	if self.OK || self.Error.Code != "M7-REV-001" {
		t.Fatalf("self review want M7-REV-001, got %+v", self.Error)
	}
	// Honest review approves the revision.
	ok := e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"cr_revision","subjectId":"`+revID+`","verdict":"approve","reviewerId":"rev-1","authorId":"author-1","reason":"ok"}`, ""))
	if !ok.OK {
		t.Fatalf("review failed: %+v", ok.Error)
	}
	view := e.Handle(ctx, m7Request(bridge.MethodReleaseGetRevision, `{"crId":"CR-2"}`, ""))
	var rv struct {
		Revisions []struct {
			Status string `json:"status"`
		} `json:"revisions"`
		Reviews []struct {
			Verdict string `json:"verdict"`
		} `json:"reviews"`
	}
	m7Decode(t, view, &rv)
	if len(rv.Revisions) != 1 || rv.Revisions[0].Status != "approved" {
		t.Fatalf("revision not approved: %+v", rv.Revisions)
	}
	if len(rv.Reviews) != 1 || rv.Reviews[0].Verdict != "approve" {
		t.Fatalf("review not projected: %+v", rv.Reviews)
	}
	// Sealing with the digest the caller saw.
	build := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+revID+`","expectedDigest":"`+digest+`"}`, "idem-bp"))
	var pkg struct {
		PackageID      string `json:"packageId"`
		ManifestDigest string `json:"manifestDigest"`
		BlobDigest     string `json:"blobDigest"`
	}
	m7Decode(t, build, &pkg)
	if len(pkg.PackageID) != 26 || len(pkg.ManifestDigest) != 64 || len(pkg.BlobDigest) != 64 {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	// getPackage re-verifies digests + signature.
	detail := e.Handle(ctx, m7Request(bridge.MethodReleaseGetPackage,
		`{"packageId":"`+pkg.PackageID+`"}`, ""))
	var dv struct {
		Package struct {
			Verified      bool `json:"verified"`
			MemberDigests []struct {
				Algorithm string `json:"algorithm"`
			} `json:"memberDigests"`
		} `json:"package"`
		SBOM struct {
			Format string `json:"format"`
		} `json:"sbom"`
	}
	m7Decode(t, detail, &dv)
	if !dv.Package.Verified || len(dv.Package.MemberDigests) != 1 ||
		dv.Package.MemberDigests[0].Algorithm != "sha256" || dv.SBOM.Format != "cyclonedx" {
		t.Fatalf("package verification failed: %+v", dv)
	}
}

func TestReleaseBuildPackageGuards(t *testing.T) {
	e, _ := newM7ReleaseHarness(t)
	ctx := context.Background()
	revID, digest := createRevisionForTest(t, e,
		`{"crId":"CR-3","manifest":{"authorId":"author-1","summary":"s","members":[{"name":"a.bin","size":3,"sha256":"`+
			strings.Repeat("aa", 32)+`"}],"sbom":{"format":"cyclonedx","digest":"`+strings.Repeat("bb", 32)+`"}},"requestId":"r1"}`)
	// Stale expected digest isolates via M7-PKG-002.
	stale := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+revID+`","expectedDigest":"`+strings.Repeat("00", 32)+`"}`, "idem-s"))
	if stale.OK || stale.Error.Code != "M7-PKG-002" {
		t.Fatalf("stale digest want M7-PKG-002, got %+v", stale.Error)
	}
	// Sealing succeeds on the still-open revision and is idempotent.
	build := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+revID+`","expectedDigest":"`+digest+`"}`, "idem-b"))
	var pkg struct {
		PackageID string `json:"packageId"`
	}
	m7Decode(t, build, &pkg)
	again := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+revID+`","expectedDigest":"`+digest+`"}`, "idem-b2"))
	var pkg2 struct {
		PackageID string `json:"packageId"`
	}
	m7Decode(t, again, &pkg2)
	if pkg2.PackageID != pkg.PackageID {
		t.Fatalf("rebuild must be idempotent: %s vs %s", pkg.PackageID, pkg2.PackageID)
	}
	// Evidence reference to a missing test run isolates via M7-PKG-002 (the
	// new revision supersedes the packaged one, like any later edit would).
	rev2, dig2 := createRevisionForTest(t, e,
		`{"crId":"CR-3","manifest":{"authorId":"author-1","summary":"s2","members":[{"name":"a.bin","size":3,"sha256":"`+
			strings.Repeat("aa", 32)+`"}],"sbom":{"format":"cyclonedx","digest":"`+strings.Repeat("bb", 32)+
			`"},"evidenceRefs":[{"kind":"test_run","id":"01ARZ3NDEKTSV4RRFFQ69G5FZZ"}]},"requestId":"r2"}`)
	missing := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+rev2+`","expectedDigest":"`+dig2+`"}`, "idem-m"))
	if missing.OK || missing.Error.Code != "M7-PKG-002" {
		t.Fatalf("missing evidence want M7-PKG-002, got %+v", missing.Error)
	}
	// The superseded packaged revision still rebuilds idempotently.
	after := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+revID+`","expectedDigest":"`+digest+`"}`, "idem-b3"))
	var pkg3 struct {
		PackageID string `json:"packageId"`
	}
	m7Decode(t, after, &pkg3)
	if pkg3.PackageID != pkg.PackageID {
		t.Fatalf("rebuild after supersede must be idempotent: %s vs %s", pkg.PackageID, pkg3.PackageID)
	}
	// Reject the open revision, then a fresh package attempt on the closed
	// revision answers M7-REV-002.
	rej := e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"cr_revision","subjectId":"`+rev2+`","verdict":"reject","reviewerId":"rev-1","authorId":"author-1","reason":"no"}`, ""))
	if !rej.OK {
		t.Fatalf("reject review failed: %+v", rej.Error)
	}
	closed := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+rev2+`","expectedDigest":"`+dig2+`"}`, "idem-c"))
	if closed.OK || closed.Error.Code != "M7-REV-002" {
		t.Fatalf("closed revision want M7-REV-002, got %+v", closed.Error)
	}
}

func TestReleaseRevisionFrozenMapsToREV002(t *testing.T) {
	e, _ := newM7ReleaseHarness(t)
	ctx := context.Background()
	// Rejected revision cannot be packaged: M7-REV-002.
	revID, _ := createRevisionForTest(t, e,
		`{"crId":"CR-4","manifest":{"authorId":"author-1","summary":"s","members":[{"name":"a","size":1,"sha256":"`+
			strings.Repeat("aa", 32)+`"}],"sbom":{"format":"spdx","digest":"`+strings.Repeat("cc", 32)+`"}},"requestId":"r1"}`)
	rej := e.Handle(ctx, m7Request(bridge.MethodReviewSubmit,
		`{"subjectType":"cr_revision","subjectId":"`+revID+`","verdict":"reject","reviewerId":"rev-1","authorId":"author-1","reason":"no"}`, ""))
	if !rej.OK {
		t.Fatalf("reject review failed: %+v", rej.Error)
	}
	dig := e.Handle(ctx, m7Request(bridge.MethodReleaseGetRevision, `{"crId":"CR-4"}`, ""))
	var rv struct {
		Revisions []struct {
			RevisionNo int64  `json:"revisionNo"`
			Status     string `json:"status"`
			Digest     string `json:"digest"`
		} `json:"revisions"`
	}
	m7Decode(t, dig, &rv)
	frozen := e.Handle(ctx, m7Request(bridge.MethodReleaseBuildPackage,
		`{"crRevisionId":"`+revID+`","expectedDigest":"`+rv.Revisions[0].Digest+`"}`, "idem-f"))
	if frozen.OK || frozen.Error.Code != "M7-REV-002" {
		t.Fatalf("frozen revision want M7-REV-002, got %+v", frozen.Error)
	}
}

func TestReleaseHandlerSchemaGuards(t *testing.T) {
	e, _ := newM7ReleaseHarness(t)
	ctx := context.Background()
	cases := []struct {
		method  bridge.Method
		payload string
	}{
		{bridge.MethodReleaseCreateRevision, `{"crId":"","manifest":{"a":"b"},"requestId":"r"}`},
		{bridge.MethodReleaseCreateRevision, `{"crId":"CR","manifest":{"a":"b"},"requestId":""}`},
		{bridge.MethodReleaseCreateRevision, `{"crId":"CR","manifest":{"only":"one"},"requestId":"r"}`},
		{bridge.MethodReleaseBuildPackage, `{"crRevisionId":"bad","expectedDigest":"` + strings.Repeat("aa", 32) + `"}`},
		{bridge.MethodReleaseBuildPackage, `{"crRevisionId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","expectedDigest":"xyz"}`},
		{bridge.MethodReleaseGetRevision, `{"crId":""}`},
		{bridge.MethodReleaseGetRevision, `{"crId":"CR","revisionNo":-2}`},
		{bridge.MethodReleaseGetPackage, `{"packageId":"not-a-ulid"}`},
	}
	for _, tc := range cases {
		resp := e.Handle(ctx, m7Request(tc.method, tc.payload, ""))
		if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("%s: want BRIDGE_SCHEMA_INVALID, got %+v", tc.method, resp.Error)
		}
	}
	// Writes without an idempotency key are rejected.
	noKey := e.Handle(ctx, validRequest(string(bridge.MethodReleaseCreateRevision),
		`{"crId":"CR","manifest":{"authorId":"a","summary":"s"},"requestId":"r"}`))
	if noKey.OK || noKey.Error.Code != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("write without idempotency key want IDEMPOTENCY_KEY_REQUIRED, got %+v", noKey.Error)
	}
}

func TestReleaseGetPackageNotFound(t *testing.T) {
	e, _ := newM7ReleaseHarness(t)
	ctx := context.Background()
	resp := e.Handle(ctx, m7Request(bridge.MethodReleaseGetPackage,
		`{"packageId":"01ARZ3NDEKTSV4RRFFQ69G5FAV"}`, ""))
	if resp.OK || resp.Error.Code != "NOT_FOUND" {
		t.Fatalf("want NOT_FOUND, got %+v", resp.Error)
	}
}
