package m8app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type personaCollector interface {
	Sweep(context.Context, time.Time, int, func(string) (bool, error)) (int, error)
}

// CollectOrphanBodies holds the same SQLite writer boundary used when Put
// publishes a body, so a newly committed reference cannot race deletion.
// Every historical version retains its body; only aged unreferenced content
// or abandoned temporary files can be removed.
func (s *ExpertService) CollectOrphanBodies(ctx context.Context, olderThan time.Time, limit int) (int, error) {
	gc, ok := s.persona.(personaCollector)
	if !ok {
		return 0, nil
	}
	if limit < 1 || limit > 256 {
		return 0, ErrPayloadInvalid
	}
	removed := 0
	err := s.uow.TransactExpert(ctx, func(tx ExpertTx) error {
		var err error
		removed, err = gc.Sweep(ctx, olderThan, limit, tx.PersonaReferenced)
		return err
	})
	return removed, err
}
func (s *ExpertService) StartPersonaMaintenance(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			run, cancelRun := context.WithTimeout(ctx, 5*time.Second)
			_, err := s.CollectOrphanBodies(run, time.Now().Add(-24*time.Hour), 256)
			cancelRun()
			if err != nil && ctx.Err() == nil {
				log.Printf("expert persona cleanup: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}
func (s *FilePersonaStore) Sweep(ctx context.Context, before time.Time, limit int, referenced func(string) (bool, error)) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed, visited, shards := 0, 0, 0
	for visited < limit && shards < 256 {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		dirPath := filepath.Join(s.root, fmt.Sprintf("%02x", s.gcShard))
		info, err := os.Lstat(dirPath)
		if errors.Is(err, os.ErrNotExist) {
			s.gcShard = (s.gcShard + 1) % 256
			s.gcOffset = 0
			shards++
			continue
		}
		if err != nil {
			return removed, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return removed, ErrExpertBodyUnavailable
		}
		dir, err := os.Open(dirPath)
		if err != nil {
			return removed, err
		}
		skip := s.gcOffset
		for skip > 0 {
			if err = ctx.Err(); err != nil {
				break
			}
			batch := min(skip, 128)
			entries, readErr := dir.ReadDir(batch)
			skip -= len(entries)
			if readErr != nil {
				err = readErr
				break
			}
		}
		if err != nil && !errors.Is(err, io.EOF) {
			dir.Close()
			return removed, err
		}
		exhausted := errors.Is(err, io.EOF)
		for !exhausted && visited < limit {
			if err = ctx.Err(); err != nil {
				dir.Close()
				return removed, err
			}
			entries, readErr := dir.ReadDir(1)
			if errors.Is(readErr, io.EOF) {
				exhausted = true
				break
			}
			if readErr != nil {
				dir.Close()
				return removed, readErr
			}
			s.gcOffset++
			visited++
			entry := entries[0]
			if !entry.Type().IsRegular() {
				continue
			}
			name := entry.Name()
			ref := strings.TrimSuffix(name, ".json")
			temporary := strings.HasPrefix(name, ".persona-") && strings.HasSuffix(name, ".tmp")
			if !temporary && (!strings.HasSuffix(name, ".json") || !validPersonaRef(ref) || ref[:2] != fmt.Sprintf("%02x", s.gcShard)) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				dir.Close()
				return removed, err
			}
			if !info.ModTime().Before(before) {
				continue
			}
			if !temporary {
				used, err := referenced(ref)
				if err != nil {
					dir.Close()
					return removed, err
				}
				if used {
					continue
				}
			}
			// The path is formed only from a validated digest or a directory entry
			// inside the fixed shard. No recursive filesystem operation is used.
			if err = os.Remove(filepath.Join(dirPath, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				dir.Close()
				return removed, err
			}
			removed++
		}
		if err = dir.Close(); err != nil {
			return removed, err
		}
		if exhausted {
			s.gcShard = (s.gcShard + 1) % 256
			s.gcOffset = 0
			shards++
		}
	}
	return removed, nil
}
