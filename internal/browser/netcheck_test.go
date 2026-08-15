package browser

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeLookup answers from a canned table, mirroring how tests swap
// defaultLookup without touching the real resolver.
type fakeLookup struct {
	ips []net.IP
	err error
}

func (f fakeLookup) lookup(string) ([]net.IP, error) { return f.ips, f.err }

// TestDnsRebind covers T-5.4.2: the IP classification corpus, mixed-answer
// refusal (zero escapes), the redirect policy and the pre-dial transport
// guard.
func TestDnsRebind(t *testing.T) {
	t.Run("classify ip corpus", func(t *testing.T) {
		blocked := []struct {
			ip   string
			want error
		}{
			{"127.0.0.1", ErrLoopbackBlocked},
			{"127.255.255.254", ErrLoopbackBlocked},
			{"::1", ErrLoopbackBlocked},
			{"10.1.2.3", ErrPrivateAddress},
			{"172.16.0.1", ErrPrivateAddress},
			{"172.31.255.255", ErrPrivateAddress},
			{"192.168.0.1", ErrPrivateAddress},
			{"169.254.1.1", ErrPrivateAddress},
			{"0.0.0.1", ErrPrivateAddress},
			{"100.64.0.1", ErrPrivateAddress},
			{"192.0.2.1", ErrPrivateAddress},
			{"240.0.0.1", ErrPrivateAddress},
			{"255.255.255.255", ErrPrivateAddress},
			{"fe80::1", ErrPrivateAddress},
			{"fc00::1", ErrPrivateAddress},
			{"ff02::1", ErrPrivateAddress},
		}
		for _, tc := range blocked {
			t.Run("blocked "+tc.ip, func(t *testing.T) {
				if err := ClassifyIP(net.ParseIP(tc.ip)); !errors.Is(err, tc.want) {
					t.Fatalf("ClassifyIP(%s) = %v, want %v", tc.ip, err, tc.want)
				}
			})
		}
		for _, ip := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700::1111", "172.32.0.1"} {
			t.Run("public "+ip, func(t *testing.T) {
				if err := ClassifyIP(net.ParseIP(ip)); err != nil {
					t.Fatalf("ClassifyIP(%s) = %v, want accepted", ip, err)
				}
			})
		}
	})

	t.Run("resolve refuses mixed answers", func(t *testing.T) {
		old := defaultLookup
		t.Cleanup(func() { defaultLookup = old })

		// One public IP plus one loopback IP: the whole lookup is refused
		// (zero escapes, BRW-002).
		defaultLookup = fakeLookup{ips: []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")}}.lookup
		if _, err := ResolveAndCheck("mixed.example"); !errors.Is(err, ErrLoopbackBlocked) {
			t.Fatalf("mixed answer escaped: %v", err)
		}

		// Pure public answers pass and return the verified list.
		defaultLookup = fakeLookup{ips: []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("1.1.1.1")}}.lookup
		ips, err := ResolveAndCheck("clean.example")
		if err != nil {
			t.Fatal(err)
		}
		if len(ips) != 2 {
			t.Fatalf("verified ips = %v", ips)
		}

		// Literal hosts classify without any DNS call.
		if _, err := ResolveAndCheck("192.168.0.1"); !errors.Is(err, ErrPrivateAddress) {
			t.Fatalf("literal private escaped: %v", err)
		}
		if _, err := ResolveAndCheck("localhost"); !errors.Is(err, ErrLoopbackBlocked) {
			t.Fatalf("localhost escaped: %v", err)
		}
	})

	t.Run("redirect policy", func(t *testing.T) {
		// Sixth hop: chain budget exhausted before any resolution happens.
		target, err := http.NewRequest(http.MethodGet, "http://public.example/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckRedirectPolicy(target, make([]*http.Request, MaxRedirects)); !errors.Is(err, ErrTooManyRedirects) {
			t.Fatalf("hop budget err = %v, want ErrTooManyRedirects", err)
		}

		// A redirect to a loopback literal is refused without DNS.
		loop, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckRedirectPolicy(loop, nil); !errors.Is(err, ErrLoopbackBlocked) {
			t.Fatalf("loopback redirect err = %v", err)
		}

		// A public literal target within budget passes.
		ok, err := http.NewRequest(http.MethodGet, "http://93.184.216.34/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckRedirectPolicy(ok, make([]*http.Request, MaxRedirects-1)); err != nil {
			t.Fatalf("public redirect err = %v", err)
		}
	})

	t.Run("guarded transport", func(t *testing.T) {
		// httptest servers listen on 127.0.0.1: the guard must refuse the
		// request before any byte leaves — the built-in negative case.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		t.Cleanup(srv.Close)
		gt := &GuardedTransport{}
		req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := gt.RoundTrip(req); !errors.Is(err, ErrLoopbackBlocked) {
			t.Fatalf("loopback server escaped the transport guard: %v", err)
		}

		// A public literal target reaches Base with the verified IPs
		// stashed on the request context.
		var gotIPs []net.IP
		base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
			gotIPs = ResolvedIPsFromRequest(r.Context())
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{},
				Body:       http.NoBody,
				Request:    r,
			}, nil
		})
		okReq, err := http.NewRequest(http.MethodGet, "http://93.184.216.34/", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := (&GuardedTransport{Base: base}).RoundTrip(okReq)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if len(gotIPs) != 1 || !gotIPs[0].Equal(net.ParseIP("93.184.216.34")) {
			t.Fatalf("context ips = %v", gotIPs)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
