//go:build windows

// lunitide-maintenance is an offline maintenance entry point. It never starts
// providers, agents, tools, workers, migrations, or the desktop host.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/maintenance"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	operation := flag.String("operation", "verify", "backup, verify, or restore")
	directory := flag.String("backup-dir", "", "absolute backup directory (new directory for backup)")
	deadline := flag.Duration("timeout", 30*time.Minute, "maximum operation duration; interrupted restore resumes at next startup")
	flag.Parse()
	if *directory == "" {
		return fmt.Errorf("backup-dir is required")
	}
	if *deadline <= 0 {
		return fmt.Errorf("positive timeout is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()
	if *operation == "verify" {
		m, err := maintenance.Verify(ctx, *directory)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"verified": true, "files": len(m.Files), "sourceRoot": m.SourceRoot})
	}
	if *operation != "backup" && *operation != "restore" {
		return fmt.Errorf("operation must be backup, verify, or restore")
	}
	root, err := datadir.PrepareProduction()
	if err != nil {
		return err
	}
	defer root.Close()
	if *operation == "backup" {
		m, err := maintenance.Backup(ctx, root, *directory)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"backupCreated": true, "files": len(m.Files), "directory": *directory})
	}
	if err = maintenance.Restore(ctx, root, *directory); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"restored": true, "restartRequired": true, "previousFilesRetained": true})
}
