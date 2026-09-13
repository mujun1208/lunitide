//go:build windows

package agenthub

import (
	"bufio"
	"context"
	"io"
	"testing"
)

func TestStartPersistentKeepsStdinOpen(t *testing.T) {
	exe := buildFakeCLI(t, `package main
import ("bufio"; "fmt"; "os")
func main() {
  sc := bufio.NewScanner(os.Stdin)
  for sc.Scan() {
    fmt.Println(sc.Text())
    if sc.Text() == "quit" {
      return
    }
  }
}
`)
	dir := t.TempDir()
	proc, err := StartPersistent(context.Background(), ProcSpec{Exe: exe, Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	if _, err = io.WriteString(proc.stdin, "one\n"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err = io.WriteString(proc.stdin, "two\n"); err != nil {
		t.Fatalf("stdin closed after start: %v", err)
	}
	br := bufio.NewReader(proc.stdout)
	if got, err := br.ReadString('\n'); err != nil || got != "one\n" {
		t.Fatalf("first line = %q %v", got, err)
	}
	if got, err := br.ReadString('\n'); err != nil || got != "two\n" {
		t.Fatalf("second line = %q %v", got, err)
	}
	if _, err = io.WriteString(proc.stdin, "quit\n"); err != nil {
		t.Fatal(err)
	}
}
