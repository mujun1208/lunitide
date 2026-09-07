package networkpolicy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFetchTrustedHeadersStayOnInitialOrigin(t *testing.T) {
	const agent = "Lunitide/test (weather; contact: weather@example.invalid)"
	const modified = "Mon, 07 Sep 2026 01:00:00 GMT"
	for _, route := range []string{"same", "other-host", "other-port", "return"} {
		t.Run(route, func(t *testing.T) {
			type headers struct{ agent, modified string }
			seen := make(chan headers, 4)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- headers{r.UserAgent(), r.Header.Get("If-Modified-Since")}
				if r.URL.Path == "/start" {
					next := "/end"
					switch route {
					case "other-host":
						next = "http://other.test/end"
					case "other-port":
						next = "http://origin.test:8080/end"
					case "return":
						next = "http://other.test/bounce"
					}
					http.Redirect(w, r, next, http.StatusFound)
					return
				}
				if r.URL.Path == "/bounce" {
					http.Redirect(w, r, "http://origin.test/end", http.StatusFound)
					return
				}
				w.Header().Set("Last-Modified", modified)
				w.Header().Set("Expires", "Mon, 07 Sep 2026 02:00:00 GMT")
				w.WriteHeader(http.StatusNotModified)
			}))
			defer srv.Close()
			resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
			})
			got, err := Fetch(context.Background(), "http://origin.test/start", FetchOptions{Policy: fetchPolicy, Resolver: resolver, DialContext: dialTo(srv.Listener.Addr().String()), UserAgent: agent, IfModifiedSince: modified})
			if err != nil || got.Status != 304 || got.LastModified != modified || got.Expires == "" || len(got.Body) != 0 {
				t.Fatalf("conditional response=%+v err=%v", got, err)
			}
			count := 2
			if route == "return" {
				count = 3
			}
			for i := 0; i < count; i++ {
				header := <-seen
				if i == 0 || route == "same" {
					if header.agent != agent || header.modified != modified {
						t.Fatalf("lost same-origin headers at %d: %+v", i, header)
					}
				} else if strings.Contains(header.agent, "example.invalid") || header.modified != "" || header.agent != "Lunitide/0.3 (local agent evidence fetch)" {
					t.Fatalf("headers leaked across origin at %d: %+v", i, header)
				}
			}
		})
	}
}

func TestFetchRejectsInvalidTrustedHeadersBeforeResolution(t *testing.T) {
	for _, values := range [][2]string{{"agent\r\nInjected: yes", ""}, {"agent\x00", ""}, {"agent\t", ""}, {strings.Repeat("a", 257), ""}, {"ok", "Mon, 07 Sep 2026 01:00:00 GMT\r\nInjected: yes"}, {"ok", "not an HTTP date"}} {
		var resolutions atomic.Int32
		resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { resolutions.Add(1); return nil, nil })
		_, err := Fetch(context.Background(), "https://example.test/", FetchOptions{Resolver: resolver, UserAgent: values[0], IfModifiedSince: values[1]})
		if err == nil || resolutions.Load() != 0 {
			t.Fatalf("invalid headers reached network: resolutions=%d err=%v", resolutions.Load(), err)
		}
	}
}
