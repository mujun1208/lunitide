package app

import "testing"

func TestShouldWidenAndRetryTable(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   widenInput
		want bool
	}{
		{name: "问候", in: widenInput{Goal: "你好", AssistantText: "你好呀"}, want: false},
		{name: "天气", in: widenInput{Goal: "今天天气怎么样", TaskRoute: RouteUnspecified, AssistantText: "今天多云，大约二十度。"}, want: false},
		{name: "无动词办公", in: widenInput{Goal: "把桌面上的报告弄好", AssistantText: "我没有这个工具"}, want: true},
		{name: "催促", in: widenInput{Goal: "你没做", PrevWasToolGoal: true, PrevSuccessfulTools: 0}, want: true},
	} {
		if got := shouldWidenAndRetry(tc.in); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestShouldWidenAndRetryOnce(t *testing.T) {
	in := widenInput{Goal: "把桌面上的报告弄好", TaskRoute: RouteR1, AssistantText: "我没有这个工具", AlreadyWidened: true}
	if shouldWidenAndRetry(in) {
		t.Fatal("already widened must not retry")
	}
	in.AlreadyWidened = false
	in.SuccessfulTools = 1
	if shouldWidenAndRetry(in) {
		t.Fatal("successful tools must not widen")
	}
}

func TestLooksLikeShortNudgeDoesNotTreatWeather(t *testing.T) {
	if looksLikeShortNudge("今天天气怎么样") || looksLikeToolRefusal("今天多云") {
		t.Fatal("weather answer must not look like a miss-call")
	}
}
