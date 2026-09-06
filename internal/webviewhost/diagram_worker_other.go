//go:build !windows

package webviewhost

import "errors"

func RunDiagramWorker(string) error {
	return errors.New("isolated diagram runtime requires Windows WebView2")
}
