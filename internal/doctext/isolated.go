package doctext

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

const workerResultCap = 14 << 20

type workerConfig struct{ executable, root string }

var parserConfig atomic.Pointer[workerConfig]
var parserSlots = make(chan struct{}, 2)

// ConfigureWorker is called once by the engine composition root. The worker
// mode runs before credentials, database connections, or the public pipe open.
func ConfigureWorker(executable, privateRoot string) error {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(privateRoot) {
		return errors.New("document parser requires absolute executable and private directory")
	}
	if err := removeExpiredParserJobs(privateRoot); err != nil {
		return err
	}
	parserConfig.Store(&workerConfig{executable: executable, root: privateRoot})
	return nil
}

func ReadSource(path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, ErrUnsupportedFormat
	}
	file, err := openSource(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxInputBytes {
		return nil, ErrBudgetExceeded
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxInputBytes+1))
	if len(raw) > MaxInputBytes {
		return nil, ErrBudgetExceeded
	}
	return raw, err
}

type parserJob struct{ Name, Media string }
type parserReply struct {
	Result Result
	Error  string
}

func ExtractContext(ctx context.Context, name string, raw []byte, media string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(raw) > MaxInputBytes {
		return Result{}, ErrBudgetExceeded
	}
	config := parserConfig.Load()
	if config == nil {
		return Extract(name, raw, media)
	} // deterministic library/test seam
	select {
	case parserSlots <- struct{}{}:
		defer func() { <-parserSlots }()
	default:
		return Result{}, errors.New("document parser is busy; retry this document")
	}
	root, err := os.MkdirTemp(config.root, "parse-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(root) // root is the concrete directory returned by MkdirTemp
	input := filepath.Join(root, "input.bin")
	if err := os.WriteFile(input, raw, 0600); err != nil {
		return Result{}, err
	}
	job, _ := json.Marshal(parserJob{Name: filepath.Base(name), Media: media})
	path := filepath.Join(root, "job.json")
	if err := os.WriteFile(path, job, 0600); err != nil {
		return Result{}, err
	}
	env := []string{"GOMEMLIMIT=268435456", "TEMP=" + root, "TMP=" + root, "TMPDIR=" + root}
	for _, key := range []string{"SYSTEMROOT", "WINDIR"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	out, err := commandworker.Run(ctx, commandworker.Spec{Exe: config.executable, Args: []string{"--doctext-worker=" + path}, Dir: root, Env: env, Timeout: 10 * time.Second, MaxOutputBytes: 4096, MaxMemoryBytes: 512 << 20}, nil, nil)
	if err != nil {
		return Result{}, err
	}
	if out.TimedOut || out.ExitCode != 0 || out.Truncated {
		return Result{}, errors.New("document parser exceeded its limits or terminated")
	}
	file, err := os.Open(filepath.Join(root, "result.json"))
	if err != nil {
		return Result{}, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, workerResultCap+1))
	if err != nil {
		return Result{}, err
	}
	if len(body) > workerResultCap {
		return Result{}, ErrBudgetExceeded
	}
	var reply parserReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return Result{}, err
	}
	if reply.Error != "" {
		switch reply.Error {
		case ErrBudgetExceeded.Error():
			return Result{}, ErrBudgetExceeded
		case ErrNoTextLayer.Error():
			return Result{}, ErrNoTextLayer
		case ErrUnsupportedFormat.Error():
			return Result{}, ErrUnsupportedFormat
		}
		return Result{}, errors.New(reply.Error)
	}
	return reply.Result, nil
}

// Staged files are never authoritative. A crash may leave a private snapshot;
// old snapshots are reclaimed on startup, using an opened root to contain the
// deletion. Recent jobs are retained for another live engine's bounded worker.
func removeExpiredParserJobs(path string) error {
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "parse-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := root.RemoveAll(entry.Name()); err != nil {
				return err
			}
		}
	}
	return nil
}

// RunWorker has no access to the engine services. It reads only the staged
// snapshot; the parent owns cancellation and kills the entire child tree.
func RunWorker(path string) error {
	debug.SetMemoryLimit(256 << 20)
	if !filepath.IsAbs(path) {
		return ErrUnsupportedFormat
	}
	jobFile, err := os.Open(path)
	if err != nil {
		return err
	}
	jobBytes, err := io.ReadAll(io.LimitReader(jobFile, 8193))
	closeErr := jobFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if len(jobBytes) > 8192 {
		return ErrBudgetExceeded
	}
	var job parserJob
	if err := json.Unmarshal(jobBytes, &job); err != nil {
		return err
	}
	root := filepath.Dir(path)
	raw, err := ReadSource(filepath.Join(root, "input.bin"))
	if err != nil {
		return err
	}
	result, parseErr := Extract(job.Name, raw, job.Media)
	reply := parserReply{Result: result}
	if parseErr != nil {
		reply.Error = parseErr.Error()
	}
	body, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	if len(body) > workerResultCap {
		return ErrBudgetExceeded
	}
	return os.WriteFile(filepath.Join(root, "result.json"), body, 0600)
}
