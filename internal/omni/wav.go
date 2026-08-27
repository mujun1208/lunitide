package omni

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
)

// WritePCM16MonoWAV writes little-endian 16-bit mono PCM as a WAV file.
func WritePCM16MonoWAV(path string, sampleRate int, pcm []byte) error {
	if sampleRate <= 0 {
		return fmt.Errorf("omni: invalid sample rate %d", sampleRate)
	}
	if len(pcm)%2 != 0 {
		return fmt.Errorf("omni: pcm length must be even")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+len(pcm)))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(pcm)))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(header); err != nil {
		return err
	}
	_, err = f.Write(pcm)
	return err
}
