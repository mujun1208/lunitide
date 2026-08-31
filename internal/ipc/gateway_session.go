package ipc

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

const (
	GatewayNonceFile     = "gateway-session.nonce"
	GatewayEnginePIDFile = "engine.pid"
)

func SaveGatewayNonce(path string, nonce []byte) error {
	if path == "" || len(nonce) != sessionSecretSize {
		return errors.New("invalid gateway nonce")
	}
	return os.WriteFile(path, nonce, 0o600)
}

func LoadGatewayNonce(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) != sessionSecretSize {
		return nil, errors.New("invalid gateway nonce")
	}
	out := make([]byte, sessionSecretSize)
	copy(out, raw)
	return out, nil
}

func sameUserPairedPID(ownerPID, clientPID int) bool {
	return ownerPID > 0 && clientPID > 0
}

func SaveEnginePID(path string, pid int) error {
	if path == "" || pid < 1 {
		return errors.New("invalid engine pid")
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func LoadEnginePID(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid < 1 {
		return 0, errors.New("invalid engine pid")
	}
	return pid, nil
}
