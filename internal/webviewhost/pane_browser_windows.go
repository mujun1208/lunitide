//go:build windows

package webviewhost

import (
	"encoding/json"
	"log"
	"math"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

const paneWindowClass = "LunitideBrowserPane"

var paneClassOnce sync.Once

func registerPaneWindowClass() {
	paneClassOnce.Do(func() {
		instance, _ := win32.GetModuleHandle(nil)
		wc := win32.WNDCLASSEX{
			CbSize:        uint32(unsafe.Sizeof(win32.WNDCLASSEX{})),
			Style:         win32.CS_HREDRAW | win32.CS_VREDRAW,
			LpfnWndProc:   syscall.NewCallback(paneWindowProc),
			HInstance:     instance,
			HbrBackground: win32.HBRUSH(win32.GetStockObject(win32.WHITE_BRUSH)),
			LpszClassName: win32.StrToPwstr(paneWindowClass),
		}
		if atom, _ := win32.RegisterClassEx(&wc); atom == 0 {
			log.Printf("side browser window class failed")
		}
	})
}

func paneWindowProc(hwnd win32.HWND, message uint32, wParam win32.WPARAM, lParam win32.LPARAM) win32.LRESULT {
	return win32.DefWindowProcW(hwnd, message, wParam, lParam)
}

func (h *Host) ensurePaneHost() bool {
	if h.paneHost != 0 {
		return true
	}
	if h.hwnd == 0 {
		return false
	}
	registerPaneWindowClass()
	instance, _ := win32.GetModuleHandle(nil)
	hwnd, _ := win32.CreateWindowEx(
		0,
		win32.StrToPwstr(paneWindowClass),
		win32.StrToPwstr("LunitideBrowserPane"),
		win32.WS_CHILD|win32.WS_CLIPSIBLINGS|win32.WS_CLIPCHILDREN,
		0, 0, 0, 0,
		h.hwnd, 0, instance, nil,
	)
	if hwnd == 0 {
		log.Printf("side browser host window failed")
		return false
	}
	h.paneHost = hwnd
	return true
}

func (h *Host) hidePaneHost() {
	if h.paneController != nil {
		h.paneController.SetIsVisible(win32.FALSE)
	}
	if h.paneHost != 0 {
		win32.ShowWindow(h.paneHost, win32.SW_HIDE)
	}
}

func (h *Host) syncPane(cmd PaneCommand) {
	switch cmd.Op {
	case PaneHide:
		h.paneWanted = false
		h.hidePaneHost()
	case PaneRead:
		h.paneRead()
	case PaneClick:
		h.paneClick(cmd.Text)
	case PaneShow:
		h.paneWanted = true
		h.paneBounds = wv2.TagRECT{Left: cmd.X, Top: cmd.Y, Right: cmd.X + cmd.Width, Bottom: cmd.Y + cmd.Height}
		h.paneKey = cmd.Key
		h.paneNextURL = cmd.URL
		if h.paneController == nil {
			h.ensurePane()
			return
		}
		h.placePane()
	}
}

func (h *Host) ensurePane() {
	if h.paneController != nil || h.paneCreating || h.environment == nil || h.hwnd == 0 {
		return
	}
	if !h.ensurePaneHost() {
		return
	}
	h.paneCreating = true
	h.paneCreateHandler = wv2.NewICoreWebView2CreateCoreWebView2ControllerCompletedHandlerByFunc(h.paneCreated, false)
	if result := h.environment.CreateCoreWebView2Controller(h.paneHost, h.paneCreateHandler); failed(win32.HRESULT(result)) {
		h.paneCreating = false
		log.Printf("side browser controller request failed: 0x%x", uint32(result))
	}
}

func (h *Host) paneCreated(code com.Error, controller *wv2.ICoreWebView2Controller) com.Error {
	h.paneCreating = false
	if failed(win32.HRESULT(code)) || controller == nil {
		log.Printf("side browser controller creation failed: 0x%x", uint32(code))
		return com.Error(win32.S_OK)
	}
	h.paneController = controller
	controller.AddRef()
	if result := controller.GetCoreWebView2(&h.paneCore); failed(win32.HRESULT(result)) || h.paneCore == nil {
		log.Printf("side browser core unavailable: 0x%x", uint32(result))
		return com.Error(win32.S_OK)
	}
	if err := h.configurePane(h.paneCore); err != nil {
		log.Printf("side browser settings: %v", err)
	}
	if err := h.registerPaneEvents(); err != nil {
		log.Printf("side browser events: %v", err)
		return com.Error(win32.S_OK)
	}
	h.paneBootHandler = wv2.NewICoreWebView2AddScriptToExecuteOnDocumentCreatedCompletedHandlerByFunc(func(com.Error, string) com.Error {
		return com.Error(win32.S_OK)
	}, false)
	if result := h.paneCore.AddScriptToExecuteOnDocumentCreated(paneDocumentScript, h.paneBootHandler); int32(result) < 0 {
		log.Printf("side browser boot script: 0x%x", uint32(result))
	}
	h.placePane()
	return com.Error(win32.S_OK)
}

func (h *Host) configurePane(core *wv2.ICoreWebView2) error {
	var base *wv2.ICoreWebView2Settings
	if result := core.GetSettings(&base); failed(win32.HRESULT(result)) || base == nil {
		return errPane("settings unavailable")
	}
	defer base.Release()
	var settings *wv2.ICoreWebView2Settings4
	if err := queryUnknown(&base.IUnknown, &wv2.IID_ICoreWebView2Settings4, unsafe.Pointer(&settings)); err != nil {
		return err
	}
	defer settings.Release()
	settings.SetIsWebMessageEnabled(win32.TRUE)
	settings.SetAreDefaultScriptDialogsEnabled(win32.TRUE)
	settings.SetIsStatusBarEnabled(win32.FALSE)
	settings.SetAreDevToolsEnabled(win32.FALSE)
	settings.SetAreDefaultContextMenusEnabled(win32.TRUE)
	settings.SetAreHostObjectsAllowed(win32.FALSE)
	settings.SetAreBrowserAcceleratorKeysEnabled(win32.TRUE)
	settings.SetIsPasswordAutosaveEnabled(win32.TRUE)
	settings.SetIsGeneralAutofillEnabled(win32.TRUE)
	return nil
}

type paneError string

func (e paneError) Error() string { return string(e) }

func errPane(text string) error { return paneError(text) }

func (h *Host) registerPaneEvents() error {
	h.paneNavHandler = wv2.NewICoreWebView2NavigationStartingEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NavigationStartingEventArgs) com.Error {
		uri, err := argumentString(args.GetUri)
		if err != nil || !PaneNavigationAllowed(uri) {
			args.SetCancel(win32.TRUE)
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.paneCore.Add_NavigationStarting(h.paneNavHandler, &h.paneNavToken); failed(win32.HRESULT(r)) {
		return errPane("navigation")
	}
	h.paneDoneHandler = wv2.NewICoreWebView2NavigationCompletedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NavigationCompletedEventArgs) com.Error {
		var ok int32
		if r := args.GetIsSuccess(&ok); failed(win32.HRESULT(r)) || ok == 0 {
			return com.Error(win32.S_OK)
		}
		h.notePaneDocument()
		if h.paneCiteHandler != nil && h.paneCore != nil {
			h.paneCore.ExecuteScript(paneCiteScript, h.paneCiteHandler)
		}
		if h.paneTitleHandler != nil && h.paneCore != nil {
			h.paneCore.ExecuteScript(paneTitleScript, h.paneTitleHandler)
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.paneCore.Add_NavigationCompleted(h.paneDoneHandler, &h.paneDoneToken); failed(win32.HRESULT(r)) {
		return errPane("navigation completed")
	}
	h.paneWindowHandler = wv2.NewICoreWebView2NewWindowRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NewWindowRequestedEventArgs) com.Error {
		args.SetHandled(win32.TRUE)
		raw, err := argumentString(args.GetUri)
		if err == nil && h.paneCore != nil {
			if next, ok := canonicalPaneURL(raw); ok {
				h.paneNextURL = next
				h.paneCore.Navigate(next)
				h.paneShownURL = next
			}
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.paneCore.Add_NewWindowRequested(h.paneWindowHandler, &h.paneWindowToken); failed(win32.HRESULT(r)) {
		return errPane("new window")
	}
	h.paneMessageHandler = wv2.NewICoreWebView2WebMessageReceivedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2WebMessageReceivedEventArgs) com.Error {
		raw, err := argumentString(args.GetWebMessageAsJson)
		if err != nil {
			return com.Error(win32.S_OK)
		}
		if text, ok := PaneCiteText(raw); ok {
			h.postPaneToApp("cite", text, "")
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.paneCore.Add_WebMessageReceived(h.paneMessageHandler, &h.paneMessageToken); failed(win32.HRESULT(r)) {
		return errPane("page message")
	}
	h.paneReadHandler = wv2.NewICoreWebView2ExecuteScriptCompletedHandlerByFunc(h.paneReadDone, false)
	h.paneCiteHandler = wv2.NewICoreWebView2ExecuteScriptCompletedHandlerByFunc(func(com.Error, string) com.Error {
		return com.Error(win32.S_OK)
	}, false)
	h.paneTitleHandler = wv2.NewICoreWebView2ExecuteScriptCompletedHandlerByFunc(h.paneTitleDone, false)
	return nil
}

func (h *Host) paneTitleDone(code com.Error, result string) com.Error {
	if failed(win32.HRESULT(code)) {
		return com.Error(win32.S_OK)
	}
	var title string
	if json.Unmarshal([]byte(result), &title) != nil {
		return com.Error(win32.S_OK)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return com.Error(win32.S_OK)
	}
	if len([]rune(title)) > 80 {
		title = string([]rune(title)[:80])
	}
	h.postPaneToApp("title", title, h.paneDocumentURL)
	return com.Error(win32.S_OK)
}

func paneScale(dip int32, dpi uint32) int32 {
	if dpi == 0 || dpi == 96 {
		return dip
	}
	return int32(math.Round(float64(dip) * float64(dpi) / 96))
}

func (h *Host) placePane() {
	if h.paneController == nil || h.paneHost == 0 || !h.paneWanted {
		return
	}
	if h.surfaceHidden {
		h.hidePaneHost()
		return
	}
	dpi := win32.GetDpiForWindow(h.hwnd)
	bounds := h.paneBounds
	width := bounds.Right - bounds.Left
	height := bounds.Bottom - bounds.Top
	if width < 1 || height < 1 {
		return
	}
	_, _ = win32.SetWindowPos(h.paneHost, win32.HWND_TOP, paneScale(bounds.Left, dpi), paneScale(bounds.Top, dpi), paneScale(width, dpi), paneScale(height, dpi), win32.SWP_NOACTIVATE|win32.SWP_SHOWWINDOW)
	h.paneController.SetBounds(wv2.TagRECT{Left: 0, Top: 0, Right: width, Bottom: height})
	h.paneController.SetIsVisible(win32.TRUE)
	if h.paneCore == nil || h.paneNextURL == "" {
		return
	}
	navigate, reload := paneShouldNavigate(h.paneNextURL, h.paneShownURL, h.paneDocumentURL, h.paneKey, h.paneShownKey)
	if reload {
		h.paneCore.Reload()
		h.paneShownKey = h.paneKey
		return
	}
	if !navigate {
		return
	}
	if result := h.paneCore.Navigate(h.paneNextURL); failed(win32.HRESULT(result)) {
		log.Printf("side browser navigation failed: 0x%x", uint32(result))
		return
	}
	h.paneShownURL = h.paneNextURL
	h.paneShownKey = h.paneKey
}

func (h *Host) notePaneDocument() {
	if h.paneCore == nil {
		return
	}
	uri, err := argumentString(h.paneCore.GetSource)
	if err != nil {
		return
	}
	canonical, ok := canonicalPaneURL(uri)
	if !ok {
		return
	}
	h.paneDocumentURL = canonical
	if canonical != h.paneShownURL {
		h.postPaneToApp("url", "", canonical)
	}
}

func (h *Host) paneRead() {
	if h.paneCore == nil || h.paneReadHandler == nil {
		h.postPaneToApp("text", "", "")
		return
	}
	h.paneScriptReply = true
	if result := h.paneCore.ExecuteScript(paneReadScript, h.paneReadHandler); failed(win32.HRESULT(result)) {
		h.paneScriptReply = false
		h.postPaneToApp("text", "", "")
	}
}

func (h *Host) paneReadDone(code com.Error, result string) com.Error {
	if !h.paneScriptReply {
		return com.Error(win32.S_OK)
	}
	h.paneScriptReply = false
	text := ""
	if !failed(win32.HRESULT(code)) {
		var decoded string
		if json.Unmarshal([]byte(result), &decoded) == nil {
			text = decoded
		}
	}
	if len([]rune(text)) > 1500 {
		text = string([]rune(text)[:1500])
	}
	h.postPaneToApp("text", text, "")
	return com.Error(win32.S_OK)
}

func (h *Host) paneClick(label string) {
	if h.paneCore == nil || h.paneCiteHandler == nil {
		return
	}
	script, ok := PaneClickScript(label)
	if !ok {
		return
	}
	h.paneCore.ExecuteScript(script, h.paneCiteHandler)
}

func (h *Host) postPaneToApp(kind, text, pageURL string) {
	if h.core == nil {
		return
	}
	payload := map[string]string{"source": "lunitide-pane", "type": kind}
	if kind == "text" || text != "" {
		payload["text"] = text
	}
	if pageURL != "" {
		payload["url"] = pageURL
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.core.PostWebMessageAsJson(string(raw))
}
