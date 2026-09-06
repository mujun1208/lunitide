package webviewhost

import (
	"strings"
	"testing"
)

func TestBrowserProxyArgumentsRejectInjectionAndVerifyActualProcess(t *testing.T) {
	for _, bad := range []string{"http://localhost:8090", "https://127.0.0.1:8090", "http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:8090/", "http://a@127.0.0.1:8090", "http://127.0.0.1:8090 --no-proxy-server", "http://127.0.0.1:8090?proxy=off"} {
		if _, err := IsolatedBrowserArguments(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	proxy := "http://127.0.0.1:8090"
	args, err := IsolatedBrowserArguments(proxy)
	if err != nil {
		t.Fatal(err)
	}
	actual := strings.Fields(args)
	if !browserArgumentsEnforced(actual, proxy) {
		t.Fatal("owned policy refused")
	}
	if browserArgumentsEnforced(actual[:len(actual)-1], proxy) {
		t.Fatal("missing UDP guard accepted")
	}
	if browserArgumentsEnforced(append(actual, "--proxy-server=direct://"), proxy) {
		t.Fatal("late policy override accepted")
	}
}
