package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Thin launcher: tray mode is the same desktop PE with --tray.
func main() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	desktop := filepath.Join(filepath.Dir(self), "Lunitide.exe")
	if _, statErr := os.Stat(desktop); statErr != nil {
		desktop = filepath.Join(filepath.Dir(self), "lunitide.exe")
	}
	args := append([]string{"--tray"}, os.Args[1:]...)
	cmd := exec.Command(desktop, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
