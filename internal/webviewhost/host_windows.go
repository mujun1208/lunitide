//go:build windows

package webviewhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lunitide/lunitide/internal/hostbridge"
	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

const (
	windowClass     = "LunitideWebView2Host"
	uiMessage       = win32.WM_APP + 1
	hostWindowStyle = win32.WS_OVERLAPPEDWINDOW
)

// enableHighResolutionRendering opts the process into per-monitor-v2 DPI
// awareness BEFORE any window exists. Without this, Windows renders the whole
// window at 96 DPI and bitmap-stretches it on scaled displays (125%/150%),
// which is the primary cause of blurry text. Per-monitor-v2 is what Chromium
// hosts (Trae/VSCode/Electron) use; Windows 10 1703+ supports it, older
// systems fall back to system-DPI awareness which still avoids stretching
// on the primary monitor.
func enableHighResolutionRendering() {
	ok, err := win32.SetProcessDpiAwarenessContext(win32.DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)
	if ok == 0 || err != win32.NO_ERROR {
		win32.SetProcessDPIAware()
	}
}

type Host struct {
	gateway        *hostbridge.Gateway
	folder         string
	userDataFolder string
	hwnd           win32.HWND

	environment *wv2.ICoreWebView2Environment
	controller  *wv2.ICoreWebView2Controller
	core        *wv2.ICoreWebView2
	core3       *wv2.ICoreWebView2_3
	core4       *wv2.ICoreWebView2_4
	loader      *syscall.DLL

	environmentHandler *wv2.ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	controllerHandler  *wv2.ICoreWebView2CreateCoreWebView2ControllerCompletedHandler
	messageHandler     *wv2.ICoreWebView2WebMessageReceivedEventHandler
	frameHandler       *wv2.ICoreWebView2FrameCreatedEventHandler
	navStartHandler    *wv2.ICoreWebView2NavigationStartingEventHandler
	navDoneHandler     *wv2.ICoreWebView2NavigationCompletedEventHandler
	newWindowHandler   *wv2.ICoreWebView2NewWindowRequestedEventHandler
	permissionHandler  *wv2.ICoreWebView2PermissionRequestedEventHandler
	downloadHandler    *wv2.ICoreWebView2DownloadStartingEventHandler
	messageToken       wv2.EventRegistrationToken
	frameToken         wv2.EventRegistrationToken
	navStartToken      wv2.EventRegistrationToken
	navDoneToken       wv2.EventRegistrationToken
	newWindowToken     wv2.EventRegistrationToken
	permissionToken    wv2.EventRegistrationToken
	downloadToken      wv2.EventRegistrationToken
	frames             []*frameRegistration
	runCtx             context.Context
	runCancel          context.CancelFunc
	initialPending     bool

	mu          sync.Mutex
	uiQueue     *BoundedQueue[func()]
	runErr      error
	closed      bool
	generation  uint64
	postMessage func(win32.HWND, uint32, win32.WPARAM, win32.LPARAM) (win32.BOOL, win32.WIN32_ERROR)
}

type frameRegistration struct {
	frame        *wv2.ICoreWebView2Frame2
	message      *wv2.ICoreWebView2FrameWebMessageReceivedEventHandler
	destroyed    *wv2.ICoreWebView2FrameDestroyedEventHandler
	messageTok   wv2.EventRegistrationToken
	destroyedTok wv2.EventRegistrationToken
}

var hosts sync.Map

var dwmSetWindowAttribute = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")

func setDarkTitleBar(hwnd win32.HWND, dark bool) bool {
	enabled := int32(0)
	if dark {
		enabled = 1
	}
	// DWMWA_USE_IMMERSIVE_DARK_MODE is 20 on current Windows 10/11 and 19 on
	// older Windows 10 builds. Failure is harmless, so try the legacy value too.
	for _, attribute := range []uintptr{20, 19} {
		result, _, _ := dwmSetWindowAttribute.Call(uintptr(hwnd), attribute, uintptr(unsafe.Pointer(&enabled)), unsafe.Sizeof(enabled))
		if win32.HRESULT(result) >= 0 {
			return true
		}
	}
	return false
}

// DefaultRendererFolder resolves the production renderer beside the executable.
func DefaultRendererFolder() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "web", "dist"), nil
}

// loadAppIcon loads the Lunitide application icon from the executable directory
// (production) or the project resources directory (development). Returns 0 if
// the icon cannot be found, allowing the window to fall back to the system default.
func loadAppIcon() win32.HICON {
	candidates := make([]string, 0, 2)
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "lunitide-icon.ico"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "resources", "lunitide-icon.ico"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		cx, _ := win32.GetSystemMetrics(win32.SM_CXICON)
		cy, _ := win32.GetSystemMetrics(win32.SM_CYICON)
		if cx <= 0 {
			cx = 32
		}
		if cy <= 0 {
			cy = 32
		}
		handle, _ := win32.LoadImageW(0, win32.StrToPwstr(p), win32.IMAGE_ICON, cx, cy, win32.LR_LOADFROMFILE)
		if handle != 0 {
			return win32.HICON(handle)
		}
	}
	return 0
}

func New(gateway *hostbridge.Gateway, rendererFolder, userDataFolder string) (*Host, error) {
	if gateway == nil {
		return nil, errors.New("WebView2 gateway is required")
	}
	if userDataFolder == "" || !filepath.IsAbs(userDataFolder) {
		return nil, errors.New("an absolute secured WebView2 user-data folder is required")
	}
	abs, err := filepath.Abs(rendererFolder)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(filepath.Join(abs, "index.html"))
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("renderer index is unavailable at %s", abs)
	}
	return &Host{gateway: gateway, folder: abs, userDataFolder: filepath.Clean(userDataFolder), uiQueue: NewBoundedQueue[func()](MaxUIQueue), postMessage: win32.PostMessage}, nil
}

// Run owns the locked OS thread, COM STA, window, and Win32 message pump.
func (h *Host) Run(ctx context.Context) error {
	h.runCtx, h.runCancel = context.WithCancel(ctx)
	runDone := make(chan struct{})
	defer close(runDone)
	defer h.runCancel()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr := win32.CoInitializeEx(nil, win32.COINIT_APARTMENTTHREADED)
	if failed(hr) {
		return fmt.Errorf("CoInitializeEx(STA) failed: 0x%x", uint32(hr))
	}
	defer win32.CoUninitialize()
	com.InitializeContext()
	defer com.UninitializeContext()
	scope := com.NewScope()
	defer scope.Leave()
	defer h.closeSTA()

	instance, _ := win32.GetModuleHandle(nil)
	enableHighResolutionRendering()
	wc := win32.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win32.WNDCLASSEX{})), Style: win32.CS_HREDRAW | win32.CS_VREDRAW, LpfnWndProc: syscall.NewCallback(windowProc), HInstance: instance, HbrBackground: win32.HBRUSH(win32.COLOR_WINDOW + 1), LpszClassName: win32.StrToPwstr(windowClass)}
	wc.HCursor, _ = win32.LoadCursor(0, win32.IDC_ARROW)
	if hIcon := loadAppIcon(); hIcon != 0 {
		wc.HIcon = hIcon
		wc.HIconSm = hIcon
	}
	if atom, _ := win32.RegisterClassEx(&wc); atom == 0 {
		return errors.New("RegisterClassEx failed")
	}
	h.hwnd, _ = win32.CreateWindowEx(0, wc.LpszClassName, win32.StrToPwstr("Lunitide 月汐"), hostWindowStyle, win32.CW_USEDEFAULT, win32.CW_USEDEFAULT, 1280, 800, 0, 0, instance, nil)
	if h.hwnd == 0 {
		return errors.New("CreateWindowEx failed")
	}
	h.applyInitialDpiSize()
	setDarkTitleBar(h.hwnd, false)
	if hIcon := wc.HIcon; hIcon != 0 {
		win32.SendMessageW(h.hwnd, win32.WM_SETICON, win32.WPARAM(win32.ICON_BIG), win32.LPARAM(hIcon))
		win32.SendMessageW(h.hwnd, win32.WM_SETICON, win32.WPARAM(win32.ICON_SMALL), win32.LPARAM(hIcon))
	}
	hosts.Store(h.hwnd, h)
	win32.ShowWindow(h.hwnd, win32.SW_SHOW)
	win32.UpdateWindow(h.hwnd)
	if err := h.createWebView(); err != nil {
		return err
	}

	if h.runCtx.Done() != nil {
		go func() {
			select {
			case <-h.runCtx.Done():
				h.dispatch(func() { win32.DestroyWindow(h.hwnd) })
			case <-runDone:
			}
		}()
	}
	if events := h.gateway.Events(); events != nil {
		go func() {
			defer h.gateway.StopEventConsumer()
			for {
				select {
				case routed, ok := <-events:
					if !ok {
						return
					}
					DeliverRoutedEvent(routed, json.Marshal, func(raw []byte) bool {
						return h.dispatchAndWait(func() bool {
							h.mu.Lock()
							closed, core, current := h.closed, h.core, h.generation
							h.mu.Unlock()
							if core != nil && GenerationCurrent(routed.Generation, current, closed) {
								if result := core.PostWebMessageAsJson(string(raw)); failed(win32.HRESULT(result)) {
									h.fail(fmt.Errorf("WebView2 event delivery failed: 0x%x", uint32(result)))
									h.gateway.CancelStreams(context.Background())
									return false
								}
								return true
							}
							return false
						})
					})
				case <-h.runCtx.Done():
					return
				}
			}
		}()
	}
	var msg win32.MSG
	for {
		ret, getErr := win32.GetMessage(&msg, 0, 0, 0)
		if ret == 0 {
			break
		}
		if ret == -1 {
			return fmt.Errorf("GetMessage failed: %v", getErr)
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessage(&msg)
	}
	return h.runErr
}

func (h *Host) createWebView() error {
	resetDeniedMicrophoneCache(h.userDataFolder)
	loader, err := loadWebView2Loader()
	if err != nil {
		return err
	}
	h.loader = loader
	var runtimeVersion string
	if result := wv2.GetAvailableCoreWebView2BrowserVersionString("", &runtimeVersion); failed(result) || runtimeVersion == "" {
		// ADR-003: the installer detects/guides runtime acquisition; when the
		// desktop still starts without a runtime (silent install, runtime later
		// removed), fail closed with an explicit, actionable dialog instead of
		// a window flash on a console-less windowsgui process.
		showRuntimeMissingDialog(h.hwnd)
		return fmt.Errorf("WebView2 Evergreen Runtime is unavailable: 0x%x", uint32(result))
	}
	h.environmentHandler = wv2.NewICoreWebView2CreateCoreWebView2EnvironmentCompletedHandlerByFunc(func(code com.Error, env *wv2.ICoreWebView2Environment) com.Error {
		if failed(win32.HRESULT(code)) || env == nil {
			h.fail(fmt.Errorf("WebView2 environment creation failed: 0x%x", uint32(code)))
			return code
		}
		h.environment = env
		env.AddRef()
		h.controllerHandler = wv2.NewICoreWebView2CreateCoreWebView2ControllerCompletedHandlerByFunc(h.controllerCreated, false)
		result := env.CreateCoreWebView2Controller(h.hwnd, h.controllerHandler)
		if failed(win32.HRESULT(result)) {
			h.fail(fmt.Errorf("WebView2 controller request failed: 0x%x", uint32(result)))
		}
		return com.Error(win32.S_OK)
	}, false)
	hr := wv2.CreateCoreWebView2EnvironmentWithOptions("", h.userDataFolder, nil, h.environmentHandler)
	if failed(hr) {
		return fmt.Errorf("CreateCoreWebView2EnvironmentWithOptions failed: 0x%x", uint32(hr))
	}
	return nil
}

func loadWebView2Loader() (*syscall.DLL, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(filepath.Dir(executable), "WebView2Loader.dll")
	loader, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("WebView2Loader.dll is unavailable beside the desktop executable: %w", err)
	}
	for _, name := range []string{"CreateCoreWebView2EnvironmentWithOptions", "GetAvailableCoreWebView2BrowserVersionString"} {
		if _, err := loader.FindProc(name); err != nil {
			_ = loader.Release()
			return nil, fmt.Errorf("WebView2Loader.dll is missing required export %s: %w", name, err)
		}
	}
	return loader, nil
}

func (h *Host) controllerCreated(code com.Error, controller *wv2.ICoreWebView2Controller) com.Error {
	if failed(win32.HRESULT(code)) || controller == nil {
		h.fail(fmt.Errorf("WebView2 controller creation failed: 0x%x", uint32(code)))
		return code
	}
	h.controller = controller
	controller.AddRef()
	if result := controller.GetCoreWebView2(&h.core); failed(win32.HRESULT(result)) || h.core == nil {
		h.fail(errors.New("ICoreWebView2 unavailable"))
		return result
	}
	if err := query(h.core, &wv2.IID_ICoreWebView2_3, unsafe.Pointer(&h.core3)); err != nil {
		h.fail(fmt.Errorf("required ICoreWebView2_3 unavailable: %w", err))
		return com.Error(win32.E_NOINTERFACE)
	}
	if err := query(h.core, &wv2.IID_ICoreWebView2_4, unsafe.Pointer(&h.core4)); err != nil {
		h.fail(fmt.Errorf("required ICoreWebView2_4 unavailable: %w", err))
		return com.Error(win32.E_NOINTERFACE)
	}
	if err := hardenSettings(h.core); err != nil {
		h.fail(err)
		return com.Error(win32.E_FAIL)
	}
	if result := h.core3.SetVirtualHostNameToFolderMapping("app.lunitide.local", h.folder, wv2.COREWEBVIEW2_HOST_RESOURCE_ACCESS_KIND.COREWEBVIEW2_HOST_RESOURCE_ACCESS_KIND_DENY_CORS); failed(win32.HRESULT(result)) {
		h.fail(fmt.Errorf("virtual host mapping failed: 0x%x", uint32(result)))
		return result
	}
	if err := h.registerCoreEvents(); err != nil {
		h.fail(err)
		return com.Error(win32.E_FAIL)
	}
	h.grantTrustedMicrophone()
	h.resize()
	h.initialPending = true
	if result := h.core.Navigate(TrustedOrigin + "/index.html"); failed(win32.HRESULT(result)) {
		h.initialPending = false
		h.fail(fmt.Errorf("initial navigation request failed: 0x%x", uint32(result)))
		return result
	}
	return com.Error(win32.S_OK)
}

func hardenSettings(core *wv2.ICoreWebView2) error {
	var base *wv2.ICoreWebView2Settings
	if result := core.GetSettings(&base); failed(win32.HRESULT(result)) || base == nil {
		return fmt.Errorf("required WebView2 settings unavailable: 0x%x", uint32(result))
	}
	defer base.Release()
	var settings *wv2.ICoreWebView2Settings4
	if err := queryUnknown(&base.IUnknown, &wv2.IID_ICoreWebView2Settings4, unsafe.Pointer(&settings)); err != nil {
		return fmt.Errorf("required ICoreWebView2Settings4 unavailable: %w", err)
	}
	defer settings.Release()
	operations := []struct {
		name  string
		set   func(int32) com.Error
		value int32
	}{
		{"web messaging", settings.SetIsWebMessageEnabled, win32.TRUE},
		{"default script dialogs", settings.SetAreDefaultScriptDialogsEnabled, win32.FALSE},
		{"status bar", settings.SetIsStatusBarEnabled, win32.FALSE},
		{"DevTools", settings.SetAreDevToolsEnabled, win32.FALSE},
		{"default context menus", settings.SetAreDefaultContextMenusEnabled, win32.FALSE},
		{"host objects", settings.SetAreHostObjectsAllowed, win32.FALSE},
		{"browser accelerator keys", settings.SetAreBrowserAcceleratorKeysEnabled, win32.FALSE},
		{"password autosave", settings.SetIsPasswordAutosaveEnabled, win32.FALSE},
		{"general autofill", settings.SetIsGeneralAutofillEnabled, win32.FALSE},
	}
	for _, operation := range operations {
		if result := operation.set(operation.value); failed(win32.HRESULT(result)) {
			return fmt.Errorf("hardening WebView2 %s failed: 0x%x", operation.name, uint32(result))
		}
	}
	return nil
}

func (h *Host) registerCoreEvents() error {
	h.navStartHandler = wv2.NewICoreWebView2NavigationStartingEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NavigationStartingEventArgs) com.Error {
		uri, err := argumentString(args.GetUri)
		if err != nil || !NavigationAllowed(uri) {
			args.SetCancel(win32.TRUE)
			log.Printf("blocked WebView2 navigation: %q", uri)
			return com.Error(win32.S_OK)
		}
		h.mu.Lock()
		h.generation = h.gateway.InvalidateGeneration(h.runCtx)
		h.mu.Unlock()
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_NavigationStarting(h.navStartHandler, &h.navStartToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("NavigationStarting registration failed: 0x%x", uint32(r))
	}
	h.navDoneHandler = wv2.NewICoreWebView2NavigationCompletedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NavigationCompletedEventArgs) com.Error {
		var ok int32
		if r := args.GetIsSuccess(&ok); failed(win32.HRESULT(r)) || ok == 0 {
			var status int32
			args.GetWebErrorStatus(&status)
			if h.initialPending {
				h.initialPending = false
				h.fail(fmt.Errorf("initial WebView2 navigation failed (status=%d)", status))
			} else {
				log.Printf("WebView2 navigation failed (status=%d)", status)
			}
			return com.Error(win32.S_OK)
		}
		if h.initialPending {
			h.initialPending = false
			log.Printf("Lunitide WebView2 initial document loaded at %s", TrustedOrigin)
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_NavigationCompleted(h.navDoneHandler, &h.navDoneToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("NavigationCompleted registration failed: 0x%x", uint32(r))
	}
	h.messageHandler = wv2.NewICoreWebView2WebMessageReceivedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2WebMessageReceivedEventArgs) com.Error {
		h.receive(args, true, func(raw []byte) {
			if result := h.core.PostWebMessageAsJson(string(raw)); failed(win32.HRESULT(result)) {
				h.fail(fmt.Errorf("WebView2 response delivery failed: 0x%x", uint32(result)))
				h.gateway.CancelStreams(context.Background())
			}
		})
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_WebMessageReceived(h.messageHandler, &h.messageToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("WebMessageReceived registration failed: 0x%x", uint32(r))
	}
	h.frameHandler = wv2.NewICoreWebView2FrameCreatedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2FrameCreatedEventArgs) com.Error {
		h.frameCreated(args)
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core4.Add_FrameCreated(h.frameHandler, &h.frameToken); failed(win32.HRESULT(r)) {
		return fmt.Errorf("FrameCreated registration failed: 0x%x", uint32(r))
	}
	h.newWindowHandler = wv2.NewICoreWebView2NewWindowRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2NewWindowRequestedEventArgs) com.Error {
		args.SetHandled(win32.TRUE)
		raw, err := argumentString(args.GetUri)
		if err == nil {
			if url, normalizeErr := NormalizeBrowserURL(raw); normalizeErr == nil {
				_ = openSystemBrowser(url)
			}
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_NewWindowRequested(h.newWindowHandler, &h.newWindowToken); failed(win32.HRESULT(r)) {
		h.newWindowHandler.Release()
		h.newWindowHandler = nil
		return fmt.Errorf("NewWindowRequested registration failed: 0x%x", uint32(r))
	}
	h.permissionHandler = wv2.NewICoreWebView2PermissionRequestedEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2PermissionRequestedEventArgs) com.Error {
		uri, uriErr := argumentString(args.GetUri)
		var kind int32
		allowed := uriErr == nil && !failed(win32.HRESULT(args.GetPermissionKind(&kind))) && MicrophonePermissionAllowed(uri, kind == wv2.COREWEBVIEW2_PERMISSION_KIND.COREWEBVIEW2_PERMISSION_KIND_MICROPHONE)
		state := wv2.COREWEBVIEW2_PERMISSION_STATE.COREWEBVIEW2_PERMISSION_STATE_DENY
		if allowed {
			state = wv2.COREWEBVIEW2_PERMISSION_STATE.COREWEBVIEW2_PERMISSION_STATE_ALLOW
		}
		args.SetState(state)
		var args2 *wv2.ICoreWebView2PermissionRequestedEventArgs2
		if err := queryUnknown(&args.IUnknown, &wv2.IID_ICoreWebView2PermissionRequestedEventArgs2, unsafe.Pointer(&args2)); err == nil {
			args2.SetHandled(win32.TRUE)
			args2.Release()
		}
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core.Add_PermissionRequested(h.permissionHandler, &h.permissionToken); failed(win32.HRESULT(r)) {
		h.permissionHandler.Release()
		h.permissionHandler = nil
		return fmt.Errorf("PermissionRequested registration failed: 0x%x", uint32(r))
	}
	h.downloadHandler = wv2.NewICoreWebView2DownloadStartingEventHandlerByFunc(func(_ *wv2.ICoreWebView2, args *wv2.ICoreWebView2DownloadStartingEventArgs) com.Error {
		args.SetCancel(win32.TRUE)
		args.SetHandled(win32.TRUE)
		return com.Error(win32.S_OK)
	}, false)
	if r := h.core4.Add_DownloadStarting(h.downloadHandler, &h.downloadToken); failed(win32.HRESULT(r)) {
		h.downloadHandler.Release()
		h.downloadHandler = nil
		return fmt.Errorf("DownloadStarting registration failed: 0x%x", uint32(r))
	}
	return nil
}

func (h *Host) frameCreated(args *wv2.ICoreWebView2FrameCreatedEventArgs) {
	var base *wv2.ICoreWebView2Frame
	if r := args.GetFrame(&base); failed(win32.HRESULT(r)) || base == nil {
		h.fail(errors.New("FrameCreated did not provide a frame"))
		return
	}
	var frame2 *wv2.ICoreWebView2Frame2
	if err := queryUnknown(&base.IUnknown, &wv2.IID_ICoreWebView2Frame2, unsafe.Pointer(&frame2)); err != nil {
		base.Release()
		h.fail(fmt.Errorf("required ICoreWebView2Frame2 unavailable: %w", err))
		return
	}
	base.Release()
	reg := &frameRegistration{frame: frame2}
	reg.message = wv2.NewICoreWebView2FrameWebMessageReceivedEventHandlerByFunc(func(_ *wv2.ICoreWebView2Frame, event *wv2.ICoreWebView2WebMessageReceivedEventArgs) com.Error {
		h.receive(event, false, func([]byte) {}) // Gateway rejects child frames; no response is posted.
		return com.Error(win32.S_OK)
	}, false)
	if r := frame2.Add_WebMessageReceived(reg.message, &reg.messageTok); failed(win32.HRESULT(r)) {
		reg.message.Release()
		frame2.Release()
		h.fail(errors.New("frame WebMessageReceived registration failed"))
		return
	}
	reg.destroyed = wv2.NewICoreWebView2FrameDestroyedEventHandlerByFunc(func(_ *wv2.ICoreWebView2Frame, _ *win32.IUnknown) com.Error {
		h.removeFrame(reg)
		return com.Error(win32.S_OK)
	}, false)
	if r := frame2.Add_Destroyed(reg.destroyed, &reg.destroyedTok); failed(win32.HRESULT(r)) {
		frame2.Remove_WebMessageReceived(reg.messageTok)
		reg.message.Release()
		reg.destroyed.Release()
		frame2.Release()
		h.fail(errors.New("frame Destroyed registration failed"))
		return
	}
	h.frames = append(h.frames, reg)
}

func (h *Host) receive(args *wv2.ICoreWebView2WebMessageReceivedEventArgs, top bool, reply func([]byte)) {
	source, err1 := argumentString(args.GetSource)
	message, err2 := argumentString(args.GetWebMessageAsJson)
	if err1 != nil || err2 != nil {
		return
	}
	h.mu.Lock()
	generation := h.generation
	h.mu.Unlock()
	Dispatch(h.runCtx, h.gateway, generation, hostbridge.Message{SourceURL: source, TopFrame: top, JSON: []byte(message)}, h.dispatch, func(raw []byte) bool {
		h.mu.Lock()
		current, closed := h.generation, h.closed
		h.mu.Unlock()
		if GenerationCurrent(generation, current, closed) {
			reply(raw)
			return true
		}
		return false
	})
}

func argumentString(get func(*win32.PWSTR) com.Error) (string, error) {
	var value win32.PWSTR
	result := get(&value)
	if value != nil {
		defer win32.CoTaskMemFree(unsafe.Pointer(value))
	}
	if failed(win32.HRESULT(result)) || value == nil {
		return "", fmt.Errorf("WebView2 event argument failed: 0x%x", uint32(result))
	}
	return win32.PwstrToStr(value), nil
}

func query(source *wv2.ICoreWebView2, iid *syscall.GUID, out unsafe.Pointer) error {
	return queryUnknown(&source.IUnknown, iid, out)
}
func queryUnknown(source *win32.IUnknown, iid *syscall.GUID, out unsafe.Pointer) error {
	hr := source.QueryInterface(iid, out)
	if failed(hr) {
		return fmt.Errorf("QueryInterface failed: 0x%x", uint32(hr))
	}
	return nil
}

func (h *Host) dispatch(fn func()) bool {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return false
	}
	accepted, notify := h.uiQueue.Push(fn)
	if !accepted && h.runErr == nil {
		h.runErr = errors.New("bounded UI queue exhausted")
	}
	h.mu.Unlock()
	if !accepted {
		h.cancelRun()
		if ok, err := h.post(win32.WM_CLOSE); !ok {
			h.recordDispatchError(fmt.Errorf("PostMessage(WM_CLOSE) failed: %v", err))
		}
		return false
	}
	if notify {
		if ok, err := h.post(uiMessage); !ok {
			h.recordDispatchError(fmt.Errorf("PostMessage(uiMessage) failed: %v", err))
			h.cancelRun()
			if closeOK, closeErr := h.post(win32.WM_CLOSE); !closeOK {
				h.recordDispatchError(fmt.Errorf("PostMessage(WM_CLOSE) failed: %v", closeErr))
			}
			return false
		}
	}
	return true
}
func (h *Host) dispatchAndWait(fn func() bool) bool {
	done := make(chan bool, 1)
	if !h.dispatch(func() { done <- fn() }) {
		return false
	}
	select {
	case delivered := <-done:
		return delivered
	case <-h.runCtx.Done():
		return false
	}
}

// SetTheme updates only DWM's native title-bar appearance on the STA thread.
func (h *Host) SetTheme(dark bool) bool {
	return h.dispatchAndWait(func() bool {
		if h.hwnd == 0 {
			return false
		}
		return setDarkTitleBar(h.hwnd, dark)
	})
}

func (h *Host) post(message uint32) (bool, win32.WIN32_ERROR) {
	if h.postMessage == nil {
		return false, win32.ERROR_INVALID_FUNCTION
	}
	ok, err := h.postMessage(h.hwnd, message, 0, 0)
	return ok != 0, err
}
func (h *Host) cancelRun() {
	if h.runCancel != nil {
		h.runCancel()
	}
}
func (h *Host) recordDispatchError(err error) {
	h.mu.Lock()
	if h.runErr == nil {
		h.runErr = err
	}
	h.mu.Unlock()
}
func (h *Host) drain() {
	h.mu.Lock()
	queue := h.uiQueue.Drain()
	h.mu.Unlock()
	for _, fn := range queue {
		fn()
	}
}
func (h *Host) fail(err error) {
	if h.runErr == nil {
		h.runErr = err
	}
	log.Printf("WebView2 host failure: %v", err)
	win32.DestroyWindow(h.hwnd)
}
func (h *Host) resize() {
	if h.controller != nil {
		var rect win32.RECT
		win32.GetClientRect(h.hwnd, &rect)
		h.controller.SetBounds(wv2.TagRECT(rect))
	}
}

// applyInitialDpiSize rescales the 1280x800 logical client area to physical
// pixels for the monitor the window was created on. With DPI awareness now
// declared, CreateWindowEx sizes are physical pixels; without this rescale
// the window would open 1280x800 physical (= visually smaller on 150%).
// The suggested-rect from WM_DPICHANGED covers later monitor moves.
func (h *Host) applyInitialDpiSize() {
	dpi := win32.GetDpiForWindow(h.hwnd)
	if dpi == 0 || dpi == 96 {
		return
	}
	rect := win32.RECT{Right: win32.MulDiv(1280, int32(dpi), 96), Bottom: win32.MulDiv(800, int32(dpi), 96)}
	_, _ = win32.AdjustWindowRectExForDpi(&rect, hostWindowStyle, 0, 0, dpi)
	_, _ = win32.SetWindowPos(h.hwnd, 0, 0, 0, rect.Right-rect.Left, rect.Bottom-rect.Top, win32.SWP_NOMOVE|win32.SWP_NOZORDER|win32.SWP_NOACTIVATE)
}

func (h *Host) removeFrame(target *frameRegistration) {
	for i, reg := range h.frames {
		if reg == target {
			h.releaseFrame(reg)
			h.frames = append(h.frames[:i], h.frames[i+1:]...)
			return
		}
	}
}
func (h *Host) releaseFrame(reg *frameRegistration) {
	reg.frame.Remove_Destroyed(reg.destroyedTok)
	reg.destroyed.Release()
	reg.frame.Remove_WebMessageReceived(reg.messageTok)
	reg.message.Release()
	reg.frame.Release()
}
func (h *Host) closeSTA() {
	if h.runCancel != nil {
		h.gateway.CancelStreams(context.Background())
		h.runCancel()
	}
	h.mu.Lock()
	h.closed = true
	h.uiQueue = nil
	h.mu.Unlock()
	for i := len(h.frames) - 1; i >= 0; i-- {
		h.releaseFrame(h.frames[i])
	}
	h.frames = nil
	if h.core4 != nil && h.downloadHandler != nil {
		h.core4.Remove_DownloadStarting(h.downloadToken)
		h.downloadHandler.Release()
	}
	if h.core != nil {
		if h.permissionHandler != nil {
			h.core.Remove_PermissionRequested(h.permissionToken)
			h.permissionHandler.Release()
		}
		if h.newWindowHandler != nil {
			h.core.Remove_NewWindowRequested(h.newWindowToken)
			h.newWindowHandler.Release()
		}
	}
	if h.core4 != nil && h.frameHandler != nil {
		h.core4.Remove_FrameCreated(h.frameToken)
		h.frameHandler.Release()
	}
	if h.core != nil {
		if h.messageHandler != nil {
			h.core.Remove_WebMessageReceived(h.messageToken)
			h.messageHandler.Release()
		}
		if h.navDoneHandler != nil {
			h.core.Remove_NavigationCompleted(h.navDoneToken)
			h.navDoneHandler.Release()
		}
		if h.navStartHandler != nil {
			h.core.Remove_NavigationStarting(h.navStartToken)
			h.navStartHandler.Release()
		}
	}
	if h.core4 != nil {
		h.core4.Release()
	}
	if h.core3 != nil {
		h.core3.ClearVirtualHostNameToFolderMapping("app.lunitide.local")
		h.core3.Release()
	}
	if h.core != nil {
		h.core.Release()
	}
	if h.controller != nil {
		h.controller.Close()
		h.controller.Release()
	}
	if h.environment != nil {
		h.environment.Release()
	}
	if h.controllerHandler != nil {
		h.controllerHandler.Release()
	}
	if h.environmentHandler != nil {
		h.environmentHandler.Release()
	}
	if h.loader != nil {
		_ = h.loader.Release()
	}
	if h.hwnd != 0 {
		hosts.Delete(h.hwnd)
	}
}

func windowProc(hwnd win32.HWND, message uint32, wParam win32.WPARAM, lParam win32.LPARAM) win32.LRESULT {
	value, _ := hosts.Load(hwnd)
	h, _ := value.(*Host)
	switch message {
	case win32.WM_SIZE:
		if h != nil {
			h.resize()
		}
		return 0
	case win32.WM_DPICHANGED:
		// Per-monitor-v2: adopt the system-suggested window rect for the new
		// monitor DPI. WM_SIZE follows and refits the WebView2 controller;
		// WebView2's built-in monitor-scale detection then refreshes its
		// rasterization scale, keeping text crisp when dragged across screens
		// with different scale factors.
		if h != nil {
			// LPARAM→pointer via address-of indirection (vet-safe pattern).
			raw := *(*unsafe.Pointer)(unsafe.Pointer(&lParam))
			if suggested := (*win32.RECT)(raw); suggested != nil {
				_, _ = win32.SetWindowPos(hwnd, 0, suggested.Left, suggested.Top, suggested.Right-suggested.Left, suggested.Bottom-suggested.Top, win32.SWP_NOZORDER|win32.SWP_NOACTIVATE)
			}
		}
		return 0
	case uiMessage:
		if h != nil {
			h.drain()
		}
		return 0
	case win32.WM_DESTROY:
		hosts.Delete(hwnd)
		win32.PostQuitMessage(0)
		return 0
	default:
		return win32.DefWindowProc(hwnd, message, wParam, lParam)
	}
}

func failed(hr win32.HRESULT) bool { return int32(hr) < 0 }
