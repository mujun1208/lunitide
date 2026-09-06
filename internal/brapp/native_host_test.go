package brapp

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNativeManagedBrowserConfirmsProcessTreeStop(t *testing.T) {
	exe := os.Getenv("LUNITIDE_BROWSER_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("set LUNITIDE_BROWSER_TEST_EXECUTABLE for hidden local browser lifecycle")
	}
	host := NewLocalHost(t.TempDir())
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	endpoint, err := host.Connect(ctx, "native-owned", ModeEdge, Settings{EdgePath: exe, BlockPrivateNetwork: true, Allowlist: []string{"https://lunitide-audit.invalid"}})
	if err != nil {
		t.Fatal(err)
	}
	if endpoint == "" || !host.IsSessionRunning("native-owned", ModeEdge) {
		t.Fatal("connected receipt without a live owned process")
	}
	started := time.Now()
	if err := host.Disconnect(ctx, "native-owned", ModeEdge); err != nil {
		t.Fatal(err)
	}
	if host.IsSessionRunning("native-owned", ModeEdge) {
		t.Fatal("disconnect retained a live process")
	}
	t.Logf("native owned browser created private context and confirmed whole process tree stop in %s", time.Since(started))
}
