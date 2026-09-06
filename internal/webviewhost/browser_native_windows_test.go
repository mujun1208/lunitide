//go:build windows

package webviewhost

import (
	"context"
	"github.com/zzl/go-win32api/v2/win32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// This opt-in test uses only a private hidden WebView profile and a loopback
// proxy that refuses every request. It never touches an existing user browser.
func TestBrowserProxyNativeWebViewEnforcesActualFlags(t *testing.T) {
	if os.Getenv("LUNITIDE_TEST_NATIVE_WEBVIEW") != "1" {
		t.Skip("requires isolated test executable beside WebView2Loader.dll")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var reached atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect && r.Host == "127.0.0.1:44444" {
			reached.Store(true)
			defer cancel()
		}
		http.Error(w, "all upstream traffic denied by test", http.StatusForbidden)
	}))
	defer proxy.Close()
	root := t.TempDir()
	host, err := NewBrowserHost(BrowserHostOptions{InitialURL: "https://127.0.0.1:44444/", ProxyURL: proxy.URL, UserDataFolder: filepath.Join(root, "owned-browser"), MainUserDataFolder: filepath.Join(root, "unused-main")})
	if err != nil {
		t.Fatal(err)
	}
	host.hidden = true
	var parentAtCleanup win32.HWND
	var parentAliveDuringCleanup bool
	cleanupCalls := 0
	host.cleanupWorker = func() {
		cleanupCalls++
		parentAtCleanup = host.hwnd
		parentAliveDuringCleanup = win32.IsWindow(parentAtCleanup) != 0
	}
	if err := host.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !reached.Load() {
		t.Fatal("verified proxy never received initial navigation")
	}
	if cleanupCalls != 1 || !parentAliveDuringCleanup {
		t.Fatalf("cleanup must run once before parent destruction: calls=%d parent_alive=%v", cleanupCalls, parentAliveDuringCleanup)
	}
	if win32.IsWindow(parentAtCleanup) != 0 || host.hwnd != 0 || host.core != nil || host.core4 != nil || host.controller != nil || host.environment != nil || host.environmentHandler != nil || host.controllerHandler != nil || host.loader != nil {
		t.Fatal("owned native resources survived host shutdown")
	}
	if err := host.Close(); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
	t.Log("native WebView process flags verified; initial CONNECT reached only refusing loopback proxy")
}

func TestBrowserNativeCanceledBeforeEnvironmentDoesNotCreateWebView(t *testing.T) {
	if os.Getenv("LUNITIDE_TEST_NATIVE_WEBVIEW") != "1" {
		t.Skip("requires hidden native WebView test opt-in")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	host, err := NewBrowserHost(BrowserHostOptions{InitialURL: "https://127.0.0.1:44444/", ProxyURL: "http://127.0.0.1:1", UserDataFolder: filepath.Join(root, "owned"), MainUserDataFolder: filepath.Join(root, "main")})
	if err != nil {
		t.Fatal(err)
	}
	host.hidden = true
	if err = host.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if host.core != nil || host.controller != nil || host.environment != nil || host.hwnd != 0 {
		t.Fatal("canceled creation retained an owned resource")
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserNativeCancelDuringCreationWaitsForCallback(t *testing.T) {
	if os.Getenv("LUNITIDE_TEST_NATIVE_WEBVIEW") != "1" {
		t.Skip("requires hidden native WebView test opt-in")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := t.TempDir()
	host, err := NewBrowserHost(BrowserHostOptions{InitialURL: "https://127.0.0.1:44444/", ProxyURL: "http://127.0.0.1:1", UserDataFolder: filepath.Join(root, "owned"), MainUserDataFolder: filepath.Join(root, "main")})
	if err != nil {
		t.Fatal(err)
	}
	host.hidden = true
	requested := false
	pendingAtCancel := false
	pendingStage := ""
	host.afterEnvironmentRequested = func() {
		requested = true
		pendingAtCancel = host.environmentPending || host.controllerPending
		if host.environmentPending {
			pendingStage = "environment"
		} else if host.controllerPending {
			pendingStage = "controller"
		}
		cancel()
		_ = host.Close()
	}
	started := time.Now()
	if err = host.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !requested || !pendingAtCancel {
		t.Fatal("test did not cancel the pending creation")
	}
	if host.environmentPending || host.controllerPending || host.core != nil || host.environment != nil || host.hwnd != 0 || !host.staClosed {
		t.Fatal("creation callback escaped shutdown")
	}
	t.Logf("pending %s callback drained and private host closed in %s", pendingStage, time.Since(started))
}
