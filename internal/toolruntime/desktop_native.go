package toolruntime

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// Native-first launch targets.
//
// Windows exposes most Settings pages and system folders through URI schemes
// (ms-settings:, shell:) that open the exact page in one process start. When
// the user asks for "蓝牙设置" or "回收站", opening that page directly is
// faster and far more reliable than launching Settings and GUI-clicking
// through the sidebar. desktop.open consults this table before the generic
// app/file resolver; a hit runs the URI through the same shell-free launcher
// media.play uses and is verified by foreground window like any other launch.

type nativeLaunchTarget struct {
	// URI is what the OS protocol handler receives.
	URI string
	// Label names the target in receipts ("蓝牙设置").
	Label string
	// Window lists title/process fragments that prove the page came up.
	Window []string
	// Aliases are folded user phrasings that resolve here.
	Aliases []string
}

var settingsWindow = []string{"设置", "Settings", "SystemSettings", "systemsettings.exe"}
var explorerWindow = []string{"explorer.exe", "explorer"}

// nativeLaunchTargets is deliberately Windows-only vocabulary; the resolver
// returns no hit on other platforms.
var nativeLaunchTargets = []nativeLaunchTarget{
	{URI: "ms-settings:", Label: "设置", Window: settingsWindow, Aliases: []string{"设置", "系统设置", "windows设置", "windows 设置", "settings", "windows settings"}},
	{URI: "ms-settings:bluetooth", Label: "蓝牙设置", Window: settingsWindow, Aliases: []string{"蓝牙", "蓝牙设置", "bluetooth", "bluetooth settings"}},
	{URI: "ms-settings:network-wifi", Label: "Wi-Fi 设置", Window: settingsWindow, Aliases: []string{"wifi", "wi-fi", "无线网络", "无线网络设置", "wifi设置", "wi-fi设置", "wlan"}},
	{URI: "ms-settings:network", Label: "网络设置", Window: settingsWindow, Aliases: []string{"网络设置", "网络和internet", "网络和 internet", "network settings"}},
	{URI: "ms-settings:display", Label: "显示设置", Window: settingsWindow, Aliases: []string{"显示设置", "屏幕设置", "分辨率", "分辩率设置", "display settings", "屏幕分辨率"}},
	{URI: "ms-settings:nightlight", Label: "夜间模式", Window: settingsWindow, Aliases: []string{"夜间模式", "护眼模式", "night light"}},
	{URI: "ms-settings:sound", Label: "声音设置", Window: settingsWindow, Aliases: []string{"声音设置", "音量设置", "音频设置", "sound settings"}},
	{URI: "ms-settings:notifications", Label: "通知设置", Window: settingsWindow, Aliases: []string{"通知设置", "notifications"}},
	{URI: "ms-settings:quiethours", Label: "专注助手", Window: settingsWindow, Aliases: []string{"专注助手", "免打扰", "勿扰模式", "focus assist"}},
	{URI: "ms-settings:powersleep", Label: "电源和睡眠", Window: settingsWindow, Aliases: []string{"电源设置", "电源和睡眠", "睡眠设置", "省电", "power settings"}},
	{URI: "ms-settings:batterysaver", Label: "电池设置", Window: settingsWindow, Aliases: []string{"电池设置", "节电模式", "battery settings"}},
	{URI: "ms-settings:storagesense", Label: "存储设置", Window: settingsWindow, Aliases: []string{"存储设置", "存储感知", "磁盘空间", "storage settings"}},
	{URI: "ms-settings:personalization-background", Label: "桌面背景", Window: settingsWindow, Aliases: []string{"桌面背景", "壁纸", "壁纸设置", "换壁纸", "背景设置", "wallpaper"}},
	{URI: "ms-settings:colors", Label: "颜色与主题", Window: settingsWindow, Aliases: []string{"主题设置", "颜色设置", "深色模式", "浅色模式", "暗色模式", "dark mode"}},
	{URI: "ms-settings:lockscreen", Label: "锁屏设置", Window: settingsWindow, Aliases: []string{"锁屏", "锁屏设置", "锁屏界面", "lock screen"}},
	{URI: "ms-settings:taskbar", Label: "任务栏设置", Window: settingsWindow, Aliases: []string{"任务栏", "任务栏设置", "taskbar"}},
	{URI: "ms-settings:appsfeatures", Label: "应用与功能", Window: settingsWindow, Aliases: []string{"应用和功能", "应用与功能", "卸载程序", "卸载软件", "已安装的应用", "程序和功能", "installed apps", "apps & features"}},
	{URI: "ms-settings:defaultapps", Label: "默认应用", Window: settingsWindow, Aliases: []string{"默认应用", "默认程序", "默认浏览器设置", "default apps"}},
	{URI: "ms-settings:startupapps", Label: "启动应用", Window: settingsWindow, Aliases: []string{"启动项", "开机启动", "启动应用", "自启动", "startup apps"}},
	{URI: "ms-settings:yourinfo", Label: "账户信息", Window: settingsWindow, Aliases: []string{"账户设置", "帐户设置", "账户信息", "account settings"}},
	{URI: "ms-settings:signinoptions", Label: "登录选项", Window: settingsWindow, Aliases: []string{"登录选项", "pin码", "指纹", "人脸识别", "sign-in options", "改密码"}},
	{URI: "ms-settings:dateandtime", Label: "日期和时间", Window: settingsWindow, Aliases: []string{"日期和时间", "时间设置", "时区", "date and time", "日期时间"}},
	{URI: "ms-settings:regionlanguage", Label: "语言与区域", Window: settingsWindow, Aliases: []string{"语言设置", "区域设置", "输入法设置", "language settings", "语言和区域"}},
	{URI: "ms-settings:mousetouchpad", Label: "鼠标设置", Window: settingsWindow, Aliases: []string{"鼠标设置", "触摸板设置", "鼠标", "mouse settings", "touchpad"}},
	{URI: "ms-settings:typing", Label: "键盘输入", Window: settingsWindow, Aliases: []string{"键盘设置", "输入设置", "typing settings"}},
	{URI: "ms-settings:printers", Label: "打印机", Window: settingsWindow, Aliases: []string{"打印机", "打印机设置", "打印机和扫描仪", "printers"}},
	{URI: "ms-settings:privacy", Label: "隐私设置", Window: settingsWindow, Aliases: []string{"隐私设置", "privacy settings"}},
	{URI: "ms-settings:privacy-microphone", Label: "麦克风权限", Window: settingsWindow, Aliases: []string{"麦克风权限", "麦克风设置", "microphone privacy"}},
	{URI: "ms-settings:privacy-webcam", Label: "摄像头权限", Window: settingsWindow, Aliases: []string{"摄像头权限", "相机权限", "摄像头设置", "camera privacy"}},
	{URI: "ms-settings:windowsupdate", Label: "Windows 更新", Window: settingsWindow, Aliases: []string{"windows更新", "windows 更新", "系统更新", "检查更新", "windows update"}},
	{URI: "ms-settings:windowsdefender", Label: "Windows 安全中心", Window: []string{"Windows 安全中心", "Windows Security", "SecHealthUI"}, Aliases: []string{"安全中心", "windows安全", "windows 安全中心", "windows defender", "防病毒", "杀毒"}},
	{URI: "ms-settings:about", Label: "系统信息", Window: settingsWindow, Aliases: []string{"关于本机", "系统信息", "电脑配置", "查看配置", "about pc", "系统版本"}},
	{URI: "ms-settings:easeofaccess-display", Label: "辅助功能", Window: settingsWindow, Aliases: []string{"辅助功能", "无障碍", "accessibility"}},
	{URI: "ms-settings:clipboard", Label: "剪贴板设置", Window: settingsWindow, Aliases: []string{"剪贴板设置", "剪贴板历史", "clipboard settings"}},
	{URI: "ms-settings:recovery", Label: "恢复", Window: settingsWindow, Aliases: []string{"恢复选项", "重置此电脑", "重置电脑", "recovery"}},
	{URI: "ms-settings:optionalfeatures", Label: "可选功能", Window: settingsWindow, Aliases: []string{"可选功能", "optional features"}},
	{URI: "ms-settings:developers", Label: "开发者选项", Window: settingsWindow, Aliases: []string{"开发者选项", "开发人员模式", "developer mode"}},
	// Shell folders: exact folder, one process start, no navigation clicks.
	{URI: "shell:RecycleBinFolder", Label: "回收站", Window: append([]string{"回收站", "Recycle Bin"}, explorerWindow...), Aliases: []string{"回收站", "recycle bin"}},
	{URI: "shell:Downloads", Label: "下载文件夹", Window: append([]string{"下载", "Downloads"}, explorerWindow...), Aliases: []string{"下载文件夹", "下载目录", "downloads", "downloads folder"}},
	{URI: "shell:Personal", Label: "文档文件夹", Window: append([]string{"文档", "Documents"}, explorerWindow...), Aliases: []string{"文档文件夹", "我的文档", "documents", "documents folder"}},
	{URI: "shell:My Pictures", Label: "图片文件夹", Window: append([]string{"图片", "Pictures"}, explorerWindow...), Aliases: []string{"图片文件夹", "我的图片", "pictures folder"}},
	{URI: "shell:My Video", Label: "视频文件夹", Window: append([]string{"视频", "Videos"}, explorerWindow...), Aliases: []string{"视频文件夹", "我的视频", "videos folder"}},
	{URI: "shell:My Music", Label: "音乐文件夹", Window: append([]string{"音乐", "Music"}, explorerWindow...), Aliases: []string{"音乐文件夹", "我的音乐", "music folder"}},
	{URI: "shell:Desktop", Label: "桌面文件夹", Window: append([]string{"桌面", "Desktop"}, explorerWindow...), Aliases: []string{"桌面文件夹", "桌面目录", "desktop folder"}},
	{URI: "shell:MyComputerFolder", Label: "此电脑", Window: append([]string{"此电脑", "This PC"}, explorerWindow...), Aliases: []string{"此电脑", "我的电脑", "this pc", "my computer"}},
	{URI: "shell:ControlPanelFolder", Label: "控制面板", Window: append([]string{"控制面板", "Control Panel"}, explorerWindow...), Aliases: []string{"控制面板", "control panel"}},
	{URI: "shell:NetworkPlacesFolder", Label: "网络", Window: append([]string{"网络", "Network"}, explorerWindow...), Aliases: []string{"网络邻居", "网上邻居", "network folder"}},
	{URI: "shell:Startup", Label: "启动文件夹", Window: append([]string{"启动", "Startup"}, explorerWindow...), Aliases: []string{"启动文件夹", "startup folder"}},
	{URI: "shell:AppsFolder", Label: "所有应用", Window: append([]string{"应用", "Applications"}, explorerWindow...), Aliases: []string{"所有应用", "应用列表", "all apps"}},
}

var nativeLaunchIndex = func() map[string]*nativeLaunchTarget {
	idx := make(map[string]*nativeLaunchTarget, len(nativeLaunchTargets)*4)
	for i := range nativeLaunchTargets {
		t := &nativeLaunchTargets[i]
		for _, a := range t.Aliases {
			idx[foldNativeQuery(a)] = t
		}
	}
	return idx
}()

// IsNativePageName reports whether name is an exact Settings/shell alias
// (after folding). Used by routing so "换壁纸" is a page to open, not chat.
// Independent of GOOS: the table is the product vocabulary; launching is
// what stays Windows-only.
func IsNativePageName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	_, ok := nativeLaunchIndex[foldNativeQuery(name)]
	return ok
}

func foldNativeQuery(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "", "　", "", "-", "", "_", "", "·", "", "，", "", ",", "", "。", "", ".", "", "的", "").Replace(s)
	return s
}

// nativeLaunchPrefixes are wrappers users put around a settings page name
// ("打开 蓝牙设置 页面", "进入设置里的蓝牙"). Stripped before the alias lookup.
var nativeLaunchPrefixes = []string{"打开", "进入", "去", "到", "看看", "查看", "帮我打开", "帮我进入", "请打开", "open", "go to", "show"}
var nativeLaunchSuffixes = []string{"页面", "界面", "面板", "选项", "设置页", "那一页", "那页", "一下", "看看", "page", "panel"}
var nativeLaunchInfixes = []string{"设置里的", "设置中的", "设置的", "系统设置里的", "系统里的", "windows的", "windows里的"}

// resolveNativeLaunch maps a desktop.open query to a native URI page. It is
// exact-alias only after folding: a fuzzy match could turn "打开设置.txt"
// into ms-settings:, so anything that looks like a file path or has an
// extension never hits.
func resolveNativeLaunch(query string) (nativeLaunchTarget, bool) {
	if runtime.GOOS != "windows" {
		return nativeLaunchTarget{}, false
	}
	q := strings.TrimSpace(query)
	if q == "" || strings.ContainsAny(q, `\/:`) {
		return nativeLaunchTarget{}, false
	}
	lower := strings.ToLower(q)
	for _, ext := range []string{".txt", ".docx", ".xlsx", ".pptx", ".pdf", ".png", ".jpg", ".exe", ".lnk", ".md", ".csv", ".zip"} {
		if strings.HasSuffix(lower, ext) {
			return nativeLaunchTarget{}, false
		}
	}
	cands := []string{q}
	stripped := q
	for _, p := range nativeLaunchPrefixes {
		if strings.HasPrefix(strings.ToLower(stripped), p) {
			stripped = strings.TrimSpace(stripped[len(p):])
			break
		}
	}
	for _, in := range nativeLaunchInfixes {
		if i := strings.Index(strings.ToLower(stripped), in); i >= 0 {
			stripped = strings.TrimSpace(stripped[i+len(in):])
			break
		}
	}
	for _, s := range nativeLaunchSuffixes {
		if strings.HasSuffix(strings.ToLower(stripped), s) {
			stripped = strings.TrimSpace(stripped[:len(stripped)-len(s)])
			break
		}
	}
	if stripped != q {
		cands = append(cands, stripped)
	}
	for _, c := range cands {
		if t, ok := nativeLaunchIndex[foldNativeQuery(c)]; ok {
			return *t, true
		}
	}
	return nativeLaunchTarget{}, false
}

// nativeLaunchArgv keeps the launch shell-free, same as media.play: the URI
// is one argv element handed to the protocol handler, never re-parsed by cmd.
func nativeLaunchArgv(uri string) []string {
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(strings.ToLower(uri), "shell:") {
			return []string{"explorer.exe", uri}
		}
		return []string{"rundll32.exe", "url.dll,FileProtocolHandler", uri}
	}
	return []string{"xdg-open", uri}
}

var startNativeLaunch = func(uri string) error {
	argv := nativeLaunchArgv(uri)
	return exec.Command(argv[0], argv[1:]...).Start()
}

func openNativeLaunch(t nativeLaunchTarget) error {
	if strings.TrimSpace(t.URI) == "" {
		return errors.New("empty native target")
	}
	return startNativeLaunch(t.URI)
}

// confirmNativeOpened waits for the target page's window like
// confirmDesktopOpened does for apps, but keyed on the page's own window
// fragments (Settings host, Explorer title) instead of the user's phrasing.
func confirmNativeOpened(t nativeLaunchTarget) (desktopOpenProof, error) {
	queries := append([]string{}, t.Window...)
	if len(queries) == 0 {
		queries = []string{t.Label}
	}
	sawProcess := false
	for i := 0; i < openVerifyTries; i++ {
		for _, q := range queries {
			_ = activateWindowFn(q)
		}
		fgTitle, fgProcess, _ := readForegroundFn()
		if openedWindowConfirmed(fgTitle, fgProcess, queries) {
			return desktopOpenProof{Kind: "foreground"}, nil
		}
		if openedVisibleOrProcess(t.Label, queries) {
			sawProcess = true
		}
		if i+1 < openVerifyTries {
			openVerifySleep()
		}
	}
	if sawProcess {
		return desktopOpenProof{Kind: "process"}, nil
	}
	return desktopOpenProof{}, errors.New("无法执行：启动了但未确认目标窗口")
}
