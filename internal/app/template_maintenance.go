package app

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
)

type assetFileReferences interface {
	AssetTemplateFileReferenced(context.Context, string) (bool, error)
}

// SetTemplateStageDirectory is bootstrap-only and points to this data root's
// protected staging directory. It avoids the global temporary directory in
// production and lets the next process reclaim abandoned uploads.
func (e *Engine) SetTemplateStageDirectory(dir string) { e.templateStageDirectory = dir }
func (e *Engine) templateStageDir() (string, error) {
	if e.templateStageDirectory != "" {
		return e.templateStageDirectory, nil
	}
	return templateStageDir()
}

// ReconcileTemplateFiles never traverses directories or deletes caller paths.
// A 24-hour grace period protects ambiguous commits; every permanent candidate
// is rechecked against all organizations while creation is excluded.
func (e *Engine) ReconcileTemplateFiles(ctx context.Context, now time.Time) error {
	refs, ok := e.assets.(assetFileReferences)
	if !ok {
		return errors.New("template reference storage unavailable")
	}
	files, ok := e.templateFiles.(attachmentapp.FileVisitor)
	if !ok {
		return errors.New("template file enumeration unavailable")
	}
	e.templateCreateMu.Lock()
	defer e.templateCreateMu.Unlock()
	if err := files.VisitFiles(ctx, func(info os.FileInfo) error {
		if !info.ModTime().Before(now.Add(-24 * time.Hour)) {
			return nil
		}
		if !validCanonicalULID(info.Name()) && !strings.HasPrefix(info.Name(), ".lunitide-ws-") {
			return nil
		}
		used, err := refs.AssetTemplateFileReferenced(ctx, info.Name())
		if err != nil {
			return err
		}
		if used {
			return nil
		}
		return e.templateFiles.DeleteFile(ctx, info.Name())
	}); err != nil {
		return err
	}
	if e.templateStageDirectory == "" {
		return nil
	} // Unknown global temp files belong to no verified data root.
	state := e.templateStage()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.evictExpired(now)
	active := make(map[string]bool, len(state.uploads))
	for _, up := range state.uploads {
		active[up.path] = true
	}
	staged := attachmentapp.NewDirFileStorage(e.templateStageDirectory)
	visitor := staged.(attachmentapp.FileVisitor)
	return visitor.VisitFiles(ctx, func(info os.FileInfo) error {
		name := info.Name()
		if len(name) < 31 || !strings.HasPrefix(name, "up-") || !validCanonicalULID(name[3:29]) || name[29] != '-' || !info.ModTime().Before(now.Add(-templateStageTTL)) {
			return nil
		}
		for path := range active {
			if strings.HasSuffix(path, string(os.PathSeparator)+name) {
				return nil
			}
		}
		return staged.DeleteFile(ctx, name)
	})
}

// StartTemplateMaintenance owns one cancellable worker. Its stop function
// drains that worker and closes all remaining uploads before storage closes.
func (e *Engine) StartTemplateMaintenance(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				runCtx, stop := context.WithTimeout(ctx, 20*time.Second)
				if err := e.ReconcileTemplateFiles(runCtx, now); err != nil && ctx.Err() == nil {
					log.Printf("template file reconciliation pending: %v", err)
				}
				stop()
			}
		}
	}()
	return func() {
		once.Do(func() {
			cancel()
			<-done
			state := e.templateStage()
			state.mu.Lock()
			defer state.mu.Unlock()
			for id, up := range state.uploads {
				up.mu.Lock()
				if err := closeTemplateUpload(up); err != nil {
					log.Printf("template shutdown cleanup pending: %v", err)
				}
				up.mu.Unlock()
				delete(state.uploads, id)
			}
		})
	}
}
