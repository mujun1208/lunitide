//go:build windows

package webviewhost

import (
	"testing"
	"unsafe"

	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

func TestBrowserEnvironmentOptionsUsesOwnedCOMStringsAndNoSSO(t *testing.T) {
	options := newBrowserEnvironmentOptions("--proxy-server=http://127.0.0.1:1234", "120.0.0.0")
	defer options.Release()
	var queried *wv2.ICoreWebView2EnvironmentOptions
	if options.QueryInterface(&wv2.IID_ICoreWebView2EnvironmentOptions, unsafe.Pointer(&queried)) != 0 || queried == nil {
		t.Fatal("environment options IID unavailable")
	}
	queried.Release()
	for _, getter := range []func(*win32.PWSTR) uint32{
		func(out *win32.PWSTR) uint32 { return uint32(options.GetAdditionalBrowserArguments(out)) },
		func(out *win32.PWSTR) uint32 { return uint32(options.GetTargetCompatibleBrowserVersion(out)) },
		func(out *win32.PWSTR) uint32 { return uint32(options.GetLanguage(out)) },
	} {
		var first, second win32.PWSTR
		if getter(&first) != 0 || getter(&second) != 0 || first == nil || second == nil || first == second {
			t.Fatal("string allocations not independently owned")
		}
		win32.CoTaskMemFree(unsafe.Pointer(first))
		win32.CoTaskMemFree(unsafe.Pointer(second))
	}
	var sso int32 = 1
	if options.GetAllowSingleSignOnUsingOSPrimaryAccount(&sso) != 0 || sso != 0 {
		t.Fatal("OS account SSO escaped isolation")
	}
	if options.SetAdditionalBrowserArguments("--no-proxy-server") == 0 {
		t.Fatal("immutable policy replaced")
	}
}

func TestBrowserCleanupIsSafeWhenCOMCloseReenters(t *testing.T) {
	host := &BrowserHost{}
	cleanups := 0
	host.cleanupWorker = func() { cleanups++; host.closeSTA() }
	host.closeSTA()
	host.closeSTA()
	if cleanups != 1 || !host.closed || !host.staClosed || host.cleanupWorker != nil {
		t.Fatalf("reentrant cleanup repeated or remained live: calls=%d", cleanups)
	}
}
