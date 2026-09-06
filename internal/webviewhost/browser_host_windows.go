//go:build windows

package webviewhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

const isolatedBrowserWindowClass = "LunitideIsolatedBrowserWebView2Host"

type BrowserHostOptions struct {
	InitialURL         string
	UserDataFolder     string
	MainUserDataFolder string
	Title              string
	ProxyURL           string
}

// BrowserHost owns a browser-only WebView2 environment, profile, controller,
// window, and dedicated STA thread. It intentionally has no bridge or renderer
// access and never posts messages to the main Host.
type BrowserHost struct {
	// Private hooks are used only by the bounded, bridge-free diagram worker.
	hidden                    bool
	beforeNavigate            func(*BrowserHost) error
	cleanupWorker             func()
	allowNavigation           func(string) bool
	afterEnvironmentRequested func()
	proxyURL                  string
	initialURL                string
	profile                   string
	title                     string

	mu        sync.Mutex
	running   bool
	closed    bool
	staClosed bool
	hwnd      win32.HWND
	runErr    error

	environment *wv2.ICoreWebView2Environment
	controller  *wv2.ICoreWebView2Controller
	core        *wv2.ICoreWebView2
	core4       *wv2.ICoreWebView2_4
	loader      *syscall.DLL

	environmentHandler  *wv2.ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	controllerHandler   *wv2.ICoreWebView2CreateCoreWebView2ControllerCompletedHandler
	navigationHandler   *wv2.ICoreWebView2NavigationStartingEventHandler
	newWindowHandler    *wv2.ICoreWebView2NewWindowRequestedEventHandler
	permissionHandler   *wv2.ICoreWebView2PermissionRequestedEventHandler
	networkGuardHandler *wv2.ICoreWebView2CallDevToolsProtocolMethodCompletedHandler
	downloadHandler     *wv2.ICoreWebView2DownloadStartingEventHandler
	navigationToken     wv2.EventRegistrationToken
	newWindowToken      wv2.EventRegistrationToken
	permissionToken     wv2.EventRegistrationToken
	downloadToken       wv2.EventRegistrationToken
	lastBounds          clientBounds
	hasBounds           bool
	environmentPending  bool
	controllerPending   bool
}

var isolatedBrowserHosts sync.Map
var isolatedBrowserWindowProcedure = syscall.NewCallback(isolatedBrowserWindowProc)

func NewBrowserHost(options BrowserHostOptions) (*BrowserHost, error) {
	if _, err := IsolatedBrowserArguments(options.ProxyURL); err != nil {
		return nil, err
	}
	initial, err := NormalizeBrowserURL(options.InitialURL)
	if err != nil {
		return nil, fmt.Errorf("invalid initial browser URL: %w", err)
	}
	profile, err := ValidateIsolatedBrowserProfile(options.UserDataFolder, options.MainUserDataFolder)
	if err != nil {
		return nil, err
	}
	title := options.Title
	if title == "" {
		title = "Lunitide Browser"
	}
	return &BrowserHost{initialURL: initial, profile: profile, title: title, proxyURL: options.ProxyURL}, nil
}

// Run starts and waits for the host's private STA. The caller's thread is never
// used as the COM apartment, so closing this host cannot terminate another
// WebView2 message pump.
func (h *BrowserHost) Run(ctx context.Context) error {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return errors.New("isolated browser host is already running")
	}
	if h.closed {
		h.mu.Unlock()
		return errors.New("isolated browser host is closed")
	}
	if ctx.Err() != nil {
		h.closed = true
		h.mu.Unlock()
		return nil
	}
	h.running = true
	h.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- h.runSTA(ctx) }()
	err := <-done
	h.mu.Lock()
	h.running = false
	h.closed = true
	h.mu.Unlock()
	return err
}

// Close requests destruction only of this host's private window. WM_DESTROY
// posts WM_QUIT to its dedicated STA thread, never to the main host thread.
func (h *BrowserHost) Close() error {
	h.mu.Lock()
	h.closed = true
	hwnd := h.hwnd
	running := h.running
	h.mu.Unlock()
	if hwnd != 0 {
		if ok, err := win32.PostMessage(hwnd, win32.WM_CLOSE, 0, 0); ok == 0 {
			return fmt.Errorf("closing isolated browser window failed: %v", err)
		}
	} else if running {
		return errors.New("isolated browser host is starting; cancellation context is required before its window exists")
	}
	return nil
}

func (h *BrowserHost) runSTA(ctx context.Context) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr := win32.CoInitializeEx(nil, win32.COINIT_APARTMENTTHREADED); failed(hr) {
		return fmt.Errorf("isolated browser CoInitializeEx(STA) failed: 0x%x", uint32(hr))
	}
	defer win32.CoUninitialize()
	com.InitializeContext()
	defer com.UninitializeContext()
	scope := com.NewScope()
	defer scope.Leave()
	defer h.closeSTA()

	instance, _ := win32.GetModuleHandle(nil)
	wc := win32.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win32.WNDCLASSEX{})), LpfnWndProc: isolatedBrowserWindowProcedure, HInstance: instance, HbrBackground: win32.HBRUSH(win32.COLOR_WINDOW + 1), LpszClassName: win32.StrToPwstr(isolatedBrowserWindowClass)}
	wc.HCursor, _ = win32.LoadCursor(0, win32.IDC_ARROW)
	if atom, registerErr := win32.RegisterClassEx(&wc); atom == 0 && registerErr != win32.ERROR_CLASS_ALREADY_EXISTS {
		return fmt.Errorf("isolated browser RegisterClassEx failed: %v", registerErr)
	}
	hwnd, createErr := win32.CreateWindowEx(0, wc.LpszClassName, win32.StrToPwstr(h.title), win32.WS_OVERLAPPEDWINDOW, win32.CW_USEDEFAULT, win32.CW_USEDEFAULT, 1100, 760, 0, 0, instance, nil)
	if hwnd == 0 {
		return fmt.Errorf("isolated browser CreateWindowEx failed: %v", createErr)
	}
	h.mu.Lock()
	h.hwnd = hwnd
	alreadyClosed := h.closed
	h.mu.Unlock()
	isolatedBrowserHosts.Store(hwnd, h)
	if alreadyClosed {
		win32.DestroyWindow(hwnd)
	} else {
		if !h.hidden {
			win32.ShowWindow(hwnd, win32.SW_SHOW)
		}
		win32.UpdateWindow(hwnd)
		if err := h.createWebView(); err != nil {
			return err
		}
	}

	cancelDone := make(chan struct{})
	defer close(cancelDone)
	go func() {
		select {
		case <-ctx.Done():
			win32.PostMessage(hwnd, win32.WM_CLOSE, 0, 0)
		case <-cancelDone:
		}
	}()
	var msg win32.MSG
	for {
		ret, err := win32.GetMessage(&msg, 0, 0, 0)
		if ret == 0 {
			break
		}
		if ret == -1 {
			return fmt.Errorf("isolated browser GetMessage failed: %v", err)
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessage(&msg)
	}
	h.mu.Lock()
	err := h.runErr
	h.mu.Unlock()
	return err
}

func (h *BrowserHost) createWebView() error {
	loader, err := loadWebView2Loader()
	if err != nil {
		return err
	}
	h.loader = loader
	var version string
	if result := wv2.GetAvailableCoreWebView2BrowserVersionString("", &version); failed(result) || version == "" {
		return fmt.Errorf("WebView2 Evergreen Runtime is unavailable: 0x%x", uint32(result))
	}
	h.environmentHandler = wv2.NewICoreWebView2CreateCoreWebView2EnvironmentCompletedHandlerByFunc(func(code com.Error, env *wv2.ICoreWebView2Environment) com.Error {
		h.environmentPending = false
		if h.isClosing() {
			h.closeSTA()
			return com.Error(win32.S_OK)
		}
		if failed(win32.HRESULT(code)) || env == nil {
			h.fail(fmt.Errorf("isolated browser environment creation failed: 0x%x", uint32(code)))
			return code
		}
		h.environment = env
		env.AddRef()
		h.controllerHandler = wv2.NewICoreWebView2CreateCoreWebView2ControllerCompletedHandlerByFunc(h.controllerCreated, false)
		h.controllerPending = true
		if result := env.CreateCoreWebView2Controller(h.hwnd, h.controllerHandler); failed(win32.HRESULT(result)) {
			h.controllerPending = false
			h.fail(fmt.Errorf("isolated browser controller request failed: 0x%x", uint32(result)))
		}
		return com.Error(win32.S_OK)
	}, false)
	var options *wv2.ICoreWebView2EnvironmentOptions
	if h.proxyURL != "" {
		args, _ := IsolatedBrowserArguments(h.proxyURL)
		options = newBrowserEnvironmentOptions(args, version)
		defer options.Release()
	}
	h.environmentPending = true
	if hr := wv2.CreateCoreWebView2EnvironmentWithOptions("", h.profile, options, h.environmentHandler); failed(hr) {
		h.environmentPending = false
		return fmt.Errorf("isolated CreateCoreWebView2EnvironmentWithOptions failed: 0x%x", uint32(hr))
	}
	if h.afterEnvironmentRequested != nil {
		h.afterEnvironmentRequested()
	}
	return nil
}

func (h *BrowserHost) controllerCreated(code com.Error, controller *wv2.ICoreWebView2Controller) com.Error {
	h.controllerPending = false
	if h.isClosing() {
		if controller != nil {
			controller.Close()
		}
		h.closeSTA()
		return com.Error(win32.S_OK)
	}
	if failed(win32.HRESULT(code)) || controller == nil {
		h.fail(fmt.Errorf("isolated browser controller creation failed: 0x%x", uint32(code)))
		return code
	}
	h.controller = controller
	controller.AddRef()
	if result := getBrowserCoreOwned(controller, &h.core); failed(win32.HRESULT(result)) || h.core == nil {
		h.fail(errors.New("isolated browser ICoreWebView2 unavailable"))
		return com.Error(win32.E_FAIL)
	}
	if err := query(h.core, &wv2.IID_ICoreWebView2_4, unsafe.Pointer(&h.core4)); err != nil {
		h.fail(fmt.Errorf("isolated browser requires ICoreWebView2_4: %w", err))
		return com.Error(win32.E_NOINTERFACE)
	}
	if err := hardenBrowserSettings(h.core); err != nil {
		h.fail(err)
		return com.Error(win32.E_FAIL)
	}
	if err := h.registerEvents(); err != nil {
		h.fail(err)
		return com.Error(win32.E_FAIL)
	}
	if h.beforeNavigate != nil {
		if err := h.beforeNavigate(h); err != nil {
			h.fail(err)
			return com.Error(win32.E_FAIL)
		}
	}
	h.resize()
	if h.proxyURL != "" {
		// Read the actual process arguments before the first untrusted document.
		// Enterprise/environment overrides must never silently bypass our proxy.
		h.networkGuardHandler = wv2.NewICoreWebView2CallDevToolsProtocolMethodCompletedHandlerByFunc(func(code com.Error, raw string) com.Error {
			if h.isClosing() {
				return com.Error(win32.S_OK)
			}
			var reply struct {
				Arguments []string `json:"arguments"`
			}
			if failed(win32.HRESULT(code)) || json.Unmarshal([]byte(raw), &reply) != nil || !browserArgumentsEnforced(reply.Arguments, h.proxyURL) {
				h.fail(errors.New("isolated browser network policy could not be verified"))
				return com.Error(win32.E_FAIL)
			}
			if h.core != nil {
				if result := h.core.Navigate(h.initialURL); failed(win32.HRESULT(result)) {
					h.fail(errors.New("isolated browser initial navigation failed"))
				}
			}
			return com.Error(win32.S_OK)
		}, false)
		result := h.core.CallDevToolsProtocolMethod("Browser.getBrowserCommandLine", "{}", h.networkGuardHandler)
		if failed(win32.HRESULT(result)) {
			h.fail(errors.New("isolated browser network verification could not start"))
		}
		return result
	}
	if result := h.core.Navigate(h.initialURL); failed(win32.HRESULT(result)) {
		h.fail(fmt.Errorf("isolated browser initial navigation failed: 0x%x", uint32(result)))
		return result
	}
	return com.Error(win32.S_OK)
}

func hardenBrowserSettings(core *wv2.ICoreWebView2) error {
	var base *wv2.ICoreWebView2Settings
	if result := getBrowserSettingsOwned(core, &base); failed(win32.HRESULT(result)) || base == nil {
		return errors.New("isolated browser WebView2 settings unavailable")
	}
	defer base.Release()
	var settings *wv2.ICoreWebView2Settings4
	if err := queryUnknown(&base.IUnknown, &wv2.IID_ICoreWebView2Settings4, unsafe.Pointer(&settings)); err != nil {
		return fmt.Errorf("isolated browser requires ICoreWebView2Settings4: %w", err)
	}
	defer settings.Release()
	operations := []struct {
		name string
		set  func(int32) com.Error
	}{
		{"web messaging", settings.SetIsWebMessageEnabled},
		{"DevTools", settings.SetAreDevToolsEnabled},
		{"context menus", settings.SetAreDefaultContextMenusEnabled},
		{"host objects", settings.SetAreHostObjectsAllowed},
		{"script dialogs", settings.SetAreDefaultScriptDialogsEnabled},
		{"accelerator keys", settings.SetAreBrowserAcceleratorKeysEnabled},
		{"password autosave", settings.SetIsPasswordAutosaveEnabled},
		{"autofill", settings.SetIsGeneralAutofillEnabled},
	}
	for _, operation := range operations {
		if result := operation.set(win32.FALSE); failed(win32.HRESULT(result)) {
			return fmt.Errorf("disabling isolated browser %s failed: 0x%x", operation.name, uint32(result))
		}
	}
	return nil
}

func (h *BrowserHost) registerEvents() error {
	h.navigationHandler = wv2.NewICoreWebView2NavigationStartingEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NavigationStartingEventArgs) com.Error {
		raw, err := argumentString(args.GetUri)
		if err != nil || !BrowserNavigationAllowed(raw) || (h.allowNavigation != nil && !h.allowNavigation(raw)) {
			args.SetCancel(win32.TRUE)
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_NavigationStarting(h.navigationHandler, &h.navigationToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("isolated NavigationStarting registration failed: 0x%x", uint32(r))
	}
	h.newWindowHandler = wv2.NewICoreWebView2NewWindowRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NewWindowRequestedEventArgs) com.Error {
		args.SetHandled(win32.TRUE)
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_NewWindowRequested(h.newWindowHandler, &h.newWindowToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("isolated NewWindowRequested registration failed: 0x%x", uint32(r))
	}
	h.permissionHandler = wv2.NewICoreWebView2PermissionRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2PermissionRequestedEventArgs) com.Error {
		args.SetState(wv2.COREWEBVIEW2_PERMISSION_STATE.COREWEBVIEW2_PERMISSION_STATE_DENY)
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_PermissionRequested(h.permissionHandler, &h.permissionToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("isolated PermissionRequested registration failed: 0x%x", uint32(r))
	}
	h.downloadHandler = wv2.NewICoreWebView2DownloadStartingEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2DownloadStartingEventArgs) com.Error {
		args.SetCancel(win32.TRUE)
		args.SetHandled(win32.TRUE)
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core4.Add_DownloadStarting(h.downloadHandler, &h.downloadToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("isolated DownloadStarting registration failed: 0x%x", uint32(r))
	}
	return nil
}

func (h *BrowserHost) fail(err error) {
	h.mu.Lock()
	if h.runErr == nil {
		h.runErr = err
	}
	hwnd := h.hwnd
	h.mu.Unlock()
	if hwnd != 0 {
		win32.PostMessage(hwnd, win32.WM_CLOSE, 0, 0)
	}
}

func (h *BrowserHost) resize() {
	if h.controller != nil && h.hwnd != 0 {
		var rect win32.RECT
		win32.GetClientRect(h.hwnd, &rect)
		if shouldSkipWebViewBounds(rect.Right-rect.Left, rect.Bottom-rect.Top) {
			h.controller.SetIsVisible(win32.FALSE)
			return
		}
		next := clientBounds{Left: rect.Left, Top: rect.Top, Right: rect.Right, Bottom: rect.Bottom}
		if h.hasBounds && h.lastBounds == next {
			h.controller.SetIsVisible(win32.TRUE)
			return
		}
		h.controller.SetBounds(wv2.TagRECT(rect))
		h.lastBounds = next
		h.hasBounds = true
		h.controller.SetIsVisible(win32.TRUE)
		h.controller.NotifyParentWindowPositionChanged()
	}
}

func (h *BrowserHost) closeSTA() {
	h.mu.Lock()
	if h.staClosed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	// Creation has no cancellation API. Keep the STA alive until its borrowed
	// callback result can be released; otherwise a new browser can be orphaned
	// after the caller was told shutdown succeeded. Manager bounds close waits
	// to five seconds and retains this single owned host on timeout.
	if h.environmentPending || h.controllerPending {
		hwnd := h.hwnd
		h.mu.Unlock()
		if hwnd != 0 {
			win32.ShowWindow(hwnd, win32.SW_HIDE)
		}
		return
	}
	h.staClosed = true
	h.mu.Unlock()
	if h.cleanupWorker != nil {
		cleanup := h.cleanupWorker
		h.cleanupWorker = nil
		cleanup()
	}
	if h.core4 != nil && h.downloadHandler != nil {
		h.core4.Remove_DownloadStarting(h.downloadToken)
		h.downloadHandler.Release()
		h.downloadHandler = nil
	}
	if h.core != nil {
		if h.permissionHandler != nil {
			h.core.Remove_PermissionRequested(h.permissionToken)
			h.permissionHandler.Release()
			h.permissionHandler = nil
		}
		if h.newWindowHandler != nil {
			h.core.Remove_NewWindowRequested(h.newWindowToken)
			h.newWindowHandler.Release()
			h.newWindowHandler = nil
		}
		if h.navigationHandler != nil {
			h.core.Remove_NavigationStarting(h.navigationToken)
			h.navigationHandler.Release()
			h.navigationHandler = nil
		}
	}
	// Close the controller while its parent HWND and owned interfaces still
	// exist. WM_CLOSE must not destroy the parent before this cleanup begins.
	if h.controller != nil {
		h.controller.Close()
	}
	if h.core4 != nil {
		h.core4.Release()
		h.core4 = nil
	}
	if h.core != nil {
		h.core.Release()
		h.core = nil
	}
	if h.controller != nil {
		h.controller.Release()
		h.controller = nil
	}
	if h.environment != nil {
		h.environment.Release()
		h.environment = nil
	}
	if h.controllerHandler != nil {
		h.controllerHandler.Release()
		h.controllerHandler = nil
	}
	if h.networkGuardHandler != nil {
		h.networkGuardHandler.Release()
		h.networkGuardHandler = nil
	}
	if h.environmentHandler != nil {
		h.environmentHandler.Release()
		h.environmentHandler = nil
	}
	if h.loader != nil {
		_ = h.loader.Release()
		h.loader = nil
	}
	h.mu.Lock()
	hwnd := h.hwnd
	h.hwnd = 0
	h.mu.Unlock()
	if hwnd != 0 {
		isolatedBrowserHosts.Delete(hwnd)
		win32.DestroyWindow(hwnd)
	}
}

func isolatedBrowserWindowProc(hwnd win32.HWND, message uint32, wParam win32.WPARAM, lParam win32.LPARAM) win32.LRESULT {
	value, _ := isolatedBrowserHosts.Load(hwnd)
	h, _ := value.(*BrowserHost)
	switch message {
	case win32.WM_CLOSE:
		if h != nil {
			h.closeSTA()
			return 0
		}
		return win32.DefWindowProc(hwnd, message, wParam, lParam)
	case win32.WM_SIZE:
		if h != nil {
			h.resize()
		}
		return 0
	case win32.WM_DESTROY:
		if h != nil {
			h.mu.Lock()
			h.hwnd = 0
			h.mu.Unlock()
			h.closeSTA()
		}
		isolatedBrowserHosts.Delete(hwnd)
		// This message queue belongs exclusively to BrowserHost.runSTA.
		win32.PostQuitMessage(0)
		return 0
	default:
		return win32.DefWindowProc(hwnd, message, wParam, lParam)
	}
}

func (h *BrowserHost) isClosing() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.closed }
