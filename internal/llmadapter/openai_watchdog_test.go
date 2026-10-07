package llmadapter

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

// Real-chain watchdog tests. The engine pins every model call behind the
// networkpolicy transport with a three-layer stream watchdog (engine.go:
// 4-minute clocks, 60-second response header): L1 byte-level
// IdleReadTimeout catches pure silence; L2 the event clock catches
// gateways that keep sending ": keepalive" comments forever - bytes flow,
// so L1 never fires, but no meaningful SSE event ever arrives; L3 the
// content clock catches empty data frames and ping events - events
// arrive, so L2 never fires, but no reasoning/text/tool delta ever lands.
// These tests drive the production NewOpenAI/NewAnthropic/NewOpenAIResponses
// Stream path against a real local SSE server over both HTTP/2+TLS and plain
// HTTP/1.1, with the same watchdog mechanics scaled down to hundreds of
// milliseconds. The Ark Plan endpoint negotiates HTTP/2, so the h2 cases
// exercise the production wire protocol; the classification a stuck stream
// receives decides whether the chat run loop retries it
// (chatModelCallRetryable accepts only TIMEOUT).

const (
	watchdogIdle   = 500 * time.Millisecond
	watchdogHeader = 400 * time.Millisecond
)

func selfSignedLoopbackCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	pool := x509.NewCertPool()
	if parsed, err := x509.ParseCertificate(der); err != nil {
		t.Fatal(err)
	} else {
		pool.AddCert(parsed)
	}
	return cert, pool
}

// h2WatchdogServer serves the handler over TLS with HTTP/2 enabled, the
// same ALPN upgrade the Ark endpoint performs in production.
func h2WatchdogServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	cert, pool := selfSignedLoopbackCert(t)
	srv := httptest.NewUnstartedServer(handler)
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv, pool
}

func watchdogConnector(t *testing.T, baseURL string, roots *x509.CertPool) Connector {
	t.Helper()
	return watchdogConnectorCfg(t, baseURL, roots, watchdogIdle, 0)
}

// watchdogConnectorCfg lets a test separate the clocks so the one that
// fired is identifiable from the elapsed time: idle is the byte-level
// IdleReadTimeout (L1), eventIdle the keepalive/event clock (L2), and the
// content clock (L3) rides on the adapter Options instead. Zero disables
// a clock; a large value isolates it from the one under test.
func watchdogConnectorCfg(t *testing.T, baseURL string, roots *x509.CertPool, idle, eventIdle time.Duration) Connector {
	t.Helper()
	o := networkpolicy.Options{
		ConnectTimeout:        2 * time.Second,
		ResponseHeaderTimeout: watchdogHeader,
		DisableOverallTimeout: true,
		IdleReadTimeout:       idle,
		EventIdleTimeout:      eventIdle,
		MaxResponseBytes:      1 << 20,
		Policy:                networkpolicy.Policy{AllowLocalhost: true, AllowHTTP: true},
	}
	if roots != nil {
		o.TLSConfig = &tls.Config{RootCAs: roots}
	}
	c, err := networkpolicy.New(context.Background(), baseURL, "", o)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func watchdogRequest() Request {
	return Request{Model: "glm-5.3", Messages: []Message{{Role: RoleUser, Content: "写一段"}}}
}

// sseFrames streams deltas then [DONE]; when hang is true the handler
// keeps the connection open in silence after the last frame, which is
// exactly what a stuck Ark stream looks like from the client side. The
// hang watches the request context so the test server closes promptly
// once the client-side watchdog aborts the connection.
func sseFrames(content string, frameGap time.Duration, hang bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"content":"`+content+`"}}]}`)
		flusher.Flush()
		if hang {
			select {
			case <-r.Context().Done():
			case <-time.After(30 * time.Second):
			}
			return
		}
		time.Sleep(frameGap)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}
}

func streamViaWatchdog(t *testing.T, c Connector, ctx context.Context) (Response, []Delta, time.Duration, error) {
	t.Helper()
	return streamViaWatchdogOpts(t, c, ctx, Options{})
}

// streamViaWatchdogOpts passes adapter Options through, which is how a
// test arms the content clock (Options.StreamContentIdle).
func streamViaWatchdogOpts(t *testing.T, c Connector, ctx context.Context, o Options) (Response, []Delta, time.Duration, error) {
	t.Helper()
	a := NewOpenAI(c, o)
	var deltas []Delta
	start := time.Now()
	resp, err := a.Stream(ctx, nil, watchdogRequest(), func(d Delta) error {
		deltas = append(deltas, d)
		return nil
	})
	return resp, deltas, time.Since(start), err
}

func gatewayErrorCode(t *testing.T, err error) (string, Stage) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error is not a gateway error: %v", err)
	}
	return e.Code, e.Stage
}

func TestWatchdogHealthyH2StreamCompletes(t *testing.T) {
	var proto string
	var mu sync.Mutex
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		proto = r.Proto
		mu.Unlock()
		sseFrames("第一段正文", 40*time.Millisecond, false)(w, r)
	})
	resp, deltas, elapsed, err := streamViaWatchdog(t, watchdogConnector(t, srv.URL, pool), context.Background())
	if err != nil {
		t.Fatalf("healthy stream failed: %v", err)
	}
	mu.Lock()
	got := proto
	mu.Unlock()
	if got != "HTTP/2.0" {
		t.Fatalf("negotiated proto=%q, the h2 production path was not exercised", got)
	}
	if resp.Message.Content != "第一段正文" || len(deltas) == 0 {
		t.Fatalf("content=%q deltas=%d", resp.Message.Content, len(deltas))
	}
	if elapsed > 5*time.Second {
		t.Fatalf("healthy stream took %s", elapsed)
	}
}

func TestWatchdogIdleHangOnH2IsRetryableTimeout(t *testing.T) {
	srv, pool := h2WatchdogServer(t, sseFrames("挂死前的部分内容", 0, true))
	resp, _, elapsed, err := streamViaWatchdog(t, watchdogConnector(t, srv.URL, pool), context.Background())
	if err == nil {
		t.Fatal("a stuck stream must fail")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "TIMEOUT" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: a stuck stream must surface as a stream-stage TIMEOUT so the run loop retries it", code, stage)
	}
	if elapsed < watchdogIdle || elapsed > 8*time.Second {
		t.Fatalf("watchdog fired at %s, want ~%s", elapsed, watchdogIdle)
	}
	if resp.Message.Content != "挂死前的部分内容" {
		t.Fatalf("partial content must survive the watchdog cut: %q", resp.Message.Content)
	}
}

func TestWatchdogHeaderHangFailsFast(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	})
	_, _, elapsed, err := streamViaWatchdog(t, watchdogConnector(t, srv.URL, pool), context.Background())
	if err == nil {
		t.Fatal("a request that never reaches a model must fail")
	}
	code, _ := gatewayErrorCode(t, err)
	if code != "TIMEOUT" && code != "OUTCOME_UNKNOWN" {
		t.Fatalf("code=%q: a header hang must surface as a timeout-family error, not as a connection failure", code)
	}
	if elapsed < watchdogHeader || elapsed > 8*time.Second {
		t.Fatalf("header watchdog fired at %s, want ~%s", elapsed, watchdogHeader)
	}
}

func TestWatchdogIdleHangOnHTTP1IsRetryableTimeout(t *testing.T) {
	srv := httptest.NewServer(sseFrames("h1挂死", 0, true))
	t.Cleanup(srv.Close)
	_, _, elapsed, err := streamViaWatchdog(t, watchdogConnector(t, srv.URL, nil), context.Background())
	if err == nil {
		t.Fatal("a stuck stream must fail")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "TIMEOUT" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: HTTP/1.1 stuck streams must classify the same as h2", code, stage)
	}
	if elapsed < watchdogIdle || elapsed > 8*time.Second {
		t.Fatalf("watchdog fired at %s, want ~%s", elapsed, watchdogIdle)
	}
}

func TestWatchdogHealthyHTTP1StreamCompletes(t *testing.T) {
	srv := httptest.NewServer(sseFrames("h1正文", 30*time.Millisecond, false))
	t.Cleanup(srv.Close)
	resp, deltas, _, err := streamViaWatchdog(t, watchdogConnector(t, srv.URL, nil), context.Background())
	if err != nil {
		t.Fatalf("healthy h1 stream failed: %v", err)
	}
	if resp.Message.Content != "h1正文" || len(deltas) == 0 {
		t.Fatalf("content=%q deltas=%d", resp.Message.Content, len(deltas))
	}
}

func TestWatchdogUserCancelIsCancelledNotTimeout(t *testing.T) {
	srv, pool := h2WatchdogServer(t, sseFrames("会一直流", 20*time.Millisecond, true))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(250 * time.Millisecond)
		cancel()
	}()
	_, _, elapsed, err := streamViaWatchdog(t, watchdogConnector(t, srv.URL, pool), ctx)
	if err == nil {
		t.Fatal("cancel must end the stream")
	}
	code, _ := gatewayErrorCode(t, err)
	if code != "CANCELLED" {
		t.Fatalf("code=%q: user cancellation must stay CANCELLED, never a watchdog TIMEOUT", code)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cancel took %s", elapsed)
	}
}

func TestWatchdogQuietReasoningGapUnderLimitStillCompletes(t *testing.T) {
	// A thinking pause between reasoning deltas that stays under the idle
	// watchdog must not trip it: healthy deep-think streams pause briefly
	// between bursts.
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"reasoning_content":"先想"}}]}`)
		flusher.Flush()
		time.Sleep(watchdogIdle - 150*time.Millisecond)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"content":"想完了"}}]}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	resp, _, _, err := streamViaWatchdog(t, watchdogConnector(t, srv.URL, pool), context.Background())
	if err != nil {
		t.Fatalf("a quiet-but-alive stream was cut: %v", err)
	}
	if !strings.Contains(resp.Message.Content, "想完了") || !strings.Contains(resp.Reasoning, "先想") {
		t.Fatalf("content=%q reasoning=%q", resp.Message.Content, resp.Reasoning)
	}
}

// The Anthropic protocol adapter shares the transport and the stream
// classifier, but it is a separate adapter: prove its stuck streams also
// surface as retryable stream-stage timeouts.
func TestWatchdogIdleHangAnthropicProtocolIsRetryableTimeout(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"anthropic挂死\"}}\n\n")
		flusher.Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	})
	a := NewAnthropic(watchdogConnector(t, srv.URL, pool), Options{})
	start := time.Now()
	resp, err := a.Stream(context.Background(), nil, Request{Model: "claude", MaxTokens: 64, Messages: []Message{{Role: RoleUser, Content: "写一段"}}}, func(Delta) error { return nil })
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a stuck anthropic stream must fail")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "TIMEOUT" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: anthropic stuck streams must classify like openai ones", code, stage)
	}
	if elapsed < watchdogIdle || elapsed > 8*time.Second {
		t.Fatalf("watchdog fired at %s, want ~%s", elapsed, watchdogIdle)
	}
	if resp.Message.Content != "anthropic挂死" {
		t.Fatalf("partial content must survive: %q", resp.Message.Content)
	}
}

// L2: the gateway keeps sending ": keepalive" comment bytes while the
// model is stuck - bytes flow, so the byte-level clock never fires. This
// is the 325s hang shape observed in production. The event clock must cut
// it as a retryable stream-stage TIMEOUT.
func TestWatchdogKeepaliveOnlyHangIsRetryableTimeout(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"content":"心跳挂死前的部分内容"}}]}`)
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Millisecond):
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	})
	// idle=4s isolates the byte clock; only the 600ms event clock can fire.
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 600*time.Millisecond)
	resp, _, elapsed, err := streamViaWatchdog(t, c, context.Background())
	if err == nil {
		t.Fatal("a keepalive-only hang must fail")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "TIMEOUT" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: keepalive hangs must surface as a stream-stage TIMEOUT so the run loop retries them", code, stage)
	}
	if elapsed < 600*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("event clock fired at %s, want ~600ms", elapsed)
	}
	if resp.Message.Content != "心跳挂死前的部分内容" {
		t.Fatalf("partial content must survive the event-clock cut: %q", resp.Message.Content)
	}
}

// L3: empty data frames are meaningful events - the event clock returns
// each one - but no reasoning/text/tool delta ever lands. Only the
// content clock, reset by real deltas alone, can cut this shape.
func TestWatchdogEmptyDataFramesAreCutByContentClock(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"content":"空帧前的部分内容"}}]}`)
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Millisecond):
				fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"delta\":{}}]}\n\n")
				flusher.Flush()
			}
		}
	})
	// idle=4s and eventIdle=0 disable L1/L2; the 500ms content clock is
	// the only one that can fire, so a cut proves L3 specifically.
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	resp, _, elapsed, err := streamViaWatchdogOpts(t, c, context.Background(), Options{StreamContentIdle: 500 * time.Millisecond})
	if err == nil {
		t.Fatal("an empty-frame hang must fail")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "TIMEOUT" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: empty-frame hangs must surface as a stream-stage TIMEOUT so the run loop retries them", code, stage)
	}
	if elapsed < 500*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("content clock fired at %s, want ~500ms", elapsed)
	}
	if resp.Message.Content != "空帧前的部分内容" {
		t.Fatalf("partial content must survive the content-clock cut: %q", resp.Message.Content)
	}
}

// No false positives: keepalives between real deltas are normal gateway
// behavior. With all three clocks armed, a stream that keeps producing
// content must complete untouched.
func TestWatchdogKeepaliveWithContentStillCompletes(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, text := range []string{"第", "二", "段"} {
			fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{"content":"`+text+`"}}]}`)
			flusher.Flush()
			for i := 0; i < 3; i++ {
				time.Sleep(40 * time.Millisecond)
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
		fmt.Fprintf(w, "data: %s\n\n", `{"id":"1","choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})
	// All three clocks at 500ms; the ~120ms content gaps stay under each.
	c := watchdogConnectorCfg(t, srv.URL, pool, watchdogIdle, watchdogIdle)
	resp, _, _, err := streamViaWatchdogOpts(t, c, context.Background(), Options{StreamContentIdle: watchdogIdle})
	if err != nil {
		t.Fatalf("a keepalive-beat stream with live content was cut: %v", err)
	}
	if resp.Message.Content != "第二段" {
		t.Fatalf("content=%q", resp.Message.Content)
	}
}

// L3 on the anthropic adapter: "event: ping" frames carry a data line, so
// they count as meaningful events and the event clock never fires. The
// content clock must cut the stuck stream.
func TestWatchdogAnthropicPingHangIsRetryableTimeout(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ping挂死前的部分内容\"}}\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Millisecond):
				fmt.Fprint(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
				flusher.Flush()
			}
		}
	})
	// L1/L2 disabled; only the 500ms content clock can fire.
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	a := NewAnthropic(c, Options{StreamContentIdle: 500 * time.Millisecond})
	start := time.Now()
	resp, err := a.Stream(context.Background(), nil, Request{Model: "claude", MaxTokens: 64, Messages: []Message{{Role: RoleUser, Content: "写一段"}}}, func(Delta) error { return nil })
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("a ping-only hang must fail")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "TIMEOUT" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: anthropic ping hangs must surface as a stream-stage TIMEOUT so the run loop retries them", code, stage)
	}
	if elapsed < 500*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("content clock fired at %s, want ~500ms", elapsed)
	}
	if resp.Message.Content != "ping挂死前的部分内容" {
		t.Fatalf("partial content must survive the content-clock cut: %q", resp.Message.Content)
	}
}

// L3 on the Responses adapter - the wire Ark Agent Plan speaks: events like
// response.in_progress carry a data line, so they count as meaningful events
// and the event clock never fires, but no text/reasoning/tool delta ever
// lands. The content clock must cut the stuck stream.
func TestWatchdogResponsesInProgressHangIsRetryableTimeout(t *testing.T) {
	srv, pool := h2WatchdogServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"进度挂死前的部分内容\"}\n\n")
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Millisecond):
				fmt.Fprint(w, "event: response.in_progress\ndata: {\"type\":\"response.in_progress\",\"response\":{}}\n\n")
				flusher.Flush()
			}
		}
	})
	// L1/L2 disabled; only the 500ms content clock can fire.
	c := watchdogConnectorCfg(t, srv.URL, pool, 4*time.Second, 0)
	a := NewOpenAIResponses(c, Options{StreamContentIdle: 500 * time.Millisecond})
	start := time.Now()
	resp, err := a.Stream(context.Background(), nil, watchdogRequest(), func(Delta) error { return nil })
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("an in-progress-only hang must fail")
	}
	code, stage := gatewayErrorCode(t, err)
	if code != "TIMEOUT" || stage != StageStream {
		t.Fatalf("code=%q stage=%q: responses in-progress hangs must surface as a stream-stage TIMEOUT so the run loop retries them", code, stage)
	}
	if elapsed < 500*time.Millisecond || elapsed > 8*time.Second {
		t.Fatalf("content clock fired at %s, want ~500ms", elapsed)
	}
	if resp.Message.Content != "进度挂死前的部分内容" {
		t.Fatalf("partial content must survive the content-clock cut: %q", resp.Message.Content)
	}
}
