package m8core_test

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

func doc(content, sensitivity string) m8core.PayloadDoc {
	return m8core.PayloadDoc{Content: content, ScopeID: "local", Sensitivity: sensitivity}
}

func TestClassifyMemoryRisk(t *testing.T) {
	cases := []struct {
		name string
		doc  m8core.PayloadDoc
		want string
	}{
		{"short-private", doc("喜欢深色主题", m8core.SensPrivate), m8core.RiskLow},
		{"short-public", doc("prefers metric units", m8core.SensPublic), m8core.RiskLow},
		{"empty", doc("   ", m8core.SensPrivate), m8core.RiskHigh},
		{"sensitive-level", doc("prefers dark theme", m8core.SensSensitive), m8core.RiskHigh},
		{"unknown-sensitivity", doc("ok", "top-secret"), m8core.RiskHigh},
		{"password-marker", doc("我的密码是 hunter2", m8core.SensPrivate), m8core.RiskHigh},
		{"english-secret", doc("api key = sk-123", m8core.SensPrivate), m8core.RiskHigh},
		{"id-marker", doc("身份证 110101...", m8core.SensPrivate), m8core.RiskHigh},
		{"too-long", doc(strings.Repeat("字", 281), m8core.SensPrivate), m8core.RiskHigh},
		{"boundary-length", doc(strings.Repeat("字", 280), m8core.SensPrivate), m8core.RiskLow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m8core.ClassifyMemoryRisk(tc.doc); got != tc.want {
				t.Fatalf("ClassifyMemoryRisk(%q/%s) = %s, want %s", tc.doc.Content, tc.doc.Sensitivity, got, tc.want)
			}
			if m8core.MemoryRiskAutoAcceptable(tc.doc) != (tc.want == m8core.RiskLow) {
				t.Fatalf("MemoryRiskAutoAcceptable mismatch for %s", tc.name)
			}
		})
	}
}
