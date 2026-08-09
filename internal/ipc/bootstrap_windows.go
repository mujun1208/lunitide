//go:build windows

package ipc

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxBootstrapPipeName = 512

// NewSessionBootstrap creates an anonymous pipe and a fresh secret. The parent
// deliberately passes the read end as the child's standard input while Go's
// controlled handle inheritance avoids accidentally leaking unrelated handles.
// This is not a defense against an attacker able to read same-user process memory.
func NewSessionBootstrap() (reader, writer *os.File, secret []byte, err error) {
	reader, writer, err = os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	secret = make([]byte, sessionSecretSize)
	if _, err = rand.Read(secret); err != nil {
		reader.Close()
		writer.Close()
		return nil, nil, nil, err
	}
	return reader, writer, secret, nil
}

func ReadSessionBootstrap(reader *os.File) ([]byte, error) {
	secret := make([]byte, sessionSecretSize)
	_, err := io.ReadFull(reader, secret)
	closeErr := reader.Close()
	if err != nil {
		zero(secret)
		return nil, fmt.Errorf("read Engine bootstrap secret: %w", err)
	}
	if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		zero(secret)
		return nil, fmt.Errorf("close Engine bootstrap handle: %w", closeErr)
	}
	return secret, nil
}

// WriteLaunchBootstrap transfers both authentication seed and the random
// broker address through the inherited anonymous pipe, never process arguments.
func WriteLaunchBootstrap(writer io.Writer, secret []byte, brokerPipe string) error {
	if len(secret) != sessionSecretSize || len(brokerPipe) == 0 || len(brokerPipe) > maxBootstrapPipeName {
		return errors.New("invalid launch bootstrap")
	}
	if err := writeFull(writer, secret); err != nil {
		return err
	}
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(brokerPipe)))
	if err := writeFull(writer, size[:]); err != nil {
		return err
	}
	return writeFull(writer, []byte(brokerPipe))
}

func ReadLaunchBootstrap(reader *os.File) ([]byte, string, error) {
	secret := make([]byte, sessionSecretSize)
	if _, err := io.ReadFull(reader, secret); err != nil {
		zero(secret)
		return nil, "", errors.New("invalid launch bootstrap")
	}
	var size [2]byte
	if _, err := io.ReadFull(reader, size[:]); err != nil {
		zero(secret)
		return nil, "", errors.New("invalid launch bootstrap")
	}
	n := binary.BigEndian.Uint16(size[:])
	if n == 0 || n > maxBootstrapPipeName {
		zero(secret)
		return nil, "", errors.New("invalid launch bootstrap")
	}
	name := make([]byte, n)
	if _, err := io.ReadFull(reader, name); err != nil {
		zero(secret)
		zero(name)
		return nil, "", errors.New("invalid launch bootstrap")
	}
	_ = reader.Close()
	pipe := string(name)
	zero(name)
	return secret, pipe, nil
}
