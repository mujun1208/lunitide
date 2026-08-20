//go:build windows

package webviewhost

import "testing"

func TestDispositionForClose(t *testing.T) {
	tests := []struct {
		name      string
		host      *Host
		want      closeDisposition
	}{
		{name: "nil host destroys", host: nil, want: closeDestroy},
		{name: "tray hides", host: &Host{trayAdded: true}, want: closeHide},
		{name: "no tray destroys", host: &Host{}, want: closeDestroy},
		{name: "force quit destroys even with tray", host: &Host{trayAdded: true, forceQuit: true}, want: closeDestroy},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.host.dispositionForClose(); got != tc.want {
				t.Fatalf("dispositionForClose()=%v want %v", got, tc.want)
			}
		})
	}
}
