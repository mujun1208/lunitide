//go:build windows

package webviewhost

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

// A preview document and its assets are served here rather than from the
// application's own origin, which is what lets them run their own scripts and
// keep their own storage without reaching anything that matters. The isolation is
// carried entirely by the response headers (PreviewResponseHeaders), because a
// document cannot loosen a policy that arrived with it.
//
// Everything below is therefore about one question: is this request naming a file
// the user asked to preview? Only PreviewTicketResolve can answer that, and it
// does so by going through the session-artifact rules the rest of the product
// already enforces.
func (h *Host) handlePreviewResourceRequested(args *wv2.ICoreWebView2WebResourceRequestedEventArgs, source, uri, method string) {
	if !PreviewRequestAllowed(source, uri) {
		h.completePreviewResponse(args, nil, http.StatusForbidden, "", nil)
		return
	}
	// A preview is a page being read. Writes have nowhere to go (form-action and
	// connect-src are 'none'), so anything but a read is a request we did not
	// design for and will not guess at.
	if method != "" && method != "GET" && method != "HEAD" {
		h.completePreviewResponse(args, nil, http.StatusMethodNotAllowed, "", nil)
		return
	}
	token, rel, ok := ParsePreviewRequest(uri)
	if !ok {
		h.completePreviewResponse(args, nil, http.StatusForbidden, "", nil)
		return
	}
	var deferral *wv2.ICoreWebView2Deferral
	if r := args.GetDeferral(&deferral); failed(win32.HRESULT(r)) || deferral == nil {
		h.completePreviewResponse(args, nil, http.StatusForbidden, "", nil)
		return
	}
	args.AddRef()
	go h.servePreviewResource(args, deferral, token, rel, method)
}

func (h *Host) servePreviewResource(args *wv2.ICoreWebView2WebResourceRequestedEventArgs, deferral *wv2.ICoreWebView2Deferral, token, rel, method string) {
	status := http.StatusForbidden
	contentType := ""
	var body []byte
	func() {
		defer func() { _ = recover() }()
		// A page with many assets must not be able to occupy every worker the host
		// has; the media broker's own budget stays separate for the same reason.
		h.previewInflight <- struct{}{}
		defer func() { <-h.previewInflight }()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if h.PreviewTicketResolve == nil {
			return
		}
		path, _, err := h.PreviewTicketResolve(ctx, token, rel)
		if err != nil || path == "" {
			// An expired ticket and a deleted file are both "this preview is over",
			// which is a different thing from "you may not ask".
			status = http.StatusGone
			return
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				status = http.StatusGone
				return
			}
			return
		}
		status = http.StatusOK
		contentType = PreviewContentType(path)
		if method != "HEAD" {
			body = data
			if strings.HasPrefix(contentType, "text/html") {
				body = injectPreviewCite(body)
			}
		}
	}()
	deliverMediaResponse(h.dispatchAndWait, func() {
		h.completePreviewResponse(args, deferral, status, contentType, body)
	}, func() {
		if deferral != nil {
			deferral.Release()
		}
		if args != nil {
			args.Release()
		}
	})
}

func (h *Host) completePreviewResponse(args *wv2.ICoreWebView2WebResourceRequestedEventArgs, deferral *wv2.ICoreWebView2Deferral, status int, contentType string, body []byte) {
	if args == nil || h == nil || h.environment == nil {
		if deferral != nil {
			deferral.Complete()
		}
		return
	}
	var stream *win32.IStream
	if len(body) > 0 && status < 400 {
		stream = createMemoryStream(body)
	}
	headers := PreviewDeniedHeaders()
	if status < 400 {
		headers = PreviewResponseHeaders(contentType)
	}
	var response *wv2.ICoreWebView2WebResourceResponse
	if r := h.environment.CreateWebResourceResponse(stream, int32(status), MediaStatusReason(status), headers, &response); failed(win32.HRESULT(r)) || response == nil {
		log.Printf("preview resource response failed: 0x%x", uint32(r))
	} else {
		args.SetResponse(response)
		response.Release()
	}
	if stream != nil {
		stream.Release()
	}
	if deferral != nil {
		deferral.Complete()
	}
}
