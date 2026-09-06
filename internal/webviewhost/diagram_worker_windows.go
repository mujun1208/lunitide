//go:build windows

package webviewhost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/lunitide/lunitide/internal/diagramrender"
	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

const diagramOrigin = "https://diagram.lunitide.local"

// RunDiagramWorker is called only in a dedicated desktop --diagram-worker
// child, already assigned to the parent's kill-on-close Windows Job Object.
// It never initializes the engine gateway, credentials, main profile or UI.
func RunDiagramWorker(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(file, 131073))
	_ = file.Close()
	if err != nil || len(body) > 131072 {
		return errors.New("invalid diagram job")
	}
	var job diagramrender.Job
	if json.Unmarshal(body, &job) != nil || !filepath.IsAbs(job.RendererDir) {
		return errors.New("invalid diagram job")
	}
	task := filepath.Dir(path)
	host, err := NewBrowserHost(BrowserHostOptions{InitialURL: diagramOrigin + "/diagram-worker.html", UserDataFolder: filepath.Join(task, "profile"), MainUserDataFolder: filepath.Join(task, "unused-main"), Title: "Diagram worker"})
	if err != nil {
		return err
	}
	host.hidden = true
	host.allowNavigation = func(raw string) bool { return raw == diagramOrigin+"/diagram-worker.html" }
	var output []byte
	host.beforeNavigate = func(h *BrowserHost) error {
		var core3 *wv2.ICoreWebView2_3
		if err := query(h.core, &wv2.IID_ICoreWebView2_3, unsafe.Pointer(&core3)); err != nil {
			return err
		}
		defer core3.Release()
		if result := core3.SetVirtualHostNameToFolderMapping("diagram.lunitide.local", job.RendererDir, wv2.COREWEBVIEW2_HOST_RESOURCE_ACCESS_KIND.COREWEBVIEW2_HOST_RESOURCE_ACCESS_KIND_DENY_CORS); failed(win32.HRESULT(result)) {
			return errors.New("diagram assets unavailable")
		}
		var settings *wv2.ICoreWebView2Settings
		if result := h.core.GetSettings(&settings); failed(win32.HRESULT(result)) {
			return errors.New("diagram settings unavailable")
		}
		result := settings.SetIsWebMessageEnabled(win32.TRUE)
		settings.Release()
		if failed(win32.HRESULT(result)) {
			return errors.New("diagram message channel unavailable")
		}
		var token wv2.EventRegistrationToken
		handler := wv2.NewICoreWebView2WebMessageReceivedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2WebMessageReceivedEventArgs) com.Error {
			source, err := argumentString(args.GetSource)
			if err != nil || source != diagramOrigin+"/diagram-worker.html" {
				return com.Error(win32.S_OK)
			}
			raw, err := argumentString(args.TryGetWebMessageAsString)
			if err != nil || len(raw) > diagramrender.MaxResultBytes {
				h.fail(errors.New("diagram output exceeds budget"))
				return com.Error(win32.S_OK)
			}
			var ready struct {
				Ready bool `json:"ready"`
			}
			if json.Unmarshal([]byte(raw), &ready) == nil && ready.Ready {
				request, _ := json.Marshal(job.Request)
				if result := h.core.PostWebMessageAsJson(string(request)); failed(win32.HRESULT(result)) {
					h.fail(errors.New("diagram input delivery failed"))
				}
			} else if json.Valid([]byte(raw)) {
				output = []byte(raw)
				_ = h.Close()
			}
			return com.Error(win32.S_OK)
		}, false)
		if result := h.core.Add_WebMessageReceived(handler, &token); failed(win32.HRESULT(result)) {
			handler.Release()
			return errors.New("diagram response registration failed")
		}
		h.cleanupWorker = func() { h.core.Remove_WebMessageReceived(token); handler.Release() }
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), diagramrender.Timeout)
	defer cancel()
	if err = host.Run(ctx); err != nil {
		return err
	}
	if len(output) == 0 {
		return errors.New("diagram worker ended without a result")
	}
	return os.WriteFile(filepath.Join(task, "result.json"), output, 0600)
}
