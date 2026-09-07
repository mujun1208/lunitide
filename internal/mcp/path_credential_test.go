package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStreamableURLCredentialRedirectCannotLeakToken(t *testing.T) {
	for _, suffix := range []string{"/mcp/token={{credential}}", "/mcp?token={{credential}}"} {
		t.Run(suffix, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Location", "https://blocked.example/mcp?token=fixture-secret")
				w.WriteHeader(http.StatusTemporaryRedirect)
			}))
			defer srv.Close()
			c := tlsTestClient(t, srv)
			c.BaseURL += suffix
			_, _, err := c.Discover(context.Background(), []byte("fixture-secret"))
			if !errors.Is(err, ErrRedirectBlocked) || strings.Contains(err.Error(), "fixture-secret") || strings.Contains(err.Error(), "blocked.example") || requests.Load() != 1 {
				t.Fatalf("redirect was followed or leaked token: %v requests=%d", err, requests.Load())
			}
		})
	}
}

func TestStreamablePathCredentialStaysWithinRequest(t *testing.T) {
	fixture := &remoteFixture{}
	var requests atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/mcp/token=fixture-secret" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("incorrect path credential request")
		}
		r.URL.Path = "/mcp"
		r.Header.Set("Authorization", "Bearer fixture-secret")
		fixture.serve(t, w, r)
	}))
	defer srv.Close()
	client := tlsTestClient(t, srv)
	client.BaseURL += "/mcp/token={{credential}}"
	if err := ValidateBaseURL(client.BaseURL); err != nil {
		t.Fatal(err)
	}
	s, _, err := client.Discover(context.Background(), []byte("fixture-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Call(context.Background(), "weather", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.Identity(), "fixture-secret") || strings.Contains(client.BaseURL, "fixture-secret") {
		t.Fatal("token persisted into remote identity")
	}
	s.Close()
	before := requests.Load()
	for _, token := range []string{"", "secret/other-path", "secret?query=1", "secret\r\nX: value", "秘密"} {
		if _, _, err := client.Discover(context.Background(), []byte(token)); err == nil || requests.Load() != before {
			t.Error("missing or malformed token reached transport")
		}
	}
	srv.Close()
	if _, _, err := client.Discover(context.Background(), []byte("fixture-secret")); err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Errorf("credential leaked through transport failure: %v", err)
	}
}

func TestStreamablePathCredentialNoLegacyFallbackOrPlaintextConfig(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(404)
	}))
	defer srv.Close()
	c := tlsTestClient(t, srv)
	c.BaseURL += "/mcp/token={{credential}}"
	if _, _, err := c.Discover(context.Background(), []byte("fixture-secret")); err == nil || calls.Load() != 1 {
		t.Fatal("path-auth endpoint attempted legacy GET")
	}
	for _, suffix := range []string{"/mcp/token=plaintext", "/mcp/token={{credential}}/suffix", "/other/{{credential}}", "/mcp/token={{credential}}?token={{credential}}", "/mcp/token=%70laintext"} {
		if err := ValidateBaseURL("https://api.tushare.pro" + suffix); err == nil || strings.Contains(err.Error(), "plaintext") {
			t.Errorf("unsafe URL accepted or echoed: %v", err)
		}
	}
}
