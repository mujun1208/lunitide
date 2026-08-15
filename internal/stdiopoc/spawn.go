package stdiopoc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SpawnSpec is the POC spawn contract: a real child process launched with
// an explicit minimal environment (never the parent's), pinned inside the
// sandbox working directory, and bounded by Job Object quotas.
type SpawnSpec struct {
	Exe  string
	Args []string
	// Dir is the child working directory (the sandbox root).
	Dir string
	// Env is the explicit minimal environment. Nothing else is inherited:
	// this is the OS-level secret-isolation boundary under test.
	Env []string
	// MaxProcs is the Job Object active-process limit for the whole tree
	// (proctree assumption). 0 means platform default.
	MaxProcs uint32
	// MemoryCapBytes is the Job Object commit cap for the whole tree
	// (resource assumption). 0 means platform default.
	MemoryCapBytes uint64
	// Timeout bounds the whole run; on expiry the tree is terminated.
	Timeout time.Duration
}

// ErrUnsupportedPlatform is returned by the spawner on non-Windows builds:
// the POC spawn engine is Windows-first (Job Object + explicit env block).
var ErrUnsupportedPlatform = errors.New("stdiopoc: spawn engine requires windows (Job Object + explicit environment)")

// Validate checks the spec before any process is created.
func (s *SpawnSpec) Validate() error {
	if strings.TrimSpace(s.Exe) == "" {
		return errors.New("stdiopoc: spec.Exe is required")
	}
	if strings.TrimSpace(s.Dir) == "" {
		return errors.New("stdiopoc: spec.Dir is required")
	}
	for _, kv := range s.Env {
		if kv == "" || !strings.Contains(kv, "=") {
			return fmt.Errorf("stdiopoc: spec.Env entry is not KEY=VALUE: %q", kv)
		}
	}
	if s.Timeout <= 0 {
		return errors.New("stdiopoc: spec.Timeout is required")
	}
	if s.MaxProcs == 0 || s.MemoryCapBytes == 0 {
		return errors.New("stdiopoc: quotas (MaxProcs, MemoryCapBytes) are required in the POC")
	}
	return nil
}

// Proc is a spawned probe process tree: framed stdio channels plus
// lifecycle control. Kill terminates the entire tree.
type Proc struct {
	// Stdin writes framed bytes to the child's stdin.
	Stdin *FrameWriter
	// Stdout reads framed bytes from the child's stdout.
	Stdout *FrameReader
	// Pid is the root child process id.
	Pid int

	kill func() error
	wait func(ctx context.Context) (int, error)
}

// Kill terminates the whole process tree (Job Object on Windows).
func (p *Proc) Kill() error {
	if p == nil || p.kill == nil {
		return nil
	}
	return p.kill()
}

// Wait blocks until the tree exits and returns the root exit code.
func (p *Proc) Wait(ctx context.Context) (int, error) {
	if p == nil || p.wait == nil {
		return -1, errors.New("stdiopoc: proc not running")
	}
	return p.wait(ctx)
}

// FrameWriter writes length-prefixed frames to an io.Writer.
type FrameWriter struct {
	w interface{ Write([]byte) (int, error) }
}

// NewFrameWriter wraps w.
func NewFrameWriter(w interface{ Write([]byte) (int, error) }) *FrameWriter {
	return &FrameWriter{w: w}
}

// Write sends one frame.
func (fw *FrameWriter) Write(payload []byte) error { return WriteFrame(fw.w, payload) }

// Close closes the underlying writer when it is a closer.
func (fw *FrameWriter) Close() error {
	if c, ok := fw.w.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

// FrameReader reads length-prefixed frames from an io.Reader.
type FrameReader struct {
	r interface{ Read([]byte) (int, error) }
}

// NewFrameReader wraps r.
func NewFrameReader(r interface{ Read([]byte) (int, error) }) *FrameReader {
	return &FrameReader{r: r}
}

// Read receives one frame.
func (fr *FrameReader) Read() ([]byte, error) { return ReadFrame(fr.r) }

// Close closes the underlying reader when it is a closer.
func (fr *FrameReader) Close() error {
	if c, ok := fr.r.(interface{ Close() error }); ok {
		return c.Close()
	}
	return nil
}

// MinimalEnv builds the explicit environment the probe child gets: exactly
// the entries in names that exist in parent, plus overrides. It is sorted
// for deterministic evidence digests.
func MinimalEnv(parent []string, names []string, overrides map[string]string) []string {
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToUpper(n)] = true
	}
	env := make([]string, 0, len(names)+len(overrides))
	for _, kv := range parent {
		key, _, ok := strings.Cut(kv, "=")
		if ok && want[strings.ToUpper(key)] {
			env = append(env, kv)
		}
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	return env
}
