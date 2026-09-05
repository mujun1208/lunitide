package volcsauc

import "testing"

func TestDefaultEndWindowMSMatchesSherpa(t *testing.T) {
	if DefaultEndWindowMS != 1200 {
		t.Fatalf("DefaultEndWindowMS=%d, want 1200", DefaultEndWindowMS)
	}
	cfg := Config{}
	req, _ := fullClientRequest(cfg)["request"].(map[string]any)
	n, ok := req["end_window_size"].(int)
	if !ok || n != 1200 {
		t.Fatalf("end_window_size=%T %v", req["end_window_size"], req["end_window_size"])
	}
}
