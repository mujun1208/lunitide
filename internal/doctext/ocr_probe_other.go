//go:build !windows

package doctext

import "context"

func probeWindowsOCRPlatform(context.Context) WindowsOCRProbe {
	return WindowsOCRProbe{State: WindowsOCRUnsupportedOS, ErrorCode: "OCR_UNSUPPORTED_OS"}
}

func repairWindowsOCRPlatform(context.Context) {}
