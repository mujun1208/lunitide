package mroapp

import (
	"strings"
	"testing"
)

func TestFilterAlternatesTripleGate(t *testing.T) {
	alts := []Alternate{
		{PNFrom: "A", PNTo: "B", CertOK: true, Effectivity: "A320", Qty: 2},
		{PNFrom: "A", PNTo: "C", CertOK: false, Effectivity: "A320", Qty: 5},
		{PNFrom: "A", PNTo: "D", CertOK: true, Effectivity: "B737", Qty: 5},
		{PNFrom: "A", PNTo: "E", CertOK: true, Effectivity: "A320", Qty: 0},
	}
	got := FilterAlternates(alts, "A320")
	if !got[0].Accepted {
		t.Fatalf("first should pass: %+v", got[0])
	}
	if got[1].Accepted || got[1].Reason != "认证无效" {
		t.Fatalf("cert: %+v", got[1])
	}
	if got[2].Accepted || got[2].Reason != "构型不适用" {
		t.Fatalf("config: %+v", got[2])
	}
	if got[3].Accepted || !strings.Contains(got[3].Reason, "询价") {
		t.Fatalf("qty: %+v", got[3])
	}
}

func TestParseAOGPaste(t *testing.T) {
	got := ParseAOGPaste("机尾: B-0001\n件号: NAS1149\n数量: 2\nAOG now")
	if got.TailNo != "B-0001" || got.PN != "NAS1149" || got.Qty != "2" {
		t.Fatalf("aog = %+v", got)
	}
	free := ParseAOGPaste("B-1234 AOG 需要 PN 3G2000-1 数量2")
	if free.TailNo != "B-1234" || free.PN != "3G2000-1" || free.Qty != "2" {
		t.Fatalf("free-text aog = %+v", free)
	}
}
