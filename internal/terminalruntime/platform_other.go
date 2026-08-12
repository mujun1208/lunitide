//go:build !windows

package terminalruntime

func startPlatform(string, uint16, uint16, func([]byte), func(uint32, error)) (platformSession, error) {
	return nil, ErrUnsupported
}
