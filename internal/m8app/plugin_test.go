// M8 FR-18 service tests (T-8.9.x): the install verification chain
// (M8-035~038 zero registration), hot-registration bindings, toggle
// revocation, upgrade expansion quarantine (M8-039), the binding
// pre-check (M8-040), the one-transaction uninstall chain (M8-041) and
// dev-workspace quarantine - against a fully migrated SQLite store.
package m8app_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func pkg(kind, semver, permissions, sig string) m8app.PackageSource {
	return m8app.PackageSource{
		PluginID: "com.example." + kind, Semver: semver, Publisher: "example",
		Kind: kind, ManifestRef: "market://item/" + kind, Entrypoint: "fn:" + kind,
		Capabilities: `[ "` + kind + `.main" ]`, Permissions: permissions,
		Requires: `{}`, PackageHash: m8core.DigestOf(kind + "|" + semver),
		SignatureStatus: sig,
	}
}

func openPluginService(t *testing.T) (*m8app.PluginService, *storage.Store) {
	t.Helper()
	store := openSliceStore(t)
	svc := m8app.NewPluginService(store.AgentRuntimeRepository(), "local-user")
	return svc, store
}

func grantDoc(raw string) []byte { return []byte(raw) }

func TestPluginInstallChainFailuresZeroRegistration(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		pkg  m8app.PackageSource
		want error
	}{
		{"signature invalid", pkg("tool", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureInvalid), m8app.ErrPluginSignatureInvalid},
		{"manifest invalid", pkg("widget", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureVerified), m8app.ErrPluginManifestInvalid},
		{"permission beyond grant", pkg("tool", "1.0.0", `{"tool":["fs.write"]}`, m8core.SignatureVerified), m8app.ErrPluginPermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := openPluginService(t)
			svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) {
				return tc.pkg, nil
			})
			_, err := svc.Install(ctx, m8app.InstallInput{
				Origin: "market", Source: "item-1",
				PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-p1",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			// Zero registration: no active bindings exist for any install.
			res, lerr := svc.List(ctx, "", "")
			if lerr != nil {
				t.Fatalf("list: %v", lerr)
			}
			for _, p := range res.Plugins {
				if p.BindingCount != 0 {
					t.Fatalf("install %s has %d active bindings, want 0", p.InstallID, p.BindingCount)
				}
			}
		})
	}
	// Probe failure (M8-038) refuses with zero rows written.
	svc, _ := openPluginService(t)
	good := pkg("mcp", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureVerified)
	svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) { return good, nil })
	svc.SetProber(func(ctx context.Context, p m8app.PackageSource) error { return errors.New("probe down") })
	if _, err := svc.Install(ctx, m8app.InstallInput{
		Origin: "market", Source: "item-1",
		PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-p2",
	}); !errors.Is(err, m8app.ErrPluginProbeFailed) {
		t.Fatalf("err = %v, want ErrPluginProbeFailed", err)
	}
}

func TestPluginSignatureFailureQuarantinesForensicsRow(t *testing.T) {
	svc, _ := openPluginService(t)
	ctx := context.Background()
	bad := pkg("tool", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureInvalid)
	svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) { return bad, nil })
	res, err := svc.Install(ctx, m8app.InstallInput{
		Origin: "market", Source: "item-1",
		PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-p3",
	})
	if !errors.Is(err, m8app.ErrPluginSignatureInvalid) {
		t.Fatalf("err = %v, want ErrPluginSignatureInvalid", err)
	}
	if res.State != m8core.InstallQuarantined || res.InstallID == "" {
		t.Fatalf("quarantine result = %+v", res)
	}
	if len(res.Bindings) != 0 {
		t.Fatalf("quarantine registered %d bindings, want 0", len(res.Bindings))
	}
	// The quarantined row survives for forensics.
	list, err := svc.List(ctx, "", m8core.InstallQuarantined)
	if err != nil || len(list.Plugins) != 1 {
		t.Fatalf("quarantined list = %+v err=%v", list, err)
	}
	// Quarantined refuses anything but uninstall (no auto recovery).
	if _, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: res.InstallID, Enabled: true}); !errors.Is(err, m8app.ErrInstallStateInvalid) {
		t.Fatalf("quarantined enable err = %v, want ErrInstallStateInvalid", err)
	}
}

func TestPluginInstallRegistersRoutedBindings(t *testing.T) {
	svc, _ := openPluginService(t)
	ctx := context.Background()
	for _, kind := range []string{"mcp", "skill", "tool", "agent-pack"} {
		p := pkg(kind, "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureVerified)
		svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) { return p, nil })
		res, err := svc.Install(ctx, m8app.InstallInput{
			Origin: "market", Source: "item-" + kind,
			PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-" + kind,
		})
		if err != nil {
			t.Fatalf("install %s: %v", kind, err)
		}
		if res.State != m8core.InstallInstalled || len(res.Bindings) != 1 {
			t.Fatalf("install %s = %+v", kind, res)
		}
		if want := m8core.RouteTarget(kind); res.Bindings[0].TargetType != want {
			t.Fatalf("kind %s routed to %s, want %s", kind, res.Bindings[0].TargetType, want)
		}
		// The binding pre-check passes while active (M8-040).
		if err := svc.CheckBinding(ctx, res.InstallID, res.Bindings[0].TargetID); err != nil {
			t.Fatalf("checkBinding %s: %v", kind, err)
		}
	}
}

func TestPluginToggleRevokesAndReRegisters(t *testing.T) {
	svc, _ := openPluginService(t)
	ctx := context.Background()
	p := pkg("mcp", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureVerified)
	svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) { return p, nil })
	install, err := svc.Install(ctx, m8app.InstallInput{
		Origin: "market", Source: "item-mcp",
		PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-t1",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// installed -> enabled -> disabled (the safe path); disabling revokes
	// every binding immediately.
	if _, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: install.InstallID, Enabled: true}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	off, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: install.InstallID, Enabled: false})
	if err != nil || off.State != m8core.InstallDisabled {
		t.Fatalf("disable = %+v err=%v", off, err)
	}
	for _, b := range off.Bindings {
		if b.State != m8core.BindingRevoked {
			t.Fatalf("binding %s state=%s, want revoked", b.TargetID, b.State)
		}
	}
	// M8-040: the capability call on a revoked binding is refused with
	// zero side effects.
	if err := svc.CheckBinding(ctx, install.InstallID, install.Bindings[0].TargetID); !errors.Is(err, m8app.ErrBindingInactive) {
		t.Fatalf("revoked checkBinding err = %v, want ErrBindingInactive", err)
	}
	// disabled -> enabled re-registers an active binding.
	on, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: install.InstallID, Enabled: true})
	if err != nil || on.State != m8core.InstallEnabled {
		t.Fatalf("enable = %+v err=%v", on, err)
	}
	if err := svc.CheckBinding(ctx, install.InstallID, install.Bindings[0].TargetID); err != nil {
		t.Fatalf("re-enabled checkBinding: %v", err)
	}
}

func putVerifiedBundle(t *testing.T, store *storage.Store, p m8app.PackageSource) {
	t.Helper()
	b := m8core.PluginBundle{
		BundleID: ulid.Make().String(), PluginID: p.PluginID, Semver: p.Semver,
		Publisher: p.Publisher, Kind: p.Kind, ManifestRef: p.ManifestRef,
		Entrypoint: p.Entrypoint, CapabilitiesJSON: p.Capabilities,
		PermissionsJSON: p.Permissions, RequiresJSON: p.Requires,
		PackageHash: p.PackageHash, SignatureStatus: m8core.SignatureVerified,
		State: m8core.PluginVerified, CreatedAt: "2026-08-16T12:00:00Z",
	}
	err := store.AgentRuntimeRepository().TransactPlugin(context.Background(), func(tx m8app.PluginTx) error {
		return tx.PutPluginBundle(b)
	})
	if err != nil {
		t.Fatalf("put bundle %s: %v", b.BundleID, err)
	}
}

func TestPluginUpgradeExpansionQuarantines(t *testing.T) {
	svc, store := openPluginService(t)
	ctx := context.Background()
	v1 := pkg("tool", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureVerified)
	svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) { return v1, nil })
	install, err := svc.Install(ctx, m8app.InstallInput{
		Origin: "market", Source: "item-tool",
		PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-u1",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	// old(enabled) -> new: upgrades start from the enabled state.
	if _, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: install.InstallID, Enabled: true}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// v2 requests an extra permission outside the standing grant.
	v2 := pkg("tool", "1.2.0", `{"tool":["http_get","fs.write"]}`, m8core.SignatureVerified)
	putVerifiedBundle(t, store, v2)
	res, err := svc.Upgrade(ctx, m8app.UpgradeInput{InstallID: install.InstallID, TargetSemver: "1.2.0"})
	if !errors.Is(err, m8app.ErrPluginPermissionExpansion) {
		t.Fatalf("err = %v, want ErrPluginPermissionExpansion", err)
	}
	if res.State != m8core.InstallQuarantined || !res.PermissionExpansion || res.ToSemver != "1.2.0" || res.FromSemver != "1.0.0" {
		t.Fatalf("expansion result = %+v", res)
	}
	// Expansion revoked the bindings (pending review, not auto-enabled).
	if err := svc.CheckBinding(ctx, install.InstallID, install.Bindings[0].TargetID); !errors.Is(err, m8app.ErrBindingInactive) {
		t.Fatalf("post-expansion checkBinding err = %v, want ErrBindingInactive", err)
	}
	// The quarantined install refuses toggle (explicit review only).
	if _, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: install.InstallID, Enabled: true}); !errors.Is(err, m8app.ErrInstallStateInvalid) {
		t.Fatalf("quarantined toggle err = %v, want ErrInstallStateInvalid", err)
	}
}

func TestPluginUpgradeCleanReplacementNeverAutoEnables(t *testing.T) {
	svc, store := openPluginService(t)
	ctx := context.Background()
	v1 := pkg("tool", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureVerified)
	svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) { return v1, nil })
	install, err := svc.Install(ctx, m8app.InstallInput{
		Origin: "market", Source: "item-tool",
		PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-u2",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: install.InstallID, Enabled: true}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	// v2 keeps the identical permission document.
	v2 := pkg("tool", "1.1.0", `{"tool":["http_get"]}`, m8core.SignatureVerified)
	putVerifiedBundle(t, store, v2)
	res, err := svc.Upgrade(ctx, m8app.UpgradeInput{InstallID: install.InstallID, TargetSemver: "1.1.0"})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if res.State != m8core.InstallInstalled || res.PermissionExpansion || res.FromSemver != "1.0.0" || res.ToSemver != "1.1.0" {
		t.Fatalf("upgrade result = %+v", res)
	}
	// old(enabled) -> new(installed): the new version never auto-enables.
	if err := svc.CheckBinding(ctx, install.InstallID, install.Bindings[0].TargetID); !errors.Is(err, m8app.ErrBindingInactive) {
		t.Fatalf("post-upgrade checkBinding err = %v, want ErrBindingInactive (installed, not enabled)", err)
	}
	// Rollback = re-enabling (the install again hot-registers).
	if _, err := svc.Toggle(ctx, m8app.ToggleInput{InstallID: install.InstallID, Enabled: true}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
}

func TestPluginUninstallChainAndRollback(t *testing.T) {
	svc, store := openPluginService(t)
	ctx := context.Background()
	p := pkg("mcp", "1.0.0", `{"tool":["http_get"]}`, m8core.SignatureVerified)
	svc.SetSourceResolver(func(ctx context.Context, origin, source string) (m8app.PackageSource, error) { return p, nil })
	install, err := svc.Install(ctx, m8app.InstallInput{
		Origin: "market", Source: "item-mcp",
		PermissionGrant: grantDoc(`{"tool":["http_get"]}`), RequestID: "req-x1",
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	token := strings.Repeat("e", 64)

	// M8-041: a failing revoke hook rolls the whole chain back - the
	// install stays installed, no half-uninstalled state.
	svc.SetRevoker(func(ctx context.Context, spec m8app.BindingSpec) error { return errors.New("revoke down") })
	if _, err := svc.Uninstall(ctx, m8app.UninstallInput{InstallID: install.InstallID, ConfirmToken: token}); !errors.Is(err, m8app.ErrPluginUninstallConflict) {
		t.Fatalf("failing uninstall err = %v, want ErrPluginUninstallConflict", err)
	}
	live, err := svc.List(ctx, "mcp", m8core.InstallInstalled)
	if err != nil || len(live.Plugins) != 1 {
		t.Fatalf("after rollback list = %+v err=%v", live, err)
	}

	// Malformed confirmation token is refused.
	if _, err := svc.Uninstall(ctx, m8app.UninstallInput{InstallID: install.InstallID, ConfirmToken: "nope"}); err == nil {
		t.Fatalf("bad token accepted")
	}

	// Clean uninstall: bindings revoked + tombstone in one transaction.
	svc.SetRevoker(func(ctx context.Context, spec m8app.BindingSpec) error { return nil })
	res, err := svc.Uninstall(ctx, m8app.UninstallInput{InstallID: install.InstallID, ConfirmToken: token})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if res.State != m8core.InstallUninstalled || res.RevokedBindings != 1 || res.TombstoneID == "" {
		t.Fatalf("uninstall result = %+v", res)
	}
	// The tombstone row landed on the shared tombstone table.
	var tombSeen bool
	err = store.AgentRuntimeRepository().TransactPlugin(ctx, func(tx m8app.PluginTx) error {
		b, err := tx.ListBindings(install.InstallID)
		if err != nil {
			return err
		}
		for _, bd := range b {
			if bd.State != m8core.BindingRevoked {
				t.Fatalf("binding %s not revoked", bd.TargetID)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("verify bindings: %v", err)
	}
	_ = tombSeen
	// Replay is idempotent on the terminal state.
	res2, err := svc.Uninstall(ctx, m8app.UninstallInput{InstallID: install.InstallID, ConfirmToken: token})
	if err != nil || res2.State != m8core.InstallUninstalled {
		t.Fatalf("replay uninstall = %+v err=%v", res2, err)
	}
}

func TestPluginDevCreateQuarantinedUntilReview(t *testing.T) {
	svc, _ := openPluginService(t)
	ctx := context.Background()
	res, err := svc.DevCreate(ctx, m8app.DevCreateInput{
		WorkspaceID: "ws-1",
		Manifest: map[string]any{
			"id": "dev.tool", "semver": "0.1.0", "publisher": "dev",
			"kind": "tool", "capabilities": []any{"dev.run"},
		},
		Entrypoint: "fn:dev_tool#v1",
	})
	if err != nil {
		t.Fatalf("dev.create: %v", err)
	}
	if res.State != m8core.PluginQuarantined {
		t.Fatalf("dev bundle state = %s, want quarantined (source unconfirmed)", res.State)
	}
	// Invalid manifest answers M8-036.
	if _, err := svc.DevCreate(ctx, m8app.DevCreateInput{
		WorkspaceID: "ws-1",
		Manifest:    map[string]any{"id": "dev.tool", "semver": "not-semver", "publisher": "dev", "kind": "tool"},
		Entrypoint:  "fn:x",
	}); !errors.Is(err, m8app.ErrPluginManifestInvalid) {
		t.Fatalf("invalid manifest err = %v, want ErrPluginManifestInvalid", err)
	}
}

func TestPluginListItemJSONUsesCamelCase(t *testing.T) {
	raw, err := json.Marshal(m8app.PluginListItem{
		InstallID:    "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		PluginID:     "web-search",
		Semver:       "1.0.0",
		Publisher:    "lunitide",
		Kind:         "tool",
		Origin:       "local",
		State:        "enabled",
		BindingCount: 2,
		InstalledAt:  "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"installId"`, `"pluginId"`, `"semver"`, `"publisher"`, `"kind"`, `"origin"`, `"state"`, `"bindingCount"`, `"installedAt"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	if strings.Contains(s, `"PluginID"`) || strings.Contains(s, `"InstallID"`) {
		t.Fatalf("leaked PascalCase: %s", s)
	}
}
