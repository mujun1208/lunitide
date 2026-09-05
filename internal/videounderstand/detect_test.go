package videounderstand

import "testing"

func TestDetectShareURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id       string
		goal     string
		platform Platform
		wantOK   bool
	}{
		{id: "bilibili-long", goal: "https://www.bilibili.com/video/BV1xx411c7mD", platform: PlatformBilibili, wantOK: true},
		{id: "b23-bare", goal: "看看 b23.tv/abcd1234", platform: PlatformBilibili, wantOK: true},
		{id: "douyin-short", goal: "https://v.douyin.com/ieFxxxx/", platform: PlatformDouyin, wantOK: true},
		{id: "youtu-bare", goal: "youtu.be/dQw4w9WgXcQ", platform: PlatformYouTube, wantOK: true},
		{id: "tencent", goal: "总结 https://v.qq.com/x/cover/mzc00200abc/n0044xyz.html", platform: PlatformTencent, wantOK: true},
		{id: "example", goal: "https://example.com/watch?v=1", wantOK: false},
		{id: "news-qq", goal: "https://news.qq.com/rain/a/20260101A00", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			_, plat, ok := DetectShareURL(tc.goal)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want %v", ok, tc.wantOK)
			}
			if tc.wantOK && plat != tc.platform {
				t.Fatalf("platform=%q want %q", plat, tc.platform)
			}
		})
	}
}
