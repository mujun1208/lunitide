//go:build windows

package ipc

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func ListenCurrentUser(pipeName string) (net.Listener, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current user SID: %w", err)
	}
	descriptor := "D:P(A;;GA;;;" + user.User.Sid.String() + ")"
	return winio.ListenPipe(pipeName, &winio.PipeConfig{SecurityDescriptor: descriptor, MessageMode: false, InputBufferSize: 65536, OutputBufferSize: 65536})
}

func Dial(ctx context.Context, pipeName string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, pipeName)
}

type handleConn interface{ Fd() uintptr }

func ClientProcessID(conn net.Conn) (uint32, error) {
	handle, ok := conn.(handleConn)
	if !ok {
		return 0, errors.New("named pipe handle unavailable")
	}
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(handle.Fd()), &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

// VerifyClientProcess performs the kernel-backed PID check used immediately
// after Accept. ServeSession repeats this check to defend the full boundary.
func VerifyClientProcess(conn net.Conn, expectedPID int) error {
	if expectedPID < 1 {
		return errors.New("invalid expected client PID")
	}
	pid, err := ClientProcessID(conn)
	if err != nil {
		return fmt.Errorf("read named pipe client PID: %w", err)
	}
	if pid != uint32(expectedPID) {
		return errors.New("named pipe client PID mismatch")
	}
	return nil
}

// AdmitClient verifies the accepted pipe peer before consuming a session slot.
func AdmitClient(conn net.Conn, expectedPID int, gate *SessionGate) (func(), bool) {
	if VerifyClientProcess(conn, expectedPID) != nil {
		return nil, false
	}
	return gate.TryEnter()
}

func ServerProcessID(conn net.Conn) (uint32, error) {
	handle, ok := conn.(handleConn)
	if !ok {
		return 0, errors.New("named pipe handle unavailable")
	}
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(windows.Handle(handle.Fd()), &pid); err != nil {
		return 0, err
	}
	return pid, nil
}
