package llmadapter

import "testing"

func TestUnavailableModelChannelGetsSafeSpecificDiagnosis(t *testing.T) {
	for _, reason := range []string{"No available channel for model hidden under group private (request id: secret)", "分组 private 下模型 gpt-image-2-all 无可用渠道（distributor）", "模型无可用通道"} {
		err := statusErrorReason(503, reason)
		if err.Code != "MODEL_CHANNEL_UNAVAILABLE" || err.Message != "configured model has no available provider channel" || err.HTTPStatus != 503 {
			t.Fatalf("unsafe or incorrect classification: %+v", err)
		}
	}
	if statusErrorReason(503, "temporary unavailable").Code != "HTTP_503" {
		t.Fatal("ordinary outage reclassified")
	}
}
