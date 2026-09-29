package webviewhost

import "testing"

func TestParseFileResolveMessageAcceptsOnlyTheRendererResolveRequest(t *testing.T) {
	token, ok := ParseFileResolveMessage(`{"kind":"lunitide.files.resolve","token":"4f0c2a7e-9b1d-4c55-8a2e-1d3f5b7c9e01"}`)
	if !ok || token != "4f0c2a7e-9b1d-4c55-8a2e-1d3f5b7c9e01" {
		t.Fatalf("resolve request rejected: token=%q ok=%v", token, ok)
	}
	for _, raw := range []string{
		`{"v":"1.0","kind":"request","id":"01ARZ3NDEKTSV4RRFFQ69G5FAV","method":"attachment.importLocal"}`,
		`{"kind":"lunitide.files.resolve"}`,
		`{"kind":"lunitide.files.resolve","token":""}`,
		`{"kind":"lunitide.files.resolve","token":"` + string(make([]byte, 129)) + `"}`,
		`{"kind":"lunitide.files.resolve","token":"bad\"quote<script>"}`,
		`not json`,
	} {
		if _, ok := ParseFileResolveMessage(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
}
