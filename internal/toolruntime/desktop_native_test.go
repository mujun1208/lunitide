package toolruntime

import (
	"runtime"
	"strings"
	"testing"
)

func TestResolveNativeLaunchAliases(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ms-settings URIs are Windows-only")
	}
	cases := map[string]string{
		"蓝牙":            "ms-settings:bluetooth",
		"蓝牙设置":          "ms-settings:bluetooth",
		"打开蓝牙设置":        "ms-settings:bluetooth",
		"打开 蓝牙设置 页面":    "ms-settings:bluetooth",
		"进入设置里的蓝牙":      "ms-settings:bluetooth",
		"Bluetooth":     "ms-settings:bluetooth",
		"WiFi":          "ms-settings:network-wifi",
		"wi-fi 设置":      "ms-settings:network-wifi",
		"壁纸":            "ms-settings:personalization-background",
		"换壁纸":           "ms-settings:personalization-background",
		"深色模式":          "ms-settings:colors",
		"Windows 更新":    "ms-settings:windowsupdate",
		"检查更新":          "ms-settings:windowsupdate",
		"回收站":           "shell:RecycleBinFolder",
		"下载文件夹":         "shell:Downloads",
		"控制面板":          "shell:ControlPanelFolder",
		"此电脑":           "shell:MyComputerFolder",
		"卸载程序":          "ms-settings:appsfeatures",
		"默认浏览器设置":       "ms-settings:defaultapps",
		"Control Panel": "shell:ControlPanelFolder",
	}
	for q, want := range cases {
		got, ok := resolveNativeLaunch(q)
		if !ok {
			t.Fatalf("%q: no native hit", q)
		}
		if got.URI != want {
			t.Fatalf("%q: got %s want %s", q, got.URI, want)
		}
		if got.Label == "" || len(got.Window) == 0 {
			t.Fatalf("%q: target needs label and window hints: %+v", q, got)
		}
	}
}

func TestResolveNativeLaunchNeverHijacksFilesOrApps(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("ms-settings URIs are Windows-only")
	}
	for _, q := range []string{
		"设置.txt", "蓝牙报告.docx", "C:\\Users\\me\\设置", "D:/回收站备份", "http://example.com",
		"微信", "网易云音乐", "计算器", "记事本", "下载", "网络", "声音", "通知", "隐私", "电源", "主题",
		"蓝牙耳机使用说明", "", "   ",
	} {
		if got, ok := resolveNativeLaunch(q); ok {
			t.Fatalf("%q must fall through to the app/file resolver, hit %s", q, got.URI)
		}
	}
}

func TestNativeLaunchArgvIsShellFree(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows launcher")
	}
	argv := nativeLaunchArgv("ms-settings:bluetooth&calc")
	if argv[0] != "rundll32.exe" || argv[len(argv)-1] != "ms-settings:bluetooth&calc" {
		t.Fatalf("settings URI must be one argv element to the protocol handler: %v", argv)
	}
	argv = nativeLaunchArgv("shell:Downloads")
	if argv[0] != "explorer.exe" || argv[1] != "shell:Downloads" {
		t.Fatalf("shell folders open through explorer: %v", argv)
	}
	for _, a := range argv {
		if strings.Contains(a, "cmd") || strings.Contains(a, "/c") {
			t.Fatalf("no cmd /c re-parsing: %v", argv)
		}
	}
}

func TestConfirmNativeOpenedUsesPageWindowNotPhrase(t *testing.T) {
	origRead, origAct, origSleep, origTries := readForegroundFn, activateWindowFn, openVerifySleep, openVerifyTries
	defer func() {
		readForegroundFn, activateWindowFn, openVerifySleep, openVerifyTries = origRead, origAct, origSleep, origTries
	}()
	openVerifySleep = func() {}
	openVerifyTries = 2
	activateWindowFn = func(string) error { return nil }
	target := nativeLaunchTarget{URI: "ms-settings:bluetooth", Label: "蓝牙设置", Window: settingsWindow}

	origList, origProc := listWindowsFn, lookupProcessImagesFn
	defer func() {
		listWindowsFn, lookupProcessImagesFn = origList, origProc
	}()
	listWindowsFn = func() []windowHint { return nil }
	lookupProcessImagesFn = func([]string) []string { return nil }
	readForegroundFn = func() (string, string, error) {
		return "设置", `C:\Windows\ImmersiveControlPanel\SystemSettings.exe`, nil
	}
	if proof, err := confirmNativeOpened(target); err != nil || proof.Kind != "foreground" {
		t.Fatalf("Settings host in front must confirm: %+v %v", proof, err)
	}
	readForegroundFn = func() (string, string, error) { return "Lunitide", `lunitide.exe`, nil }
	if _, err := confirmNativeOpened(target); err == nil {
		t.Fatal("companion in front must not count as the page opening")
	}
	readForegroundFn = func() (string, string, error) { return "微信", `WeChat.exe`, nil }
	if _, err := confirmNativeOpened(target); err == nil {
		t.Fatal("unrelated foreground must fail the launch receipt")
	}
}

func TestKnownLaunchAppNamesIncludeNativePages(t *testing.T) {
	names := KnownLaunchAppNames()
	for _, want := range []string{"蓝牙", "壁纸", "锁屏", "蓝牙设置", "回收站", "控制面板"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("routing vocabulary must know native page %q", want)
		}
	}
	joined := strings.ToLower(strings.Join(names, "|"))
	if !strings.Contains(joined, "windows 更新") && !strings.Contains(joined, "windows更新") {
		t.Fatal("routing vocabulary must know Windows Update")
	}
	if !IsNativePageName("换壁纸") || !IsNativePageName("蓝牙") {
		t.Fatal("short native aliases must resolve as page names for routing")
	}
	if IsNativePageName("微信") || IsNativePageName("网络") || IsNativePageName("") {
		t.Fatal("apps and ambiguous short words are not native pages")
	}
}
