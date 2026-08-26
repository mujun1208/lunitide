package ccapp

import "testing"

func TestClassifyDialogOrdinaryConfirm(t *testing.T) {
	ok, reason := ClassifyDialog("保存确认", "notepad.exe", "#32770", []string{"确定", "取消"})
	if !ok || reason != "" {
		t.Fatalf("ordinary dialog should be confirmable, got %v %q", ok, reason)
	}
	if ConfirmButtonName([]string{"确定", "取消"}, "") != "确定" {
		t.Fatalf("expected 确定, got %q", ConfirmButtonName([]string{"确定", "取消"}, ""))
	}
}

func TestClassifyDialogUAC(t *testing.T) {
	ok, reason := ClassifyDialog("用户账户控制", "consent.exe", "#32770", []string{"是", "否"})
	if ok || reason != "uac dialog" {
		t.Fatalf("UAC must be refused, got %v %q", ok, reason)
	}
	ok, reason = ClassifyDialog("User Account Control", "app.exe", "", []string{"Yes", "No"})
	if ok || reason != "uac dialog" {
		t.Fatalf("UAC title must be refused, got %v %q", ok, reason)
	}
}

func TestClassifyDialogElevation(t *testing.T) {
	ok, reason := ClassifyDialog("你要允许此应用对你的设备进行更改吗？", "installer.exe", "", []string{"是", "否"})
	if ok || reason != "elevation dialog" {
		t.Fatalf("elevation must be refused, got %v %q", ok, reason)
	}
}

func TestClassifyDialogFilePicker(t *testing.T) {
	ok, reason := ClassifyDialog("打开", "explorer.exe", "#32770", []string{"打开", "取消"})
	if ok || reason != "file open/save dialog" {
		t.Fatalf("file picker must be refused, got %v %q", ok, reason)
	}
	ok, reason = ClassifyDialog("另存为", "winword.exe", "#32770", []string{"保存", "取消"})
	if ok || reason != "file open/save dialog" {
		t.Fatalf("save-as must be refused, got %v %q", ok, reason)
	}
	// A MessageBox titled 确定? with 确定/取消 is not a file picker.
	ok, reason = ClassifyDialog("要保存更改吗？", "notepad.exe", "#32770", []string{"确定", "取消"})
	if !ok {
		t.Fatalf("message box should stay confirmable, got %v %q", ok, reason)
	}
}

func TestConfirmButtonAliases(t *testing.T) {
	buttons := []string{"&Yes", "No"}
	if got := ConfirmButtonName(buttons, "yes"); got != "&Yes" {
		t.Fatalf("alias yes -> &Yes, got %q", got)
	}
	if got := ConfirmButtonName([]string{"确定(&O)", "取消"}, "ok"); got != "确定(&O)" {
		t.Fatalf("alias ok -> 确定, got %q", got)
	}
	if ConfirmButtonName([]string{"下一步", "取消"}, "") != "" {
		t.Fatal("Next/Install must not auto-confirm")
	}
}

func TestSensitiveSurfaceReason(t *testing.T) {
	if got := SensitiveSurfaceReason("用户账户控制", "consent.exe", "#32770", []string{"是", "否"}); got != "uac dialog" {
		t.Fatalf("UAC = %q", got)
	}
	if got := SensitiveSurfaceReason("打开", "explorer.exe", "#32770", []string{"打开", "取消"}); got != "file open/save dialog" {
		t.Fatalf("picker = %q", got)
	}
	if got := SensitiveSurfaceReason("要保存更改吗？", "notepad.exe", "#32770", []string{"确定", "取消"}); got != "" {
		t.Fatalf("ordinary dialog should not be sensitive, got %q", got)
	}
	if got := SensitiveSurfaceReason("Notes", "notepad.exe", "", nil); got != "" {
		t.Fatalf("ordinary window should not be sensitive, got %q", got)
	}
}

func TestIsConfirmButton(t *testing.T) {
	for _, cap := range []string{"OK", "Yes", "确定", "确认", "是", "&OK"} {
		if !IsConfirmButton(cap) {
			t.Fatalf("%q should be confirm", cap)
		}
	}
	for _, cap := range []string{"取消", "No", "安装", "Run", ""} {
		if IsConfirmButton(cap) {
			t.Fatalf("%q must not be confirm", cap)
		}
	}
}
