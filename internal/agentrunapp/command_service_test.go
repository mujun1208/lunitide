package agentrunapp_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/canonpath"
	"github.com/lunitide/lunitide/internal/commandworker"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

func TestCommandServiceCwdMustMatchExecuteGrant(t *testing.T) {
	svc, _, run := runtimeHarness(t, testBudget())
	ctx := context.Background()
	root := t.TempDir()
	for _, rel := range []string{"safe", "restricted", "project"} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	registration, err := svc.WorkspaceRegister(ctx, "cmd-scope-reg", "test", map[string]any{"root": root}, root)
	if err != nil {
		t.Fatal(err)
	}
	scope := []byte(`{"operations":["execute"],"paths":["safe/**"]}`)
	grant, err := svc.WorkspaceGrant(ctx, "cmd-scope-grant", "test", scope, registration.ID, scope, 3600)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquireLease(ctx, "cmd-scope-lease", "test", map[string]string{"grantId": grant.ID}, grant.ID, 900)
	if err != nil {
		t.Fatal(err)
	}
	base := agentrunapp.CommandStartInput{RunID: run.ID, LeaseID: lease.ID, FencingToken: lease.FencingToken, TemplateID: "go.test", TemplateVersion: "1.0.0"}
	for i, cwd := range []string{"", "restricted", "project"} {
		in := base
		in.WorkDir = cwd
		_, err = svc.CommandReviewRequest(ctx, "cmd-scope-denied-"+string(rune('a'+i)), "test", in, in)
		if !errors.Is(err, agentrunapp.ErrFsScopeDenied) {
			t.Fatalf("cwd %q: err=%v", cwd, err)
		}
	}

	gotSpec := make(chan commandworker.Spec, 1)
	svc.SetCommandRunner(func(ctx context.Context, spec commandworker.Spec, guard commandworker.StartGuard, onOutput func([]byte)) (commandworker.Outcome, error) {
		gotSpec <- spec
		return commandworker.Outcome{ExitCode: 0}, nil
	})
	allowed := base
	allowed.WorkDir = "safe"
	review, err := svc.CommandReviewRequest(ctx, "cmd-scope-allowed-review", "test", allowed, allowed)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := svc.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ReviewDecide(ctx, "cmd-scope-allowed-decide", "test", review, run.ID, paused.Version, review.ApprovalDigest, agentrun.ReviewApproved); err != nil {
		t.Fatal(err)
	}
	allowed.ExpectedSpecDigest = review.CommandSpecDigest
	allowed.ApprovalDigest = review.ApprovalDigest
	if runtime.GOOS == "windows" {
		safe := filepath.Join(root, "safe")
		moved := filepath.Join(root, "safe-approved")
		if err := os.Rename(safe, moved); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", safe, filepath.Join(root, "restricted"))
		if output, linkErr := cmd.CombinedOutput(); linkErr != nil {
			_ = os.Rename(moved, safe)
			t.Skipf("directory junction unavailable: %v: %s", linkErr, output)
		}
		if _, err = svc.CommandStart(ctx, "cmd-scope-substitution-start", "test", allowed, allowed); err == nil {
			t.Fatal("approval-time cwd substitution unexpectedly started")
		}
		select {
		case <-gotSpec:
			t.Fatal("runner invoked for substituted cwd")
		default:
		}
		if err := os.Remove(safe); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(moved, safe); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = svc.CommandStart(ctx, "cmd-scope-allowed-start", "test", allowed, allowed); err != nil {
		t.Fatal(err)
	}
	spec := <-gotSpec
	// Compared canonically because the spec carries the directory as the
	// operating system names it, which differs from however this test's
	// temp root happened to be spelled.
	wantDir, err := canonpath.Canonical(filepath.Join(root, "safe"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Dir != wantDir {
		t.Fatalf("dir=%q want %q", spec.Dir, wantDir)
	}
	if len(spec.Args) != 3 || spec.Args[0] != "test" || spec.Args[1] != "-count=1" || spec.Args[2] != "./..." {
		t.Fatalf("argv=%v", spec.Args)
	}
}
