package toolruntime

import (
	"context"
	"strings"
	"testing"
)

func TestParseLocationLineUsesShanghaiForChinaStandardTime(t *testing.T) {
	fix, err := parseLocationLine("31.2304|121.4737|50|China Standard Time")
	if err != nil {
		t.Fatal(err)
	}
	if fix.Latitude != 31.2304 || fix.Longitude != 121.4737 || fix.Timezone != "Asia/Shanghai" || fix.Source != "windows" {
		t.Fatalf("%+v", fix)
	}
}

func TestParseLocationLineExplainsADeniedFix(t *testing.T) {
	_, err := parseLocationLine("LOCATION_PERMISSION")
	if err == nil || !strings.Contains(err.Error(), "隐私设置") {
		t.Fatal(err)
	}
}

func TestLocationGetReturnsAFixOrASettingsReason(t *testing.T) {
	r := newProductRuntime(t)
	out, err := r.Execute(context.Background(), FullAccess, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "location.get", []byte(`{}`), true)
	if err != nil {
		if !strings.Contains(err.Error(), "请在 Windows 隐私设置里允许桌面应用使用位置") && !strings.Contains(err.Error(), "没有读到本机位置") && !strings.Contains(err.Error(), "这台系统没有本机定位") {
			t.Fatal(err)
		}
		return
	}
	if !strings.Contains(out.Output, `"source":"windows"`) {
		t.Fatalf("location payload: %s", out.Output)
	}
	zone := out.Output
	if i := strings.Index(zone, `"timezone":"`); i >= 0 {
		zone = zone[i+len(`"timezone":"`):]
		if j := strings.IndexByte(zone, '"'); j >= 0 {
			zone = zone[:j]
		}
	}
	if zone == "" || zone == "UTC" {
		t.Fatalf("windows location did not resolve an IANA timezone: %s", out.Output)
	}
	t.Logf("timezone=%s", zone)
}
