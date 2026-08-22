package toolruntime

import "testing"

func TestBuildMediaSearchURLTargets(t *testing.T) {
	cases := []struct {
		target string
		query  string
		want   string
	}{
		{"netease", "晴天", "https://music.163.com/#/search/m/?s="},
		{"qqmusic", "晴天", "https://y.qq.com/n/ryqq/search?w="},
		{"browser", "hello", "https://music.youtube.com/search?q="},
	}
	for _, tc := range cases {
		got, err := buildMediaSearchURL(tc.target, tc.query)
		if err != nil {
			t.Fatalf("%s: %v", tc.target, err)
		}
		if !containsPrefix(got, tc.want) {
			t.Fatalf("target=%s got=%q want prefix %q", tc.target, got, tc.want)
		}
	}
}

func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
