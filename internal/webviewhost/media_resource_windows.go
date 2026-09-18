//go:build windows

package webviewhost

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lunitide/lunitide/internal/mediaapp"
	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

var shCreateMemStream = syscall.NewLazyDLL("shlwapi.dll").NewProc("SHCreateMemStream")

func (h *Host) registerMediaResourceBroker() error {
	if h.core == nil || h.environment == nil {
		return nil
	}
	if r := h.core.AddWebResourceRequestedFilter(MediaResourceFilterURI, wv2.COREWEBVIEW2_WEB_RESOURCE_CONTEXT.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_MEDIA); failed(win32.HRESULT(r)) {
		return fmt.Errorf("media resource filter failed: 0x%x", uint32(r))
	}
	h.resourceHandler = wv2.NewICoreWebView2WebResourceRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2WebResourceRequestedEventArgs) com.Error {
		h.handleMediaResourceRequested(args)
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_WebResourceRequested(h.resourceHandler, &h.resourceToken); failed(win32.HRESULT(r)) {
		h.resourceHandler.Release()
		h.resourceHandler = nil
		return fmt.Errorf("WebResourceRequested registration failed: 0x%x", uint32(r))
	}
	return nil
}

func (h *Host) handleMediaResourceRequested(args *wv2.ICoreWebView2WebResourceRequestedEventArgs) {
	if args == nil {
		return
	}
	var context int32
	_ = args.GetResourceContext(&context)
	source, _ := argumentString(h.core.GetSource)
	var request *wv2.ICoreWebView2WebResourceRequest
	if r := args.GetRequest(&request); failed(win32.HRESULT(r)) || request == nil {
		h.completeMediaResponse(args, nil, mediaDenied(http.StatusForbidden), nil)
		return
	}
	uri, uriErr := argumentString(request.GetUri)
	method, _ := argumentString(request.GetMethod)
	rangeHeader, cookie := mediaRequestHeaders(request)
	request.Release()
	if uriErr != nil || cookie != "" || !MediaResourceAllowed(source, context, uri) {
		h.completeMediaResponse(args, nil, mediaDenied(http.StatusForbidden), nil)
		return
	}
	token, ok := ParseMediaAssetTicket(uri)
	if !ok {
		h.completeMediaResponse(args, nil, mediaDenied(http.StatusForbidden), nil)
		return
	}
	var deferral *wv2.ICoreWebView2Deferral
	if r := args.GetDeferral(&deferral); failed(win32.HRESULT(r)) || deferral == nil {
		h.completeMediaResponse(args, nil, mediaDenied(http.StatusForbidden), nil)
		return
	}
	args.AddRef()
	go h.serveMediaResource(args, deferral, token, method, rangeHeader)
}

func (h *Host) serveMediaResource(args *wv2.ICoreWebView2WebResourceRequestedEventArgs, deferral *wv2.ICoreWebView2Deferral, token, method, rangeHeader string) {
	defer args.Release()
	defer deferral.Release()
	result := mediaDenied(http.StatusForbidden)
	var body []byte
	func() {
		h.mediaInflight <- struct{}{}
		defer func() { <-h.mediaInflight }()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if h.MediaTicketResolve == nil {
			return
		}
		path, mime, err := h.MediaTicketResolve(ctx, token)
		if err != nil || path == "" {
			if err != nil && (strings.Contains(err.Error(), "MEDIA_ASSET_CHANGED") || strings.Contains(err.Error(), "MEDIA_ASSET_NOT_FOUND")) {
				result = mediaDenied(http.StatusGone)
				return
			}
			result = mediaDenied(http.StatusForbidden)
			return
		}
		served, serveErr := mediaapp.ServeMediaHTTP(path, method, rangeHeader, mime)
		if serveErr != nil {
			if os.IsNotExist(serveErr) {
				result = mediaDenied(http.StatusGone)
				return
			}
			result = mediaDenied(http.StatusForbidden)
			return
		}
		result = served
		body = served.Body
	}()
	if !h.dispatch(func() {
		h.completeMediaResponse(args, deferral, result, body)
	}) {
		deferral.Complete()
	}
}

func (h *Host) completeMediaResponse(args *wv2.ICoreWebView2WebResourceRequestedEventArgs, deferral *wv2.ICoreWebView2Deferral, result mediaapp.RangeResult, body []byte) {
	if args == nil {
		if deferral != nil {
			deferral.Complete()
		}
		return
	}
	var stream *win32.IStream
	if len(body) > 0 && result.Status < 400 {
		stream = createMemoryStream(body)
	}
	var response *wv2.ICoreWebView2WebResourceResponse
	status := int32(result.Status)
	if status < 100 {
		status = http.StatusForbidden
	}
	if r := h.environment.CreateWebResourceResponse(stream, status, MediaStatusReason(int(status)), MediaResponseHeaders(result), &response); failed(win32.HRESULT(r)) || response == nil {
		log.Printf("media resource response failed: 0x%x", uint32(r))
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

func mediaRequestHeaders(request *wv2.ICoreWebView2WebResourceRequest) (rangeHeader, cookie string) {
	var headers *wv2.ICoreWebView2HttpRequestHeaders
	if r := request.GetHeaders(&headers); failed(win32.HRESULT(r)) || headers == nil {
		return "", ""
	}
	defer headers.Release()
	rangeHeader, _ = argumentString(func(value *win32.PWSTR) com.Error { return headers.GetHeader("Range", value) })
	cookie, _ = argumentString(func(value *win32.PWSTR) com.Error { return headers.GetHeader("Cookie", value) })
	return rangeHeader, cookie
}

func mediaDenied(status int) mediaapp.RangeResult {
	return mediaapp.RangeResult{Status: status, ContentType: "application/octet-stream"}
}

func createMemoryStream(data []byte) *win32.IStream {
	var ptr uintptr
	var n uint32
	if len(data) > 0 {
		ptr = uintptr(unsafe.Pointer(&data[0]))
		n = uint32(len(data))
	}
	ret, _, _ := shCreateMemStream.Call(ptr, uintptr(n))
	runtime.KeepAlive(data)
	if ret == 0 {
		return nil
	}
	// Vet forbids converting a uintptr syscall result straight back to
	// unsafe.Pointer. Address-of round-trip keeps the COM pointer.
	return (*win32.IStream)(*(*unsafe.Pointer)(unsafe.Pointer(&ret)))
}
